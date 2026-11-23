import { Directive, ElementRef, OnDestroy, effect, inject, input } from '@angular/core';
import { MediaMemoryCacheService } from '../../services/media-memory-cache.service';

/**
 * Sets img[src] from the session media cache when near the viewport; clears the
 * element src when far away (blob stays cached for instant reuse).
 */
@Directive({
  selector: 'img[appLazyThumb]',
  standalone: true,
})
export class LazyThumbDirective implements OnDestroy {
  private el = inject(ElementRef<HTMLImageElement>).nativeElement;
  private cache = inject(MediaMemoryCacheService);
  readonly appLazyThumb = input<string | null>(null);

  private observer: IntersectionObserver | null = null;
  private visible = false;
  private gen = 0;

  constructor() {
    if (typeof IntersectionObserver !== 'undefined') {
      const scrollRoot = document.getElementById('app-scroll');
      this.observer = new IntersectionObserver(
        (entries) => {
          for (const e of entries) {
            this.visible = e.isIntersecting;
            this.sync();
          }
        },
        {
          root: scrollRoot instanceof Element ? scrollRoot : null,
          rootMargin: '200px 0px',
          threshold: 0.01,
        },
      );
      this.observer.observe(this.el);
    } else {
      this.visible = true;
    }

    effect(() => {
      this.appLazyThumb();
      this.sync();
    });
  }

  ngOnDestroy(): void {
    this.observer?.disconnect();
    this.gen++;
  }

  private sync(): void {
    const url = this.appLazyThumb();
    if (!this.visible || !url) {
      if (this.el.hasAttribute('src')) this.el.removeAttribute('src');
      return;
    }

    const cached = this.cache.peek(url);
    if (cached) {
      if (this.el.getAttribute('src') !== cached) this.el.src = cached;
      return;
    }

    const myGen = ++this.gen;
    this.cache
      .ensure(url)
      .then((objectUrl) => {
        if (myGen !== this.gen || !this.visible || this.appLazyThumb() !== url) return;
        this.el.src = objectUrl;
      })
      .catch(() => {
        if (myGen !== this.gen) return;
        // Fall back to direct URL so a cache/CORS glitch still shows the tile.
        this.el.src = url;
      });
  }
}
