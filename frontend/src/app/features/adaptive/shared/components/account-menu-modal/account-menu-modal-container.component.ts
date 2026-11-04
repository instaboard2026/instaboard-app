import {
  Component,
  ChangeDetectionStrategy,
  inject,
  ElementRef,
  effect,
  signal,
} from '@angular/core';
import { CommonModule } from '@angular/common';
import { AccountMenuModalService } from '../../../services/account-menu-modal.service';
import { AccountMenuModalComponent } from './account-menu-modal.component';

@Component({
  selector: 'app-account-menu-modal-container',
  standalone: true,
  imports: [CommonModule, AccountMenuModalComponent],
  templateUrl: './account-menu-modal-container.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountMenuModalContainerComponent {
  menuService = inject(AccountMenuModalService);
  private el = inject(ElementRef<HTMLElement>);

  isOpen = this.menuService.isOpen;
  /** Where the menu opens: above the expanded sidebar's account block, or beside the rail avatar. */
  position = signal({ left: 8, bottom: 72 });
  private removeDocClickListener: (() => void) | null = null;

  constructor() {
    effect(() => {
      if (this.isOpen()) {
        this.position.set(this.positionFromTrigger());
        this.attachDocClickListener();
      } else {
        this.detachDocClickListener();
      }
    });
  }

  private positionFromTrigger(): { left: number; bottom: number } {
    const trigger = this.menuService.getTriggerElement();
    if (!trigger) return { left: 8, bottom: 72 };
    const r = trigger.getBoundingClientRect();
    const narrow = r.width < 64; // avatar in the collapsed icon rail
    return narrow
      ? { left: Math.round(r.right + 10), bottom: Math.round(window.innerHeight - r.bottom) }
      : { left: Math.round(r.left), bottom: Math.round(window.innerHeight - r.top + 8) };
  }

  private attachDocClickListener(): void {
    this.detachDocClickListener();
    const handler = (e: MouseEvent) => {
      const target = e.target as Node;
      const host = this.el.nativeElement;
      if (host.contains(target)) return;
      const trigger = this.menuService.getTriggerElement();
      const clickedTrigger = trigger && (trigger === target || trigger.contains(target));
      if (clickedTrigger) {
        e.stopPropagation();
        e.preventDefault();
        this.menuService.close();
        this.detachDocClickListener();
        return;
      }
      this.menuService.close();
      this.detachDocClickListener();
    };
    setTimeout(() => {
      document.addEventListener('click', handler, true);
      this.removeDocClickListener = () => {
        document.removeEventListener('click', handler, true);
        this.removeDocClickListener = null;
      };
    }, 0);
  }

  private detachDocClickListener(): void {
    if (this.removeDocClickListener) {
      this.removeDocClickListener();
    }
  }
}
