import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { NgTemplateOutlet } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { CdkDrag, CdkDragEnd, CdkDragMove, CdkDragPlaceholder, CdkDragPreview, CdkDropList } from '@angular/cdk/drag-drop';
import { CalendarScheduleService } from '../../services/calendar-schedule.service';
import { CardDragService, isCalendarDrop } from '../../services/card-drag.service';
import { DumpService, Save, SaveMedia } from '../../services/dump.service';

interface CalendarCell {
  key: string;
  day: number;
  weekday: string;
  isToday: boolean;
  isSunday: boolean;
}

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;
/** Portrait reels that fit a ~100px day cell without spilling into the next day. */
const MAX_THUMBS = 2;

@Component({
  selector: 'app-calendar-page',
  standalone: true,
  imports: [FormsModule, NgTemplateOutlet, CdkDropList, CdkDrag, CdkDragPreview, CdkDragPlaceholder],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="flex h-full min-h-0 flex-col px-4 py-4 text-ink dark:text-slate-100">
      <div class="flex shrink-0 items-center justify-between gap-3">
        <div class="flex items-baseline gap-3">
          <h1 class="text-xl font-semibold">Calendar</h1>
          <h2 class="text-sm font-medium text-ink-secondary dark:text-slate-400">{{ monthLabel() }}</h2>
        </div>
        <div class="flex items-center gap-1">
          <button
            type="button"
            (click)="shiftMonth(-1)"
            aria-label="Previous month"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-ink-secondary hover:bg-[#f1f1f1] dark:text-slate-400 dark:hover:bg-slate-800">
            <span class="material-icons-outlined !text-xl">chevron_left</span>
          </button>
          <button
            type="button"
            (click)="goToday()"
            class="h-8 rounded-lg px-2.5 text-sm font-medium text-ink-secondary hover:bg-[#f1f1f1] dark:text-slate-400 dark:hover:bg-slate-800">
            Today
          </button>
          <button
            type="button"
            (click)="shiftMonth(1)"
            aria-label="Next month"
            class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-ink-secondary hover:bg-[#f1f1f1] dark:text-slate-400 dark:hover:bg-slate-800">
            <span class="material-icons-outlined !text-xl">chevron_right</span>
          </button>
        </div>
      </div>

      <div class="mt-4 grid min-h-0 flex-1 grid-cols-1 gap-4 lg:grid-cols-2">
        <section class="min-h-0 overflow-y-auto rounded-2xl border border-[#ebebeb] bg-white dark:border-slate-800 dark:bg-card-bg">
          <div class="grid grid-cols-2 gap-2 p-2 sm:grid-cols-3 lg:grid-cols-4" role="grid" [attr.aria-label]="monthLabel()">
            @for (cell of cells(); track cell.key) {
              <button
                type="button"
                role="gridcell"
                [attr.data-drop-calendar]="cell.key"
                (click)="selectDay(cell)"
                class="group relative flex min-h-[140px] min-w-0 flex-col overflow-hidden rounded-xl border border-[#ebebeb] p-2.5 text-left transition-colors hover:bg-[#fafafa] dark:border-slate-800 dark:hover:bg-white/5 sm:min-h-[160px]"
                [class.bg-rose-50]="cell.isSunday && selectedKey() !== cell.key"
                [class.dark:bg-rose-500/10]="cell.isSunday && selectedKey() !== cell.key"
                [class.border-rose-100]="cell.isSunday && selectedKey() !== cell.key"
                [class.dark:border-rose-500/20]="cell.isSunday && selectedKey() !== cell.key"
                [class.hover:bg-rose-100]="cell.isSunday && selectedKey() !== cell.key"
                [class.bg-blue-50]="selectedKey() === cell.key"
                [class.dark:bg-blue-500/10]="selectedKey() === cell.key"
                [class.ring-2]="cell.isToday || selectedKey() === cell.key || cardDrag.isCalendarHovered(cell.key)"
                [class.ring-blue-500]="cell.isToday || selectedKey() === cell.key || cardDrag.isCalendarHovered(cell.key)">
                <div class="flex items-start justify-between gap-1">
                  <div class="min-w-0">
                    <div
                      class="text-sm font-semibold tabular-nums leading-none"
                      [class.text-ink]="!cell.isSunday"
                      [class.text-rose-600]="cell.isSunday"
                      [class.dark:text-slate-100]="!cell.isSunday"
                      [class.dark:text-rose-300]="cell.isSunday">
                      {{ cell.day }}
                    </div>
                    <div
                      class="mt-1 text-[10px] font-medium uppercase leading-none tracking-wide"
                      [class.text-ink-secondary]="!cell.isSunday"
                      [class.dark:text-slate-500]="!cell.isSunday"
                      [class.text-rose-500]="cell.isSunday"
                      [class.dark:text-rose-300]="cell.isSunday">
                      {{ cell.weekday }}
                    </div>
                  </div>
                  @if (savesOn(cell.key).length; as n) {
                    @if (n > 0) {
                      <span
                        class="inline-flex h-5 min-w-5 shrink-0 items-center justify-center rounded-full bg-blue-600 px-1 text-[10px] font-semibold tabular-nums text-white"
                        [attr.aria-label]="n + (n === 1 ? ' item scheduled' : ' items scheduled')">
                        {{ n }}
                      </span>
                    }
                  }
                </div>
                @if (savesOn(cell.key); as daySaves) {
                  @if (daySaves.length > 0) {
                    <div class="mt-auto flex w-full min-w-0 items-end gap-1 overflow-hidden pt-2">
                      @for (save of daySaves.slice(0, maxThumbs); track save.id) {
                        <div class="aspect-[9/16] min-w-0 max-w-11 flex-1 basis-0 overflow-hidden rounded-md bg-black shadow-sm ring-1 ring-blue-400/70 dark:ring-blue-400/50">
                          @if (thumb(save); as media) {
                            @if (media.type === 'video') {
                              <video
                                [src]="media.url"
                                muted
                                playsinline
                                preload="metadata"
                                draggable="false"
                                class="pointer-events-none h-full w-full object-cover"></video>
                            } @else {
                              <img [src]="media.url" alt="" draggable="false" class="pointer-events-none h-full w-full object-cover" />
                            }
                          } @else {
                            <div class="flex h-full items-center justify-center bg-[#f5f6f7] dark:bg-slate-800">
                              <span class="material-icons-outlined !text-base text-slate-400">movie</span>
                            </div>
                          }
                        </div>
                      }
                    </div>
                  }
                }
              </button>
            }
          </div>
        </section>

        <aside class="flex min-h-[280px] flex-col overflow-hidden rounded-2xl border border-[#ebebeb] bg-white dark:border-slate-800 dark:bg-card-bg lg:min-h-0">
          @if (selectedKey(); as key) {
            <div class="flex items-center justify-between gap-2 border-b border-[#ebebeb] px-4 py-3 dark:border-slate-800">
              <div>
                <p class="text-sm font-semibold">{{ selectedLabel() }}</p>
                <p class="text-xs text-ink-secondary dark:text-slate-500">Notes</p>
              </div>
              <button
                type="button"
                (click)="clearSelection()"
                aria-label="Close notes"
                class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-ink-secondary hover:bg-[#f1f1f1] dark:text-slate-400 dark:hover:bg-slate-800">
                <span class="material-icons-outlined !text-xl">close</span>
              </button>
            </div>
            @if (savesOn(key); as daySaves) {
              @if (daySaves.length > 0) {
                <div
                  cdkDropList
                  cdkDropListSortingDisabled
                  class="flex flex-wrap gap-2 border-b border-[#ebebeb] px-4 py-3 dark:border-slate-800">
                  @for (save of daySaves; track save.id) {
                    <div
                      cdkDrag
                      [cdkDragData]="save"
                      [cdkDragStartDelay]="{ touch: 250, mouse: 0 }"
                      (cdkDragStarted)="onReelDragStarted(save)"
                      (cdkDragMoved)="onReelDragMoved($event)"
                      (cdkDragEnded)="onReelDragEnded(save, $event)"
                      tabindex="0"
                      aria-label="Drag to another day"
                      class="relative w-14 shrink-0 cursor-grab active:cursor-grabbing">
                      <ng-container *ngTemplateOutlet="reelFace; context: { $implicit: save }" />
                      <ng-template cdkDragPreview>
                        <div class="w-14 shadow-2xl">
                          <ng-container *ngTemplateOutlet="reelFace; context: { $implicit: save }" />
                        </div>
                      </ng-template>
                      <button
                        type="button"
                        (pointerdown)="$event.stopPropagation()"
                        (mousedown)="$event.stopPropagation()"
                        (click)="unschedule(save.id, $event)"
                        title="Remove from this day"
                        aria-label="Remove from this day"
                        class="absolute -right-1.5 -top-1.5 z-10 inline-flex h-6 w-6 items-center justify-center rounded-full bg-nav-bg text-nav-muted ring-1 ring-white/20 hover:bg-red-600 hover:text-white">
                        <span class="material-icons-outlined !text-sm">close</span>
                      </button>
                      <div *cdkDragPlaceholder class="w-14 rounded-xl border-2 border-dashed border-blue-400/50 aspect-[9/16]"></div>
                    </div>
                  }
                </div>
              }
            }
            <textarea
              class="min-h-0 flex-1 resize-none border-0 bg-transparent px-4 py-3 text-sm text-ink outline-none placeholder:text-ink-placeholder dark:text-slate-100"
              [ngModel]="schedule.noteFor(key)"
              (ngModelChange)="schedule.setNote(key, $event)"
              placeholder="Write notes for this day…"
              aria-label="Day notes"></textarea>
          } @else {
            <div class="flex flex-1 flex-col items-center justify-center gap-2 px-6 text-center">
              <span class="material-icons-outlined !text-4xl text-slate-300 dark:text-slate-600">edit_note</span>
              <p class="text-sm font-medium text-ink-secondary dark:text-slate-400">Select a day</p>
              <p class="text-xs text-ink-secondary dark:text-slate-500">Click a date to open notes on this side.</p>
            </div>
          }
        </aside>
      </div>
    </div>

    <ng-template #reelFace let-save>
      <div class="overflow-hidden rounded-xl bg-black ring-1 ring-blue-400/70 aspect-[9/16]">
        @if (thumb(save); as media) {
          @if (media.type === 'video') {
            <video
              [src]="media.url"
              muted
              playsinline
              preload="metadata"
              draggable="false"
              class="pointer-events-none h-full w-full object-cover"></video>
          } @else {
            <img [src]="media.url" alt="" draggable="false" class="pointer-events-none h-full w-full object-cover" />
          }
        } @else {
          <div class="flex h-full items-center justify-center bg-[#f5f6f7] dark:bg-slate-800">
            <span class="material-icons-outlined !text-xl text-slate-400">movie</span>
          </div>
        }
      </div>
    </ng-template>
  `,
})
export class CalendarPageComponent {
  readonly schedule = inject(CalendarScheduleService);
  readonly cardDrag = inject(CardDragService);
  private dumpService = inject(DumpService);

  private readonly cursor = signal(startOfMonth(new Date()));
  readonly selectedKey = signal<string | null>(null);
  readonly maxThumbs = MAX_THUMBS;

  readonly monthLabel = computed(() =>
    this.cursor().toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
  );

  readonly cells = computed(() => buildMonthDays(this.cursor()));

  readonly savesByDate = computed(() => {
    const byId = this.dumpService.byId();
    const map = new Map<string, Save[]>();
    for (const [id, date] of this.schedule.bySave()) {
      const save = byId.get(id);
      if (!save) continue;
      const list = map.get(date) ?? [];
      list.push(save);
      map.set(date, list);
    }
    return map;
  });

  readonly selectedLabel = computed(() => {
    const key = this.selectedKey();
    if (!key) return '';
    const [y, m, d] = key.split('-').map(Number);
    return new Date(y, m - 1, d).toLocaleDateString(undefined, {
      weekday: 'long',
      month: 'long',
      day: 'numeric',
      year: 'numeric',
    });
  });

  savesOn(dateKey: string): Save[] {
    return this.savesByDate().get(dateKey) ?? [];
  }

  thumb(save: Save): SaveMedia | null {
    return save.download?.media?.[0] ?? null;
  }

  shiftMonth(delta: number): void {
    const d = this.cursor();
    this.cursor.set(new Date(d.getFullYear(), d.getMonth() + delta, 1));
    this.selectedKey.set(null);
  }

  goToday(): void {
    const now = new Date();
    this.cursor.set(startOfMonth(now));
    this.selectedKey.set(dateKey(now));
  }

  selectDay(cell: CalendarCell): void {
    this.selectedKey.set(cell.key);
  }

  clearSelection(): void {
    this.selectedKey.set(null);
  }

  unschedule(saveId: number, event: Event): void {
    event.stopPropagation();
    this.schedule.unschedule(saveId);
  }

  onReelDragStarted(save: Save): void {
    this.cardDrag.dragging.set(save);
  }

  onReelDragMoved(event: CdkDragMove<Save>): void {
    const target = this.cardDrag.targetAt(event.pointerPosition.x, event.pointerPosition.y);
    this.cardDrag.hoverTarget.set(isCalendarDrop(target) ? target : null);
  }

  /** Dropping on a different day moves the reel. Same day, or anywhere else, leaves it. */
  onReelDragEnded(save: Save, event: CdkDragEnd<Save>): void {
    const target =
      this.cardDrag.targetAt(event.dropPoint.x, event.dropPoint.y) ?? this.cardDrag.hoverTarget();
    this.cardDrag.dragging.set(null);
    this.cardDrag.hoverTarget.set(null);
    if (!isCalendarDrop(target) || target.date === this.schedule.dateFor(save.id)) return;
    this.schedule.schedule(save.id, target.date);
  }
}

function startOfMonth(d: Date): Date {
  return new Date(d.getFullYear(), d.getMonth(), 1);
}

function dateKey(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

/** In-month days only, for a compact 4-per-row tile grid. */
function buildMonthDays(monthStart: Date): CalendarCell[] {
  const year = monthStart.getFullYear();
  const month = monthStart.getMonth();
  const todayKey = dateKey(new Date());
  const daysInMonth = new Date(year, month + 1, 0).getDate();
  const cells: CalendarCell[] = [];

  for (let day = 1; day <= daysInMonth; day++) {
    const d = new Date(year, month, day);
    const key = dateKey(d);
    cells.push({
      key,
      day,
      weekday: WEEKDAYS[d.getDay()],
      isToday: key === todayKey,
      isSunday: d.getDay() === 0,
    });
  }

  return cells;
}
