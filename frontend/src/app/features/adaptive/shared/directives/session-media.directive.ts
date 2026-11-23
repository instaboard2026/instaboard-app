import { Directive, ElementRef, OnDestroy, effect, inject, input } from '@angular/core';
import { MediaMemoryCacheService } from '../../services/media-memory-cache.service';

/**
 * Loads video (or audio) through the session blob cache so revisiting the same
 * signed path does not re-download from disk/SD.
 */
@Directive({
  selector: 'video[appSessionMedia], audio[appSessionMedia]',
  standalone: true,
})
export class SessionMediaDirective implements OnDestroy {
  private el = inject(ElementRef<HTMLMediaElement>).nativeElement;
  private cache = inject(MediaMemoryCacheService);
  readonly appSessionMedia = input<string | null>(null);

  private gen = 0;

  constructor() {
    effect(() => {
      const url = this.appSessionMedia();
      this.gen++;
      const myGen = this.gen;
      if (!url) {
        this.el.removeAttribute('src');
        this.el.load();
        return;
      }
      const cached = this.cache.peek(url);
      if (cached) {
        if (this.el.getAttribute('src') !== cached) {
          this.el.src = cached;
          this.el.load();
        }
        return;
      }
      this.cache
        .ensure(url)
        .then((objectUrl) => {
          if (myGen !== this.gen || this.appSessionMedia() !== url) return;
          this.el.src = objectUrl;
          this.el.load();
        })
        .catch(() => {
          if (myGen !== this.gen) return;
          this.el.src = url;
          this.el.load();
        });
    });
  }

  ngOnDestroy(): void {
    this.gen++;
  }
}
