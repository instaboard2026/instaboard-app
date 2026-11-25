import { Injectable, signal } from '@angular/core';
import { DROP_TARGET_ATTR } from './card-drag.service';
import { MoveTarget } from './dump.service';

/** Camera files being dragged onto Home or a sidebar folder (copied in as cards). */
export type CameraCopyDrag = { kind: 'camera'; paths: string[] };

/** Shared state while camera tiles are dragged onto Home or a folder. */
@Injectable({ providedIn: 'root' })
export class CameraDragService {
  readonly dragging = signal<CameraCopyDrag | null>(null);
  /** Home or folder under the pointer. */
  readonly hoverTarget = signal<MoveTarget | null>(null);

  /** Home or folder under the pointer (not Bin). */
  targetAt(x: number, y: number): MoveTarget | null {
    const stack = document.elementsFromPoint(x, y);
    for (const under of stack) {
      if (!(under instanceof Element)) continue;
      if (under.closest('.cdk-drag-preview, .cdk-drag-placeholder, .camera-drag-preview')) continue;
      const value = under.closest(`[${DROP_TARGET_ATTR}]`)?.getAttribute(DROP_TARGET_ATTR);
      if (!value) continue;
      if (value === 'bin') return null;
      if (value === 'dump') return 'dump';
      const id = Number(value);
      return Number.isFinite(id) ? id : null;
    }
    return null;
  }

  isHovered(target: MoveTarget): boolean {
    return this.dragging() !== null && this.hoverTarget() === target;
  }

  clear(): void {
    this.dragging.set(null);
    this.hoverTarget.set(null);
  }
}
