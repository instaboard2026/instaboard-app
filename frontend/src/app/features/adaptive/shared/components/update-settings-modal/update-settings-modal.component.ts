/**
 * App update popup (footer chip): check GitHub Releases, download zip, or update-and-restart.
 */
import { ChangeDetectionStrategy, Component, HostListener, inject, output } from '@angular/core';
import { DatePipe } from '@angular/common';
import { UpdateService } from '../../../services/update.service';

const FALLBACK_RELEASES = 'https://github.com/instaboard2026/instaboard-app/releases';

@Component({
  selector: 'app-update-settings-modal',
  standalone: true,
  imports: [DatePipe],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="fixed inset-0 z-[1000] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="update-title">
      <div class="absolute inset-0 bg-black/60" (click)="onBackdrop()"></div>
      <div class="relative w-full max-w-md rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-6 text-sm text-slate-700 dark:text-slate-300 shadow-xl">
        <div class="flex items-start justify-between gap-4">
          <h2 id="update-title" class="flex items-center gap-2 text-lg font-bold text-slate-900 dark:text-white">
            <span class="material-icons-outlined">system_update</span>
            App update
          </h2>
          <button
            type="button"
            (click)="close.emit()"
            [disabled]="updates.busy()"
            aria-label="Close"
            class="-m-1 p-1 rounded-lg text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-40">
            <span class="material-icons-outlined">close</span>
          </button>
        </div>

        @if (updates.info(); as info) {
          <a
            [href]="info.releases_url || fallbackReleases"
            target="_blank"
            rel="noopener noreferrer"
            class="mt-4 flex items-start gap-3 rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-950/40 px-3 py-3 text-blue-900 dark:text-blue-100 hover:bg-blue-100 dark:hover:bg-blue-950/70 transition-colors">
            <span class="material-icons-outlined !text-xl shrink-0">download</span>
            <div class="min-w-0">
              <p class="font-semibold">Download from GitHub Releases</p>
              <p class="mt-0.5 text-[12px] break-all opacity-90">{{ info.releases_url || fallbackReleases }}</p>
              <p class="mt-1 text-[12px] opacity-80">Get <code class="font-mono">Instaboard-mac.zip</code> from the latest release.</p>
            </div>
            <span class="material-icons-outlined !text-base shrink-0 ml-auto opacity-70">open_in_new</span>
          </a>

          <dl class="mt-4 grid grid-cols-[auto_1fr] gap-x-3 gap-y-2 text-[13px]">
            <dt class="text-slate-500 dark:text-slate-400">Your version</dt>
            <dd class="font-mono font-semibold text-slate-900 dark:text-slate-100">{{ info.current || '—' }}</dd>
            <dt class="text-slate-500 dark:text-slate-400">Latest release</dt>
            <dd class="font-mono text-slate-900 dark:text-slate-100">
              @if (info.release_url) {
                <a [href]="info.release_url" target="_blank" rel="noopener noreferrer" class="text-blue-600 dark:text-blue-400 hover:underline">
                  {{ info.latest || '—' }}
                </a>
              } @else {
                {{ info.latest || '—' }}
              }
            </dd>
            @if (info.published_at) {
              <dt class="text-slate-500 dark:text-slate-400">Last updated</dt>
              <dd class="text-slate-900 dark:text-slate-100">{{ info.published_at | date: 'mediumDate' }}</dd>
            }
          </dl>

          <div class="mt-4 flex flex-wrap gap-2">
            <button
              type="button"
              (click)="updates.check(true)"
              [disabled]="updates.busy()"
              class="px-4 py-2 rounded-lg border border-slate-300 dark:border-slate-600 font-semibold hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-60">
              {{ updates.checking() ? 'Checking…' : 'Check for updates' }}
            </button>
            @if (!info.skipped && updates.updateAvailable()) {
              <button
                type="button"
                (click)="updates.applyAndRestart()"
                [disabled]="updates.busy()"
                class="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-semibold disabled:opacity-60">
                {{ updates.applying() ? 'Updating…' : 'Update and restart' }}
              </button>
            }
          </div>

          @if (updates.applying()) {
            <div class="mt-4 rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-950/40 px-3 py-3 text-blue-900 dark:text-blue-100" role="status">
              <p class="font-semibold">Downloading and installing {{ info.latest }}…</p>
              <p class="mt-1 text-[13px] opacity-90">Installing under ~/.instaboard/runtime — no new macOS Allow prompt.</p>
            </div>
          }

          @if (info.skipped) {
            <div class="mt-4 flex items-start gap-3 rounded-lg bg-slate-100 dark:bg-slate-800 px-3 py-3">
              <span class="material-icons-outlined !text-xl shrink-0 text-slate-500">info</span>
              <p class="text-[13px]">This is a development build ({{ info.current }}). Use the Releases link above for the Mac zip.</p>
            </div>
          } @else if (updates.updateAvailable()) {
            <div class="mt-4 flex items-start gap-3 rounded-lg bg-amber-50 dark:bg-amber-900/20 px-3 py-3 text-amber-800 dark:text-amber-300">
              <span class="material-icons-outlined !text-xl shrink-0">new_releases</span>
              <div>
                <p class="font-semibold">Instaboard {{ info.latest }} is available</p>
                @if (info.notes) {
                  <p class="mt-1 text-[13px] line-clamp-4 opacity-90">{{ info.notes }}</p>
                }
              </div>
            </div>

            @if (updates.downloadedPath(); as path) {
              <div class="mt-4 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 px-3 py-3 text-emerald-800 dark:text-emerald-300 space-y-1">
                <p class="font-semibold">Saved to Downloads</p>
                <p class="font-mono text-[12px] break-all opacity-90">{{ path }}</p>
                <p class="text-[13px] opacity-90">Or use <strong>Update and restart</strong> to apply automatically.</p>
              </div>
            } @else if (!updates.applying()) {
              <div class="mt-3">
                <button
                  type="button"
                  (click)="updates.download()"
                  [disabled]="updates.busy()"
                  class="text-[13px] font-medium text-blue-600 dark:text-blue-400 hover:underline disabled:opacity-60">
                  {{ updates.downloading() ? 'Downloading zip…' : 'Download zip only' }}
                </button>
              </div>
            }
          } @else {
            <div class="mt-4 flex items-start gap-3 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 px-3 py-3 text-emerald-800 dark:text-emerald-300">
              <span class="material-icons-outlined !text-xl shrink-0">check_circle</span>
              <div>
                <p class="font-semibold">You're on the latest version</p>
                <p class="mt-0.5 text-[13px] opacity-90">No update needed right now.</p>
              </div>
            </div>
          }

          @if (updates.message()) {
            <p class="mt-3 text-red-600 dark:text-red-400" role="alert">{{ updates.message() }}</p>
          }
        } @else {
          <p class="mt-4 text-slate-500 dark:text-slate-400">Checking for updates…</p>
        }
      </div>
    </div>
  `,
})
export class UpdateSettingsModalComponent {
  readonly updates = inject(UpdateService);
  readonly close = output<void>();
  readonly fallbackReleases = FALLBACK_RELEASES;

  constructor() {
    this.updates.check();
  }

  onBackdrop(): void {
    if (!this.updates.busy()) this.close.emit();
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (!this.updates.busy()) this.close.emit();
  }
}
