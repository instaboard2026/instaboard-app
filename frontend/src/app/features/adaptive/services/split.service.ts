import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

export type SplitStatus = 'ready' | 'detecting' | 'splitting' | 'done' | 'error';

export interface SplitClip {
  id: number;
  project_id: number;
  position: number;
  start_ms: number;
  end_ms: number;
  file_name: string;
  created_at: string;
  url: string;
  type: 'video' | 'image';
}

export interface SplitProject {
  id: number;
  source_type: 'save' | 'upload';
  source_save_id?: number | null;
  source_file?: string | null;
  title?: string | null;
  status: SplitStatus;
  error_message?: string | null;
  master_file: string;
  duration_ms?: number | null;
  detect_threshold: number;
  created_at: string;
  updated_at: string;
  master_url: string;
  clip_count: number;
  clips?: SplitClip[];
}

/**
 * Split video analysis projects — stored in backend SQLite, files under the download dir.
 */
@Injectable({ providedIn: 'root' })
export class SplitService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);

  private readonly _projects = signal<SplitProject[]>([]);
  private readonly _loading = signal(false);
  private readonly _loaded = signal(false);

  readonly projects = computed(() => this._projects());
  readonly loading = computed(() => this._loading());
  readonly loaded = computed(() => this._loaded());

  load(): Observable<SplitProject[]> {
    if (!this.authToken.getToken()) {
      this.clear();
      return of([]);
    }
    this._loading.set(true);
    return this.http.get<ApiWrapper<SplitProject[]>>(`${environment.apiUrl}/api/split/projects`, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => res.data ?? []),
      tap((list) => {
        this._projects.set(list);
        this._loading.set(false);
        this._loaded.set(true);
      }),
      catchError((err) => {
        this._loading.set(false);
        return throwError(() => this.toError(err));
      }),
    );
  }

  clear(): void {
    this._projects.set([]);
    this._loading.set(false);
    this._loaded.set(false);
  }

  createFromSave(saveId: number, file?: string): Observable<SplitProject> {
    const body: { source_type: 'save'; save_id: number; file?: string } = {
      source_type: 'save',
      save_id: saveId,
    };
    if (file) body.file = file;
    return this.http.post<ApiWrapper<SplitProject>>(`${environment.apiUrl}/api/split/projects`, body, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Could not create project');
        return res.data;
      }),
      tap((p) => this._projects.update((list) => [p, ...list.filter((x) => x.id !== p.id)])),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  createFromUpload(file: File): Observable<SplitProject> {
    const form = new FormData();
    form.append('file', file, file.name);
    return this.http.post<ApiWrapper<SplitProject>>(`${environment.apiUrl}/api/split/projects`, form, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Could not upload video');
        return res.data;
      }),
      tap((p) => this._projects.update((list) => [p, ...list.filter((x) => x.id !== p.id)])),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  get(id: number): Observable<SplitProject> {
    return this.http.get<ApiWrapper<SplitProject>>(`${environment.apiUrl}/api/split/projects/${id}`, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Project not found');
        return res.data;
      }),
      tap((p) => this.upsert(p)),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  cut(id: number, threshold?: number): Observable<SplitProject> {
    const body = threshold != null ? { threshold } : {};
    return this.http.post<ApiWrapper<SplitProject>>(`${environment.apiUrl}/api/split/projects/${id}/cut`, body, {
      headers: this.authHeaders(),
    }).pipe(
      map((res) => {
        if (!res.data) throw new Error(res.message || 'Cut failed');
        return res.data;
      }),
      tap((p) => this.upsert(p)),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  delete(id: number): Observable<void> {
    return this.http.delete<ApiWrapper<unknown>>(`${environment.apiUrl}/api/split/projects/${id}`, {
      headers: this.authHeaders(),
    }).pipe(
      map(() => undefined),
      tap(() => this._projects.update((list) => list.filter((p) => p.id !== id))),
      catchError((err) => throwError(() => this.toError(err))),
    );
  }

  private upsert(p: SplitProject): void {
    this._projects.update((list) => {
      const i = list.findIndex((x) => x.id === p.id);
      if (i < 0) return [p, ...list];
      const next = list.slice();
      next[i] = { ...next[i], ...p };
      return next;
    });
  }

  private authHeaders(): HttpHeaders {
    const token = this.authToken.getToken();
    return token ? new HttpHeaders({ Authorization: `Bearer ${token}` }) : new HttpHeaders();
  }

  private toError(err: unknown): Error {
    if (err instanceof HttpErrorResponse) {
      const msg = (err.error as { message?: string } | null)?.message;
      return new Error(msg || err.message || 'Request failed');
    }
    if (err instanceof Error) return err;
    return new Error('Request failed');
  }
}
