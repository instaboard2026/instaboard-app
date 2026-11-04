import { Injectable, computed, inject, signal } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { AuthTokenService } from './auth-token.service';
import { AppLifecycleService } from './app-lifecycle.service';
import { ConfirmDialogService } from './confirm-dialog.service';
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
 * Checks GitHub Releases via the local API, downloads Instaboard-mac.zip, or applies + restarts.
 */
@Injectable({ providedIn: 'root' })
export class UpdateService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private lifecycle = inject(AppLifecycleService);
  private confirm = inject(ConfirmDialogService);

  readonly info = signal<UpdateInfo | null>(null);
  readonly checking = signal(false);
  readonly downloading = signal(false);
  readonly applying = signal(false);
  readonly downloadedPath = signal<string | null>(null);
  readonly message = signal<string | null>(null);

  readonly busy = computed(() => this.checking() || this.downloading() || this.applying());

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

  /** @param refresh When true, bypasses the API cache (manual Check for updates). */
  check(refresh = false): void {
    const headers = this.authHeaders();
    if (!headers) return;
    if (refresh) this.checking.set(true);
    this.message.set(null);
    const url = refresh
      ? `${environment.apiUrl}/api/update?refresh=1`
      : `${environment.apiUrl}/api/update`;
    this.http.get<ApiWrapper<UpdateInfo>>(url, { headers }).subscribe({
      next: (res) => {
        this.checking.set(false);
        if (res?.status !== 'SUCCESS' || !res.data) {
          if (refresh) this.message.set(res?.message || 'Could not check for updates.');
          return;
        }
        this.info.set(res.data);
        this.downloadedPath.set(null);
      },
      error: (err: unknown) => {
        this.checking.set(false);
        if (!refresh) return;
        this.message.set(
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Could not check for updates.'
            : (err as Error)?.message || 'Could not check for updates.'
        );
      },
    });
  }

  download(): void {
    if (this.busy()) return;
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
        this.message.set(
          err instanceof HttpErrorResponse
            ? err.error?.message || 'Could not download the update.'
            : (err as Error)?.message || 'Could not download the update.'
        );
      },
    });
  }

  /** Download, replace the Instaboard binary, and restart into the new version. */
  async applyAndRestart(): Promise<void> {
    if (this.busy() || !this.updateAvailable()) return;
    const latest = this.info()?.latest ?? 'the latest version';
    const ok = await this.confirm.ask({
      title: `Update to ${latest}?`,
      message: 'Instaboard will download the update and restart. You should not need to Allow it again in macOS Settings — the app you already opened stays the same.',
      confirmLabel: 'Update and restart',
    });
    if (!ok) return;
    this.postApply(false, `Could not apply the update.`);
  }

  /**
   * Reinstall the latest GitHub release into ~/.instaboard/runtime even when already
   * on that version (repair / same-version test). No zip left for the user to open.
   */
  async reinstallLatest(): Promise<void> {
    if (this.busy()) return;
    const latest = this.info()?.latest ?? 'the latest release';
    const ok = await this.confirm.ask({
      title: `Reinstall ${latest}?`,
      message: 'Downloads the same release again and replaces the installed binary, then restarts. Use this to repair a broken install without fetching a zip yourself.',
      confirmLabel: 'Reinstall and restart',
    });
    if (!ok) return;
    this.postApply(true, `Could not reinstall.`);
  }

  private postApply(force: boolean, failMsg: string): void {
    const headers = this.authHeaders();
    if (!headers) {
      this.message.set('Please wait for Instaboard to connect.');
      return;
    }
    this.applying.set(true);
    this.message.set(null);
    const url = force
      ? `${environment.apiUrl}/api/update/apply?force=1`
      : `${environment.apiUrl}/api/update/apply`;
    this.http.post<ApiWrapper<{ path: string }>>(url, null, { headers }).subscribe({
      next: (res) => {
        this.applying.set(false);
        if (res?.status !== 'SUCCESS') {
          this.message.set(res?.message || failMsg);
          return;
        }
        this.lifecycle.beginRestartWait();
      },
      error: (err: unknown) => {
        this.applying.set(false);
        // Server may close during restart after a successful apply.
        if (err instanceof HttpErrorResponse && (err.status === 0 || err.status === 502 || err.status === 504)) {
          this.lifecycle.beginRestartWait();
          return;
        }
        this.message.set(
          err instanceof HttpErrorResponse
            ? err.error?.message || failMsg
            : (err as Error)?.message || failMsg
        );
      },
    });
  }

  private authHeaders(): HttpHeaders | null {
    const token = this.authToken.getToken();
    return token ? new HttpHeaders({ Authorization: `Bearer ${token}` }) : null;
  }
}
