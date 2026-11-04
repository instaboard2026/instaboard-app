import { Injectable, computed, inject, signal } from '@angular/core';
import { DownloadsInfo, DumpService } from './dump.service';

/**
 * Download folder settings (footer "Downloads" popup). Changing the folder reloads the Dump so
 * Downloaded / Not downloaded reflect the new folder.
 */
@Injectable({ providedIn: 'root' })
export class DownloadSettingsService {
  private dumpService = inject(DumpService);

  readonly info = signal<DownloadsInfo | null>(null);
  readonly choosing = signal(false);
  readonly message = signal<string | null>(null);

  /** True when something needs the user's attention (missing folder or tools). */
  readonly needsAttention = computed(() => {
    const i = this.info();
    return !!i && (!i.exists || !i.tools_available);
  });

  /** Last path segment, for the footer button label. */
  readonly folderName = computed(() => this.info()?.dir.split('/').filter(Boolean).pop() ?? null);

  load(): void {
    this.dumpService.downloadsInfo().subscribe({ next: (info) => this.info.set(info), error: () => {} });
  }

  change(): void {
    if (this.choosing()) return;
    this.choosing.set(true);
    this.message.set(null);
    this.dumpService.chooseDownloadFolder().subscribe({
      next: (info) => this.changed(info),
      error: (err: Error) => {
        this.choosing.set(false);
        if (err.message !== 'No folder was chosen') this.message.set(err.message);
      },
    });
  }

  useDefault(): void {
    this.message.set(null);
    this.dumpService.resetDownloadFolder().subscribe({
      next: (info) => this.changed(info),
      error: (err: Error) => this.message.set(err.message),
    });
  }

  clear(): void {
    this.info.set(null);
    this.message.set(null);
  }

  private changed(info: DownloadsInfo): void {
    this.choosing.set(false);
    this.info.set(info);
    this.dumpService.load().subscribe({ error: () => {} });
  }
}
