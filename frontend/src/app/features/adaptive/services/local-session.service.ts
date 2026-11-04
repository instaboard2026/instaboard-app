import { Injectable, inject, signal } from '@angular/core';
import { HttpClient } from '@angular/common/http';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { UserContextService } from './user-context.service';

export type SessionStatus = 'connecting' | 'ready' | 'error';

/**
 * Instaboard runs on the user's own computer, so there is no sign-in: the local server issues a session
 * for its single user, named after the computer account (e.g. "Chandra Shekar").
 */
@Injectable({ providedIn: 'root' })
export class LocalSessionService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private userContext = inject(UserContextService);

  readonly status = signal<SessionStatus>('connecting');

  /** Asks the local server for a fresh session (a stored token may be from an older signing key). */
  start(): void {
    this.status.set('connecting');
    this.http.post<{ token: string }>(`${environment.apiUrl}/auth/local`, {}).subscribe({
      next: ({ token }) => {
        this.authToken.setToken(token);
        this.status.set(this.userContext.setUserFromToken(token) ? 'ready' : 'error');
      },
      error: () => this.status.set('error'),
    });
  }
}
