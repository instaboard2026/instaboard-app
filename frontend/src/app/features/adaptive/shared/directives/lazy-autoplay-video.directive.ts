import { Directive, ElementRef, OnDestroy, effect, inject, input } from '@angular/core';
import { PlaybackService } from '../../services/playback.service';

const MAX_CONCURRENT = 4;

/** Shared slots so the camera grid never decodes dozens of videos at once. */
class VideoPlaySlots {
  private active = new Set<HTMLVideoElement>();
  private waiters = new Set<() => void>();

  tryClaim(el: HTMLVideoElement): boolean {
    if (this.active.has(el)) return true;
    if (this.active.size >= MAX_CONCURRENT) return false;
    this.active.add(el);
    return true;
  }

  release(el: HTMLVideoElement): void {
    if (!this.active.delete(el)) return;
    for (const w of [...this.waiters]) w();
  }

  onRelease(fn: () => void): () => void {
    this.waiters.add(fn);
    return () => this.waiters.delete(fn);
  }
}

const slots = new VideoPlaySlots();

/**
 * Progressive-stream a video only while near the viewport, muted autoplay,
 * capped to a few concurrent players. Clears src when off-screen.
 */
@Directive({
  selector: 'video[appLazyAutoplayVideo]',
  standalone: true,
})
export class LazyAutoplayVideoDirective implements OnDestroy {
  private video = inject(ElementRef<HTMLVideoElement>).nativeElement;
  private playback = inject(PlaybackService);
  readonly appLazyAutoplayVideo = input<string | null>(null);

  private observer: IntersectionObserver | null = null;
  private visible = false;
  private claimed = false;
  private unsubSlots: (() => void) | null = null;

  constructor() {
    this.video.muted = true;
    this.video.playsInline = true;
    this.video.loop = true;
    this.video.preload = 'none';

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
          rootMargin: '100px 0px',
          threshold: 0.15,
        },
      );
      this.observer.observe(this.video);
    } else {
      this.visible = true;
    }

    this.unsubSlots = slots.onRelease(() => this.sync());

    effect(() => {
      this.appLazyAutoplayVideo();
      this.playback.autoplay();
      this.playback.dragPause();
      this.sync();
    });
  }

  ngOnDestroy(): void {
    this.observer?.disconnect();
    this.unsubSlots?.();
    this.teardown();
  }

  private sync(): void {
    const url = this.appLazyAutoplayVideo();
    const want =
      !!url &&
      this.visible &&
      this.playback.autoplay() &&
      !this.playback.dragPause();

    if (!want) {
      this.teardown();
      return;
    }

    if (!this.claimed && !slots.tryClaim(this.video)) {
      this.teardownMediaOnly();
      return;
    }
    this.claimed = true;

    if (this.video.getAttribute('src') !== url) {
      this.video.preload = 'metadata';
      this.video.src = url!;
      this.video.load();
    }
    this.video.muted = true;
    this.video.play().catch(() => {});
  }

  private teardownMediaOnly(): void {
    this.video.pause();
    if (this.video.hasAttribute('src')) {
      this.video.removeAttribute('src');
      this.video.load();
    }
  }

  private teardown(): void {
    this.teardownMediaOnly();
    if (this.claimed) {
      this.claimed = false;
      slots.release(this.video);
    }
  }
}
