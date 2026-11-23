import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, switchMap, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { CameraService } from './camera.service';

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

export interface LocalMediaFolder {
  id: number;
  name: string;
  position: number;
  count: number;
}

export interface LocalMediaItem {
  id: number;
  folder_id: number;
  original_name: string;
  stored_name: string;
  kind: 'image' | 'video';
  byte_size: number;
  source_rel_path?: string | null;
  source_root?: string | null;
  created_at: string;
  url: string;
  thumb_url: string;
  type: 'image' | 'video';
}

export interface LocalMediaCopyJob {
  id: string;
  folder_id: number;
  status: 'pending' | 'running' | 'done' | 'error';
  total: number;
  done: number;
  copied: number;
  skipped: number;
  failed: number;
  bytes_done: number;
  copied_names: string[];
  skipped_paths: string[];
  errors: { rel_path: string; error: string }[];
  message?: string;
}

const NOTICE_MS = 4000;

/** Local media folders + items under the download dir (SQLite-backed). */
@Injectable({ providedIn: 'root' })
export class LocalMediaService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private camera = inject(CameraService);

  readonly folders = signal<LocalMediaFolder[]>([]);
  readonly loaded = signal(false);
  readonly renamingId = signal<number | null>(null);
  readonly notice = signal<string | null>(null);
  readonly itemsByFolder = signal<Map<number, LocalMediaItem[]>>(new Map());
  /** Live copy job for the progress bar (null when idle). */
  readonly activeCopy = signal<LocalMediaCopyJob | null>(null);

  readonly byId = computed(() => new Map(this.folders().map((f) => [f.id, f])));
  readonly copying = computed(() => {
    const j = this.activeCopy();
    return !!j && (j.status === 'pending' || j.status === 'running');
  });
  readonly copyPercent = computed(() => {
    const j = this.activeCopy();
    if (!j || j.total <= 0) return 0;
    return Math.min(100, Math.round((100 * j.done) / j.total));
  });

  private noticeTimer: ReturnType<typeof setTimeout> | null = null;
  private copyTimer: ReturnType<typeof setTimeout> | null = null;

  loadFolders(): void {
    this.request<LocalMediaFolder[]>('GET', '/api/local-media/folders').subscribe({
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
    this.itemsByFolder.set(new Map());
  }

  createFolder(): Observable<LocalMediaFolder> {
    return this.request<LocalMediaFolder>('POST', '/api/local-media/folders', {}).pipe(
      tap((folder) => {
        this.folders.update((list) => [...list, { ...folder, count: 0 }]);
        this.renamingId.set(folder.id);
      }),
      catchError((err: Error) => {
        this.showNotice(err.message);
        return throwError(() => err);
      }),
    );
  }

  renameFolder(id: number, name: string): Observable<LocalMediaFolder> {
    return this.request<LocalMediaFolder>('PATCH', `/api/local-media/folders/${id}`, { name }).pipe(
      tap((updated) =>
        this.folders.update((list) => list.map((f) => (f.id === id ? { ...f, name: updated.name } : f))),
      ),
    );
  }

  removeFolder(id: number): Observable<number> {
    return this.request<{ deleted_items: number }>('DELETE', `/api/local-media/folders/${id}`).pipe(
      map((res) => res.deleted_items),
      tap(() => {
        this.folders.update((list) => list.filter((f) => f.id !== id));
        this.itemsByFolder.update((m) => {
          const next = new Map(m);
          next.delete(id);
          return next;
        });
      }),
      catchError((err: Error) => {
        this.showNotice(err.message);
        return throwError(() => err);
      }),
    );
  }

  reorderFolders(fromIndex: number, toIndex: number): void {
    const before = this.folders();
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || toIndex >= before.length) return;
    const next = [...before];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    this.folders.set(next);
    this.request<{ ok: string }>('PUT', '/api/local-media/folders/order', { ids: next.map((f) => f.id) }).subscribe({
      error: (err: Error) => {
        this.folders.set(before);
        this.showNotice(`Couldn't reorder folders. ${err.message}`);
      },
    });
  }

  loadItems(folderId: number): Observable<LocalMediaItem[]> {
    return this.request<LocalMediaItem[]>('GET', `/api/local-media/folders/${folderId}/items`).pipe(
      tap((items) => {
        this.itemsByFolder.update((m) => {
          const next = new Map(m);
          next.set(folderId, items);
          return next;
        });
      }),
    );
  }

  /** Starts a background copy job; poll with getCopyJob. */
  startCopyJob(folderId: number, relPaths: string[]): Observable<LocalMediaCopyJob> {
    return this.request<LocalMediaCopyJob>('POST', `/api/local-media/folders/${folderId}/copy`, {
      rel_paths: relPaths,
    });
  }

  getCopyJob(jobId: string): Observable<LocalMediaCopyJob> {
    return this.request<LocalMediaCopyJob>('GET', `/api/local-media/copy-jobs/${jobId}`);
  }

  /**
   * Starts a copy and polls until done. Updates `activeCopy` for the shared progress bar.
   * Emits the final job snapshot when complete.
   */
  runCopyJob(folderId: number, relPaths: string[]): Observable<LocalMediaCopyJob> {
    if (this.copying() || !relPaths.length) {
      return throwError(() => new Error(this.copying() ? 'A copy is already running.' : 'Nothing to copy.'));
    }
    this.activeCopy.set({
      id: '',
      folder_id: folderId,
      status: 'pending',
      total: relPaths.length,
      done: 0,
      copied: 0,
      skipped: 0,
      failed: 0,
      bytes_done: 0,
      copied_names: [],
      skipped_paths: [],
      errors: [],
    });
    return this.startCopyJob(folderId, relPaths).pipe(
      tap((job) => this.activeCopy.set(job)),
      switchMap((job) => this.waitForCopyJob(job.id, folderId)),
      catchError((err: Error) => {
        this.activeCopy.set(null);
        return throwError(() => err);
      }),
    );
  }

  private waitForCopyJob(jobId: string, folderId: number): Observable<LocalMediaCopyJob> {
    return new Observable<LocalMediaCopyJob>((subscriber) => {
      const tick = () => {
        this.getCopyJob(jobId).subscribe({
          next: (job) => {
            this.activeCopy.set(job);
            if (job.copied_names?.length) this.camera.markCopied(job.copied_names);
            if (job.status === 'done' || job.status === 'error') {
              this.applyCopyJobResult(folderId, job);
              subscriber.next(job);
              subscriber.complete();
              this.copyTimer = setTimeout(() => this.activeCopy.set(null), 900);
              return;
            }
            this.copyTimer = setTimeout(tick, 400);
          },
          error: (err: Error) => {
            this.activeCopy.set(null);
            subscriber.error(err);
          },
        });
      };
      tick();
      return () => {
        if (this.copyTimer) clearTimeout(this.copyTimer);
      };
    });
  }

  applyCopyJobResult(folderId: number, job: LocalMediaCopyJob): void {
    if (job.copied > 0) {
      this.folders.update((list) =>
        list.map((f) => (f.id === folderId ? { ...f, count: f.count + job.copied } : f)),
      );
      // Refresh folder items if already loaded.
      if (this.itemsByFolder().has(folderId)) {
        this.loadItems(folderId).subscribe({ error: () => {} });
      }
    }
    const parts: string[] = [];
    if (job.copied) parts.push(`Copied ${job.copied}`);
    if (job.skipped) parts.push(`skipped ${job.skipped} already in library`);
    if (job.failed) parts.push(`${job.failed} failed`);
    if (parts.length) this.showNotice(parts.join(' · '));
  }

  /** Opens the on-disk Local media library folder in Finder. */
  openInFinder(folderId: number): Observable<{ path: string }> {
    return this.request<{ path: string }>('POST', `/api/local-media/folders/${folderId}/open`);
  }

  /**
   * Moves a Dump/Home card's downloaded files into a Local media folder (save is deleted).
   */
  importFromSave(folderId: number, saveId: number): Observable<{ imported: number; items: LocalMediaItem[] }> {
    return this.request<{ imported: number; items: LocalMediaItem[] }>(
      'POST',
      `/api/local-media/folders/${folderId}/from-save`,
      { save_id: saveId },
    ).pipe(
      tap((res) => {
        const items = res?.items ?? [];
        if (!items.length) return;
        this.itemsByFolder.update((m) => {
          const next = new Map(m);
          if (next.has(folderId)) {
            const existing = new Set((next.get(folderId) ?? []).map((it) => it.id));
            next.set(folderId, [...items.filter((it) => !existing.has(it.id)), ...(next.get(folderId) ?? [])]);
          }
          return next;
        });
        this.folders.update((list) =>
          list.map((f) => (f.id === folderId ? { ...f, count: f.count + (res.imported || items.length) } : f)),
        );
      }),
    );
  }

  /** Drops items from local UI state after they were promoted into Dump/Home. */
  removeItemsLocally(folderId: number, ids: number[]): void {
    const idSet = new Set(ids);
    const before = this.itemsByFolder().get(folderId)?.length ?? 0;
    this.itemsByFolder.update((m) => {
      const next = new Map(m);
      if (next.has(folderId)) {
        next.set(
          folderId,
          (next.get(folderId) ?? []).filter((it) => !idSet.has(it.id)),
        );
      }
      return next;
    });
    const after = this.itemsByFolder().get(folderId)?.length ?? Math.max(0, before - ids.length);
    const removed = Math.max(0, before - after);
    if (removed > 0) {
      this.folders.update((list) =>
        list.map((f) => (f.id === folderId ? { ...f, count: Math.max(0, f.count - removed) } : f)),
      );
    } else if (ids.length) {
      this.folders.update((list) =>
        list.map((f) => (f.id === folderId ? { ...f, count: Math.max(0, f.count - ids.length) } : f)),
      );
    }
  }

  /** Moves items into toFolderId (library files stay on disk; only folder membership changes). */
  moveItems(toFolderId: number, ids: number[], fromFolderId?: number): Observable<{ moved: number }> {
    return this.request<{ moved: number }>('POST', `/api/local-media/folders/${toFolderId}/move`, { ids }).pipe(
      tap((res) => {
        const moved = res?.moved ?? 0;
        if (moved <= 0) return;
        const idSet = new Set(ids);
        this.itemsByFolder.update((m) => {
          const next = new Map(m);
          let moving: LocalMediaItem[] = [];
          if (fromFolderId != null && next.has(fromFolderId)) {
            const src = next.get(fromFolderId) ?? [];
            moving = src.filter((it) => idSet.has(it.id));
            next.set(
              fromFolderId,
              src.filter((it) => !idSet.has(it.id)),
            );
          } else {
            for (const [fid, list] of next) {
              if (fid === toFolderId) continue;
              const keep = list.filter((it) => !idSet.has(it.id));
              if (keep.length !== list.length) {
                moving = moving.concat(list.filter((it) => idSet.has(it.id)));
                next.set(fid, keep);
              }
            }
          }
          if (next.has(toFolderId) && moving.length) {
            const dest = next.get(toFolderId) ?? [];
            const existing = new Set(dest.map((it) => it.id));
            next.set(toFolderId, [
              ...moving
                .filter((it) => !existing.has(it.id))
                .map((it) => ({ ...it, folder_id: toFolderId })),
              ...dest,
            ]);
          }
          return next;
        });
        this.folders.update((list) =>
          list.map((f) => {
            if (f.id === toFolderId) return { ...f, count: f.count + moved };
            if (fromFolderId != null && f.id === fromFolderId) {
              return { ...f, count: Math.max(0, f.count - moved) };
            }
            return f;
          }),
        );
        // If we didn't know fromFolderId, refresh folder list counts from server next load;
        // still adjust by scanning known item maps when from was unknown.
        if (fromFolderId == null) {
          this.loadFolders();
        }
      }),
    );
  }

  deleteItem(folderId: number, id: number): Observable<void> {
    return this.request<{ ok: string }>('DELETE', `/api/local-media/${id}`).pipe(
      map(() => undefined),
      tap(() => {
        this.itemsByFolder.update((m) => {
          const next = new Map(m);
          next.set(
            folderId,
            (next.get(folderId) ?? []).filter((it) => it.id !== id),
          );
          return next;
        });
        this.folders.update((list) =>
          list.map((f) => (f.id === folderId ? { ...f, count: Math.max(0, f.count - 1) } : f)),
        );
      }),
    );
  }

  showNotice(message: string): void {
    this.notice.set(message);
    if (this.noticeTimer) clearTimeout(this.noticeTimer);
    this.noticeTimer = setTimeout(() => this.notice.set(null), NOTICE_MS);
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
      }),
    );
  }
}
