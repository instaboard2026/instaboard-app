/**
 * Downloads popup (opened from the footer): where downloads are saved, how files are named,
 * the Instagram login note, and Change / Use default for the folder.
 */
import { ChangeDetectionStrategy, Component, HostListener, inject, output } from '@angular/core';
import { DownloadSettingsService } from '../../../services/download-settings.service';

@Component({
  selector: 'app-download-settings-modal',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="fixed inset-0 z-[1000] flex items-center justify-center p-4" role="dialog" aria-modal="true" aria-labelledby="downloads-title">
      <div class="absolute inset-0 bg-black/60" (click)="close.emit()"></div>
      <div class="relative w-full max-w-lg rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-6 text-sm text-slate-700 dark:text-slate-300 shadow-xl">
        <div class="flex items-start justify-between gap-4">
          <h2 id="downloads-title" class="flex items-center gap-2 text-lg font-bold text-slate-900 dark:text-white">
            <span class="material-icons-outlined">folder</span>
            Downloads
          </h2>
          <button type="button" (click)="close.emit()" aria-label="Close" class="-m-1 p-1 rounded-lg text-slate-500 hover:bg-slate-100 dark:hover:bg-slate-800">
            <span class="material-icons-outlined">close</span>
          </button>
        </div>

        @if (settings.info(); as info) {
          <p class="mt-4 text-xs font-semibold uppercase tracking-wide text-slate-500 dark:text-slate-400">Saving to</p>
          <p class="mt-1 rounded-lg bg-slate-100 dark:bg-slate-800 px-3 py-2 font-mono text-[13px] text-slate-900 dark:text-slate-100 break-all">{{ info.dir }}</p>
          @if (!info.exists) {
            <p class="mt-2 text-amber-700 dark:text-amber-400">This folder was renamed, moved or deleted. Choose a folder to keep downloading.</p>
          }

          <div class="mt-3 flex flex-wrap items-center gap-2">
            @if (info.picker_available) {
              <button type="button" (click)="settings.change()" [disabled]="settings.choosing()" class="px-4 py-2 rounded-lg bg-blue-600 hover:bg-blue-700 text-white font-semibold disabled:opacity-60">
                {{ settings.choosing() ? 'Waiting for you to pick…' : 'Choose folder' }}
              </button>
            }
            @if (!info.is_default) {
              <button type="button" (click)="settings.useDefault()" [disabled]="settings.choosing()" class="px-4 py-2 rounded-lg border border-slate-300 dark:border-slate-600 font-semibold hover:bg-slate-100 dark:hover:bg-slate-800 disabled:opacity-60">Use default</button>
            }
          </div>
          @if (settings.choosing()) {
            <p class="mt-2 text-xs text-slate-500 dark:text-slate-400">A "Choose a folder" window opened on this computer. If you don't see it, check behind the browser.</p>
          }
          @if (settings.message()) {
            <p class="mt-2 text-red-600 dark:text-red-400" role="alert">{{ settings.message() }}</p>
          }

          <ul class="mt-5 space-y-2 border-t border-slate-200 dark:border-slate-700 pt-4 text-[13px]">
            <li class="flex gap-2"><span class="material-icons-outlined !text-base text-slate-400">movie</span><span>Reels save as <code class="font-mono">&lt;id&gt;.mp4</code>.</span></li>
            <li class="flex gap-2"><span class="material-icons-outlined !text-base text-slate-400">collections</span><span>Carousels save as a <code class="font-mono">&lt;id&gt;/</code> folder with <code class="font-mono">1.jpg</code>, <code class="font-mono">2.mp4</code>, … in post order.</span></li>
            <li class="flex gap-2"><span class="material-icons-outlined !text-base text-slate-400">swap_horiz</span><span>Changing the folder applies right away. Files already downloaded stay where they are; move them into the new folder to keep them marked as downloaded.</span></li>
            @if (info.cookies_browser && info.cookies_browser !== 'none') {
              <li class="flex gap-2"><span class="material-icons-outlined !text-base text-slate-400">login</span><span>Uses your Instagram login from {{ info.cookies_browser }} on this computer. Stay logged into instagram.com there. The first download may ask for your Mac password to read it (choose Always Allow).</span></li>
            }
          </ul>

          @if (!info.tools_available) {
            <p class="mt-4 rounded-lg bg-amber-50 dark:bg-amber-900/20 px-3 py-2 text-amber-800 dark:text-amber-300">
              Download tools aren't installed. Run <code class="font-mono">brew install gallery-dl yt-dlp ffmpeg</code>, then restart the backend.
            </p>
          }
        } @else {
          <p class="mt-4 text-slate-500 dark:text-slate-400">Loading download settings…</p>
        }
      </div>
    </div>
  `,
})
export class DownloadSettingsModalComponent {
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
