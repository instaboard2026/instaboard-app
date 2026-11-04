import { Injectable, effect, inject, signal } from '@angular/core';
import { CacheService } from './cache.service';

const AUTOPLAY_KEY = 'videos_autoplay';

/** App-wide video playback preference (one switch for every video), remembered per browser. */
@Injectable({ providedIn: 'root' })
export class PlaybackService {
  private cacheService = inject(CacheService);

  readonly autoplay = signal(this.cacheService.getItem<boolean>(AUTOPLAY_KEY) ?? true);
  /** True while a card is being dragged: videos pause so the drag stays smooth. */
  readonly dragPause = signal(false);

  constructor() {
    effect(() => this.cacheService.setItem(AUTOPLAY_KEY, this.autoplay()));
  }

  toggleAutoplay(): void {
    this.autoplay.update((on) => !on);
  }
}
