import { Injectable, signal } from '@angular/core';
import { DROP_TARGET_ATTR, FolderDropTarget } from './card-drag.service';

/** Attribute on Local media folder rows / pages that accept camera or Local media drops. */
export const DROP_LOCAL_MEDIA_ATTR = 'data-drop-local-media';

/** Camera files to copy into a Local media folder. */
export type CameraCopyDrag = { kind: 'camera'; paths: string[] };

/** Local media items to move into another Local media folder or Dump/Home folder. */
export type LocalMediaMoveDrag = { kind: 'local-move'; fromFolderId: number; ids: number[] };

export type LocalMediaDropDrag = CameraCopyDrag | LocalMediaMoveDrag;

/** Drop target while dragging Local media: LM folder id, Home (`dump`), or a Dump folder id. */
export type LocalMediaDropTarget =
  | { type: 'local-media'; folderId: number }
  | { type: 'dump'; scope: Exclude<FolderDropTarget, 'bin'> };

/** Shared state while camera tiles or Local media tiles are dragged onto folders. */
@Injectable({ providedIn: 'root' })
export class CameraDragService {
  readonly dragging = signal<LocalMediaDropDrag | null>(null);
  /** Local media folder id under the pointer (camera copy / LM↔LM move). */
  readonly hoverFolderId = signal<number | null>(null);
  /** Home or Dump folder under the pointer during a Local media move. */
  readonly hoverDumpTarget = signal<Exclude<FolderDropTarget, 'bin'> | null>(null);

  targetFolderAt(x: number, y: number): number | null {
    const stack = document.elementsFromPoint(x, y);
    for (const under of stack) {
      if (!(under instanceof Element)) continue;
      if (under.closest('.cdk-drag-preview, .cdk-drag-placeholder, .camera-drag-preview')) continue;
      const el = under.closest(`[${DROP_LOCAL_MEDIA_ATTR}]`);
      const value = el?.getAttribute(DROP_LOCAL_MEDIA_ATTR);
      if (!value) continue;
      const id = Number(value);
      return Number.isFinite(id) ? id : null;
    }
    return null;
  }

  /** Home / Dump folder under the pointer (not Bin). */
  dumpTargetAt(x: number, y: number): Exclude<FolderDropTarget, 'bin'> | null {
    const stack = document.elementsFromPoint(x, y);
    for (const under of stack) {
      if (!(under instanceof Element)) continue;
      if (under.closest('.cdk-drag-preview, .cdk-drag-placeholder, .camera-drag-preview')) continue;
      const el = under.closest(`[${DROP_TARGET_ATTR}]`);
      const value = el?.getAttribute(DROP_TARGET_ATTR);
      if (!value) continue;
      if (value === 'bin') return null;
      if (value === 'dump') return 'dump';
      const id = Number(value);
      return Number.isFinite(id) ? id : null;
    }
    return null;
  }

  /** Prefer Local media folder, else Home/Dump folder (for local-move drops). */
  dropTargetAt(x: number, y: number): LocalMediaDropTarget | null {
    const lm = this.targetFolderAt(x, y);
    if (lm != null) return { type: 'local-media', folderId: lm };
    const dump = this.dumpTargetAt(x, y);
    if (dump != null) return { type: 'dump', scope: dump };
    return null;
  }

  isHovered(folderId: number): boolean {
    const drag = this.dragging();
    if (!drag || this.hoverFolderId() !== folderId) return false;
    if (drag.kind === 'local-move' && drag.fromFolderId === folderId) return false;
    return true;
  }

  isDumpHovered(scope: Exclude<FolderDropTarget, 'bin'>): boolean {
    return this.dragging()?.kind === 'local-move' && this.hoverDumpTarget() === scope;
  }

  clear(): void {
    this.dragging.set(null);
    this.hoverFolderId.set(null);
    this.hoverDumpTarget.set(null);
  }
}
