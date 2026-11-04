import { Injectable, signal } from '@angular/core';
import { Save, SaveScope } from './dump.service';

/** Attribute on sidebar rows that accept dropped cards: a folder id, "dump", or "bin". */
export const DROP_TARGET_ATTR = 'data-drop-folder';

/** Attribute on calendar day cells: YYYY-MM-DD. */
export const DROP_CALENDAR_ATTR = 'data-drop-calendar';

/** Sidebar folder/bin/dump drop target. */
export type FolderDropTarget = SaveScope | 'bin';

/** Calendar day drop target (schedule, do not move). */
export type CalendarDropTarget = { type: 'calendar'; date: string };

/** Drop target for a dragged card. */
export type DropTarget = FolderDropTarget | CalendarDropTarget;

export function isCalendarDrop(target: DropTarget | null | undefined): target is CalendarDropTarget {
  return target != null && typeof target === 'object' && target.type === 'calendar';
}

/** Shared state while a card is dragged from a page onto the sidebar or a calendar day. */
@Injectable({ providedIn: 'root' })
export class CardDragService {
  readonly dragging = signal<Save | null>(null);
  /** Drop target under the pointer, if any. */
  readonly hoverTarget = signal<DropTarget | null>(null);

  /** Finds the drop target under a viewport point (calendar day preferred over folder). */
  targetAt(x: number, y: number): DropTarget | null {
    const stack = document.elementsFromPoint(x, y);
    for (const under of stack) {
      if (!(under instanceof Element)) continue;
      // Skip the floating drag preview so we hit the pane/sidebar underneath.
      if (under.closest('.cdk-drag-preview, .cdk-drag-placeholder')) continue;

      const dayEl = under.closest(`[${DROP_CALENDAR_ATTR}]`);
      const date = dayEl?.getAttribute(DROP_CALENDAR_ATTR);
      if (date && /^\d{4}-\d{2}-\d{2}$/.test(date)) {
        return { type: 'calendar', date };
      }

      const el = under.closest(`[${DROP_TARGET_ATTR}]`);
      const value = el?.getAttribute(DROP_TARGET_ATTR);
      if (!value) continue;
      if (value === 'dump') return 'dump';
      if (value === 'bin') return 'bin';
      const id = Number(value);
      return Number.isFinite(id) ? id : null;
    }
    return null;
  }

  isHovered(scope: FolderDropTarget): boolean {
    const t = this.hoverTarget();
    return this.dragging() !== null && !isCalendarDrop(t) && t === scope;
  }

  isCalendarHovered(dateKey: string): boolean {
    const t = this.hoverTarget();
    return this.dragging() !== null && isCalendarDrop(t) && t.date === dateKey;
  }
}
