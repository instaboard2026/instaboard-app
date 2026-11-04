import { Injectable, signal } from '@angular/core';

export type AccountMenuAnchor = 'sidebar';

@Injectable({ providedIn: 'root' })
export class AccountMenuModalService {
  private _isOpen = signal(false);
  private _anchor = signal<AccountMenuAnchor>('sidebar');
  private _triggerElement: HTMLElement | null = null;

  isOpen = this._isOpen.asReadonly();
  anchor = this._anchor.asReadonly();

  setTriggerElement(el: HTMLElement | null): void {
    this._triggerElement = el;
  }

  getTriggerElement(): HTMLElement | null {
    return this._triggerElement;
  }

  open(from: AccountMenuAnchor = 'sidebar', triggerEl?: HTMLElement | null): void {
    this._triggerElement = triggerEl ?? null;
    this._anchor.set(from);
    this._isOpen.set(true);
  }

  close(): void {
    this._isOpen.set(false);
  }

  toggle(from: AccountMenuAnchor = 'sidebar', triggerEl?: HTMLElement | null): void {
    if (triggerEl !== undefined) this._triggerElement = triggerEl;
    if (this._isOpen()) {
      this._isOpen.set(false);
    } else {
      this._anchor.set(from);
      this._isOpen.set(true);
    }
  }
}
