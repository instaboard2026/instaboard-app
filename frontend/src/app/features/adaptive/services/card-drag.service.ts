import { Injectable, signal } from '@angular/core';
import { Save, SaveScope } from './dump.service';

/** Attribute on sidebar rows that accept dropped cards: a folder id, or "dump". */
export const DROP_TARGET_ATTR = 'data-drop-folder';

/** Shared state while a card is dragged from a page onto the sidebar (Dump or a folder). */
@Injectable({ providedIn: 'root' })
export class CardDragService {
  readonly dragging = signal<Save | null>(null);
  /** Drop target under the pointer, if any. */
  readonly hoverTarget = signal<SaveScope | null>(null);

  /** Finds the drop target under a viewport point. */
  targetAt(x: number, y: number): SaveScope | null {
    const el = document.elementFromPoint(x, y)?.closest(`[${DROP_TARGET_ATTR}]`);
    const value = el?.getAttribute(DROP_TARGET_ATTR);
    if (!value) return null;
    if (value === 'dump') return 'dump';
    const id = Number(value);
    return Number.isFinite(id) ? id : null;
  }

  isHovered(scope: SaveScope): boolean {
    return this.dragging() !== null && this.hoverTarget() === scope;
  }
}
