import { Injectable, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { ConfirmDialogService } from './confirm-dialog.service';

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

/**
 * Quit / Restart the local Instaboard process from the UI (footer controls).
 */
@Injectable({ providedIn: 'root' })
export class AppLifecycleService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private confirm = inject(ConfirmDialogService);

  readonly busy = signal(false);
  /** True after a successful Quit — show “stopped” overlay. */
  readonly quitDone = signal(false);
  /** True after Restart was requested — show “restarting” until the new server is up. */
  readonly restarting = signal(false);

  async quit(): Promise<void> {
    if (this.busy()) return;
    const ok = await this.confirm.ask({
      title: 'Quit Instaboard?',
      message: 'This stops the app — same as pressing Ctrl+C in Terminal. Open Instaboard again anytime to come back.',
      confirmLabel: 'Quit',
      danger: true,
    });
    if (!ok) return;
    this.busy.set(true);
    this.post('quit').subscribe({
      next: () => {
        this.busy.set(false);
        this.quitDone.set(true);
      },
      error: () => {
        this.busy.set(false);
        this.quitDone.set(true);
      },
    });
  }

  async restart(): Promise<void> {
    if (this.busy()) return;
    const ok = await this.confirm.ask({
      title: 'Restart Instaboard?',
      message: 'Instaboard will stop briefly, then start again and reopen in your browser.',
      confirmLabel: 'Restart',
    });
    if (!ok) return;
    this.busy.set(true);
    this.restarting.set(true);
    this.post('restart').subscribe({
      next: () => {
        this.busy.set(false);
        this.waitForServer();
      },
      error: () => {
        this.busy.set(false);
        this.waitForServer();
      },
    });
  }

  private post(action: 'quit' | 'restart'): Observable<void> {
    const headers = this.authHeaders();
    if (!headers) return throwError(() => new Error('Please wait for Instaboard to connect.'));
    return this.http.post<ApiWrapper<unknown>>(`${environment.apiUrl}/api/${action}`, null, { headers }).pipe(
      map(() => undefined),
      catchError((err: unknown) => {
        if (err instanceof HttpErrorResponse && (err.status === 0 || err.status === 502 || err.status === 504)) {
          return of(undefined);
        }
        return throwError(() => err);
      })
    );
  }

  /** Poll health until the relaunched process is listening, then reload this tab. */
  private waitForServer(): void {
    const started = Date.now();
    const tick = () => {
      fetch(`${environment.apiUrl}/health`, { cache: 'no-store' })
        .then((r) => {
          if (r.ok) {
            window.location.reload();
            return;
          }
          retry();
        })
        .catch(() => retry());
    };
    const retry = () => {
      if (Date.now() - started > 20000) {
        this.restarting.set(false);
        this.quitDone.set(true);
        return;
      }
      setTimeout(tick, 400);
    };
    setTimeout(tick, 600);
  }

  private authHeaders(): HttpHeaders | null {
    const token = this.authToken.getToken();
    return token ? new HttpHeaders({ Authorization: `Bearer ${token}` }) : null;
  }
}
