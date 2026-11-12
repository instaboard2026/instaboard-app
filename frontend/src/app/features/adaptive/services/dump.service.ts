import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';

export type SaveSource = 'instagram' | 'pinterest' | 'youtube' | 'local';

/** One downloaded file with a signed, expiring URL the browser can load directly. */
export interface SaveMedia {
  url: string;
  type: 'video' | 'image';
}

/** What's in the download folder for a save: `<id>.mp4` (video) or `<id>/` with numbered files (post). */
export interface SaveDownload {
  kind: 'video' | 'post';
  files: number;
  /** In display order; empty if the files couldn't be listed. */
  media?: SaveMedia[];
  /** Absolute path on this computer (click to reveal in Finder). */
  path?: string;
}

/** One saved link from GET/POST /api/dump (matches backend saveResponse). */
export interface Save {
  id: number;
  url: string;
  source: SaveSource;
  added_from: string;
  /** null = in the Dump. */
  folder_id: number | null;
  /** Manual board order within Home or a folder (lower = earlier). */
  position: number;
  created_at: string;
  /** Set when the save is in the Bin (soft-deleted). */
  deleted_at?: string | null;
  /** Instagram engagement snapshot at download time (absent until downloaded). */
  like_count?: number | null;
  comment_count?: number | null;
  repost_count?: number | null;
  share_count?: number | null;
  play_count?: number | null;
  /** null = not downloaded yet. */
  download: SaveDownload | null;
}

/** GET /api/stats: downloads across Home and all folders. */
export interface DownloadStats {
  total_downloads: number;
  storage_bytes: number;
  by_source: Record<SaveSource, number>;
}

/** GET /api/downloads/info (also returned after changing the folder). */
export interface DownloadsInfo {
  dir: string;
  is_default: boolean;
  /** False when a chosen folder was renamed, moved or deleted. */
  exists: boolean;
  tools_available: boolean;
  picker_available: boolean;
  cookies_browser: string;
}

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

/** Where a list of saves lives: the Dump, the Bin, or a folder id. */
export type SaveScope = 'dump' | 'bin' | number;
/** Home or a folder — targets for move/restore (not the Bin). */
export type MoveTarget = Exclude<SaveScope, 'bin'>;

const scopeKey = (scope: SaveScope) => String(scope);
const scopePath = (scope: SaveScope) =>
  scope === 'dump' ? '/api/dump' : scope === 'bin' ? '/api/bin' : `/api/folders/${scope}/saves`;
const orderPath = (scope: MoveTarget) =>
  scope === 'dump' ? '/api/dump/order' : `/api/folders/${scope}/saves/order`;
const newestDeletedFirst = (a: Save, b: Save) =>
  (b.deleted_at ?? '').localeCompare(a.deleted_at ?? '') || b.id - a.id;

/**
 * Saves per scope (the Dump, Bin, and each opened folder), shared by the sidebar badge, the Dump
 * page and folder pages. Moves between scopes are applied locally first (see FoldersService).
 */
@Injectable({ providedIn: 'root' })
export class DumpService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);

  private _lists = signal<ReadonlyMap<string, Save[]>>(new Map());
  private _loading = signal<ReadonlySet<string>>(new Set());
  private _loaded = signal<ReadonlySet<string>>(new Set());

  /** The Dump list (sidebar badge and Dump page). */
  saves = computed(() => this.listFor('dump'));
  loading = computed(() => this.isLoading('dump'));
  loaded = computed(() => this.isLoaded('dump'));
  count = computed(() => this.saves().length);

  /** All loaded saves by id (Home, folders, Bin) — used for calendar thumbnails. */
  readonly byId = computed(() => {
    const map = new Map<number, Save>();
    for (const list of this._lists().values()) {
      for (const save of list) map.set(save.id, save);
    }
    return map as ReadonlyMap<number, Save>;
  });

  listFor(scope: SaveScope): Save[] {
    return this._lists().get(scopeKey(scope)) ?? [];
  }

  isLoading(scope: SaveScope): boolean {
    return this._loading().has(scopeKey(scope));
  }

  isLoaded(scope: SaveScope): boolean {
    return this._loaded().has(scopeKey(scope));
  }

  /** Loads the saves of a scope from the API. Returns an empty list when logged out. */
  load(scope: SaveScope = 'dump'): Observable<Save[]> {
    const headers = this.authHeaders();
    if (!headers) {
      this.clear();
      return of([]);
    }
    const key = scopeKey(scope);
    this._loading.update((s) => new Set(s).add(key));
    const done = () =>
      this._loading.update((s) => {
        const next = new Set(s);
        next.delete(key);
        return next;
      });
    return this.http.get<ApiWrapper<Save[]>>(`${environment.apiUrl}${scopePath(scope)}`, { headers }).pipe(
      map((res) => (res?.status === 'SUCCESS' && Array.isArray(res.data) ? res.data : [])),
      tap((saves) => {
        this.setList(scope, saves);
        this._loaded.update((s) => new Set(s).add(key));
        done();
      }),
      catchError((err) => {
        done();
        return throwError(() => err);
      })
    );
  }

  /** Saves a pasted link into a scope. Errors carry the backend message (e.g. duplicate or unsupported link). */
  add(url: string, scope: SaveScope = 'dump'): Observable<Save> {
    const headers = this.authHeaders();
    if (!headers) {
      return throwError(() => new Error('Please sign in to save links.'));
    }
    return this.http.post<ApiWrapper<Save>>(`${environment.apiUrl}${scopePath(scope)}`, { url }, { headers }).pipe(
      map((res) => {
        if (res?.status !== 'SUCCESS' || !res.data) {
          throw new Error(res?.message || 'Could not save this link.');
        }
        return res.data;
      }),
      tap((save) => this.putSave(save, scope)),
      catchError((err: unknown) => {
        const message =
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Could not save this link. Please try again.'
            : (err as Error)?.message || 'Could not save this link.';
        return throwError(() => new Error(capitalize(message)));
      })
    );
  }

  /**
   * Uploads local images into Home or a folder (one card per file, already previewable).
   * Field name must be `file` (repeatable). Bin is not supported.
   */
  upload(files: File[], scope: SaveScope = 'dump'): Observable<Save[]> {
    if (scope === 'bin') {
      return throwError(() => new Error('Drop images on Home or a folder, not the Bin.'));
    }
    const list = files.filter((f) => f && f.size > 0);
    if (!list.length) {
      return throwError(() => new Error('Drop one or more image files.'));
    }
    const headers = this.authHeaders();
    if (!headers) {
      return throwError(() => new Error('Please sign in to save images.'));
    }
    const form = new FormData();
    for (const f of list) {
      form.append('file', f, f.name || 'image');
    }
    const path =
      scope === 'dump' ? '/api/dump/upload' : `/api/folders/${scope}/saves/upload`;
    return this.http.post<ApiWrapper<Save[]>>(`${environment.apiUrl}${path}`, form, { headers }).pipe(
      map((res) => {
        if (res?.status !== 'SUCCESS' || !Array.isArray(res.data)) {
          throw new Error(res?.message || 'Could not save those images.');
        }
        return res.data;
      }),
      tap((saves) => {
        // Backend inserts each at the front; reverse so the first dropped file stays first.
        for (const save of [...saves].reverse()) {
          this.putSave(save, scope);
        }
      }),
      catchError((err: unknown) => {
        const message =
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Could not save those images. Please try again.'
            : (err as Error)?.message || 'Could not save those images.';
        return throwError(() => new Error(capitalize(message)));
      })
    );
  }

  /** Opens the downloaded file in Finder (macOS) / File Explorer. */
  reveal(id: number): Observable<{ path: string }> {
    const headers = this.authHeaders();
    if (!headers) {
      return throwError(() => new Error('Please sign in.'));
    }
    return this.http
      .post<ApiWrapper<{ path: string }>>(`${environment.apiUrl}/api/dump/${id}/reveal`, null, { headers })
      .pipe(
        map((res) => {
          if (res?.status !== 'SUCCESS' || !res.data?.path) {
            throw new Error(res?.message || 'Could not open that location.');
          }
          return res.data;
        }),
        catchError((err: unknown) => {
          const message =
            err instanceof HttpErrorResponse
              ? err.error?.message || 'Could not open that location.'
              : (err as Error)?.message || 'Could not open that location.';
          return throwError(() => new Error(capitalize(message)));
        })
      );
  }

  /** Downloads a saved Instagram reel/post into the download folder on this computer (can take a while). */
  download(id: number): Observable<Save> {
    const headers = this.authHeaders();
    if (!headers) {
      return throwError(() => new Error('Please sign in.'));
    }
    return this.http.post<ApiWrapper<Save>>(`${environment.apiUrl}/api/dump/${id}/download`, null, { headers }).pipe(
      map((res) => {
        if (res?.status !== 'SUCCESS' || !res.data) {
          throw new Error(res?.message || 'Download failed.');
        }
        return res.data;
      }),
      tap((updated) => this.replaceSave(updated)),
      catchError((err: unknown) => {
        const message =
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Download failed. Is the backend running?'
            : (err as Error)?.message || 'Download failed.';
        return throwError(() => new Error(capitalize(message)));
      })
    );
  }

  /** Download counts and disk usage for the Home stats strip. */
  stats(): Observable<DownloadStats | null> {
    const headers = this.authHeaders();
    if (!headers) return of(null);
    return this.http
      .get<ApiWrapper<DownloadStats>>(`${environment.apiUrl}/api/stats`, { headers })
      .pipe(map((res) => (res?.status === 'SUCCESS' && res.data ? res.data : null)));
  }

  /** Download folder path and whether gallery-dl is installed on this computer. */
  downloadsInfo(): Observable<DownloadsInfo | null> {
    const headers = this.authHeaders();
    if (!headers) return of(null);
    return this.http
      .get<ApiWrapper<DownloadsInfo>>(`${environment.apiUrl}/api/downloads/info`, { headers })
      .pipe(map((res) => (res?.status === 'SUCCESS' && res.data ? res.data : null)));
  }

  /** Opens the native folder picker on this computer; resolves when the user picks (or errors on cancel). */
  chooseDownloadFolder(): Observable<DownloadsInfo> {
    return this.folderRequest('POST');
  }

  /** Switches back to the default download folder. */
  resetDownloadFolder(): Observable<DownloadsInfo> {
    return this.folderRequest('DELETE');
  }

  /** Installs or reinstalls gallery-dl, yt-dlp and ffmpeg on this Mac (may take a few minutes). */
  installDownloadTools(): Observable<DownloadsInfo> {
    return this.toolsRequest('POST', 'Could not install download tools.');
  }

  /** Uninstalls tools from Instaboard (downloads stay off until you install again). */
  uninstallDownloadTools(): Observable<DownloadsInfo> {
    return this.toolsRequest('DELETE', 'Could not uninstall download tools.');
  }

  private toolsRequest(method: 'POST' | 'DELETE', fallback: string): Observable<DownloadsInfo> {
    const headers = this.authHeaders();
    if (!headers) {
      return throwError(() => new Error('Please sign in.'));
    }
    return this.http
      .request<ApiWrapper<DownloadsInfo>>(method, `${environment.apiUrl}/api/downloads/tools`, { headers })
      .pipe(
        map((res) => {
          if (res?.status !== 'SUCCESS' || !res.data) {
            throw new Error(res?.message || fallback);
          }
          return res.data;
        }),
        catchError((err: unknown) => {
          const message =
            err instanceof HttpErrorResponse ? err.error?.message || fallback : (err as Error)?.message || fallback;
          return throwError(() => new Error(message));
        }),
      );
  }

  private folderRequest(method: 'POST' | 'DELETE'): Observable<DownloadsInfo> {
    const headers = this.authHeaders();
    if (!headers) {
      return throwError(() => new Error('Please sign in.'));
    }
    return this.http.request<ApiWrapper<DownloadsInfo>>(method, `${environment.apiUrl}/api/downloads/folder`, { headers }).pipe(
      map((res) => {
        if (res?.status !== 'SUCCESS' || !res.data) {
          throw new Error(res?.message || 'Could not change the folder.');
        }
        return res.data;
      }),
      catchError((err: unknown) => {
        const message =
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Could not change the folder.'
            : (err as Error)?.message || 'Could not change the folder.';
        return throwError(() => new Error(capitalize(message)));
      })
    );
  }

  clear(): void {
    this._lists.set(new Map());
    this._loaded.set(new Set());
    this._loading.set(new Set());
  }

  /** Removes a save from whichever list holds it; returns where it was. */
  takeSave(id: number): SaveScope | null {
    for (const [key, list] of this._lists()) {
      if (list.some((s) => s.id === id)) {
        this._lists.update((m) => new Map(m).set(key, list.filter((s) => s.id !== id)));
        return key === 'dump' ? 'dump' : Number(key);
      }
    }
    return null;
  }

  /**
   * Adds a save to a scope's list (if that list is loaded). Home/folder: front of the board.
   * Bin: newest deleted first.
   */
  putSave(save: Save, scope: SaveScope): void {
    const key = scopeKey(scope);
    const list = this._lists().get(key);
    if (!list && !this._loaded().has(key)) return;
    const rest = (list ?? []).filter((s) => s.id !== save.id);
    const next = scope === 'bin' ? [...rest, save].sort(newestDeletedFirst) : [save, ...rest];
    this._lists.update((m) => new Map(m).set(key, next));
  }

  /**
   * Reorders saves on Home or a folder (optimistic). Rolls back if the server rejects the order.
   */
  reorder(scope: MoveTarget, fromIndex: number, toIndex: number): Observable<Save[]> {
    const before = this.listFor(scope);
    if (fromIndex === toIndex || fromIndex < 0 || toIndex < 0 || toIndex >= before.length) {
      return of(before);
    }
    const next = [...before];
    const [moved] = next.splice(fromIndex, 1);
    next.splice(toIndex, 0, moved);
    const positioned = next.map((s, i) => ({ ...s, position: i + 1 }));
    this.setList(scope, positioned);

    const headers = this.authHeaders();
    if (!headers) {
      this.setList(scope, before);
      return throwError(() => new Error('Please sign in.'));
    }
    return this.http
      .put<ApiWrapper<Save[]>>(`${environment.apiUrl}${orderPath(scope)}`, { ids: positioned.map((s) => s.id) }, { headers })
      .pipe(
        map((res) => {
          if (res?.status !== 'SUCCESS' || !Array.isArray(res.data)) {
            throw new Error(res?.message || 'Could not reorder.');
          }
          return res.data;
        }),
        tap((saves) => this.setList(scope, saves)),
        catchError((err: unknown) => {
          this.setList(scope, before);
          const message =
            err instanceof HttpErrorResponse
              ? err.error?.message || 'Could not reorder.'
              : (err as Error)?.message || 'Could not reorder.';
          return throwError(() => new Error(capitalize(message)));
        })
      );
  }

  /** Replaces a save wherever it is listed (e.g. after a download). */
  replaceSave(updated: Save): void {
    this._lists.update((m) => {
      const next = new Map(m);
      for (const [key, list] of m) {
        if (list.some((s) => s.id === updated.id)) {
          next.set(key, list.map((s) => (s.id === updated.id ? updated : s)));
        }
      }
      return next;
    });
  }

  /** Forgets a folder's list (after the folder is deleted). */
  dropScope(scope: SaveScope): void {
    const key = scopeKey(scope);
    this._lists.update((m) => {
      const next = new Map(m);
      next.delete(key);
      return next;
    });
    this._loaded.update((s) => {
      const next = new Set(s);
      next.delete(key);
      return next;
    });
  }

  /** Replaces a scope's list in memory (e.g. after emptying the Bin). */
  replaceList(scope: SaveScope, saves: Save[]): void {
    this.setList(scope, saves);
    this._loaded.update((s) => new Set(s).add(scopeKey(scope)));
  }

  private setList(scope: SaveScope, saves: Save[]): void {
    this._lists.update((m) => new Map(m).set(scopeKey(scope), saves));
  }

  private authHeaders(): HttpHeaders | null {
    const token = this.authToken.getToken();
    return token ? new HttpHeaders({ Authorization: `Bearer ${token}` }) : null;
  }
}

function capitalize(s: string): string {
  return s ? s.charAt(0).toUpperCase() + s.slice(1) : s;
}
