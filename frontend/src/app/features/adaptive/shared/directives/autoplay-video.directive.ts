import { Directive, ElementRef, effect, inject } from '@angular/core';
import { PlaybackService } from '../../services/playback.service';

/**
 * Plays or pauses a <video> following the app-wide Autoplay switch. Videos stay muted when started
 * automatically (browsers block unmuted autoplay); the user can still play/unmute with the controls.
 */
@Directive({
  selector: 'video[appAutoplayVideo]',
  standalone: true,
})
export class AutoplayVideoDirective {
  private video = inject<ElementRef<HTMLVideoElement>>(ElementRef).nativeElement;
  private playback = inject(PlaybackService);

  constructor() {
    effect(() => {
      if (this.playback.autoplay() && !this.playback.dragPause()) {
        this.video.muted = true;
        this.video.play().catch(() => {});
      } else {
        this.video.pause();
      }
    });
  }
}
