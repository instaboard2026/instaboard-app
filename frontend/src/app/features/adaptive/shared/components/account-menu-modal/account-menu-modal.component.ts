import { Component, ChangeDetectionStrategy, inject } from '@angular/core';
import { CommonModule } from '@angular/common';
import { Router } from '@angular/router';
import { AccountMenuModalService } from '../../../services/account-menu-modal.service';
import { UserContextService } from '../../../services/user-context.service';

@Component({
  selector: 'app-account-menu-modal',
  standalone: true,
  imports: [CommonModule],
  templateUrl: './account-menu-modal.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
})
export class AccountMenuModalComponent {
  private menuService = inject(AccountMenuModalService);
  private userContext = inject(UserContextService);
  private router = inject(Router);

  email = this.userContext.email;

  goToProfile(): void {
    this.menuService.close();
    this.router.navigate(['/account/profile']);
  }
}
