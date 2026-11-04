import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { AuthTokenService } from './auth-token.service';
import { environment } from 'environments';

export interface UpdateInfo {
  current: string;
  latest: string;
  update_available: boolean;
  download_url?: string;
  notes?: string;
  repo_url?: string;
  releases_url?: string;
  release_url?: string;
  published_at?: string;
  checked_at?: string;
  skipped?: boolean;
}

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

/**
 * Checks GitHub Releases via the local API and downloads Instaboard-mac.zip to ~/Downloads.
 */
@Injectable({ providedIn: 'root' })
export class UpdateService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);

  readonly info = signal<UpdateInfo | null>(null);
  readonly downloading = signal(false);
  readonly downloadedPath = signal<string | null>(null);
  readonly message = signal<string | null>(null);

  /** True when a newer GitHub Release exists (footer badge). */
  readonly updateAvailable = computed(() => {
    const i = this.info();
    return !!i && i.update_available && !i.skipped;
  });

  /** Footer chip label. */
  readonly chipLabel = computed(() => {
    const i = this.info();
    if (!i) return 'Updates';
    if (i.skipped) return 'Up to date';
    if (i.update_available) return 'Update available';
    return 'Up to date';
  });

  check(): void {
    const headers = this.authHeaders();
    if (!headers) return;
    this.http.get<ApiWrapper<UpdateInfo>>(`${environment.apiUrl}/api/update`, { headers }).subscribe({
      next: (res) => {
        if (res?.status !== 'SUCCESS' || !res.data) return;
        this.info.set(res.data);
        this.downloadedPath.set(null);
        this.message.set(null);
      },
      error: () => {},
    });
  }

  download(): void {
    if (this.downloading()) return;
    const headers = this.authHeaders();
    if (!headers) {
      this.message.set('Please wait for Instaboard to connect.');
      return;
    }
    this.downloading.set(true);
    this.message.set(null);
    this.http.post<ApiWrapper<{ path: string }>>(`${environment.apiUrl}/api/update/download`, null, { headers }).subscribe({
      next: (res) => {
        this.downloading.set(false);
        if (res?.status !== 'SUCCESS' || !res.data?.path) {
          this.message.set(res?.message || 'Could not download the update.');
          return;
        }
        this.downloadedPath.set(res.data.path);
      },
      error: (err: unknown) => {
        this.downloading.set(false);
        const message =
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Could not download the update.'
            : (err as Error)?.message || 'Could not download the update.';
        this.message.set(message);
      },
    });
  }

  private authHeaders(): HttpHeaders | null {
    const token = this.authToken.getToken();
    return token ? new HttpHeaders({ Authorization: `Bearer ${token}` }) : null;
  }
}
