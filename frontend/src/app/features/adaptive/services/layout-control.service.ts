import { Injectable, signal } from '@angular/core';

@Injectable({ providedIn: 'root' })
export class LayoutControlService {
  isShellVisible = signal(true);

  setShellVisible(visible: boolean): void {
    this.isShellVisible.set(visible);
  }
}
