/**
 * Shows a save's downloaded media in the Instagram reel shape (9:16). With several items, click the
 * left / right half to go back / forward (an arrow shows on hover); trackpad and touch swipes also
 * work, and a counter + dots show the position. Videos follow the app-wide Autoplay switch.
 */
import { ChangeDetectionStrategy, Component, ElementRef, input, signal, viewChild } from '@angular/core';
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
                (mousedown)="keepControlsUsable($event)"
                (touchstart)="keepControlsUsable($event)"
                class="h-full w-full select-none object-contain"></video>
            } @else {
              <img [src]="item.url" [alt]="'Item ' + (i + 1)" loading="lazy" draggable="false" class="h-full w-full select-none object-contain" />
            }
          </div>
        }
      </div>

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
  readonly index = signal(0);

  private track = viewChild.required<ElementRef<HTMLDivElement>>('track');

  onScroll(): void {
    const el = this.track().nativeElement;
    this.index.set(Math.round(el.scrollLeft / Math.max(el.clientWidth, 1)));
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
}
