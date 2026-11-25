import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, switchMap, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { CameraService } from './camera.service';
import { DumpService, MoveTarget } from './dump.service';
import { FoldersService } from './folders.service';

export interface CameraCopyJob {
  id: string;
  status: 'pending' | 'running' | 'done' | 'error';
  total: number;
  done: number;
  copied: number;
  skipped: number;
  failed: number;
  bytes_done: number;
  copied_names: string[] | null;
  message?: string;
}

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

/** Copies camera / SD card files into Home or a folder as cards, with a shared progress bar. */
@Injectable({ providedIn: 'root' })
export class CameraCopyService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private camera = inject(CameraService);
  private dump = inject(DumpService);
  private folders = inject(FoldersService);

  /** Live copy job for the progress bar (null when idle). */
  readonly activeCopy = signal<CameraCopyJob | null>(null);
  readonly copying = computed(() => {
    const j = this.activeCopy();
    return !!j && (j.status === 'pending' || j.status === 'running');
  });
  readonly copyPercent = computed(() => {
    const j = this.activeCopy();
    if (!j || j.total <= 0) return 0;
    return Math.min(100, Math.round((100 * j.done) / j.total));
  });

  private timer: ReturnType<typeof setTimeout> | null = null;

  /** Starts a copy into `target` and polls until done; emits the final job. */
  copy(target: MoveTarget, relPaths: string[]): Observable<CameraCopyJob> {
    if (this.copying() || !relPaths.length) {
      return throwError(() => new Error(this.copying() ? 'A copy is already running.' : 'Nothing to copy.'));
    }
    this.activeCopy.set({
      id: '', status: 'pending', total: relPaths.length, done: 0, copied: 0, skipped: 0, failed: 0, bytes_done: 0, copied_names: [],
    });
    const folder_id = target === 'dump' ? null : target;
    return this.request<CameraCopyJob>('POST', '/api/camera/copy', { rel_paths: relPaths, folder_id }).pipe(
      tap((job) => this.activeCopy.set(job)),
      switchMap((job) => this.wait(job.id, target)),
      catchError((err: Error) => {
        this.activeCopy.set(null);
        return throwError(() => err);
      }),
    );
  }

  /** Short summary of a finished job for a notice. */
  summary(job: CameraCopyJob, targetName: string): string {
    const parts: string[] = [];
    if (job.copied) parts.push(`Copied ${job.copied} to ${targetName}`);
    if (job.skipped) parts.push(`skipped ${job.skipped} already copied`);
    if (job.failed) parts.push(`${job.failed} failed`);
    return parts.join(' · ') || 'Nothing copied.';
  }

  private wait(jobId: string, target: MoveTarget): Observable<CameraCopyJob> {
    return new Observable<CameraCopyJob>((subscriber) => {
      const tick = () => {
        this.request<CameraCopyJob>('GET', `/api/camera/copy-jobs/${jobId}`).subscribe({
          next: (job) => {
            this.activeCopy.set(job);
            if (job.copied_names?.length) this.camera.markCopied(job.copied_names);
            if (job.status === 'done' || job.status === 'error') {
              if (job.copied > 0) {
                this.folders.noteAddedSaves(target, job.copied);
                // Refresh that board if it is already loaded so the new cards appear.
                if (this.dump.isLoaded(target)) this.dump.load(target).subscribe({ error: () => {} });
              }
              subscriber.next(job);
              subscriber.complete();
              this.timer = setTimeout(() => this.activeCopy.set(null), 900);
              return;
            }
            this.timer = setTimeout(tick, 400);
          },
          error: (err: Error) => {
            this.activeCopy.set(null);
            subscriber.error(err);
          },
        });
      };
      tick();
      return () => {
        if (this.timer) clearTimeout(this.timer);
      };
    });
  }

  private request<T>(method: string, path: string, body?: unknown): Observable<T> {
    const token = this.authToken.getToken();
    if (!token) return throwError(() => new Error('Please sign in.'));
    const headers = new HttpHeaders({ Authorization: `Bearer ${token}` });
    return this.http.request<ApiWrapper<T>>(method, `${environment.apiUrl}${path}`, { headers, body }).pipe(
      map((res) => {
        if (res?.status !== 'SUCCESS') throw new Error(res?.message || 'Something went wrong.');
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
