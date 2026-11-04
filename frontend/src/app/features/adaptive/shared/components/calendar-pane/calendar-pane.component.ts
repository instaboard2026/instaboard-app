import {
  ChangeDetectionStrategy,
  Component,
  HostListener,
  computed,
  inject,
  input,
  output,
  signal,
} from '@angular/core';
import { FormsModule } from '@angular/forms';
import { CardDragService, isCalendarDrop } from '../../../services/card-drag.service';
import { CalendarScheduleService } from '../../../services/calendar-schedule.service';
import { DumpService, Save, SaveMedia } from '../../../services/dump.service';

interface CalendarCell {
  key: string;
  day: number;
  weekday: string;
  isToday: boolean;
}

const WEEKDAYS = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'] as const;
/** Max mini cards shown per day tile before a +N overflow. */
const MAX_THUMBS = 4;

@Component({
  selector: 'app-calendar-pane',
  standalone: true,
  imports: [FormsModule],
  changeDetection: ChangeDetectionStrategy.OnPush,
  templateUrl: './calendar-pane.component.html',
})
export class CalendarPaneComponent {
  readonly isOpen = input(true);
  readonly isInitializing = input(false);
  readonly togglePane = output<void>();

  readonly cardDrag = inject(CardDragService);
  readonly schedule = inject(CalendarScheduleService);
  private dumpService = inject(DumpService);

  private readonly cursor = signal(startOfMonth(new Date()));
  /** Day open in the popup (null = popup closed; tiles stay as-is). */
  readonly selectedKey = signal<string | null>(null);
  readonly maxThumbs = MAX_THUMBS;

  /** Day under the pointer while a card is dragged (keeps OnPush pane in sync with CardDragService). */
  readonly hoverDate = computed(() => {
    const t = this.cardDrag.hoverTarget();
    return this.cardDrag.dragging() && isCalendarDrop(t) ? t.date : null;
  });

  readonly monthLabel = computed(() =>
    this.cursor().toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
  );

  readonly cells = computed(() => buildMonthDays(this.cursor()));

  /** dateKey → scheduled saves (with media when loaded). */
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
    this.closeDayPopup();
  }

  goToday(): void {
    const now = new Date();
    this.cursor.set(startOfMonth(now));
    this.selectedKey.set(dateKey(now));
  }

  /** Open the day popup (bigger reels + notes). Compact tiles stay unchanged underneath. */
  openDay(cell: CalendarCell): void {
    if (this.cardDrag.dragging()) return;
    this.selectedKey.set(cell.key);
  }

  closeDayPopup(): void {
    this.selectedKey.set(null);
  }

  unschedule(saveId: number, event: Event): void {
    event.stopPropagation();
    this.schedule.unschedule(saveId);
  }

  onToggle(): void {
    this.togglePane.emit();
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.selectedKey()) this.closeDayPopup();
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
