/**
 * App sidebar nav: Home (badge = links not in a folder), the user's Folders (create with inline
 * rename, menu to rename / move / delete, drag to reorder), and a Bin. Home, folder rows, and the Bin
 * are drop targets for cards (see CardDragService); dropping on Bin soft-deletes. Click Bin to open it.
 */
import { Component, ChangeDetectionStrategy, HostListener, input, output, inject, effect, signal, untracked } from '@angular/core';
import { CommonModule, NgTemplateOutlet } from '@angular/common';
import { NavigationEnd, Router, RouterLink, RouterLinkActive } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { filter, map } from 'rxjs';
import { CdkDrag, CdkDragDrop, CdkDropList } from '@angular/cdk/drag-drop';
import { UserContextService } from '../../../services/user-context.service';
import { AccountMenuModalService } from '../../../services/account-menu-modal.service';
import { DumpService } from '../../../services/dump.service';
import { Folder, FoldersService } from '../../../services/folders.service';
import { LocalMediaFolder, LocalMediaService } from '../../../services/local-media.service';
import { CardDragService } from '../../../services/card-drag.service';
import { CameraDragService } from '../../../services/camera-drag.service';
import { ConfirmDialogService } from '../../../services/confirm-dialog.service';
import { AutofocusSelectDirective } from '../../directives/autofocus-select.directive';
import { TooltipDirective } from '../../directives/tooltip.directive';

@Component({
  selector: 'app-sidebar',
  standalone: true,
  imports: [CommonModule, NgTemplateOutlet, RouterLink, RouterLinkActive, CdkDropList, CdkDrag, AutofocusSelectDirective, TooltipDirective],
  templateUrl: './sidebar.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class SidebarComponent {
  private userContext = inject(UserContextService);
  private accountMenuModalService = inject(AccountMenuModalService);
  private dumpService = inject(DumpService);
  private router = inject(Router);
  private confirm = inject(ConfirmDialogService);
  readonly foldersService = inject(FoldersService);
  readonly localMediaService = inject(LocalMediaService);
  readonly cardDrag = inject(CardDragService);
  readonly cameraDrag = inject(CameraDragService);

  readonly folders = this.foldersService.folders;
  readonly renamingId = this.foldersService.renamingId;
  readonly menuOpenId = signal<number | null>(null);
  readonly renameError = signal<string | null>(null);
  readonly creating = signal(false);

  readonly localMediaFolders = this.localMediaService.folders;
  readonly localMediaRenamingId = this.localMediaService.renamingId;
  readonly localMediaMenuOpenId = signal<number | null>(null);
  readonly localMediaRenameError = signal<string | null>(null);
  readonly creatingLocalMedia = signal(false);
  private readonly currentPath = toSignal(
    this.router.events.pipe(
      filter((e): e is NavigationEnd => e instanceof NavigationEnd),
      map((e) => e.urlAfterRedirects.split('?')[0])
    ),
    { initialValue: this.router.url.split('?')[0] }
  );
  /** Set while a folder row is being dragged, so the click that ends the drag doesn't navigate. */
  private reordering = false;
  private savingRename = false;
  private localMediaReordering = false;
  private savingLocalMediaRename = false;

  constructor() {
    effect(() => {
      if (!this.isLoggedIn()) {
        untracked(() => {
          this.dumpService.clear();
          this.foldersService.clear();
          this.localMediaService.clear();
        });
        return;
      }
      untracked(() => {
        if (!this.dumpService.loaded() && !this.dumpService.loading()) {
          this.dumpService.load().subscribe({ error: () => {} });
        }
        if (!this.foldersService.loaded()) {
          this.foldersService.load();
        }
        if (!this.localMediaService.loaded()) {
          this.localMediaService.loadFolders();
        }
      });
    });
  }

  @HostListener('document:click')
  closeMenu(): void {
    this.menuOpenId.set(null);
    this.localMediaMenuOpenId.set(null);
  }

  createFolder(): void {
    if (this.creating()) return;
    this.creating.set(true);
    this.renameError.set(null);
    this.foldersService.create().subscribe({
      next: () => this.creating.set(false),
      error: () => this.creating.set(false),
    });
  }

  startRename(folder: Folder, event?: Event): void {
    event?.stopPropagation();
    this.menuOpenId.set(null);
    this.renameError.set(null);
    this.renamingId.set(folder.id);
  }

  commitRename(folder: Folder, value: string): void {
    if (this.renamingId() !== folder.id || this.savingRename) return;
    const name = value.trim();
    if (!name || name === folder.name) {
      this.cancelRename();
      return;
    }
    this.savingRename = true;
    this.foldersService.rename(folder.id, name).subscribe({
      next: () => {
        this.savingRename = false;
        this.cancelRename();
      },
      error: (err: Error) => {
        this.savingRename = false;
        this.renameError.set(err.message);
      },
    });
  }

  cancelRename(): void {
    this.renameError.set(null);
    this.renamingId.set(null);
  }

  toggleMenu(folder: Folder, event: Event): void {
    event.preventDefault();
    event.stopPropagation();
    this.menuOpenId.update((id) => (id === folder.id ? null : folder.id));
  }

  moveFolder(index: number, delta: number, event: Event): void {
    event.stopPropagation();
    this.menuOpenId.set(null);
    this.foldersService.reorder(index, index + delta);
  }

  async deleteFolder(folder: Folder, event: Event): Promise<void> {
    event.stopPropagation();
    this.menuOpenId.set(null);
    const items = folder.count === 1 ? '1 save' : `${folder.count} saves`;
    const ok = await this.confirm.ask({
      title: `Delete "${folder.name}"?`,
      message:
        folder.count > 0
          ? `This deletes the folder and its ${items}. Downloaded files stay in your download folder on disk.`
          : 'This folder is empty.',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.foldersService.remove(folder.id).subscribe({
      next: () => {
        if (this.router.url.split('?')[0] === `/folders/${folder.id}`) {
          this.router.navigate(['/home']);
        }
      },
      error: () => {},
    });
  }

  onReorderStarted(): void {
    this.reordering = true;
    this.menuOpenId.set(null);
  }

  onReorderDropped(event: CdkDragDrop<Folder[]>): void {
    this.foldersService.reorder(event.previousIndex, event.currentIndex);
    setTimeout(() => (this.reordering = false));
  }

  openFolder(folder: Folder): void {
    if (this.reordering || this.renamingId() === folder.id) return;
    this.router.navigate(['/folders', folder.id]);
    this.onNavLinkClick();
  }

  isActiveFolder(folder: Folder): boolean {
    return this.currentPath() === `/folders/${folder.id}`;
  }

  createLocalMediaFolder(): void {
    if (this.creatingLocalMedia()) return;
    this.creatingLocalMedia.set(true);
    this.localMediaRenameError.set(null);
    this.localMediaService.createFolder().subscribe({
      next: () => this.creatingLocalMedia.set(false),
      error: () => this.creatingLocalMedia.set(false),
    });
  }

  createLocalMediaFolderFromRail(): void {
    this.expandSidebar.emit();
    this.createLocalMediaFolder();
  }

  startLocalMediaRename(folder: LocalMediaFolder, event?: Event): void {
    event?.stopPropagation();
    this.localMediaMenuOpenId.set(null);
    this.localMediaRenameError.set(null);
    this.localMediaRenamingId.set(folder.id);
  }

  commitLocalMediaRename(folder: LocalMediaFolder, value: string): void {
    if (this.localMediaRenamingId() !== folder.id || this.savingLocalMediaRename) return;
    const name = value.trim();
    if (!name || name === folder.name) {
      this.cancelLocalMediaRename();
      return;
    }
    this.savingLocalMediaRename = true;
    this.localMediaService.renameFolder(folder.id, name).subscribe({
      next: () => {
        this.savingLocalMediaRename = false;
        this.cancelLocalMediaRename();
      },
      error: (err: Error) => {
        this.savingLocalMediaRename = false;
        this.localMediaRenameError.set(err.message);
      },
    });
  }

  cancelLocalMediaRename(): void {
    this.localMediaRenameError.set(null);
    this.localMediaRenamingId.set(null);
  }

  toggleLocalMediaMenu(folder: LocalMediaFolder, event: Event): void {
    event.preventDefault();
    event.stopPropagation();
    this.localMediaMenuOpenId.update((id) => (id === folder.id ? null : folder.id));
  }

  moveLocalMediaFolder(index: number, delta: number, event: Event): void {
    event.stopPropagation();
    this.localMediaMenuOpenId.set(null);
    this.localMediaService.reorderFolders(index, index + delta);
  }

  async deleteLocalMediaFolder(folder: LocalMediaFolder, event: Event): Promise<void> {
    event.stopPropagation();
    this.localMediaMenuOpenId.set(null);
    const items = folder.count === 1 ? '1 file' : `${folder.count} files`;
    const ok = await this.confirm.ask({
      title: `Delete "${folder.name}"?`,
      message:
        folder.count > 0
          ? `This deletes the folder and its ${items} from Local media.`
          : 'This folder is empty.',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    this.localMediaService.removeFolder(folder.id).subscribe({
      next: () => {
        if (this.router.url.split('?')[0] === `/local-media/${folder.id}`) {
          this.router.navigate(['/camera']);
        }
      },
      error: () => {},
    });
  }

  onLocalMediaReorderStarted(): void {
    this.localMediaReordering = true;
    this.localMediaMenuOpenId.set(null);
  }

  onLocalMediaReorderDropped(event: CdkDragDrop<LocalMediaFolder[]>): void {
    this.localMediaService.reorderFolders(event.previousIndex, event.currentIndex);
    setTimeout(() => (this.localMediaReordering = false));
  }

  openLocalMediaFolder(folder: LocalMediaFolder): void {
    if (this.localMediaReordering || this.localMediaRenamingId() === folder.id) return;
    this.router.navigate(['/local-media', folder.id]);
    this.onNavLinkClick();
  }

  isActiveLocalMediaFolder(folder: LocalMediaFolder): boolean {
    return this.currentPath() === `/local-media/${folder.id}`;
  }

  isActiveBin(): boolean {
    return this.currentPath() === '/bin';
  }

  openBin(): void {
    this.router.navigate(['/bin']);
    this.onNavLinkClick();
  }

  /** Expanded sidebar (true) or icon rail (false); on phones, true = drawer over the page. */
  isOpen = input.required<boolean>();
  isMobile = input<boolean>(false);
  isLoading = input<boolean>(false);
  isInitializing = input<boolean>(false);
  closeSidebar = output<void>();
  toggleSidebar = output<void>();
  expandSidebar = output<void>();

  /** Key hint shown in the collapse / expand tooltip. */
  readonly shortcutKeys =
    typeof navigator !== 'undefined' && /Mac|iPhone|iPad/.test(navigator.platform) ? ['⌘', 'B'] : ['Ctrl', 'B'];

  isLoggedIn = this.userContext.isLoggedIn;
  displayName = this.userContext.displayName;
  dumpCount = this.dumpService.count;

  onClose() {
    this.closeSidebar.emit();
  }

  onToggle(): void {
    this.toggleSidebar.emit();
  }

  /** "+" in the icon rail: open the full sidebar so the new folder can be renamed inline. */
  createFolderFromRail(): void {
    this.expandSidebar.emit();
    this.createFolder();
  }

  onNavLinkClick(): void {
    if (this.isMobile() && this.isOpen()) {
      this.onClose();
    }
  }

  openAccountMenu(event: Event): void {
    const trigger = event.currentTarget as HTMLElement;
    this.accountMenuModalService.toggle('sidebar', trigger);
    this.onNavLinkClick();
  }

  goToProfile(): void {
    this.router.navigate(['/account/profile']);
    this.onNavLinkClick();
  }
}
