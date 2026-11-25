import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders, HttpParams } from '@angular/common/http';
import { Observable, catchError, map, of, switchMap, tap, throwError, timer } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

export type CameraScanStatus = 'idle' | 'running' | 'done' | 'error';

export type CameraKindFilter = 'all' | 'video' | 'image' | 'junk';

export type CameraSortOrder = 'latest' | 'oldest';

export interface CameraStatus {
  root?: string;
  label?: string;
  opened_at?: string;
  available: boolean;
  has_root: boolean;
  scan: CameraScanStatus;
  indexed_count: number;
  truncated: boolean;
  timed_out: boolean;
  scan_error?: string;
  all_count: number;
  video_count: number;
  image_count: number;
  junk_count: number;
  free_bytes?: number;
  total_bytes?: number;
}

export interface CameraItem {
  rel_path: string;
  name: string;
  kind: 'image' | 'video' | 'junk';
  byte_size: number;
  mtime_ns?: number;
  url: string;
  thumb_url: string;
  copied: boolean;
  /** Video length in ms once its thumbnail exists (absent = not known yet). */
  duration_ms?: number;
}

export interface CameraDeleteResult {
  deleted: number;
  failed?: string[];
}

export interface CameraItemsPage {
  root: string;
  label: string;
  items: CameraItem[];
  offset: number;
  limit: number;
  total: number;
  next_cursor?: string;
  has_more: boolean;
  scan: CameraScanStatus;
  truncated: boolean;
  timed_out: boolean;
}

export interface CameraOpenResponse {
  root: string;
  label: string;
  scan: CameraScanStatus;
  indexed_count?: number;
}

/** Manage camera: persisted SD/folder root + offset pages + thumbs. */
@Injectable({ providedIn: 'root' })
export class CameraService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);

  readonly status = signal<CameraStatus | null>(null);
  readonly items = signal<CameraItem[]>([]);
  readonly root = signal<string | null>(null);
  readonly label = signal<string | null>(null);
  readonly truncated = signal(false);
  readonly timedOut = signal(false);
  readonly loading = signal(false);
  readonly scanning = signal(false);
  readonly pageKind = signal<CameraKindFilter>('all');
  readonly pageSort = signal<CameraSortOrder>('latest');
  readonly pageIndex = signal(0); // 0-based
  readonly pageSize = signal(12);
  readonly total = signal(0);

  readonly pageCount = computed(() => {
    const size = this.pageSize();
    const total = this.total();
    if (size <= 0) return 1;
    return Math.max(1, Math.ceil(total / size));
  });

  loadStatus(): Observable<CameraStatus> {
    if (!this.authToken.getToken()) {
      this.clear();
      return of({
        available: false, has_root: false, scan: 'idle',
        indexed_count: 0, truncated: false, timed_out: false,
        all_count: 0, video_count: 0, image_count: 0, junk_count: 0,
      });
    }
    return this.http.get<ApiWrapper<CameraStatus>>(`${environment.apiUrl}/api/camera/status`, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => res.data ?? {
        available: false, has_root: false, scan: 'idle' as const,
        indexed_count: 0, truncated: false, timed_out: false,
        all_count: 0, video_count: 0, image_count: 0, junk_count: 0,
      }),
      tap((s) => {
        this.status.set(s);
        this.scanning.set(s.scan === 'running');
        this.truncated.set(!!s.truncated);
        this.timedOut.set(!!s.timed_out);
        if (s.label) this.label.set(s.label);
        if (s.root) this.root.set(s.root);
      }),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  open(): Observable<CameraOpenResponse> {
    this.loading.set(true);
    return this.http.post<ApiWrapper<CameraOpenResponse>>(`${environment.apiUrl}/api/camera/open`, {}, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Could not open folder');
        return res.data;
      }),
      tap((data) => {
        this.root.set(data.root);
        this.label.set(data.label);
        this.items.set([]);
        this.total.set(0);
        this.pageIndex.set(0);
        this.scanning.set(true);
        this.loading.set(false);
        this.status.update((s) => ({
          ...(s ?? {
            available: true, has_root: true, scan: 'running',
            indexed_count: 0, truncated: false, timed_out: false,
            all_count: 0, video_count: 0, image_count: 0, junk_count: 0,
          }),
          root: data.root, label: data.label, available: true, has_root: true, scan: 'running',
        }));
      }),
      catchError((err) => {
        this.loading.set(false);
        return throwError(() => this.toError(err));
      }),
    );
  }

  refresh(): Observable<CameraOpenResponse> {
    this.loading.set(true);
    return this.http.post<ApiWrapper<CameraOpenResponse>>(`${environment.apiUrl}/api/camera/refresh`, {}, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Could not refresh');
        return res.data;
      }),
      tap(() => {
        this.items.set([]);
        this.total.set(0);
        this.pageIndex.set(0);
        this.scanning.set(true);
        this.loading.set(false);
      }),
      catchError((err) => {
        this.loading.set(false);
        return throwError(() => this.toError(err));
      }),
    );
  }

  /** Load a discrete page (replaces items). pageIndex is 0-based. */
  loadPage(
    pageIndex: number,
    kind: CameraKindFilter = 'all',
    pageSize?: number,
    sort: CameraSortOrder = 'latest',
  ): Observable<CameraItemsPage> {
    const size = pageSize ?? this.pageSize();
    this.pageKind.set(kind);
    this.pageSort.set(sort);
    this.pageSize.set(size);
    const page = Math.max(0, pageIndex);
    this.pageIndex.set(page);
    this.loading.set(true);
    const offset = page * size;
    return this.fetchOffset(offset, size, kind, sort).pipe(
      tap((data) => {
        this.applyPage(data);
        this.loading.set(false);
      }),
      catchError((err) => {
        this.loading.set(false);
        return throwError(() => this.toError(err));
      }),
    );
  }

  deleteFiles(relPaths: string[]): Observable<CameraDeleteResult> {
    return this.http.post<ApiWrapper<CameraDeleteResult>>(`${environment.apiUrl}/api/camera/delete`, {
      rel_paths: relPaths,
    }, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Could not delete files');
        return res.data;
      }),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  /** Open the Mac's AirDrop window for card files, read in place (no browser copy). */
  airDrop(relPaths: string[]): Observable<void> {
    return this.http.post<ApiWrapper<{ files: number }>>(`${environment.apiUrl}/api/camera/airdrop`, {
      rel_paths: relPaths,
    }, {
      headers: this.authHeaders(),
    }).pipe(
      map(() => undefined),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  /** Poll status while scanning; refresh current page periodically. */
  pollWhileScanning(
    kind: CameraKindFilter,
    pageSize: number,
    sort: CameraSortOrder = 'latest',
  ): Observable<CameraStatus> {
    return timer(0, 1500).pipe(
      switchMap(() => this.loadStatus()),
      tap((s) => {
        if (s.scan === 'running') {
          this.fetchOffset(this.pageIndex() * pageSize, pageSize, kind, sort).subscribe({
            next: (page) => this.applyPage(page),
            error: () => {},
          });
        }
      }),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  clearRoot(): Observable<void> {
    return this.http.delete<ApiWrapper<{ ok: string }>>(`${environment.apiUrl}/api/camera/root`, {
      headers: this.authHeaders(),
    }).pipe(
      map(() => undefined),
      tap(() => this.clear()),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  markCopied(names: string[]): void {
    if (!names.length) return;
    const set = new Set(names);
    this.items.update((list) => list.map((it) => (set.has(it.name) ? { ...it, copied: true } : it)));
  }

  clear(): void {
    this.status.set(null);
    this.items.set([]);
    this.root.set(null);
    this.label.set(null);
    this.loading.set(false);
    this.scanning.set(false);
    this.truncated.set(false);
    this.timedOut.set(false);
    this.total.set(0);
    this.pageIndex.set(0);
  }

  private fetchOffset(
    offset: number,
    limit: number,
    kind: CameraKindFilter,
    sort: CameraSortOrder = 'latest',
  ): Observable<CameraItemsPage> {
    let params = new HttpParams()
      .set('limit', String(limit))
      .set('offset', String(offset))
      .set('sort', sort);
    if (kind !== 'all') params = params.set('kind', kind);
    return this.http.get<ApiWrapper<CameraItemsPage>>(`${environment.apiUrl}/api/camera/items`, {
      headers: this.authHeaders(),
      params,
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Could not load camera items');
        return res.data;
      }),
    );
  }

  private applyPage(page: CameraItemsPage): void {
    this.root.set(page.root);
    this.label.set(page.label);
    this.truncated.set(!!page.truncated);
    this.timedOut.set(!!page.timed_out);
    this.scanning.set(page.scan === 'running');
    this.total.set(page.total ?? 0);
    this.items.set(page.items ?? []);
  }

  private authHeaders(): HttpHeaders {
    return new HttpHeaders({ Authorization: `Bearer ${this.authToken.getToken()}` });
  }

  private toError(err: unknown): Error {
    const message =
      err instanceof HttpErrorResponse
        ? err.error?.message || 'Something went wrong. Is the backend running?'
        : (err as Error)?.message || 'Something went wrong.';
    return new Error(message.charAt(0).toUpperCase() + message.slice(1));
  }
}
