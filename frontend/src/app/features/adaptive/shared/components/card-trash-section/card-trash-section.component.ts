import { ChangeDetectionStrategy, Component, HostListener, OnInit, inject, signal } from '@angular/core';
import { PlaybackService } from '../../../services/playback.service';
import { CardTrashItem, CardTrashService } from '../../../services/card-trash.service';
import { ConfirmDialogService } from '../../../services/confirm-dialog.service';
import { LazyThumbDirective } from '../../directives/lazy-thumb.directive';

/** Bin: camera card files moved to the Trash from Manage camera, with Put back / Delete forever. */
@Component({
  selector: 'app-card-trash-section',
  standalone: true,
  imports: [LazyThumbDirective],
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (trash.count() > 0) {
      <section class="mb-8">
        <div class="mb-3 flex items-baseline gap-2">
          <span class="material-icons-outlined !text-lg text-ink-secondary dark:text-slate-400">sd_card</span>
          <h2 class="text-sm font-semibold">From camera card</h2>
          <span class="text-xs text-ink-secondary dark:text-slate-400">
            {{ trash.count() }} {{ trash.count() === 1 ? 'file' : 'files' }} · in the card's Trash until you put them back or delete them
          </span>
        </div>
        @if (error(); as err) {
          <p class="mb-3 rounded-lg bg-red-500/10 px-3 py-2 text-xs text-red-500 dark:text-red-300" role="alert">{{ err }}</p>
        }
        <div class="grid grid-cols-3 gap-4 sm:grid-cols-4 md:grid-cols-6 xl:grid-cols-8">
          @for (item of trash.items(); track item.id) {
            <div class="group flex min-w-0 flex-col gap-1.5" [class.opacity-60]="!item.card_in">
              <div
                class="relative aspect-[9/16] overflow-hidden rounded-2xl bg-[#1a1a1a]"
                [class.cursor-pointer]="!!item.url"
                [attr.role]="item.url ? 'button' : null"
                [attr.tabindex]="item.url ? 0 : null"
                [attr.aria-label]="item.url ? 'Preview ' + item.name : null"
                (click)="openPreview(item)"
                (keydown.enter)="openPreview(item)">
                @if (item.thumb_url && !thumbFailed().has(item.id)) {
                  <img [appLazyThumb]="item.thumb_url" alt="" (error)="onThumbError(item.id)" class="h-full w-full object-cover" />
                } @else {
                  <div class="flex h-full w-full items-center justify-center text-white/60">
                    <span class="material-icons-outlined !text-3xl">{{ item.kind === 'video' ? 'movie' : 'image' }}</span>
                  </div>
                }
                @if (item.kind === 'video' && item.url) {
                  <span class="pointer-events-none absolute right-2 top-2 inline-flex h-7 w-7 items-center justify-center rounded-full bg-black/55 text-white">
                    <span class="material-icons-outlined !text-[18px]">play_arrow</span>
                  </span>
                }
                @if (!item.card_in) {
                  <span class="absolute inset-x-2 bottom-2 rounded-md bg-black/70 px-1.5 py-1 text-center text-[10px] text-white">Plug the card in to put back</span>
                } @else {
                  <div class="absolute inset-x-2 bottom-2 flex gap-1 opacity-0 transition-opacity group-hover:opacity-100 focus-within:opacity-100">
                    <button
                      type="button"
                      (click)="$event.stopPropagation(); restore(item)"
                      [disabled]="busy() === item.id"
                      class="flex-1 rounded-lg bg-white/90 px-1.5 py-1 text-[11px] font-semibold text-ink hover:bg-white disabled:opacity-50">
                      Put back
                    </button>
                    <button
                      type="button"
                      (click)="$event.stopPropagation(); deleteForever(item)"
                      [disabled]="busy() === item.id"
                      [attr.aria-label]="'Delete ' + item.name + ' forever'"
                      title="Delete forever"
                      class="inline-flex w-7 items-center justify-center rounded-lg bg-red-600 text-white hover:bg-red-500 disabled:opacity-50">
                      <span class="material-icons-outlined !text-base">delete_forever</span>
                    </button>
                  </div>
                }
              </div>
              <div class="px-0.5">
                <p class="truncate text-xs font-semibold" [title]="item.name">{{ item.name }}</p>
                <p class="text-[11px] text-ink-secondary dark:text-slate-400">{{ formatBytes(item.byte_size) }}</p>
              </div>
            </div>
          }
        </div>
      </section>
    }

    @if (preview(); as item) {
      <div class="fixed inset-0 z-[1000] flex items-center justify-center bg-black/70 p-4" role="dialog" aria-modal="true" (click)="closePreview()">
        <div class="relative w-full max-w-3xl overflow-hidden rounded-2xl bg-black shadow-2xl" (click)="$event.stopPropagation()">
          <div class="flex items-center justify-between gap-3 border-b border-white/10 px-3 py-2">
            <p class="min-w-0 truncate text-sm text-white/90">{{ item.name }} <span class="text-white/50">· in the Bin</span></p>
            <div class="flex shrink-0 items-center gap-1">
              <button type="button" (click)="restore(item); closePreview()" class="h-8 rounded-lg bg-white/90 px-3 text-xs font-semibold text-ink hover:bg-white">Put back</button>
              <button type="button" (click)="closePreview()" aria-label="Close preview" class="inline-flex h-8 w-8 items-center justify-center rounded-lg text-white/80 hover:bg-white/10 hover:text-white">
                <span class="material-icons-outlined !text-xl">close</span>
              </button>
            </div>
          </div>
          <div class="flex max-h-[calc(90vh-3rem)] items-center justify-center bg-black">
            @if (item.kind === 'video') {
              <video [src]="item.url" controls autoplay playsinline class="max-h-[calc(90vh-3rem)] max-w-full"></video>
            } @else {
              <img [src]="item.url" [alt]="item.name" class="max-h-[calc(90vh-3rem)] max-w-full object-contain" />
            }
          </div>
        </div>
      </div>
    }
  `,
})
export class CardTrashSectionComponent implements OnInit {
  readonly trash = inject(CardTrashService);
  private confirm = inject(ConfirmDialogService);
  readonly busy = signal<number | null>(null);
  readonly error = signal<string | null>(null);
  readonly thumbFailed = signal<Set<number>>(new Set());
  readonly preview = signal<CardTrashItem | null>(null);
  private playback = inject(PlaybackService);

  openPreview(item: CardTrashItem): void {
    if (!item.url) return;
    this.playback.previewPause.set(item.kind === 'video');
    this.preview.set(item);
  }

  closePreview(): void {
    this.playback.previewPause.set(false);
    this.preview.set(null);
  }

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.preview()) this.closePreview();
  }

  onThumbError(id: number): void {
    this.thumbFailed.update((s) => new Set(s).add(id));
  }

  ngOnInit(): void {
    this.trash.load().subscribe({ error: () => {} });
  }

  restore(item: CardTrashItem): void {
    this.run(item, this.trash.restore(item));
  }

  async deleteForever(item: CardTrashItem): Promise<void> {
    const ok = await this.confirm.ask({
      title: `Delete "${item.name}" forever?`,
      message: 'It is removed from the card and cannot be put back.',
      confirmLabel: 'Delete forever',
      danger: true,
    });
    if (ok) this.run(item, this.trash.deleteForever(item));
  }

  formatBytes(bytes: number): string {
    if (!Number.isFinite(bytes) || bytes <= 0) return '—';
    const units = ['B', 'KB', 'MB', 'GB', 'TB'];
    let n = bytes;
    let i = 0;
    while (n >= 1024 && i < units.length - 1) {
      n /= 1024;
      i++;
    }
    return `${n.toFixed(n >= 10 || i === 0 ? 0 : 1)} ${units[i]}`;
  }

  private run(item: CardTrashItem, op: import('rxjs').Observable<void>): void {
    this.busy.set(item.id);
    this.error.set(null);
    op.subscribe({
      next: () => this.busy.set(null),
      error: (err: Error) => {
        this.busy.set(null);
        this.error.set(err.message);
      },
    });
  }
}
