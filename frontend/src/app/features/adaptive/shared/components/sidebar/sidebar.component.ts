/**
 * App sidebar nav: Home (badge = links not in a folder), the user's Folders grouped into optional
 * sections (create with inline rename, menus to rename / move / delete, drag folders to reorder or
 * between sections), and a Bin. Home, folder rows, and the Bin
 * are drop targets for cards (see CardDragService); dropping on Bin soft-deletes. Click Bin to open it.
 */
import { Component, ChangeDetectionStrategy, HostListener, input, output, inject, effect, signal, untracked, computed } from '@angular/core';
import { CommonModule, NgTemplateOutlet } from '@angular/common';
import { NavigationEnd, Router, RouterLink, RouterLinkActive } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { filter, map } from 'rxjs';
import { CdkDrag, CdkDragDrop, CdkDropList, CdkDropListGroup } from '@angular/cdk/drag-drop';
import { UserContextService } from '../../../services/user-context.service';
import { AccountMenuModalService } from '../../../services/account-menu-modal.service';
import { DumpService } from '../../../services/dump.service';
import { Folder, FolderGroup, FolderSection, FoldersService } from '../../../services/folders.service';
import { CardDragService } from '../../../services/card-drag.service';
import { CameraDragService } from '../../../services/camera-drag.service';
import { CameraService } from '../../../services/camera.service';
import { ConfirmDialogService } from '../../../services/confirm-dialog.service';
import { AutofocusSelectDirective } from '../../directives/autofocus-select.directive';
import { TooltipDirective } from '../../directives/tooltip.directive';

@Component({
  selector: 'app-sidebar',
  standalone: true,
  imports: [CommonModule, NgTemplateOutlet, RouterLink, RouterLinkActive, CdkDropList, CdkDropListGroup, CdkDrag, AutofocusSelectDirective, TooltipDirective],
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
  readonly cardDrag = inject(CardDragService);
  readonly cameraDrag = inject(CameraDragService);
  private camera = inject(CameraService);

  /** Card plugged in: the AirDrop row shows under Manage camera. */
  readonly cardIn = computed(() => !!this.camera.status()?.available);
  /** Files in the card's AIRDROP folder, and whether its AirDrop window is open. */
  readonly airdropCount = computed(() => this.camera.status()?.airdrop_count ?? 0);
  readonly airdropSending = computed(() => !!this.camera.status()?.airdrop_sending);

  readonly folders = this.foldersService.folders;
  readonly renamingId = this.foldersService.renamingId;
  readonly menuOpenId = signal<number | null>(null);
  readonly renameError = signal<string | null>(null);
  readonly creating = signal(false);

  readonly sections = this.foldersService.sections;
  readonly groups = this.foldersService.groups;
  readonly renamingSectionId = this.foldersService.renamingSectionId;
  readonly sectionMenuOpenId = signal<number | null>(null);
  readonly sectionRenameError = signal<string | null>(null);
  readonly creatingSection = signal(false);
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
  private savingSectionRename = false;

  constructor() {
    effect(() => {
      if (!this.isLoggedIn()) {
        untracked(() => {
          this.dumpService.clear();
          this.foldersService.clear();
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
        this.camera.watch();
      });
    });
  }

  isActiveAirDrop(): boolean {
    return this.currentPath() === '/camera/airdrop';
  }

  @HostListener('document:click')
  closeMenu(): void {
    this.menuOpenId.set(null);
    this.sectionMenuOpenId.set(null);
  }

  createFolder(sectionId: number | null = null, event?: Event): void {
    event?.stopPropagation();
    this.sectionMenuOpenId.set(null);
    if (this.creating()) return;
    this.creating.set(true);
    this.renameError.set(null);
    this.foldersService.create(sectionId).subscribe({
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

  moveFolder(folder: Folder, delta: number, event: Event): void {
    event.stopPropagation();
    this.menuOpenId.set(null);
    this.foldersService.nudgeFolder(folder.id, delta);
  }

  /** Puts a folder into another section (null = ungrouped), at the bottom of it. */
  moveFolderToSection(folder: Folder, sectionId: number | null, event: Event): void {
    event.stopPropagation();
    this.menuOpenId.set(null);
    const dest = this.groups().find((g) => (g.section?.id ?? null) === sectionId);
    this.foldersService.placeFolder(folder.id, sectionId, dest?.folders.length ?? 0);
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

  /** A folder dropped within or between section lists. */
  onFolderDropped(event: CdkDragDrop<FolderGroup, FolderGroup, Folder>): void {
    const sectionId = event.container.data.section?.id ?? null;
    const moved = event.container !== event.previousContainer || event.previousIndex !== event.currentIndex;
    if (moved) this.foldersService.placeFolder(event.item.data.id, sectionId, event.currentIndex);
    setTimeout(() => (this.reordering = false));
  }

  /** Lists connected for drag: every group's list id. */
  readonly groupListIds = (groups: FolderGroup[]) => groups.map((g) => this.groupListId(g));

  groupListId(group: FolderGroup): string {
    return group.section ? `folder-section-${group.section.id}` : 'folder-section-none';
  }

  createSection(): void {
    if (this.creatingSection()) return;
    this.creatingSection.set(true);
    this.sectionRenameError.set(null);
    this.foldersService.createSection().subscribe({
      next: () => this.creatingSection.set(false),
      error: () => this.creatingSection.set(false),
    });
  }

  toggleSectionMenu(section: FolderSection, event: Event): void {
    event.preventDefault();
    event.stopPropagation();
    this.menuOpenId.set(null);
    this.sectionMenuOpenId.update((id) => (id === section.id ? null : section.id));
  }

  startSectionRename(section: FolderSection, event?: Event): void {
    event?.stopPropagation();
    this.sectionMenuOpenId.set(null);
    this.sectionRenameError.set(null);
    this.renamingSectionId.set(section.id);
  }

  commitSectionRename(section: FolderSection, value: string): void {
    if (this.renamingSectionId() !== section.id || this.savingSectionRename) return;
    const name = value.trim();
    if (!name || name === section.name) {
      this.cancelSectionRename();
      return;
    }
    this.savingSectionRename = true;
    this.foldersService.renameSection(section.id, name).subscribe({
      next: () => {
        this.savingSectionRename = false;
        this.cancelSectionRename();
      },
      error: (err: Error) => {
        this.savingSectionRename = false;
        this.sectionRenameError.set(err.message);
      },
    });
  }

  cancelSectionRename(): void {
    this.sectionRenameError.set(null);
    this.renamingSectionId.set(null);
  }

  moveSection(section: FolderSection, delta: number, event: Event): void {
    event.stopPropagation();
    this.sectionMenuOpenId.set(null);
    this.foldersService.nudgeSection(section.id, delta);
  }

  async deleteSection(section: FolderSection, event: Event): Promise<void> {
    event.stopPropagation();
    this.sectionMenuOpenId.set(null);
    const ok = await this.confirm.ask({
      title: `Delete section "${section.name}"?`,
      message: 'Only the section goes away. Its folders and everything in them stay, ungrouped at the bottom.',
      confirmLabel: 'Delete section',
      danger: true,
    });
    if (!ok) return;
    this.foldersService.removeSection(section.id).subscribe({ error: () => {} });
  }

  openFolder(folder: Folder): void {
    if (this.reordering || this.renamingId() === folder.id) return;
    this.router.navigate(['/folders', folder.id]);
    this.onNavLinkClick();
  }

  isActiveFolder(folder: Folder): boolean {
    return this.currentPath() === `/folders/${folder.id}`;
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

  isFirstSection(section: FolderSection): boolean {
    return this.sections()[0]?.id === section.id;
  }

  isLastSection(section: FolderSection): boolean {
    const list = this.sections();
    return list[list.length - 1]?.id === section.id;
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
