import { Component, ChangeDetectionStrategy, effect, inject, signal } from '@angular/core';
import { CommonModule } from '@angular/common';
import { ThemeToggleComponent } from '../theme-toggle/theme-toggle.component';
import { PlaybackService } from '../../../services/playback.service';
import { DownloadSettingsService } from '../../../services/download-settings.service';
import { UpdateService } from '../../../services/update.service';
import { AppLifecycleService } from '../../../services/app-lifecycle.service';
import { UserContextService } from '../../../services/user-context.service';
import { DownloadSettingsModalComponent } from '../download-settings-modal/download-settings-modal.component';
import { ToolsSettingsModalComponent } from '../tools-settings-modal/tools-settings-modal.component';
import { UpdateSettingsModalComponent } from '../update-settings-modal/update-settings-modal.component';

@Component({
  selector: 'app-footer',
  standalone: true,
  imports: [
    CommonModule,
    ThemeToggleComponent,
    DownloadSettingsModalComponent,
    ToolsSettingsModalComponent,
    UpdateSettingsModalComponent,
  ],
  templateUrl: './footer.component.html',
  styles: [':host { display: contents }'],
  changeDetection: ChangeDetectionStrategy.OnPush
})
export class FooterComponent {
  readonly playback = inject(PlaybackService);
  readonly downloads = inject(DownloadSettingsService);
  readonly updates = inject(UpdateService);
  readonly lifecycle = inject(AppLifecycleService);
  readonly isLoggedIn = inject(UserContextService).isLoggedIn;
  readonly downloadsOpen = signal(false);
  readonly toolsOpen = signal(false);
  readonly updateOpen = signal(false);

  constructor() {
    effect(() => (this.isLoggedIn() ? this.downloads.load() : this.downloads.clear()));
  }
}
