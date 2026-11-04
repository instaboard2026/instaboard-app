import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { DumpService, Save, SaveScope } from './dump.service';

/** One folder from GET /api/folders. */
export interface Folder {
  id: number;
  name: string;
  position: number;
  count: number;
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
  readonly loaded = signal(false);
  /** Folder whose name is being edited inline in the sidebar (set right after creating one). */
  readonly renamingId = signal<number | null>(null);
  /** Short error shown in the sidebar (e.g. a failed move); clears itself. */
  readonly notice = signal<string | null>(null);

  readonly byId = computed(() => new Map(this.folders().map((f) => [f.id, f])));

  private noticeTimer: ReturnType<typeof setTimeout> | null = null;

  load(): void {
    this.request<Folder[]>('GET', '/api/folders').subscribe({
      next: (folders) => {
        this.folders.set(folders);
        this.loaded.set(true);
      },
      error: () => {},
    });
  }

  clear(): void {
    this.folders.set([]);
    this.loaded.set(false);
    this.renamingId.set(null);
  }

  /** Creates the next "Folder N" at the bottom and puts it into rename mode. */
  create(): Observable<Folder> {
    return this.request<Folder>('POST', '/api/folders', {}).pipe(
      tap((folder) => {
        this.folders.update((list) => [...list, { ...folder, count: 0 }]);
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

  /** Moves a folder from one sidebar position to another (optimistic). */
  reorder(fromIndex: number, toIndex: number): void {
    const before = this.folders();
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || toIndex >= before.length) return;
    const next = [...before];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    this.folders.set(next);
    this.request<Folder[]>('PUT', '/api/folders/order', { ids: next.map((f) => f.id) }).subscribe({
      error: (err: Error) => {
        this.folders.set(before);
        this.showNotice(`Couldn't reorder folders. ${err.message}`);
      },
    });
  }

  /**
   * Moves a save into a folder or back to the Dump (optimistic). The card leaves its current list
   * immediately and counts update; everything is restored if the server rejects the move.
   */
  moveSave(save: Save, to: SaveScope): void {
    const toFolderId = to === 'dump' ? null : to;
    if (save.folder_id === toFolderId) return;

    const from = this.dump.takeSave(save.id) ?? (save.folder_id ?? 'dump');
    const moved: Save = { ...save, folder_id: toFolderId };
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

  showNotice(message: string): void {
    this.notice.set(message);
    if (this.noticeTimer) clearTimeout(this.noticeTimer);
    this.noticeTimer = setTimeout(() => this.notice.set(null), NOTICE_MS);
  }

  private adjustCount(scope: SaveScope, delta: number): void {
    if (scope === 'dump') return; // the Dump badge is the length of the Dump list
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
