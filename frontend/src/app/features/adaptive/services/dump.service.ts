import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';

export type SaveSource = 'instagram' | 'pinterest' | 'youtube';

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
}

/** One saved link from GET/POST /api/dump (matches backend saveResponse). */
export interface Save {
  id: number;
  url: string;
  source: SaveSource;
  added_from: string;
  /** null = in the Dump. */
  folder_id: number | null;
  created_at: string;
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

/** Where a list of saves lives: the Dump or a folder id. */
export type SaveScope = 'dump' | number;

const scopeKey = (scope: SaveScope) => String(scope);
const scopePath = (scope: SaveScope) => (scope === 'dump' ? '/api/dump' : `/api/folders/${scope}/saves`);
const newestFirst = (a: Save, b: Save) => b.created_at.localeCompare(a.created_at) || b.id - a.id;

/**
 * Saves per scope (the Dump and each opened folder), shared by the sidebar badge, the Dump page and
 * folder pages. Moves between scopes are applied locally first (see FoldersService.moveSave).
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

  /** Adds a save to a scope's list (if that list is loaded), keeping newest first. */
  putSave(save: Save, scope: SaveScope): void {
    const key = scopeKey(scope);
    const list = this._lists().get(key);
    if (!list && !this._loaded().has(key)) return;
    const next = [...(list ?? []).filter((s) => s.id !== save.id), save].sort(newestFirst);
    this._lists.update((m) => new Map(m).set(key, next));
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
