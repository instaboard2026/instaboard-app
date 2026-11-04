/**
 * Tools popup (footer "Tools ready" / "Install tools"): gallery-dl, yt-dlp, ffmpeg status,
 * install, reinstall and uninstall.
 */
import {
  ChangeDetectionStrategy,
  Component,
  HostListener,
  OnDestroy,
  effect,
  inject,
  output,
  signal,
  untracked,
} from '@angular/core';
import { DownloadSettingsService } from '../../../services/download-settings.service';

const INSTALL_HINTS = [
  'Fetching gallery-dl, yt-dlp and ffmpeg…',
  'This can take a few minutes the first time…',
  'Almost there — linking tools for Instaboard…',
];

@Component({
  selector: 'app-tools-settings-modal',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  styles: [
    `
      .tools-progress-track {
        height: 6px;
        overflow: hidden;
        border-radius: 9999px;
        background: rgb(226 232 240);
      }
      :host-context(.dark) .tools-progress-track {
        background: rgb(51 65 85);
      }
      .tools-progress-bar {
        height: 100%;
        width: 40%;
        border-radius: 9999px;
        background: linear-gradient(90deg, #2563eb, #38bdf8);
        animation: tools-progress-slide 1.2s ease-in-out infinite;
      }
      @keyframes tools-progress-slide {
        0% {
          transform: translateX(-120%);
        }
        100% {
          transform: translateX(320%);
        }
      }
    `,
  ],
  template: `
    <div class="fixed inset-0 z-[1000] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="tools-title">
      <div class="absolute inset-0 bg-black/60" (click)="onBackdrop()"></div>
      <div class="relative w-full max-w-md rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-6 text-sm text-slate-700 dark:text-slate-300 shadow-xl">
        <div class="flex items-start justify-between gap-4">
          <h2 id="tools-title" class="flex items-center gap-2 text-lg font-bold text-slate-900 dark:text-white">
            <span class="material-icons-outlined">build</span>
            Download tools
          </h2>
          <button
            type="button"
            (click)="close.emit()"
            [disabled]="settings.toolsBusy()"
            aria-label="Close"
            class="-m-1 p-1 rounded-lg text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-40">
            <span class="material-icons-outlined">close</span>
          </button>
        </div>

        <p class="mt-3 text-[13px] text-slate-600 dark:text-slate-400">
          Instaboard uses <code class="font-mono text-[12px]">gallery-dl</code>, <code class="font-mono text-[12px]">yt-dlp</code> and
          <code class="font-mono text-[12px]">ffmpeg</code> on this Mac to fetch reels, carousels and videos.
        </p>

        @if (settings.toolsBusy()) {
          <div class="mt-4 rounded-lg border border-blue-200 dark:border-blue-800 bg-blue-50 dark:bg-blue-950/40 px-3 py-3" role="status" aria-live="polite">
            <div class="flex items-center gap-2 text-blue-900 dark:text-blue-100">
              <span class="material-icons-outlined !text-xl animate-spin">progress_activity</span>
              <p class="font-semibold">
                {{ settings.uninstalling() ? 'Uninstalling tools…' : (settings.info()?.tools_available ? 'Reinstalling tools…' : 'Installing tools…') }}
              </p>
            </div>
            <div class="tools-progress-track mt-3" aria-hidden="true">
              <div class="tools-progress-bar"></div>
            </div>
            <p class="mt-2 text-[12px] text-blue-800/80 dark:text-blue-200/80">
              {{ settings.uninstalling() ? 'Turning downloads off in Instaboard…' : hint() }}
            </p>
          </div>
        }

        @if (settings.info(); as info) {
          @if (info.tools_available) {
            @if (!settings.toolsBusy()) {
              <div class="mt-4 flex items-start gap-3 rounded-lg bg-emerald-50 dark:bg-emerald-900/20 px-3 py-3 text-emerald-800 dark:text-emerald-300">
                <span class="material-icons-outlined !text-xl shrink-0">check_circle</span>
                <div>
                  <p class="font-semibold">Tools are installed</p>
                  <p class="mt-0.5 text-[13px] opacity-90">You're ready to download. Reinstall to refresh, or uninstall to turn downloads off in Instaboard.</p>
                </div>
              </div>
            }
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
            @if (!settings.toolsBusy()) {
              <div class="mt-4 rounded-lg bg-amber-50 dark:bg-amber-900/20 px-3 py-3 text-amber-800 dark:text-amber-300 space-y-3">
                <p class="font-semibold">Tools aren't installed</p>
                <p class="text-[13px] opacity-90">Instaboard can install them on this Mac. It may take a few minutes the first time.</p>
              </div>
            }
            <div class="mt-4">
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
        } @else if (!settings.toolsBusy()) {
          <p class="mt-4 text-slate-500 dark:text-slate-400">Checking tools…</p>
        }
      </div>
    </div>
  `,
})
export class ToolsSettingsModalComponent implements OnDestroy {
  readonly settings = inject(DownloadSettingsService);
  readonly close = output<void>();
  readonly hint = signal(INSTALL_HINTS[0]);

  private hintTimer: ReturnType<typeof setInterval> | null = null;

  constructor() {
    this.settings.load();
    effect(() => {
      const installing = this.settings.installing();
      untracked(() => (installing ? this.startHints() : this.stopHints()));
    });
  }

  ngOnDestroy(): void {
    this.stopHints();
  }

  onBackdrop(): void {
    if (!this.settings.toolsBusy()) this.close.emit();
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (!this.settings.toolsBusy()) this.close.emit();
  }

  private startHints(): void {
    this.stopHints();
    let i = 0;
    this.hint.set(INSTALL_HINTS[0]);
    this.hintTimer = setInterval(() => {
      i = (i + 1) % INSTALL_HINTS.length;
      this.hint.set(INSTALL_HINTS[i]);
    }, 3500);
  }

  private stopHints(): void {
    if (this.hintTimer) {
      clearInterval(this.hintTimer);
      this.hintTimer = null;
    }
  }
}
