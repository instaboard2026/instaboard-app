import {
  ChangeDetectionStrategy,
  Component,
  HostListener,
  OnDestroy,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { Subscription, catchError, firstValueFrom, interval, of, switchMap, takeWhile } from 'rxjs';
import { CdkDrag, CdkDragEnd, CdkDragMove, CdkDragPlaceholder, CdkDragPreview, CdkDropList } from '@angular/cdk/drag-drop';
import {
  CameraItem,
  CameraKindFilter,
  CameraService,
  CameraSortOrder,
} from '../../services/camera.service';
import { CameraDragService } from '../../services/camera-drag.service';
import { CameraCopyService } from '../../services/camera-copy.service';
import { FoldersService } from '../../services/folders.service';
import { MoveTarget } from '../../services/dump.service';
import { CacheService } from '../../services/cache.service';
import { UserContextService } from '../../services/user-context.service';
import { LazyThumbDirective } from '../../shared/directives/lazy-thumb.directive';
import { MediaMemoryCacheService } from '../../services/media-memory-cache.service';
import { LazyAutoplayVideoDirective } from '../../shared/directives/lazy-autoplay-video.directive';
import { PlaybackService } from '../../services/playback.service';

const PER_ROW_KEY = 'dump_per_row';
const PER_ROW_OPTIONS = [4, 6, 8, 12] as const;
type PerRow = (typeof PER_ROW_OPTIONS)[number];

const PAGE_SIZE_KEY = 'camera_page_size';
const PAGE_SIZE_OPTIONS = [12, 24, 36, 48, 60, 72, 84, 96, 120] as const;
type PageSize = (typeof PAGE_SIZE_OPTIONS)[number];

const SORT_KEY = 'camera_sort';
const SORT_OPTIONS = ['latest', 'oldest'] as const satisfies readonly CameraSortOrder[];

type MediaFilter = CameraKindFilter;

const MEDIA_FILTERS: readonly { id: MediaFilter; icon: string; label: string }[] = [
  { id: 'all', icon: 'apps', label: 'All' },
  { id: 'video', icon: 'movie', label: 'Videos' },
  { id: 'image', icon: 'image', label: 'Images' },
  { id: 'junk', icon: 'broken_image', label: 'Junk' },
];

const SORT_FILTERS: readonly { id: CameraSortOrder; label: string }[] = [
  { id: 'latest', label: 'Latest' },
  { id: 'oldest', label: 'Oldest' },
];

@Component({
  selector: 'app-camera-page',
  standalone: true,
  imports: [LazyThumbDirective, LazyAutoplayVideoDirective, CdkDropList, CdkDrag, CdkDragPreview, CdkDragPlaceholder],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './camera-page.component.html',
})
export class CameraPageComponent implements OnDestroy {
  private camera = inject(CameraService);
  private cameraCopy = inject(CameraCopyService);
  private foldersService = inject(FoldersService);
  private user = inject(UserContextService);
  private cacheService = inject(CacheService);
  readonly cameraDrag = inject(CameraDragService);
  readonly playback = inject(PlaybackService);

  readonly perRowOptions = PER_ROW_OPTIONS;
  readonly perRow = signal<PerRow>(6);
  readonly pageSizeOptions = PAGE_SIZE_OPTIONS;
  readonly pageSizeChoice = signal<PageSize>(24);
  readonly mediaFilters = MEDIA_FILTERS;
  readonly mediaFilter = signal<MediaFilter>('all');
  readonly sortFilters = SORT_FILTERS;
  readonly sortOrder = signal<CameraSortOrder>('latest');
  readonly items = this.camera.items;
  readonly loading = this.camera.loading;
  readonly scanning = this.camera.scanning;
  readonly label = this.camera.label;
  readonly truncated = this.camera.truncated;
  readonly timedOut = this.camera.timedOut;
  readonly status = this.camera.status;
  readonly pageIndex = this.camera.pageIndex;
  readonly pageCount = this.camera.pageCount;
  readonly total = this.camera.total;
  readonly isLoggedIn = computed(() => this.user.isLoggedIn());

  readonly selectMode = signal(false);
  readonly selected = signal<Set<string>>(new Set());
  readonly lastClicked = signal<string | null>(null);
  readonly notice = signal<string | null>(null);
  readonly opening = signal(false);
  readonly ejecting = signal(false);
  private cardWatch: Subscription | null = null;
  readonly canEject = computed(() => !!this.status()?.ejectable);
  readonly deleting = signal(false);
  readonly sharing = signal(false);
  readonly copying = this.cameraCopy.copying;
  readonly activeCopy = this.cameraCopy.activeCopy;
  readonly copyPercent = this.cameraCopy.copyPercent;
  readonly thumbFailed = signal<Set<string>>(new Set());
  private mediaCache = inject(MediaMemoryCacheService);
  /** "0:42" / "1:05:09" for a video tile, or null while unknown. */
  durationLabel(item: CameraItem): string | null {
    const ms = item.duration_ms || this.mediaCache.durationMs(item.thumb_url);
    if (!ms || ms <= 0) return null;
    const total = Math.round(ms / 1000);
    const h = Math.floor(total / 3600);
    const m = Math.floor((total % 3600) / 60);
    const sec = String(total % 60).padStart(2, '0');
    return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${sec}` : `${m}:${sec}`;
  }
  /** Full-screen / modal preview for one camera video (or image). */
  readonly preview = signal<CameraItem | null>(null);

  readonly selectedCount = computed(() => this.selected().size);
  readonly selectedBytesLabel = computed(() => {
    const sel = this.selected();
    if (sel.size === 0) return '';
    let bytes = 0;
    for (const item of this.items()) {
      if (sel.has(item.rel_path)) bytes += item.byte_size || 0;
    }
    return formatStorage(bytes);
  });
  readonly count = computed(() => this.status()?.all_count ?? this.total());
  readonly indexedCount = computed(() => this.status()?.indexed_count ?? this.total());
  readonly displayPage = computed(() => this.pageIndex() + 1);
  /** What the user picked. Autoplay itself caps how many videos decode at once. */
  readonly pageSize = computed(() => this.pageSizeChoice());
  readonly canPrev = computed(() => this.pageIndex() > 0);
  readonly canNext = computed(() => this.pageIndex() + 1 < this.pageCount());

  readonly mediaFilterCounts = computed(() => {
    const s = this.status();
    if (s) {
      return {
        all: s.all_count,
        video: s.video_count,
        image: s.image_count,
        junk: s.junk_count ?? 0,
      } as const;
    }
    return { all: this.total(), video: 0, image: 0, junk: 0 } as const;
  });

  readonly needsReconnect = computed(() => {
    const s = this.status();
    return !!s?.has_root && !s.available;
  });
  readonly folderStorageLabel = computed(() => {
    const s = this.status();
    if (!s?.has_root || !s.available) return null;
    const name = (s.label || this.label() || 'Camera folder').trim();
    const free = s.free_bytes ?? 0;
    const total = s.total_bytes ?? 0;
    if (total <= 0) return name;
    return `${name} · ${formatStorage(free)} free of ${formatStorage(total)}`;
  });

  readonly gridClass =
    'grid-cols-2 sm:grid-cols-3 md:[grid-template-columns:repeat(var(--per-row),minmax(0,1fr))]';

  private scanSub: Subscription | null = null;

  constructor() {
    const saved = this.cacheService.getItem<number>(PER_ROW_KEY);
    if (saved !== null && (PER_ROW_OPTIONS as readonly number[]).includes(saved)) {
      this.perRow.set(saved as PerRow);
    }
    effect(() => this.cacheService.setItem(PER_ROW_KEY, this.perRow()));

    const savedPageSize = this.cacheService.getItem<number>(PAGE_SIZE_KEY);
    if (savedPageSize !== null && (PAGE_SIZE_OPTIONS as readonly number[]).includes(savedPageSize)) {
      this.pageSizeChoice.set(savedPageSize as PageSize);
    }
    effect(() => this.cacheService.setItem(PAGE_SIZE_KEY, this.pageSizeChoice()));

    const savedSort = this.cacheService.getItem<string>(SORT_KEY);
    if (savedSort !== null && (SORT_OPTIONS as readonly string[]).includes(savedSort)) {
      this.sortOrder.set(savedSort as CameraSortOrder);
    }
    effect(() => this.cacheService.setItem(SORT_KEY, this.sortOrder()));

    if (this.user.isLoggedIn()) {
      this.camera.loadStatus().subscribe({
        next: (s) => {
          this.watchCard();
          if (s.available) {
            this.reloadPage(0).subscribe({
              next: () => {
                if (s.scan === 'running' || this.camera.scanning()) this.startScanPoll();
              },
              error: (err: Error) => this.notice.set(err.message),
            });
          }
        },
        error: (err: Error) => this.notice.set(err.message),
      });
    }
  }

  /**
   * Checks every few seconds whether the card is still there: pulled out → tiles disappear;
   * plugged back in → its files load again without clicking anything.
   */
  private watchCard(): void {
    this.cardWatch?.unsubscribe();
    let wasAvailable = !!this.status()?.available;
    this.cardWatch = interval(4000)
      .pipe(switchMap(() => this.camera.loadStatus().pipe(catchError(() => of(null)))))
      .subscribe((s) => {
        if (!s || !s.has_root) return;
        if (wasAvailable && !s.available) {
          this.closePreview();
          this.selected.set(new Set());
          this.camera.items.set([]);
        } else if (!wasAvailable && s.available) {
          this.notice.set(null);
          this.reloadPage(0).subscribe({ error: () => {} });
        }
        wasAvailable = s.available;
      });
  }

  ngOnDestroy(): void {
    this.cardWatch?.unsubscribe();
    this.scanSub?.unsubscribe();
    this.playback.previewPause.set(false);
  }

  setPerRow(n: PerRow): void {
    this.perRow.set(n);
    this.selected.set(new Set());
    this.reloadPage(0).subscribe({ error: (err: Error) => this.notice.set(err.message) });
  }

  setPageSize(n: PageSize): void {
    if (this.pageSizeChoice() === n) return;
    this.pageSizeChoice.set(n);
    this.selected.set(new Set());
    this.reloadPage(0).subscribe({ error: (err: Error) => this.notice.set(err.message) });
  }

  onPageSizeSelect(event: Event): void {
    const raw = Number((event.target as HTMLSelectElement).value);
    if ((PAGE_SIZE_OPTIONS as readonly number[]).includes(raw)) {
      this.setPageSize(raw as PageSize);
    }
  }

  setMediaFilter(filter: MediaFilter): void {
    this.mediaFilter.set(filter);
    this.selected.set(new Set());
    this.lastClicked.set(null);
    this.reloadPage(0).subscribe({ error: (err: Error) => this.notice.set(err.message) });
  }

  setSortOrder(sort: CameraSortOrder): void {
    if (this.sortOrder() === sort) return;
    this.sortOrder.set(sort);
    this.selected.set(new Set());
    this.lastClicked.set(null);
    this.reloadPage(0).subscribe({ error: (err: Error) => this.notice.set(err.message) });
  }

  goPrev(): void {
    if (!this.canPrev()) return;
    this.selected.set(new Set());
    this.reloadPage(this.pageIndex() - 1).subscribe({ error: (err: Error) => this.notice.set(err.message) });
  }

  goNext(): void {
    if (!this.canNext()) return;
    this.selected.set(new Set());
    this.reloadPage(this.pageIndex() + 1).subscribe({ error: (err: Error) => this.notice.set(err.message) });
  }

  openFolder(): void {
    if (this.opening()) return;
    this.opening.set(true);
    this.notice.set(null);
    this.camera.open().subscribe({
      next: () => {
        this.opening.set(false);
        this.selected.set(new Set());
        this.startScanPoll();
        this.reloadPage(0).subscribe({ error: () => {} });
      },
      error: (err: Error) => {
        this.opening.set(false);
        this.notice.set(err.message);
      },
    });
  }

  /** Stops everything reading the card (players, autoplay), then ejects it like Finder does. */
  async ejectCard(): Promise<void> {
    if (this.ejecting() || this.copying() || this.scanning()) return;
    this.ejecting.set(true);
    this.notice.set(null);
    this.closePreview();
    this.playback.previewPause.set(true);
    // Give the browser a moment to drop its open video streams from the card.
    await new Promise((r) => setTimeout(r, 600));
    try {
      const name = await firstValueFrom(this.camera.eject());
      this.selected.set(new Set());
      this.notice.set(`"${name}" was ejected. It's safe to remove the card.`);
      this.camera.loadStatus().subscribe({ error: () => {} });
    } catch (err) {
      this.notice.set(err instanceof Error ? err.message : 'Could not eject the card.');
    } finally {
      this.playback.previewPause.set(false);
      this.ejecting.set(false);
    }
  }

  refresh(): void {
    this.camera.refresh().subscribe({
      next: () => {
        this.startScanPoll();
        this.reloadPage(0).subscribe({ error: (err: Error) => this.notice.set(err.message) });
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  forgetCard(): void {
    this.scanSub?.unsubscribe();
    this.camera.clearRoot().subscribe({
      next: () => {
        this.selected.set(new Set());
        this.notice.set(null);
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  isSelected(rel: string): boolean {
    return this.selected().has(rel);
  }

  toggleSelectMode(): void {
    const next = !this.selectMode();
    this.selectMode.set(next);
    if (!next) {
      this.selected.set(new Set());
      this.lastClicked.set(null);
    }
  }

  openPreview(item: CameraItem, event?: Event): void {
    event?.stopPropagation();
    event?.preventDefault();
    if (item.kind === 'junk') return;
    this.playback.previewPause.set(item.kind === 'video');
    this.preview.set(item);
  }

  closePreview(): void {
    this.playback.previewPause.set(false);
    this.preview.set(null);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.preview()) this.closePreview();
  }

  onTileClick(item: CameraItem, event: MouseEvent): void {
    if (!this.selectMode()) {
      this.openPreview(item, event);
      return;
    }
    const list = this.items();
    const next = new Set(this.selected());
    if (event.shiftKey && this.lastClicked()) {
      const a = list.findIndex((i) => i.rel_path === this.lastClicked());
      const b = list.findIndex((i) => i.rel_path === item.rel_path);
      if (a >= 0 && b >= 0) {
        const [lo, hi] = a < b ? [a, b] : [b, a];
        for (let i = lo; i <= hi; i++) next.add(list[i].rel_path);
      }
    } else if (event.metaKey || event.ctrlKey) {
      if (next.has(item.rel_path)) next.delete(item.rel_path);
      else next.add(item.rel_path);
    } else {
      if (next.size === 1 && next.has(item.rel_path)) next.clear();
      else {
        next.clear();
        next.add(item.rel_path);
      }
    }
    this.selected.set(next);
    this.lastClicked.set(item.rel_path);
  }

  selectAll(): void {
    if (!this.selectMode()) this.selectMode.set(true);
    this.selected.set(new Set(this.items().map((i) => i.rel_path)));
  }

  clearSelection(): void {
    this.selected.set(new Set());
  }

  async shareSelected(): Promise<void> {
    if (this.sharing() || this.deleting() || this.loading() || this.scanning()) return;
    const sel = this.selected();
    const items = this.items().filter((it) => sel.has(it.rel_path) && it.kind !== 'junk');
    if (!items.length) {
      this.notice.set(sel.size > 0 ? 'Nothing to share (junk files skipped).' : 'Nothing to share.');
      return;
    }

    this.sharing.set(true);
    this.notice.set(null);
    try {
      await firstValueFrom(this.camera.airDrop(items.map((it) => it.rel_path)));
    } catch (err) {
      this.notice.set(err instanceof Error ? err.message : 'Could not open AirDrop.');
    } finally {
      this.sharing.set(false);
    }
  }

  deleteSelected(): void {
    const paths = [...this.selected()];
    if (!paths.length || this.deleting()) return;
    const n = paths.length;
    const ok = confirm(
      n === 1
        ? 'Delete this file from the camera card? This cannot be undone.'
        : `Delete ${n} files from the camera card? This cannot be undone.`,
    );
    if (!ok) return;
    this.deleting.set(true);
    this.notice.set(null);
    this.camera.deleteFiles(paths).subscribe({
      next: (res) => {
        this.deleting.set(false);
        this.selected.set(new Set());
        this.lastClicked.set(null);
        const failed = res.failed?.length ?? 0;
        if (failed && res.deleted) {
          this.notice.set(`Deleted ${res.deleted}; ${failed} could not be removed.`);
        } else if (failed) {
          this.notice.set(`Could not delete ${failed} file${failed === 1 ? '' : 's'} (card may be locked).`);
        } else {
          this.notice.set(`Deleted ${res.deleted} file${res.deleted === 1 ? '' : 's'}.`);
        }
        this.camera.loadStatus().subscribe({ next: () => {} });
        this.reloadPage(this.pageIndex()).subscribe({
          next: () => {
            if (this.items().length === 0 && this.pageIndex() > 0) {
              this.reloadPage(this.pageIndex() - 1).subscribe({
                error: (err: Error) => this.notice.set(err.message),
              });
            }
          },
          error: (err: Error) => this.notice.set(err.message),
        });
      },
      error: (err: Error) => {
        this.deleting.set(false);
        this.notice.set(err.message);
      },
    });
  }

  onThumbError(rel: string): void {
    this.thumbFailed.update((s) => new Set(s).add(rel));
  }

  /** Files being dragged (the tile, or the whole selection when the tile is part of it). */
  readonly dragPaths = signal<string[]>([]);

  /** Same drag as folder cards: small chip follows the pointer, videos pause, sidebar highlights. */
  onTileDragStarted(item: CameraItem): void {
    let paths = [...this.selected()];
    if (!paths.includes(item.rel_path)) {
      paths = [item.rel_path];
      this.selected.set(new Set(paths));
    }
    this.dragPaths.set(paths);
    this.cameraDrag.dragging.set({ kind: 'camera', paths });
    this.playback.dragPause.set(true);
  }

  onTileDragMoved(event: CdkDragMove<CameraItem>): void {
    this.cameraDrag.hoverTarget.set(this.cameraDrag.targetAt(event.pointerPosition.x, event.pointerPosition.y));
  }

  /** Dropped on Home or a folder: copy the files in. Anywhere else: the tile glides back. */
  onTileDragEnded(event: CdkDragEnd<CameraItem>): void {
    const target = this.cameraDrag.targetAt(event.dropPoint.x, event.dropPoint.y) ?? this.cameraDrag.hoverTarget();
    const paths = this.dragPaths();
    this.cameraDrag.clear();
    this.playback.dragPause.set(false);
    this.dragPaths.set([]);
    if (target != null && paths.length) this.copyToFolder(target, paths);
  }

  /** Thumbnail already in memory for the drag chip (no extra card reads). */
  dragChipThumb(item: CameraItem): string | null {
    return item.thumb_url ? this.mediaCache.peek(item.thumb_url) : null;
  }

  /** "Copy 1 video" / "Copy 3 files" for the drag chip. */
  dragChipLabel(): string {
    const paths = new Set(this.dragPaths());
    const n = paths.size;
    const kinds = new Set(this.items().filter((i) => paths.has(i.rel_path)).map((i) => i.kind));
    const noun = kinds.size === 1 && kinds.has('video') ? 'video' : kinds.size === 1 && kinds.has('image') ? 'photo' : 'file';
    return `Copy ${n} ${noun}${n === 1 ? '' : 's'}`;
  }

  formatCopyBytes(bytes: number): string {
    return formatStorage(bytes);
  }

  private reloadPage(pageIndex: number) {
    return this.camera.loadPage(pageIndex, this.mediaFilter(), this.pageSize(), this.sortOrder());
  }

  /** Copies camera files into Home or a folder as cards (the card itself is left untouched). */
  private copyToFolder(target: MoveTarget, paths: string[]): void {
    if (this.copying()) return;
    this.notice.set(null);
    const name = target === 'dump' ? 'Home' : (this.foldersService.byId().get(target)?.name ?? 'folder');
    this.cameraCopy.copy(target, paths).subscribe({
      next: (job) => {
        this.notice.set(this.cameraCopy.summary(job, name));
        this.selected.set(new Set());
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }


  private startScanPoll(): void {
    this.scanSub?.unsubscribe();
    const size = this.pageSize();
    this.scanSub = this.camera
      .pollWhileScanning(this.mediaFilter(), size, this.sortOrder())
      .pipe(takeWhile((s) => s.scan === 'running', true))
      .subscribe({
        next: () => {},
        error: () => {},
        complete: () => {
          this.camera.loadStatus().subscribe({ next: () => {} });
          this.reloadPage(0).subscribe({ error: () => {} });
        },
      });
  }
}

function formatStorage(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'] as const;
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const digits = i === 0 ? 0 : n >= 10 ? 0 : 1;
  return `${n.toFixed(digits)} ${units[i]}`;
}
