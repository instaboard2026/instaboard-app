/**
 * Tools popup (footer "Tools ready" / "Install tools"): gallery-dl, yt-dlp, ffmpeg status,
 * install, reinstall and uninstall.
 */
import { ChangeDetectionStrategy, Component, HostListener, inject, output } from '@angular/core';
import { DownloadSettingsService } from '../../../services/download-settings.service';

@Component({
  selector: 'app-tools-settings-modal',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="fixed inset-0 z-[1000] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="tools-title">
      <div class="absolute inset-0 bg-black/60" (click)="close.emit()"></div>
      <div class="relative w-full max-w-md rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-6 text-sm text-slate-700 dark:text-slate-300 shadow-xl">
        <div class="flex items-start justify-between gap-4">
          <h2 id="tools-title" class="flex items-center gap-2 text-lg font-bold text-slate-900 dark:text-white">
            <span class="material-icons-outlined">build</span>
            Download tools
          </h2>
          <button type="button" (click)="close.emit()" aria-label="Close" class="-m-1 p-1 rounded-lg text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800">
            <span class="material-icons-outlined">close</span>
          </button>
        </div>

        <p class="mt-3 text-[13px] text-slate-600 dark:text-slate-400">
          Instaboard uses <code class="font-mono text-[12px]">gallery-dl</code>, <code class="font-mono text-[12px]">yt-dlp</code> and
          <code class="font-mono text-[12px]">ffmpeg</code> on this Mac to fetch reels, carousels and videos.
        </p>

        @if (settings.info(); as info) {
          @if (info.tools_available) {
            <div class="mt-4 flex items-start gap-3 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 px-3 py-3 text-emerald-800 dark:text-emerald-300">
              <span class="material-icons-outlined !text-xl shrink-0">check_circle</span>
              <div>
                <p class="font-semibold">Tools are installed</p>
                <p class="mt-0.5 text-[13px] opacity-90">You're ready to download. Reinstall to refresh, or uninstall to turn downloads off in Instaboard.</p>
              </div>
            </div>
            <div class="mt-4 flex flex-wrap gap-2">
              <button
                type="button"
                (click)="settings.installTools()"
                [disabled]="settings.toolsBusy()"
                class="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-semibold disabled:opacity-60">
                {{ settings.installing() ? 'Reinstalling…' : 'Reinstall' }}
              </button>
              <button
                type="button"
                (click)="settings.uninstallTools()"
                [disabled]="settings.toolsBusy()"
                class="px-4 py-2 rounded-lg border border-slate-300 dark:border-slate-600 font-semibold hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-60">
                {{ settings.uninstalling() ? 'Uninstalling…' : 'Uninstall' }}
              </button>
            </div>
          } @else {
            <div class="mt-4 rounded-lg bg-amber-50 dark:bg-amber-900/20 px-3 py-3 text-amber-800 dark:text-amber-300 space-y-3">
              <p class="font-semibold">Tools aren't installed</p>
              <p class="text-[13px] opacity-90">Instaboard can install them on this Mac. It may take a few minutes the first time.</p>
              <button
                type="button"
                (click)="settings.installTools()"
                [disabled]="settings.toolsBusy()"
                class="px-4 py-2 rounded-lg bg-amber-600 hover:bg-amber-700 text-white font-semibold disabled:opacity-60">
                {{ settings.installing() ? 'Installing…' : 'Install download tools' }}
              </button>
            </div>
          }
          @if (settings.message()) {
            <p class="mt-3 text-red-600 dark:text-red-400" role="alert">{{ settings.message() }}</p>
          }
        } @else {
          <p class="mt-4 text-slate-500 dark:text-slate-400">Checking tools…</p>
        }
      </div>
    </div>
  `,
})
export class ToolsSettingsModalComponent {
  readonly settings = inject(DownloadSettingsService);
  readonly close = output<void>();

  constructor() {
    this.settings.load();
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    this.close.emit();
  }
}
