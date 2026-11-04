import { ChangeDetectionStrategy, Component, computed, inject, signal } from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CalendarScheduleService } from '../../services/calendar-schedule.service';
import { CardDragService } from '../../services/card-drag.service';
import { DumpService, Save, SaveMedia } from '../../services/dump.service';

interface CalendarCell {
  key: string;
  day: number;
  weekday: string;
  isToday: boolean;
}

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;
const MAX_THUMBS = 3;

@Component({
  selector: 'app-calendar-page',
  standalone: true,
  imports: [FormsModule],
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
                class="group relative flex min-h-[140px] flex-col rounded-xl border border-[#ebebeb] p-2.5 text-left transition-colors hover:bg-[#fafafa] dark:border-slate-800 dark:hover:bg-white/5 sm:min-h-[160px]"
                [class.bg-blue-50]="selectedKey() === cell.key"
                [class.dark:bg-blue-500/10]="selectedKey() === cell.key"
                [class.ring-2]="cell.isToday || selectedKey() === cell.key || cardDrag.isCalendarHovered(cell.key)"
                [class.ring-blue-500]="cell.isToday || selectedKey() === cell.key || cardDrag.isCalendarHovered(cell.key)">
                <div class="flex items-baseline gap-1.5">
                  <span
                    class="inline-flex h-7 min-w-[1.75rem] items-center justify-center rounded-full px-1.5 text-sm font-semibold"
                    [class.text-ink]="!cell.isToday"
                    [class.dark:text-slate-100]="!cell.isToday"
                    [class.bg-blue-600]="cell.isToday"
                    [class.text-white]="cell.isToday">
                    {{ cell.day }}
                  </span>
                  <span class="text-xs font-medium uppercase tracking-wide text-ink-secondary dark:text-slate-500">
                    {{ cell.weekday }}
                  </span>
                </div>
                <div class="mt-2 flex min-h-0 flex-1 items-end gap-1.5">
                  @for (save of savesOn(cell.key); track save.id; let i = $index) {
                    @if (i < maxThumbs) {
                      <div class="w-11 shrink-0 overflow-hidden rounded-lg bg-black shadow-sm ring-1 ring-blue-400/70 aspect-[9/16] dark:ring-blue-400/50">
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
                            <span class="material-icons-outlined !text-lg text-slate-400">movie</span>
                          </div>
                        }
                      </div>
                    }
                  }
                  @if (savesOn(cell.key).length > maxThumbs) {
                    <span class="mb-1 text-xs font-semibold text-blue-600 dark:text-blue-300">
                      +{{ savesOn(cell.key).length - maxThumbs }}
                    </span>
                  }
                </div>
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
                <div class="flex gap-2 overflow-x-auto border-b border-[#ebebeb] px-4 py-3 dark:border-slate-800">
                  @for (save of daySaves; track save.id) {
                    <div class="w-14 shrink-0 overflow-hidden rounded-xl bg-black ring-1 ring-blue-400/70 aspect-[9/16]">
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
    });
  }

  return cells;
}
