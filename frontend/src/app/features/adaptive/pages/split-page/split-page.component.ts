import {
  ChangeDetectionStrategy,
  Component,
  HostListener,
  computed,
  effect,
  inject,
  signal,
} from '@angular/core';
import { RouterLink } from '@angular/router';
import { forkJoin, of } from 'rxjs';
import { catchError } from 'rxjs/operators';
import { AutoplayVideoDirective } from '../../shared/directives/autoplay-video.directive';
import { CacheService } from '../../services/cache.service';
import { CardDragService, isSplitDrop } from '../../services/card-drag.service';
import { DumpService, Save } from '../../services/dump.service';
import { SplitProject, SplitService } from '../../services/split.service';
import { UserContextService } from '../../services/user-context.service';

const PER_ROW_KEY = 'dump_per_row';
const PER_ROW_OPTIONS = [4, 6, 8, 12] as const;
type PerRow = (typeof PER_ROW_OPTIONS)[number];

@Component({
  selector: 'app-split-page',
  standalone: true,
  imports: [RouterLink, AutoplayVideoDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './split-page.component.html',
})
export class SplitPageComponent {
  private split = inject(SplitService);
  private dump = inject(DumpService);
  private user = inject(UserContextService);
  private cacheService = inject(CacheService);
  readonly cardDrag = inject(CardDragService);

  readonly perRowOptions = PER_ROW_OPTIONS;
  readonly projects = this.split.projects;
  readonly loading = this.split.loading;
  readonly isLoggedIn = computed(() => this.user.isLoggedIn());
  readonly count = computed(() => this.projects().length);

  readonly dropping = signal(false);
  readonly busyId = signal<number | null>(null);
  readonly notice = signal<string | null>(null);
  readonly detailsProject = signal<SplitProject | null>(null);
  readonly resultsProject = signal<SplitProject | null>(null);
  readonly cutting = signal(false);
  readonly saving = signal(false);
  readonly perRow = signal<PerRow>(6);

  readonly gridClass =
    'grid-cols-2 sm:grid-cols-3 md:[grid-template-columns:repeat(var(--per-row),minmax(0,1fr))]';

  /** Projects pinned to the page board as master + clips windows (id → full project). */
  private readonly boardsById = signal<ReadonlyMap<number, SplitProject>>(new Map());

  readonly boardList = computed(() => {
    const boards = this.boardsById();
    return this.projects()
      .map((p) => boards.get(p.id))
      .filter((p): p is SplitProject => !!p && (p.clips?.length ?? 0) > 0);
  });

  /** Projects not yet shown as a cut window (no saved board / no clips). */
  readonly pendingProjects = computed(() => {
    const boards = this.boardsById();
    return this.projects().filter((p) => !boards.has(p.id) || !(boards.get(p.id)?.clips?.length));
  });

  constructor() {
    const saved = this.cacheService.getItem<number>(PER_ROW_KEY);
    if (saved !== null && (PER_ROW_OPTIONS as readonly number[]).includes(saved)) {
      this.perRow.set(saved as PerRow);
    }
    effect(() => this.cacheService.setItem(PER_ROW_KEY, this.perRow()));

    if (this.user.isLoggedIn()) {
      this.split.load().subscribe({
        next: (list) => this.hydrateBoards(list),
        error: (err: Error) => this.notice.set(err.message),
      });
    }
  }

  setPerRow(n: PerRow): void {
    this.perRow.set(n);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.resultsProject()) {
      this.closeResults();
      return;
    }
    if (this.detailsProject()) this.closeDetails();
  }

  onDragOver(event: DragEvent): void {
    if (!this.isLoggedIn()) return;
    event.preventDefault();
    this.dropping.set(true);
  }

  onDragLeave(): void {
    this.dropping.set(false);
  }

  onFileDrop(event: DragEvent): void {
    event.preventDefault();
    this.dropping.set(false);
    if (!this.isLoggedIn()) return;

    if (this.cardDrag.dragging()) return;

    const files = event.dataTransfer?.files;
    if (!files?.length) return;
    const file = files[0];
    if (!file.type.startsWith('video/') && !/\.(mp4|mov|m4v|webm)$/i.test(file.name)) {
      this.notice.set('Drop an mp4, mov, m4v, or webm video');
      return;
    }
    this.ingestUpload(file);
  }

  onFilePicked(event: Event): void {
    const input = event.target as HTMLInputElement;
    const file = input.files?.[0];
    input.value = '';
    if (file) this.ingestUpload(file);
  }

  ingestUpload(file: File): void {
    this.notice.set(null);
    this.split.createFromUpload(file).subscribe({
      error: (err: Error) => this.notice.set(err.message),
    });
  }

  openDetails(project: SplitProject): void {
    this.detailsProject.set(project);
  }

  closeDetails(): void {
    this.detailsProject.set(null);
  }

  detailsSave(project: SplitProject): Save | null {
    const id = project.source_save_id;
    if (id == null) return null;
    return this.dump.byId().get(id) ?? null;
  }

  openCut(project: SplitProject): void {
    this.resultsProject.set(project);
    if (project.status === 'done' && project.clip_count > 0) {
      this.cutting.set(true);
      this.split.get(project.id).subscribe({
        next: (p) => {
          this.cutting.set(false);
          this.resultsProject.set(p);
        },
        error: (err: Error) => {
          this.cutting.set(false);
          this.notice.set(err.message);
        },
      });
      return;
    }
    this.runCut(project.id);
  }

  closeResults(): void {
    this.resultsProject.set(null);
    this.cutting.set(false);
    this.saving.set(false);
  }

  /** Pin master + clips onto the Split page board and close the cuts window. */
  saveResults(): void {
    const project = this.resultsProject();
    if (!project) return;
    if (!(project.clips?.length)) {
      this.notice.set('Cut the video first, then save');
      return;
    }
    this.saving.set(true);
    this.pinBoard(project);
    this.saving.set(false);
    this.closeResults();
    this.notice.set(null);
  }

  runCut(id: number): void {
    this.cutting.set(true);
    this.notice.set(null);
    this.split.cut(id).subscribe({
      next: (p) => {
        this.cutting.set(false);
        this.resultsProject.set(p);
        if ((p.clips?.length ?? 0) === 0) {
          this.notice.set('No hard cuts found — master is shown as-is');
        }
      },
      error: (err: Error) => {
        this.cutting.set(false);
        this.notice.set(err.message);
        this.split.get(id).subscribe({
          next: (p) => this.resultsProject.set(p),
          error: () => {},
        });
      },
    });
  }

  deleteProject(project: SplitProject, event: Event): void {
    event.stopPropagation();
    this.busyId.set(project.id);
    this.split.delete(project.id).subscribe({
      next: () => {
        this.busyId.set(null);
        this.unpinBoard(project.id);
        if (this.resultsProject()?.id === project.id) this.closeResults();
        if (this.detailsProject()?.id === project.id) this.closeDetails();
      },
      error: (err: Error) => {
        this.busyId.set(null);
        this.notice.set(err.message);
      },
    });
  }

  formatMs(ms: number | null | undefined): string {
    if (ms == null || !Number.isFinite(ms) || ms < 0) return '0:00';
    const totalSec = Math.floor(ms / 1000);
    const m = Math.floor(totalSec / 60);
    const s = totalSec % 60;
    const frac = Math.floor((ms % 1000) / 100);
    if (totalSec < 60 && frac > 0) {
      return `${m}:${s.toString().padStart(2, '0')}.${frac}`;
    }
    return `${m}:${s.toString().padStart(2, '0')}`;
  }

  formatRange(startMs: number, endMs: number): string {
    return `${this.formatMs(startMs)} – ${this.formatMs(endMs)}`;
  }

  isSplitDropHover(): boolean {
    return isSplitDrop(this.cardDrag.hoverTarget());
  }

  private hydrateBoards(list: SplitProject[]): void {
    const needing = list.filter((p) => p.clip_count > 0 && p.status === 'done');
    if (!needing.length) return;
    forkJoin(
      needing.map((p) =>
        this.split.get(p.id).pipe(catchError(() => of(null))),
      ),
    ).subscribe((loaded) => {
      for (const p of loaded) {
        if (p && (p.clips?.length ?? 0) > 0) this.pinBoard(p);
      }
    });
  }

  private pinBoard(project: SplitProject): void {
    this.boardsById.update((m) => {
      const next = new Map(m);
      next.set(project.id, project);
      return next;
    });
  }

  private unpinBoard(id: number): void {
    this.boardsById.update((m) => {
      if (!m.has(id)) return m;
      const next = new Map(m);
      next.delete(id);
      return next;
    });
  }
}
