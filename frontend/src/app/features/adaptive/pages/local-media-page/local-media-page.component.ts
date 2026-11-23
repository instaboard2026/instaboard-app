import {
  ChangeDetectionStrategy,
  Component,
  HostListener,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { ActivatedRoute } from '@angular/router';
import { toSignal } from '@angular/core/rxjs-interop';
import { forkJoin, map } from 'rxjs';
import { LazyThumbDirective } from '../../shared/directives/lazy-thumb.directive';
import { LazyAutoplayVideoDirective } from '../../shared/directives/lazy-autoplay-video.directive';
import { CacheService } from '../../services/cache.service';
import { CameraDragService } from '../../services/camera-drag.service';
import { DumpService, MoveTarget } from '../../services/dump.service';
import { FoldersService } from '../../services/folders.service';
import { LocalMediaItem, LocalMediaService } from '../../services/local-media.service';
import { PlaybackService } from '../../services/playback.service';
import { UserContextService } from '../../services/user-context.service';
import { ConfirmDialogService } from '../../services/confirm-dialog.service';

const PER_ROW_KEY = 'dump_per_row';
const PER_ROW_OPTIONS = [4, 6, 8, 12] as const;
type PerRow = (typeof PER_ROW_OPTIONS)[number];

const PAGE_SIZE_KEY = 'local_media_page_size';
const PAGE_SIZE_OPTIONS = [12, 24, 36, 48, 60, 72, 84, 96, 120] as const;
type PageSize = (typeof PAGE_SIZE_OPTIONS)[number];

type MediaFilter = 'all' | 'video' | 'image';

const MEDIA_FILTERS: readonly { id: MediaFilter; icon: string; label: string }[] = [
  { id: 'all', icon: 'apps', label: 'All' },
  { id: 'video', icon: 'movie', label: 'Videos' },
  { id: 'image', icon: 'image', label: 'Images' },
];

@Component({
  selector: 'app-local-media-page',
  standalone: true,
  imports: [LazyThumbDirective, LazyAutoplayVideoDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './local-media-page.component.html',
  host: {
    '(dragover)': 'onDragOver($event)',
    '(drop)': 'onDrop($event)',
  },
})
export class LocalMediaPageComponent {
  private route = inject(ActivatedRoute);
  private localMedia = inject(LocalMediaService);
  private dump = inject(DumpService);
  private folders = inject(FoldersService);
  private user = inject(UserContextService);
  private cacheService = inject(CacheService);
  private confirm = inject(ConfirmDialogService);
  readonly cameraDrag = inject(CameraDragService);
  readonly playback = inject(PlaybackService);

  readonly perRowOptions = PER_ROW_OPTIONS;
  readonly perRow = signal<PerRow>(6);
  readonly pageSizeOptions = PAGE_SIZE_OPTIONS;
  readonly pageSizeChoice = signal<PageSize>(24);
  readonly pageIndex = signal(0);
  readonly mediaFilters = MEDIA_FILTERS;
  readonly mediaFilter = signal<MediaFilter>('all');
  readonly loading = signal(false);
  readonly notice = signal<string | null>(null);
  readonly deleting = signal(false);
  readonly openingFinder = signal(false);
  readonly copying = this.localMedia.copying;
  readonly activeCopy = this.localMedia.activeCopy;
  readonly copyPercent = this.localMedia.copyPercent;
  readonly heicFailed = signal<Set<number>>(new Set());
  readonly dropping = signal(false);

  readonly selectMode = signal(false);
  readonly selected = signal<Set<number>>(new Set());
  readonly lastClicked = signal<number | null>(null);
  readonly preview = signal<LocalMediaItem | null>(null);

  readonly isLoggedIn = computed(() => this.user.isLoggedIn());
  readonly folderId = toSignal(
    this.route.paramMap.pipe(map((p) => Number(p.get('id')))),
    { initialValue: Number(this.route.snapshot.paramMap.get('id')) },
  );

  readonly folder = computed(() => {
    const id = this.folderId();
    if (!Number.isFinite(id)) return null;
    return this.localMedia.byId().get(id) ?? null;
  });

  readonly allItems = computed(() => {
    const id = this.folderId();
    if (!Number.isFinite(id)) return [] as LocalMediaItem[];
    return this.localMedia.itemsByFolder().get(id) ?? [];
  });

  readonly filteredItems = computed(() => {
    const filter = this.mediaFilter();
    const list = this.allItems();
    if (filter === 'all') return list;
    return list.filter((it) => (it.kind || it.type) === filter);
  });

  readonly pageSize = computed(() => {
    const n = this.pageSizeChoice();
    return this.mediaFilter() === 'video' ? Math.min(n, 24) : n;
  });

  readonly pageCount = computed(() => {
    const size = this.pageSize();
    const total = this.filteredItems().length;
    if (size <= 0) return 1;
    return Math.max(1, Math.ceil(total / size));
  });

  readonly displayPage = computed(() => this.pageIndex() + 1);
  readonly canPrev = computed(() => this.pageIndex() > 0);
  readonly canNext = computed(() => this.pageIndex() + 1 < this.pageCount());

  readonly pageItems = computed(() => {
    const size = this.pageSize();
    const start = this.pageIndex() * size;
    return this.filteredItems().slice(start, start + size);
  });

  readonly count = computed(() => this.allItems().length);
  readonly selectedCount = computed(() => this.selected().size);
  readonly selectedBytesLabel = computed(() => {
    const sel = this.selected();
    if (sel.size === 0) return '';
    let bytes = 0;
    for (const item of this.pageItems()) {
      if (sel.has(item.id)) bytes += item.byte_size || 0;
    }
    return formatStorage(bytes);
  });

  readonly mediaFilterCounts = computed(() => {
    const list = this.allItems();
    let video = 0;
    let image = 0;
    for (const it of list) {
      if ((it.kind || it.type) === 'video') video++;
      else image++;
    }
    return { all: list.length, video, image } as const;
  });

  readonly gridClass =
    'grid-cols-2 sm:grid-cols-3 md:[grid-template-columns:repeat(var(--per-row),minmax(0,1fr))]';

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

    effect(() => {
      const id = this.folderId();
      if (!this.user.isLoggedIn() || !Number.isFinite(id) || id <= 0) return;
      if (!this.localMedia.loaded()) this.localMedia.loadFolders();
      this.loading.set(true);
      this.selected.set(new Set());
      this.lastClicked.set(null);
      this.pageIndex.set(0);
      this.localMedia.loadItems(id).subscribe({
        next: () => this.loading.set(false),
        error: (err: Error) => {
          this.loading.set(false);
          this.notice.set(err.message);
        },
      });
    });

    // Clamp page index when filter/size shrinks the page count.
    effect(() => {
      const last = Math.max(0, this.pageCount() - 1);
      if (this.pageIndex() > last) this.pageIndex.set(last);
    });
  }

  setPerRow(n: PerRow): void {
    this.perRow.set(n);
  }

  setPageSize(n: PageSize): void {
    if (this.pageSizeChoice() === n) return;
    this.pageSizeChoice.set(n);
    this.selected.set(new Set());
    this.pageIndex.set(0);
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
    this.pageIndex.set(0);
  }

  goPrev(): void {
    if (!this.canPrev()) return;
    this.selected.set(new Set());
    this.pageIndex.update((i) => Math.max(0, i - 1));
  }

  goNext(): void {
    if (!this.canNext()) return;
    this.selected.set(new Set());
    this.pageIndex.update((i) => i + 1);
  }

  isSelected(id: number): boolean {
    return this.selected().has(id);
  }

  toggleSelectMode(): void {
    const next = !this.selectMode();
    this.selectMode.set(next);
    if (!next) {
      this.selected.set(new Set());
      this.lastClicked.set(null);
    }
  }

  openPreview(item: LocalMediaItem, event?: Event): void {
    event?.stopPropagation();
    event?.preventDefault();
    this.preview.set(item);
  }

  closePreview(): void {
    this.preview.set(null);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.preview()) this.closePreview();
  }

  onTileClick(item: LocalMediaItem, event: MouseEvent): void {
    if (!this.selectMode()) {
      this.openPreview(item, event);
      return;
    }
    const list = this.pageItems();
    const next = new Set(this.selected());
    if (event.shiftKey && this.lastClicked() != null) {
      const a = list.findIndex((i) => i.id === this.lastClicked());
      const b = list.findIndex((i) => i.id === item.id);
      if (a >= 0 && b >= 0) {
        const [lo, hi] = a < b ? [a, b] : [b, a];
        for (let i = lo; i <= hi; i++) next.add(list[i].id);
      }
    } else if (event.metaKey || event.ctrlKey) {
      if (next.has(item.id)) next.delete(item.id);
      else next.add(item.id);
    } else {
      if (next.size === 1 && next.has(item.id)) next.clear();
      else {
        next.clear();
        next.add(item.id);
      }
    }
    this.selected.set(next);
    this.lastClicked.set(item.id);
  }

  selectAll(): void {
    if (!this.selectMode()) this.selectMode.set(true);
    this.selected.set(new Set(this.pageItems().map((i) => i.id)));
  }

  clearSelection(): void {
    this.selected.set(new Set());
  }

  async deleteSelected(): Promise<void> {
    const ids = [...this.selected()];
    if (!ids.length || this.deleting()) return;
    const n = ids.length;
    const ok = await this.confirm.ask({
      title: n === 1 ? 'Delete this file?' : `Delete ${n} files?`,
      message: 'Removes them from Local media. Originals on your camera card are not touched.',
      confirmLabel: 'Delete',
      danger: true,
    });
    if (!ok) return;
    const folderId = this.folderId();
    this.deleting.set(true);
    this.notice.set(null);
    forkJoin(ids.map((id) => this.localMedia.deleteItem(folderId, id))).subscribe({
      next: () => {
        this.deleting.set(false);
        this.selected.set(new Set());
        this.lastClicked.set(null);
        this.notice.set(`Deleted ${n} file${n === 1 ? '' : 's'}.`);
      },
      error: (err: Error) => {
        this.deleting.set(false);
        this.notice.set(err.message);
        this.localMedia.loadItems(folderId).subscribe({ error: () => {} });
      },
    });
  }

  onDragStart(event: DragEvent, item: LocalMediaItem): void {
    const fromFolderId = this.folderId();
    if (!Number.isFinite(fromFolderId)) return;
    let ids = [...this.selected()];
    if (!ids.includes(item.id)) {
      ids = [item.id];
      this.selected.set(new Set(ids));
    }
    this.cameraDrag.dragging.set({ kind: 'local-move', fromFolderId, ids });
    event.dataTransfer?.setData('application/x-instaboard-local-media', JSON.stringify(ids));
    event.dataTransfer!.effectAllowed = 'move';
  }

  @HostListener('document:dragover', ['$event'])
  onDocDragOver(event: DragEvent): void {
    if (this.cameraDrag.dragging()?.kind !== 'local-move') return;
    event.preventDefault();
    if (event.dataTransfer) event.dataTransfer.dropEffect = 'move';
    const target = this.cameraDrag.dropTargetAt(event.clientX, event.clientY);
    this.cameraDrag.hoverFolderId.set(target?.type === 'local-media' ? target.folderId : null);
    this.cameraDrag.hoverDumpTarget.set(target?.type === 'dump' ? target.scope : null);
  }

  @HostListener('document:drop', ['$event'])
  onDocDrop(event: DragEvent): void {
    const drag = this.cameraDrag.dragging();
    if (drag?.kind !== 'local-move' || !drag.ids.length) return;
    event.preventDefault();
    const target = this.cameraDrag.dropTargetAt(event.clientX, event.clientY);
    this.cameraDrag.clear();
    this.dropping.set(false);
    if (!target) return;
    if (target.type === 'local-media') {
      if (target.folderId === drag.fromFolderId) return;
      this.moveToFolder(target.folderId, drag.ids, drag.fromFolderId);
      return;
    }
    this.promoteToDump(target.scope, drag.ids, drag.fromFolderId);
  }

  @HostListener('document:dragend')
  onDocDragEnd(): void {
    if (this.cameraDrag.dragging()?.kind === 'local-move') {
      this.cameraDrag.clear();
      this.dropping.set(false);
    }
  }

  onDragOver(event: DragEvent): void {
    const drag = this.cameraDrag.dragging();
    if (!drag) return;
    if (drag.kind === 'local-move' && drag.fromFolderId === this.folderId()) return;
    event.preventDefault();
    if (event.dataTransfer) {
      event.dataTransfer.dropEffect = drag.kind === 'local-move' ? 'move' : 'copy';
    }
    this.dropping.set(true);
    this.cameraDrag.hoverFolderId.set(this.folderId());
  }

  onDrop(event: DragEvent): void {
    event.preventDefault();
    this.dropping.set(false);
    const drag = this.cameraDrag.dragging();
    this.cameraDrag.clear();
    const id = this.folderId();
    if (!drag || !Number.isFinite(id)) return;
    if (drag.kind === 'camera') {
      if (!drag.paths.length) return;
      this.copyPaths(id, drag.paths);
      return;
    }
    if (drag.kind === 'local-move' && drag.fromFolderId !== id && drag.ids.length) {
      this.moveToFolder(id, drag.ids, drag.fromFolderId);
    }
  }

  formatCopyBytes(bytes: number): string {
    return formatStorage(bytes);
  }

  private copyPaths(folderId: number, paths: string[]): void {
    if (this.copying()) return;
    this.notice.set(null);
    this.localMedia.runCopyJob(folderId, paths).subscribe({
      next: (job) => {
        this.notice.set(this.localMedia.notice() || (job.message ?? null));
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  private moveToFolder(toFolderId: number, ids: number[], fromFolderId: number): void {
    this.notice.set(null);
    this.localMedia.moveItems(toFolderId, ids, fromFolderId).subscribe({
      next: (res) => {
        this.selected.set(new Set());
        this.lastClicked.set(null);
        const n = res.moved ?? ids.length;
        if (n > 0) this.notice.set(`Moved ${n} file${n === 1 ? '' : 's'}.`);
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  private promoteToDump(scope: MoveTarget, ids: number[], fromFolderId: number): void {
    this.notice.set(null);
    this.dump.promoteFromLocalMedia(ids, scope).subscribe({
      next: (saves) => {
        this.localMedia.removeItemsLocally(fromFolderId, ids);
        this.folders.noteAddedSaves(scope, saves.length);
        this.selected.set(new Set());
        this.lastClicked.set(null);
        const n = saves.length;
        const where = scope === 'dump' ? 'Home' : 'folder';
        this.notice.set(`Moved ${n} file${n === 1 ? '' : 's'} to ${where}.`);
      },
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  onImgError(id: number): void {
    this.heicFailed.update((s) => new Set(s).add(id));
  }

  openInFinder(): void {
    const id = this.folderId();
    if (!Number.isFinite(id) || id <= 0 || this.openingFinder()) return;
    this.openingFinder.set(true);
    this.notice.set(null);
    this.localMedia.openInFinder(id).subscribe({
      next: () => this.openingFinder.set(false),
      error: (err: Error) => {
        this.openingFinder.set(false);
        this.notice.set(err.message);
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
