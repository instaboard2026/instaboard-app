import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, tap, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';

/** A camera card file moved to the Mac's Trash from Manage camera. */
export interface CardTrashItem {
  id: number;
  name: string;
  kind: 'image' | 'video' | 'junk';
  byte_size: number;
  trashed_at: string;
  /** Card plugged in, so Put back / Delete forever work. */
  card_in: boolean;
  thumb_url?: string;
  /** Streams the file itself, for preview / play (card in only). */
  url?: string;
}

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

/** Card files in the Bin (they sit in the card's own Trash until put back or deleted). */
@Injectable({ providedIn: 'root' })
export class CardTrashService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);

  readonly items = signal<CardTrashItem[]>([]);
  readonly count = computed(() => this.items().length);

  load(): Observable<CardTrashItem[]> {
    return this.request<CardTrashItem[]>('GET', '/api/camera/trash').pipe(tap((list) => this.items.set(list ?? [])));
  }

  restore(item: CardTrashItem): Observable<void> {
    return this.request<unknown>('POST', `/api/camera/trash/${item.id}/restore`).pipe(
      tap(() => this.drop(item.id)),
      map(() => undefined),
    );
  }

  deleteForever(item: CardTrashItem): Observable<void> {
    return this.request<unknown>('DELETE', `/api/camera/trash/${item.id}`).pipe(
      tap(() => this.drop(item.id)),
      map(() => undefined),
    );
  }

  private drop(id: number): void {
    this.items.update((list) => list.filter((i) => i.id !== id));
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
