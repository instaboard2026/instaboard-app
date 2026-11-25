import { Injectable, signal } from '@angular/core';
import { DROP_TARGET_ATTR } from './card-drag.service';
import { MoveTarget } from './dump.service';

/** Camera files being dragged onto Home or a folder (copied in) or the Bin (moved to the Mac's Trash). */
export type CameraCopyDrag = { kind: 'camera'; paths: string[] };

/** Where a camera drag can land: Home/folder (copy), Bin (Trash) or the AirDrop tray (queue). */
export type CameraDropTarget = MoveTarget | 'bin' | 'airdrop';

/** Attribute on the sidebar AirDrop tray row. */
export const DROP_AIRDROP_ATTR = 'data-drop-airdrop';

/** Shared state while camera tiles are dragged onto Home or a folder. */
@Injectable({ providedIn: 'root' })
export class CameraDragService {
  readonly dragging = signal<CameraCopyDrag | null>(null);
  /** Home, folder or Bin under the pointer. */
  readonly hoverTarget = signal<CameraDropTarget | null>(null);

  /** Home, folder or Bin under the pointer. */
  targetAt(x: number, y: number): CameraDropTarget | null {
    const stack = document.elementsFromPoint(x, y);
    for (const under of stack) {
      if (!(under instanceof Element)) continue;
      if (under.closest('.cdk-drag-preview, .cdk-drag-placeholder, .camera-drag-preview')) continue;
      if (under.closest(`[${DROP_AIRDROP_ATTR}]`)) return 'airdrop';
      const value = under.closest(`[${DROP_TARGET_ATTR}]`)?.getAttribute(DROP_TARGET_ATTR);
      if (!value) continue;
      if (value === 'bin') return 'bin';
      if (value === 'dump') return 'dump';
      const id = Number(value);
      return Number.isFinite(id) ? id : null;
    }
    return null;
  }

  isHovered(target: CameraDropTarget): boolean {
    return this.dragging() !== null && this.hoverTarget() === target;
  }

  clear(): void {
    this.dragging.set(null);
    this.hoverTarget.set(null);
  }
}
