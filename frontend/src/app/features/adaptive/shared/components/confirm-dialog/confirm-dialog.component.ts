import { ChangeDetectionStrategy, Component, HostListener, inject } from '@angular/core';
import { ConfirmDialogService } from '../../../services/confirm-dialog.service';

@Component({
  selector: 'app-confirm-dialog',
  standalone: true,
  changeDetection: ChangeDetectionStrategy.OnPush,
  template: `
    @if (dialog.pending(); as p) {
      <div class="fixed inset-0 z-[1100] flex items-center justify-center p-4" role="alertdialog" aria-modal="true" aria-labelledby="confirm-title">
        <div class="absolute inset-0 bg-black/60" (click)="dialog.answer(false)"></div>
        <div class="relative w-full max-w-sm rounded-2xl border border-slate-200 dark:border-slate-700 bg-white dark:bg-slate-900 p-6 shadow-xl">
          <h2 id="confirm-title" class="text-base font-bold text-slate-900 dark:text-white">{{ p.title }}</h2>
          <p class="mt-2 text-sm text-slate-600 dark:text-slate-300">{{ p.message }}</p>
          <div class="mt-5 flex justify-end gap-2">
            <button type="button" (click)="dialog.answer(false)" class="px-4 py-2 rounded-lg border border-slate-300 dark:border-slate-600 text-sm font-semibold text-slate-700 dark:text-slate-200 hover:bg-slate-100 dark:hover:bg-slate-800">Cancel</button>
            <button
              type="button"
              (click)="dialog.answer(true)"
              class="px-4 py-2 rounded-lg text-sm font-semibold text-white"
              [class]="p.danger ? 'bg-red-600 hover:bg-red-700' : 'bg-blue-600 hover:bg-blue-700'">
              {{ p.confirmLabel }}
            </button>
          </div>
        </div>
      </div>
    }
  `,
})
export class ConfirmDialogComponent {
  readonly dialog = inject(ConfirmDialogService);

  @HostListener('document:keydown.escape')
  onEscape(): void {
    if (this.dialog.pending()) this.dialog.answer(false);
  }
}
