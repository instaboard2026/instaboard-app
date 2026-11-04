import { Injectable, signal } from '@angular/core';

export interface ConfirmRequest {
  title: string;
  message: string;
  confirmLabel: string;
  danger?: boolean;
}

interface PendingConfirm extends ConfirmRequest {
  resolve: (ok: boolean) => void;
}

/** App-wide confirm dialog (rendered once in the app root). */
@Injectable({ providedIn: 'root' })
export class ConfirmDialogService {
  readonly pending = signal<PendingConfirm | null>(null);

  ask(request: ConfirmRequest): Promise<boolean> {
    this.pending()?.resolve(false);
    return new Promise((resolve) => this.pending.set({ ...request, resolve }));
  }

  answer(ok: boolean): void {
    const p = this.pending();
    this.pending.set(null);
    p?.resolve(ok);
  }
}
