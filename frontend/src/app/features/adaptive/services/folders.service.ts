import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { DumpService, MoveTarget, Save, SaveScope } from './dump.service';

/** One folder from GET /api/folders. */
export interface Folder {
  id: number;
  name: string;
  position: number;
  count: number;
  /** Sidebar section the folder sits in (null = ungrouped, listed after all sections). */
  section_id: number | null;
}

/** A named group of folders in the sidebar. */
export interface FolderSection {
  id: number;
  name: string;
  position: number;
}

/** One block of the sidebar: ungrouped folders (section null) or a section with its folders. */
export interface FolderGroup {
  section: FolderSection | null;
  folders: Folder[];
}

interface SidebarData {
  sections: FolderSection[];
  folders: Folder[];
}

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

const NOTICE_MS = 4000;

/**
 * The user's folders (sidebar) and moving saves between folders and the Dump. Reorders and moves
 * update the UI first and roll back with a short notice if the server rejects them.
 */
@Injectable({ providedIn: 'root' })
export class FoldersService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private dump = inject(DumpService);

  readonly folders = signal<Folder[]>([]);
  readonly sections = signal<FolderSection[]>([]);
  /** Section whose name is being edited inline (set right after creating one). */
  readonly renamingSectionId = signal<number | null>(null);

  /** Sidebar blocks in order: each section with its folders, then ungrouped folders last. */
  readonly groups = computed<FolderGroup[]>(() => {
    const sections = this.sections();
    const known = new Set(sections.map((s) => s.id));
    const folders = this.folders();
    const inSection = (id: number | null) =>
      folders.filter((f) => (id === null ? f.section_id == null || !known.has(f.section_id) : f.section_id === id));
    return [...sections.map((s) => ({ section: s, folders: inSection(s.id) })), { section: null, folders: inSection(null) }];
  });
  readonly loaded = signal(false);
  /** Folder whose name is being edited inline in the sidebar (set right after creating one). */
  readonly renamingId = signal<number | null>(null);
  /** Short error shown in the sidebar (e.g. a failed move); clears itself. */
  readonly notice = signal<string | null>(null);

  readonly byId = computed(() => new Map(this.folders().map((f) => [f.id, f])));

  private noticeTimer: ReturnType<typeof setTimeout> | null = null;

  load(): void {
    this.request<SidebarData>('GET', '/api/sidebar').subscribe({
      next: (data) => {
        this.applySidebar(data);
        this.loaded.set(true);
      },
      error: () => {},
    });
  }

  private applySidebar(data: SidebarData | null | undefined): void {
    if (!data) return;
    this.sections.set(data.sections ?? []);
    this.folders.set(data.folders ?? []);
  }

  clear(): void {
    this.folders.set([]);
    this.sections.set([]);
    this.loaded.set(false);
    this.renamingId.set(null);
    this.renamingSectionId.set(null);
  }

  /** Creates the next "Folder N" at the bottom (of a section, if given) and puts it into rename mode. */
  create(sectionId: number | null = null): Observable<Folder> {
    return this.request<Folder>('POST', '/api/folders', sectionId ? { section_id: sectionId } : {}).pipe(
      tap((folder) => {
        this.folders.update((list) => [...list, { ...folder, count: 0, section_id: folder.section_id ?? sectionId }]);
        this.renamingId.set(folder.id);
      }),
      catchError((err: Error) => {
        this.showNotice(err.message);
        return throwError(() => err);
      })
    );
  }

  rename(id: number, name: string): Observable<Folder> {
    return this.request<Folder>('PATCH', `/api/folders/${id}`, { name }).pipe(
      tap((updated) => this.folders.update((list) => list.map((f) => (f.id === id ? { ...f, name: updated.name } : f))))
    );
  }

  remove(id: number): Observable<number> {
    return this.request<{ deleted_saves: number }>('DELETE', `/api/folders/${id}`).pipe(
      map((res) => res.deleted_saves),
      tap(() => {
        this.folders.update((list) => list.filter((f) => f.id !== id));
        this.dump.dropScope(id);
      }),
      catchError((err: Error) => {
        this.showNotice(err.message);
        return throwError(() => err);
      })
    );
  }

  /** Creates the next "Section N" at the bottom and puts it into rename mode. */
  createSection(): Observable<FolderSection> {
    return this.request<FolderSection>('POST', '/api/folder-sections', {}).pipe(
      tap((section) => {
        this.sections.update((list) => [...list, section]);
        this.renamingSectionId.set(section.id);
      }),
      catchError((err: Error) => {
        this.showNotice(err.message);
        return throwError(() => err);
      }),
    );
  }

  renameSection(id: number, name: string): Observable<FolderSection> {
    return this.request<FolderSection>('PATCH', `/api/folder-sections/${id}`, { name }).pipe(
      tap((updated) => this.sections.update((list) => list.map((s) => (s.id === id ? { ...s, name: updated.name } : s)))),
    );
  }

  /** Deletes a section; its folders stay and become ungrouped. */
  removeSection(id: number): Observable<void> {
    return this.request<SidebarData>('DELETE', `/api/folder-sections/${id}`).pipe(
      tap((data) => this.applySidebar(data)),
      map(() => undefined),
      catchError((err: Error) => {
        this.showNotice(err.message);
        return throwError(() => err);
      }),
    );
  }

  /**
   * Puts a folder into a section (null = ungrouped) at an index within that group (optimistic).
   * Used by sidebar drag-and-drop and the Move up / Move down menu.
   */
  placeFolder(folderId: number, sectionId: number | null, index: number): void {
    const groups = this.groups().map((g) => ({ section: g.section, folders: g.folders.filter((f) => f.id !== folderId) }));
    const folder = this.byId().get(folderId);
    const dest = groups.find((g) => (g.section?.id ?? null) === sectionId);
    if (!folder || !dest) return;
    const at = Math.max(0, Math.min(index, dest.folders.length));
    dest.folders.splice(at, 0, { ...folder, section_id: sectionId });
    this.saveLayout(this.sections(), groups.flatMap((g) => g.folders));
  }

  /** Moves a folder one step up or down within its group. */
  nudgeFolder(folderId: number, delta: number): void {
    const group = this.groups().find((g) => g.folders.some((f) => f.id === folderId));
    if (!group) return;
    const index = group.folders.findIndex((f) => f.id === folderId);
    const to = index + delta;
    if (to < 0 || to >= group.folders.length) return;
    this.placeFolder(folderId, group.section?.id ?? null, to);
  }

  /** Moves a section one step up or down. */
  nudgeSection(sectionId: number, delta: number): void {
    const list = [...this.sections()];
    const index = list.findIndex((s) => s.id === sectionId);
    const to = index + delta;
    if (index < 0 || to < 0 || to >= list.length) return;
    const [moved] = list.splice(index, 1);
    list.splice(to, 0, moved);
    const order = new Map(list.map((s, i) => [s.id, i]));
    // Folders follow their sections' new order; ungrouped stay last.
    const rank = (g: FolderGroup) => (g.section ? order.get(g.section.id)! : list.length);
    const folders = this.groups()
      .slice()
      .sort((a, b) => rank(a) - rank(b))
      .flatMap((g) => g.folders);
    this.saveLayout(list, folders);
  }

  private saveLayout(sections: FolderSection[], folders: Folder[]): void {
    const beforeSections = this.sections();
    const beforeFolders = this.folders();
    this.sections.set(sections.map((s, i) => ({ ...s, position: i + 1 })));
    this.folders.set(folders.map((f, i) => ({ ...f, position: i + 1 })));
    this.request<SidebarData>('PUT', '/api/sidebar/layout', {
      sections: sections.map((s) => s.id),
      folders: folders.map((f) => ({ id: f.id, section_id: f.section_id ?? null })),
    }).subscribe({
      error: (err: Error) => {
        this.sections.set(beforeSections);
        this.folders.set(beforeFolders);
        this.showNotice(`Couldn't save the folder order. ${err.message}`);
      },
    });
  }

  /**
   * Moves a save into a folder or back to the Dump (optimistic). The card leaves its current list
   * immediately and counts update; everything is restored if the server rejects the move.
   */
  moveSave(save: Save, to: MoveTarget): void {
    const toFolderId = to === 'dump' ? null : to;
    if (save.folder_id === toFolderId && !save.deleted_at) return;

    const from = this.dump.takeSave(save.id) ?? (save.folder_id ?? 'dump');
    const moved: Save = { ...save, folder_id: toFolderId, deleted_at: null };
    this.dump.putSave(moved, to);
    this.adjustCount(from, -1);
    this.adjustCount(to, +1);

    this.request<Save>('PATCH', `/api/saves/${save.id}`, { folder_id: toFolderId }).subscribe({
      next: (updated) => this.dump.replaceSave(updated),
      error: (err: Error) => {
        this.dump.takeSave(save.id);
        this.dump.putSave(save, from);
        this.adjustCount(to, -1);
        this.adjustCount(from, +1);
        this.showNotice(`Couldn't move that item. ${err.message}`);
      },
    });
  }

  /**
   * Moves a save into the Bin (soft-delete, optimistic). Downloaded files stay until the Bin is emptied.
   */
  deleteSave(save: Save): void {
    if (save.deleted_at) return;
    const from = this.dump.takeSave(save.id) ?? (save.folder_id ?? 'dump');
    this.adjustCount(from, -1);

    this.request<Save>('DELETE', `/api/saves/${save.id}`).subscribe({
      next: (updated) => this.dump.putSave(updated, 'bin'),
      error: (err: Error) => {
        this.dump.putSave(save, from);
        this.adjustCount(from, +1);
        this.showNotice(`Couldn't move that item to the Bin. ${err.message}`);
      },
    });
  }

  /**
   * Restores a binned save into the Dump or a folder (optimistic).
   */
  restoreSave(save: Save, to: MoveTarget): void {
    const toFolderId = to === 'dump' ? null : to;
    this.dump.takeSave(save.id);
    const restored: Save = { ...save, folder_id: toFolderId, deleted_at: null };
    this.dump.putSave(restored, to);
    this.adjustCount(to, +1);

    this.request<Save>('POST', `/api/saves/${save.id}/restore`, { folder_id: toFolderId }).subscribe({
      next: (updated) => this.dump.replaceSave(updated),
      error: (err: Error) => {
        this.dump.takeSave(save.id);
        this.dump.putSave(save, 'bin');
        this.adjustCount(to, -1);
        this.showNotice(`Couldn't restore that item. ${err.message}`);
      },
    });
  }

  /**
   * Permanently deletes every save in the Bin and their downloaded files (optimistic).
   */
  clearBin(): void {
    const before = this.dump.listFor('bin');
    this.dump.replaceList('bin', []);

    this.request<{ deleted: number }>('DELETE', '/api/bin').subscribe({
      error: (err: Error) => {
        this.dump.replaceList('bin', before);
        this.showNotice(`Couldn't empty the Bin. ${err.message}`);
      },
    });
  }

  /** Bumps a Home/folder badge count after cards were added (e.g. copied from the camera). */
  noteAddedSaves(scope: MoveTarget, count: number): void {
    if (count) this.adjustCount(scope, count);
  }

  /** Lowers a Home/folder/bin badge after a save was removed elsewhere. */
  noteRemovedSaves(scope: SaveScope, count: number): void {
    if (count) this.adjustCount(scope, -count);
  }

  showNotice(message: string): void {
    this.notice.set(message);
    if (this.noticeTimer) clearTimeout(this.noticeTimer);
    this.noticeTimer = setTimeout(() => this.notice.set(null), NOTICE_MS);
  }

  private adjustCount(scope: SaveScope, delta: number): void {
    if (scope === 'dump' || scope === 'bin') return; // badges are the length of those lists
    this.folders.update((list) => list.map((f) => (f.id === scope ? { ...f, count: Math.max(0, f.count + delta) } : f)));
  }

  private request<T>(method: string, path: string, body?: unknown): Observable<T> {
    const token = this.authToken.getToken();
    if (!token) {
      return method === 'GET' ? of([] as unknown as T) : throwError(() => new Error('Please sign in.'));
    }
    const headers = new HttpHeaders({ Authorization: `Bearer ${token}` });
    return this.http.request<ApiWrapper<T>>(method, `${environment.apiUrl}${path}`, { headers, body }).pipe(
      map((res) => {
        if (res?.status !== 'SUCCESS') {
          throw new Error(res?.message || 'Something went wrong.');
        }
        return res.data as T;
      }),
      catchError((err: unknown) => {
        const message =
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Something went wrong. Is the backend running?'
            : (err as Error)?.message || 'Something went wrong.';
        return throwError(() => new Error(message.charAt(0).toUpperCase() + message.slice(1)));
      })
    );
  }
}
