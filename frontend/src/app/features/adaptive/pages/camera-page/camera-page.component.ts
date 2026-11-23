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
import { Subscription, takeWhile } from 'rxjs';
import { CameraItem, CameraKindFilter, CameraService } from '../../services/camera.service';
import { CameraDragService } from '../../services/camera-drag.service';
import { LocalMediaService } from '../../services/local-media.service';
import { CacheService } from '../../services/cache.service';
import { UserContextService } from '../../services/user-context.service';
import { LazyThumbDirective } from '../../shared/directives/lazy-thumb.directive';
import { LazyAutoplayVideoDirective } from '../../shared/directives/lazy-autoplay-video.directive';
import { PlaybackService } from '../../services/playback.service';

const PER_ROW_KEY = 'dump_per_row';
const PER_ROW_OPTIONS = [4, 6, 8, 12] as const;
type PerRow = (typeof PER_ROW_OPTIONS)[number];

const PAGE_SIZE_KEY = 'camera_page_size';
const PAGE_SIZE_OPTIONS = [12, 24, 36, 48, 60, 72, 84, 96, 120] as const;
type PageSize = (typeof PAGE_SIZE_OPTIONS)[number];

type MediaFilter = CameraKindFilter;

const MEDIA_FILTERS: readonly { id: MediaFilter; icon: string; label: string }[] = [
  { id: 'all', icon: 'apps', label: 'All' },
  { id: 'video', icon: 'movie', label: 'Videos' },
  { id: 'image', icon: 'image', label: 'Images' },
  { id: 'junk', icon: 'broken_image', label: 'Junk' },
];

@Component({
  selector: 'app-camera-page',
  standalone: true,
  imports: [LazyThumbDirective, LazyAutoplayVideoDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './camera-page.component.html',
})
export class CameraPageComponent implements OnDestroy {
  private camera = inject(CameraService);
  private localMedia = inject(LocalMediaService);
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
  readonly folders = this.localMedia.folders;
  readonly isLoggedIn = computed(() => this.user.isLoggedIn());

  readonly selectMode = signal(false);
  readonly selected = signal<Set<string>>(new Set());
  readonly lastClicked = signal<string | null>(null);
  readonly notice = signal<string | null>(null);
  readonly opening = signal(false);
  readonly deleting = signal(false);
  readonly copying = this.localMedia.copying;
  readonly activeCopy = this.localMedia.activeCopy;
  readonly copyPercent = this.localMedia.copyPercent;
  readonly thumbFailed = signal<Set<string>>(new Set());
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
  /** Videos clamp to 24 so a large "per page" choice cannot mount a wall of decoders. */
  readonly pageSize = computed(() => {
    const n = this.pageSizeChoice();
    return this.mediaFilter() === 'video' ? Math.min(n, 24) : n;
  });
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
  readonly noFolders = computed(() => this.folders().length === 0);
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

    if (this.user.isLoggedIn()) {
      if (!this.localMedia.loaded()) this.localMedia.loadFolders();
      this.camera.loadStatus().subscribe({
        next: (s) => {
          if (s.available || s.has_root) {
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

  ngOnDestroy(): void {
    this.scanSub?.unsubscribe();
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
    this.preview.set(item);
  }

  closePreview(): void {
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

  onDragStart(event: DragEvent, item: CameraItem): void {
    let paths = [...this.selected()];
    if (!paths.includes(item.rel_path)) {
      paths = [item.rel_path];
      this.selected.set(new Set(paths));
    }
    this.cameraDrag.dragging.set({ kind: 'camera', paths });
    event.dataTransfer?.setData('application/x-instaboard-camera', JSON.stringify(paths));
    event.dataTransfer!.effectAllowed = 'copy';
  }

  @HostListener('document:dragover', ['$event'])
  onDocDragOver(event: DragEvent): void {
    if (this.cameraDrag.dragging()?.kind !== 'camera') return;
    event.preventDefault();
    this.cameraDrag.hoverFolderId.set(this.cameraDrag.targetFolderAt(event.clientX, event.clientY));
  }

  @HostListener('document:drop', ['$event'])
  onDocDrop(event: DragEvent): void {
    const drag = this.cameraDrag.dragging();
    if (drag?.kind !== 'camera' || !drag.paths.length) return;
    event.preventDefault();
    const folderId = this.cameraDrag.targetFolderAt(event.clientX, event.clientY);
    this.cameraDrag.clear();
    if (folderId == null) return;
    this.copyToFolder(folderId, drag.paths);
  }

  @HostListener('document:dragend')
  onDocDragEnd(): void {
    this.cameraDrag.clear();
  }

  copySelectedTo(folderId: number): void {
    const paths = [...this.selected()];
    if (!paths.length) return;
    this.copyToFolder(folderId, paths);
  }

  formatCopyBytes(bytes: number): string {
    return formatStorage(bytes);
  }

  private reloadPage(pageIndex: number) {
    return this.camera.loadPage(pageIndex, this.mediaFilter(), this.pageSize());
  }

  private copyToFolder(folderId: number, paths: string[]): void {
    if (this.copying()) return;
    if (this.noFolders()) {
      this.notice.set('Create a Local media folder in the sidebar first.');
      return;
    }
    this.notice.set(null);
    this.localMedia.runCopyJob(folderId, paths).subscribe({
      next: () => {
        if (this.localMedia.notice()) this.notice.set(this.localMedia.notice());
        this.selected.set(new Set());
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  private startScanPoll(): void {
    this.scanSub?.unsubscribe();
    const size = this.pageSize();
    this.scanSub = this.camera.pollWhileScanning(this.mediaFilter(), size).pipe(
      takeWhile((s) => s.scan === 'running', true),
    ).subscribe({
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
