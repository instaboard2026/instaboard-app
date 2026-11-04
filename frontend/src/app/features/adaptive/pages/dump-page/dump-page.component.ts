/**
 * Board page for the Dump (/dump: links not in a folder) and for a folder (/folders/:id). Download
 * fetches an Instagram reel or carousel into the backend's download folder (`<id>.mp4` or `<id>/`);
 * a link is "Downloaded" when that exists. Cards can be dragged onto a sidebar folder or the Dump.
 * "Per row" is remembered per browser.
 */
import { ChangeDetectionStrategy, Component, HostListener, computed, effect, inject, signal, untracked } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { ActivatedRoute, Router } from '@angular/router';
import { Title } from '@angular/platform-browser';
import { toSignal } from '@angular/core/rxjs-interop';
import { map } from 'rxjs';
import { FormsModule } from '@angular/forms';
import { DownloadStats, DumpService, Save, SaveScope, SaveSource } from '../../services/dump.service';
import { FoldersService } from '../../services/folders.service';
import { CardDragService } from '../../services/card-drag.service';
import { PlaybackService } from '../../services/playback.service';
import { CdkDrag, CdkDragEnd, CdkDragMove, CdkDragPlaceholder, CdkDragPreview, CdkDropList } from '@angular/cdk/drag-drop';
import { UserContextService } from '../../services/user-context.service';
import { LocalSessionService } from '../../services/local-session.service';
import { CacheService } from '../../services/cache.service';
import { MediaCarouselComponent } from '../../shared/components/media-carousel/media-carousel.component';
import { AutofocusSelectDirective } from '../../shared/directives/autofocus-select.directive';

const PER_ROW_KEY = 'dump_per_row';
const PER_ROW_OPTIONS = [4, 6, 8, 12] as const;
type PerRow = (typeof PER_ROW_OPTIONS)[number];

const SOURCE_BADGE: Record<SaveSource, string> = {
  instagram: 'IG',
  pinterest: 'PIN',
  youtube: 'YT',
};

const ADDED_FROM_LABEL: Record<string, string> = {
  web: 'Web',
  iphone: 'iPhone',
  android: 'Android',
};

@Component({
  selector: 'app-dump-page',
  standalone: true,
  imports: [FormsModule, NgTemplateOutlet, MediaCarouselComponent, CdkDropList, CdkDrag, CdkDragPreview, CdkDragPlaceholder, AutofocusSelectDirective],
  templateUrl: './dump-page.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class DumpPageComponent {
  private dumpService = inject(DumpService);
  private foldersService = inject(FoldersService);
  private router = inject(Router);
  private title = inject(Title);
  private cardDrag = inject(CardDragService);
  private playback = inject(PlaybackService);
  private userContext = inject(UserContextService);
  private session = inject(LocalSessionService);
  private cacheService = inject(CacheService);

  readonly perRowOptions = PER_ROW_OPTIONS;

  isLoggedIn = this.userContext.isLoggedIn;
  readonly sessionStatus = this.session.status;
  /** Home stats strip (downloads across Home and all folders). */
  stats = signal<DownloadStats | null>(null);
  /** "Chandra's Instaboard" on Home. */
  readonly boardTitle = computed(() => {
    const first = (this.userContext.displayName() ?? '').trim().split(/\s+/)[0];
    return first ? `${first.charAt(0).toUpperCase()}${first.slice(1)}'s Instaboard` : 'Your Instaboard';
  });

  /** Folder id from /folders/:id, or null on /dump. */
  readonly folderId = toSignal(
    inject(ActivatedRoute).paramMap.pipe(
      map((p) => {
        const id = Number(p.get('id'));
        return Number.isInteger(id) && id > 0 ? id : null;
      })
    ),
    { initialValue: null }
  );
  readonly scope = computed<SaveScope>(() => this.folderId() ?? 'dump');
  readonly folder = computed(() => {
    const id = this.folderId();
    return id === null ? null : (this.foldersService.byId().get(id) ?? null);
  });
  readonly heading = computed(() => (this.folderId() === null ? 'Home' : (this.folder()?.name ?? 'Folder')));
  /**
   * Home: fixed 142px tracks centered in the column (as on Shopify's home feed); auto-fit collapses
   * empty tracks so a short row stays centered. Folders: the chosen number per row.
   */
  readonly gridClass = computed(() =>
    this.folderId() === null
      ? 'justify-center [grid-template-columns:repeat(auto-fit,142px)]'
      : 'grid-cols-2 sm:grid-cols-3 md:[grid-template-columns:repeat(var(--per-row),minmax(0,1fr))]',
  );

  saves = computed(() => this.dumpService.listFor(this.scope()));
  count = computed(() => this.saves().length);
  loading = computed(() => this.dumpService.isLoading(this.scope()) && !this.dumpService.isLoaded(this.scope()));
  copiedId = signal<number | null>(null);
  downloadingIds = signal<ReadonlySet<number>>(new Set());
  downloadErrors = signal<ReadonlyMap<number, string>>(new Map());

  perRow = signal<PerRow>(6);
  linkInput = signal('');
  adding = signal(false);
  addError = signal<string | null>(null);
  /** Add-link popup on folder pages. */
  addDialogOpen = signal(false);
  loadError = signal<string | null>(null);

  constructor() {
    const saved = this.cacheService.getItem<number>(PER_ROW_KEY);
    if (saved !== null && (PER_ROW_OPTIONS as readonly number[]).includes(saved)) {
      this.perRow.set(saved as PerRow);
    }
    effect(() => this.cacheService.setItem(PER_ROW_KEY, this.perRow()));

    // Load whenever the page switches between the Dump and a folder (the component is reused).
    effect(() => {
      this.scope();
      if (this.isLoggedIn()) untracked(() => this.reload());
    });
    // A folder that no longer exists (deleted, or a stale link) goes back to the Dump.
    effect(() => {
      if (this.folderId() !== null && this.foldersService.loaded() && !this.folder()) {
        untracked(() => this.router.navigate(['/dump']));
      }
    });
    effect(() => this.title.setTitle(`Instaboard - ${this.heading()}`));
  }

  reload(): void {
    const scope = this.scope();
    this.loadError.set(null);
    this.downloadErrors.set(new Map());
    this.addDialogOpen.set(false);
    if (scope === 'dump') this.loadStats();
    this.dumpService.load(scope).subscribe({
      error: (err: { status?: number }) => {
        if (err?.status === 404 && scope !== 'dump') {
          this.router.navigate(['/dump']);
          return;
        }
        this.loadError.set(`Could not load ${scope === 'dump' ? 'your saves' : 'this folder'}. Is the backend running?`);
      },
    });
  }

  loadStats(): void {
    this.dumpService.stats().subscribe({ next: (stats) => this.stats.set(stats), error: () => {} });
  }

  /** Fills the Home box from the clipboard (one click instead of Cmd+V). */
  async pasteFromClipboard(input: HTMLInputElement): Promise<void> {
    try {
      const text = (await navigator.clipboard.readText()).trim();
      if (text) {
        this.linkInput.set(text);
        this.addError.set(null);
      }
    } catch {
      this.addError.set('Your browser blocked reading the clipboard. Paste with Cmd+V instead.');
    }
    input.focus();
  }

  formatBytes(bytes: number): string {
    if (bytes < 1024) return `${bytes} B`;
    const units = ['KB', 'MB', 'GB', 'TB'];
    let value = bytes / 1024;
    let i = 0;
    while (value >= 1024 && i < units.length - 1) {
      value /= 1024;
      i++;
    }
    return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ${units[i]}`;
  }

  setPerRow(n: PerRow): void {
    this.perRow.set(n);
  }

  addLink(): void {
    const url = this.linkInput().trim();
    if (!url || this.adding()) return;
    if (!this.isLoggedIn()) {
      this.addError.set("Instaboard isn't connected yet. Try again in a moment.");
      return;
    }
    this.adding.set(true);
    this.addError.set(null);
    this.dumpService.add(url, this.scope()).subscribe({
      next: () => {
        this.linkInput.set('');
        this.adding.set(false);
        this.addDialogOpen.set(false);
      },
      error: (err: Error) => {
        this.addError.set(err.message);
        this.adding.set(false);
      },
    });
  }

  openAddDialog(): void {
    this.linkInput.set('');
    this.addError.set(null);
    this.addDialogOpen.set(true);
  }

  closeAddDialog(): void {
    if (this.adding()) return;
    this.addDialogOpen.set(false);
    this.addError.set(null);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.addDialogOpen()) this.closeAddDialog();
  }

  /** Card drag: pause videos and remember what's being dragged (the sidebar highlights drop targets). */
  onCardDragStarted(save: Save): void {
    this.cardDrag.dragging.set(save);
    this.playback.dragPause.set(true);
  }

  onCardDragMoved(event: CdkDragMove<Save>): void {
    const target = this.cardDrag.targetAt(event.pointerPosition.x, event.pointerPosition.y);
    this.cardDrag.hoverTarget.set(target !== null && target !== this.scope() ? target : null);
  }

  /** Dropped on a sidebar folder or the Dump: move it (optimistic). Anywhere else: it slides back. */
  onCardDragEnded(save: Save, _event: CdkDragEnd<Save>): void {
    const target = this.cardDrag.hoverTarget();
    this.cardDrag.dragging.set(null);
    this.cardDrag.hoverTarget.set(null);
    this.playback.dragPause.set(false);
    if (target !== null) {
      this.foldersService.moveSave(save, target);
    }
  }

  download(save: Save): void {
    if (this.downloadingIds().has(save.id)) return;
    this.downloadingIds.update((ids) => new Set(ids).add(save.id));
    this.setDownloadError(save.id, null);
    const done = () =>
      this.downloadingIds.update((ids) => {
        const next = new Set(ids);
        next.delete(save.id);
        return next;
      });
    this.dumpService.download(save.id).subscribe({
      next: () => {
        done();
        if (this.scope() === 'dump') this.loadStats();
      },
      error: (err: Error) => {
        done();
        this.setDownloadError(save.id, err.message);
      },
    });
  }

  private setDownloadError(id: number, message: string | null): void {
    this.downloadErrors.update((errors) => {
      const next = new Map(errors);
      if (message) next.set(id, message);
      else next.delete(id);
      return next;
    });
  }

  copyLink(save: Save): void {
    const copied = () => {
      this.copiedId.set(save.id);
      setTimeout(() => {
        if (this.copiedId() === save.id) this.copiedId.set(null);
      }, 1500);
    };
    const fallback = () => {
      if (copyWithTextarea(save.url)) copied();
    };
    if (navigator.clipboard?.writeText) {
      navigator.clipboard.writeText(save.url).then(copied, fallback);
    } else {
      fallback();
    }
  }

  canDownload(save: Save): boolean {
    return save.source === 'instagram';
  }

  downloadLabel(save: Save): string {
    const d = save.download;
    if (!d) return '';
    return d.kind === 'video' ? 'Video' : `${d.files} ${d.files === 1 ? 'file' : 'files'}`;
  }

  retrySession(): void {
    this.session.start();
  }

  badge(save: Save): string {
    return SOURCE_BADGE[save.source] ?? save.source.toUpperCase();
  }

  addedFrom(save: Save): string {
    return ADDED_FROM_LABEL[save.added_from] ?? save.added_from;
  }


  timeAgo(save: Save): string {
    const diffMs = Date.now() - new Date(save.created_at).getTime();
    const minutes = Math.floor(diffMs / 60_000);
    if (minutes < 1) return 'just now';
    if (minutes < 60) return `${minutes} min ago`;
    const hours = Math.floor(minutes / 60);
    if (hours < 24) return `${hours} h ago`;
    const days = Math.floor(hours / 24);
    if (days === 1) return 'yesterday';
    if (days < 7) return `${days} days ago`;
    return new Date(save.created_at).toLocaleDateString(undefined, { day: 'numeric', month: 'short' });
  }
}

/** Copies text where the async Clipboard API is unavailable or blocked (e.g. some embedded browsers). */
function copyWithTextarea(text: string): boolean {
  const el = document.createElement('textarea');
  el.value = text;
  el.setAttribute('readonly', '');
  el.style.position = 'fixed';
  el.style.opacity = '0';
  document.body.appendChild(el);
  el.select();
  let ok = false;
  try {
    ok = document.execCommand('copy');
  } catch {
    ok = false;
  }
  el.remove();
  return ok;
}
