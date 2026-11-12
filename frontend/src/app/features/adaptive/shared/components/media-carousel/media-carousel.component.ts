/**
 * Shows a save's downloaded media in the Instagram reel shape (9:16). With several items, click the
 * left / right half to go back / forward (an arrow shows on hover); trackpad and touch swipes also
 * work, and a counter + dots show the position. Videos follow the app-wide Autoplay switch.
 * Optional engagement inputs render an Instagram-style right rail (likes / comments / reposts / send).
 */
import { ChangeDetectionStrategy, Component, ElementRef, computed, input, output, signal, viewChild } from '@angular/core';
import { SaveMedia } from '../../../services/dump.service';
import { AutoplayVideoDirective } from '../../directives/autoplay-video.directive';

@Component({
  selector: 'app-media-carousel',
  standalone: true,
  imports: [AutoplayVideoDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    <div class="relative w-full aspect-[9/16] overflow-hidden rounded-2xl bg-black">
      <div
        #track
        class="hide-scroll flex h-full w-full snap-x snap-mandatory overflow-x-auto overscroll-x-contain"
        (scroll)="onScroll()">
        @for (item of media(); track item.url; let i = $index) {
          <div class="h-full w-full shrink-0 snap-center">
            @if (item.type === 'video') {
              <video
                appAutoplayVideo
                [src]="item.url"
                [muted]="true"
                loop
                playsinline
                controls
                preload="metadata"
                draggable="false"
                (loadedmetadata)="onVideoMeta($event, i)"
                (mousedown)="keepControlsUsable($event)"
                (touchstart)="keepControlsUsable($event)"
                class="h-full w-full select-none object-contain"></video>
            } @else {
              <img [src]="item.url" [alt]="'Item ' + (i + 1)" loading="lazy" draggable="false" class="h-full w-full select-none object-contain" />
            }
          </div>
        }
      </div>

      @if (showEngagement()) {
        <!-- Instagram Reels-style engagement rail (display only). -->
        <div
          class="pointer-events-none absolute right-1.5 top-1/2 z-10 flex -translate-y-1/2 flex-col items-center gap-2.5 text-white"
          style="text-shadow: 0 1px 2px rgba(0,0,0,0.55)"
          aria-hidden="true">
          @if (likeCount() != null) {
            <div class="flex flex-col items-center gap-0.5">
              <svg viewBox="0 0 24 24" class="h-[18px] w-[18px]" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M12 21s-6.7-4.35-9.33-7.6C.8 11.2 1.1 7.7 3.6 5.9c2-1.45 4.55-.95 6.05.85L12 9.1l2.35-2.35c1.5-1.8 4.05-2.3 6.05-.85 2.5 1.8 2.8 5.3.93 7.5C18.7 16.65 12 21 12 21z" />
              </svg>
              <span class="text-[10px] font-semibold leading-none tabular-nums">{{ formatCount(likeCount()!) }}</span>
            </div>
          }
          @if (commentCount() != null) {
            <div class="flex flex-col items-center gap-0.5">
              <svg viewBox="0 0 24 24" class="h-[18px] w-[18px]" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M7 18.5 4.5 21V7.5A3.5 3.5 0 0 1 8 4h8a3.5 3.5 0 0 1 3.5 3.5v7A3.5 3.5 0 0 1 16 18H7z" />
              </svg>
              <span class="text-[10px] font-semibold leading-none tabular-nums">{{ formatCount(commentCount()!) }}</span>
            </div>
          }
          @if (repostCount() != null) {
            <div class="flex flex-col items-center gap-0.5">
              <svg viewBox="0 0 24 24" class="h-[18px] w-[18px]" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
                <path d="M17 1v4H7a4 4 0 0 0-4 4v2" />
                <path d="m17 1 3 3-3 3" />
                <path d="M7 23v-4h10a4 4 0 0 0 4-4v-2" />
                <path d="m7 23-3-3 3-3" />
              </svg>
              <span class="text-[10px] font-semibold leading-none tabular-nums">{{ formatCount(repostCount()!) }}</span>
            </div>
          }
          <div class="flex flex-col items-center pt-0.5">
            <svg viewBox="0 0 24 24" class="h-[18px] w-[18px]" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
              <path d="M22 2 11 13" />
              <path d="M22 2 15 22l-4-9-9-4 20-7z" />
            </svg>
          </div>
        </div>
      }

      @if (media().length > 1) {
        <!-- Click zones; they stop above the bottom so video controls stay usable. -->
        <button
          type="button"
          (click)="goTo(index() - 1)"
          [disabled]="index() === 0"
          aria-label="Previous item"
          class="group absolute left-0 top-0 bottom-12 w-1/2 flex items-center justify-start pl-2 disabled:cursor-default">
          <span class="material-icons-outlined flex h-8 w-8 items-center justify-center rounded-full bg-black/50 !text-xl text-white opacity-0 transition-opacity group-hover:opacity-100 group-disabled:!opacity-0">chevron_left</span>
        </button>
        <button
          type="button"
          (click)="goTo(index() + 1)"
          [disabled]="index() === media().length - 1"
          aria-label="Next item"
          class="group absolute right-0 top-0 bottom-12 w-1/2 flex items-center justify-end pr-2 disabled:cursor-default">
          <span class="material-icons-outlined flex h-8 w-8 items-center justify-center rounded-full bg-black/50 !text-xl text-white opacity-0 transition-opacity group-hover:opacity-100 group-disabled:!opacity-0">chevron_right</span>
        </button>

        <span class="pointer-events-none absolute top-2 right-2 rounded bg-black/60 px-1.5 py-0.5 text-[10px] font-bold text-white">
          {{ index() + 1 }}/{{ media().length }}
        </span>
        <div class="absolute bottom-2 left-0 right-0 flex justify-center gap-1.5">
          @for (item of media(); track item.url; let i = $index) {
            <button
              type="button"
              (click)="goTo(i)"
              [attr.aria-label]="'Show item ' + (i + 1)"
              [attr.aria-current]="i === index() ? 'true' : null"
              class="h-1.5 rounded-full transition-all"
              [class]="i === index() ? 'w-4 bg-white' : 'w-1.5 bg-white/50 hover:bg-white/80'"></button>
          }
        </div>
      }
    </div>
  `,
})
export class MediaCarouselComponent {
  readonly media = input.required<SaveMedia[]>();
  /** Instagram engagement snapshot (null/undefined = unknown; omit that icon). */
  readonly likeCount = input<number | null | undefined>(null);
  readonly commentCount = input<number | null | undefined>(null);
  readonly repostCount = input<number | null | undefined>(null);
  readonly showEngagement = computed(
    () => this.likeCount() != null || this.commentCount() != null || this.repostCount() != null
  );
  /** First video's duration in seconds (for card chrome outside the carousel). */
  readonly durationSec = output<number>();
  readonly index = signal(0);

  private track = viewChild.required<ElementRef<HTMLDivElement>>('track');
  private reportedDuration = false;

  onScroll(): void {
    const el = this.track().nativeElement;
    this.index.set(Math.round(el.scrollLeft / Math.max(el.clientWidth, 1)));
  }

  onVideoMeta(event: Event, i: number): void {
    if (this.reportedDuration) return;
    // Prefer the lead item; otherwise the first video that reports metadata.
    const isLeadVideo = this.media()[0]?.type === 'video' ? i === 0 : true;
    if (!isLeadVideo && i !== this.media().findIndex((m) => m.type === 'video')) return;
    const video = event.currentTarget as HTMLVideoElement;
    const sec = video.duration;
    if (!Number.isFinite(sec) || sec <= 0) return;
    this.reportedDuration = true;
    this.durationSec.emit(sec);
  }

  /** Presses on the video's control bar (bottom 48px) shouldn't start dragging the card. */
  keepControlsUsable(event: MouseEvent | TouchEvent): void {
    const video = event.currentTarget as HTMLVideoElement;
    const rect = video.getBoundingClientRect();
    const y = 'touches' in event ? event.touches[0]?.clientY : event.clientY;
    if (y !== undefined && y > rect.bottom - 48) event.stopPropagation();
  }

  goTo(i: number): void {
    const last = this.media().length - 1;
    const target = Math.min(Math.max(i, 0), last);
    const el = this.track().nativeElement;
    el.scrollTo({ left: target * el.clientWidth, behavior: 'smooth' });
  }

  formatCount(n: number): string {
    if (!Number.isFinite(n) || n < 0) return '0';
    if (n < 1000) return String(Math.round(n));
    if (n < 1_000_000) {
      const v = n / 1000;
      return `${v >= 100 ? v.toFixed(0) : v.toFixed(1).replace(/\.0$/, '')}K`;
    }
    const v = n / 1_000_000;
    return `${v >= 100 ? v.toFixed(0) : v.toFixed(1).replace(/\.0$/, '')}M`;
  }
}
