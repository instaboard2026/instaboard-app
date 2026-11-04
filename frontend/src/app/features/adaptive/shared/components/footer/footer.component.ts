import { Component, ChangeDetectionStrategy, effect, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ThemeToggleComponent } from '../theme-toggle/theme-toggle.component';
import { PlaybackService } from '../../../services/playback.service';
import { DownloadSettingsService } from '../../../services/download-settings.service';
import { UserContextService } from '../../../services/user-context.service';
import { DownloadSettingsModalComponent } from '../download-settings-modal/download-settings-modal.component';

@Component({
  selector: 'app-footer',
  standalone: true,
  imports: [CommonModule, ThemeToggleComponent, DownloadSettingsModalComponent],
  templateUrl: './footer.component.html',
  styles: [':host { display: contents }'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class FooterComponent {
  readonly playback = inject(PlaybackService);
  readonly downloads = inject(DownloadSettingsService);
  readonly isLoggedIn = inject(UserContextService).isLoggedIn;
  readonly downloadsOpen = signal(false);

  constructor() {
    effect(() => (this.isLoggedIn() ? this.downloads.load() : this.downloads.clear()));
  }
}
