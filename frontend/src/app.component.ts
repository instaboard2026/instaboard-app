import { Component, inject, ChangeDetectionStrategy, computed, effect, signal, OnInit, HostListener } from '@angular/core';
import { FooterComponent } from './app/features/adaptive/shared/components/footer/footer.component';
import { ThemeService } from './app/features/adaptive/services/theme.service';
import { CommonModule } from '@angular/common';
import { SidebarComponent } from './app/features/adaptive/shared/components/sidebar/sidebar.component';
import { CacheService } from './app/features/adaptive/services/cache.service';
import { NavigationEnd, Router, RouterOutlet } from '@angular/router';
import { CdkScrollable } from '@angular/cdk/scrolling';
import { filter } from 'rxjs';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { APP_SCROLL_ID, scrollPageToTop } from './app/features/adaptive/shared/utils/page-scroll';
import { LayoutControlService } from './app/features/adaptive/services/layout-control.service';
import { AccountMenuModalContainerComponent } from './app/features/adaptive/shared/components/account-menu-modal/account-menu-modal-container.component';
import { ConfirmDialogComponent } from './app/features/adaptive/shared/components/confirm-dialog/confirm-dialog.component';
import { UserContextService } from './app/features/adaptive/services/user-context.service';
import { LocalSessionService } from './app/features/adaptive/services/local-session.service';
import { UpdateService } from './app/features/adaptive/services/update.service';
import { AppLifecycleService } from './app/features/adaptive/services/app-lifecycle.service';

const SIDEBAR_STATE_KEY = 'sidebar_open';
/** Below this width the navigation is an icon rail that opens as a drawer over the page. */
const MOBILE_BREAKPOINT = 768;
/** Must match the sidebar's `w-[220px]` / `w-14`. */
const SIDEBAR_WIDTH = 220;
const RAIL_WIDTH = 56;
const APP_LOADED_KEY = 'app_loaded_session';

@Component({
  selector: 'app-root',
  templateUrl: './app.component.html',
  changeDetection: ChangeDetectionStrategy.OnPush,
  standalone: true,
  imports: [
    CommonModule,
    FooterComponent,
    SidebarComponent,
    RouterOutlet,
    CdkScrollable,
    AccountMenuModalContainerComponent,
    ConfirmDialogComponent,
  ],
})
export class AppComponent implements OnInit {
  themeService = inject(ThemeService);
  cacheService = inject(CacheService);
  layoutControlService = inject(LayoutControlService);
  userContext = inject(UserContextService);
  private session = inject(LocalSessionService);
  private updates = inject(UpdateService);
  readonly lifecycle = inject(AppLifecycleService);

  readonly appScrollId = APP_SCROLL_ID;
  isShellVisible = this.layoutControlService.isShellVisible;
  /** Desktop: expanded sidebar (true) or icon rail (false). Phones: drawer open over the page. */
  isSidebarOpen = signal(false);
  isMobile = signal(typeof window !== 'undefined' && window.innerWidth < MOBILE_BREAKPOINT);
  isAppLoading = signal(true);
  isInitializing = signal(true);

  isDarkMode = this.themeService.isDarkMode;

  /** Left offset of the page: the expanded sidebar's width, or the icon rail's. */
  contentOffset = computed(() => {
    if (!this.isShellVisible()) return null;
    return this.isMobile() || !this.isSidebarOpen() ? `${RAIL_WIDTH}px` : `${SIDEBAR_WIDTH}px`;
  });

  constructor() {
    const savedSidebarState = this.cacheService.getItem<boolean>(SIDEBAR_STATE_KEY);
    if (this.isMobile()) {
      this.isSidebarOpen.set(false);
    } else {
      this.isSidebarOpen.set(savedSidebarState ?? true);
    }
    // Remember the desktop choice only; the phone drawer always starts closed.
    effect(() => {
      const open = this.isSidebarOpen();
      if (!this.isMobile()) this.cacheService.setItem(SIDEBAR_STATE_KEY, open);
    });
    effect(() => {
      if (this.isDarkMode()) {
        document.documentElement.classList.add('dark');
      } else {
        document.documentElement.classList.remove('dark');
      }
    });
    // Check for a newer GitHub Release once the local session is ready.
    effect(() => {
      if (this.session.status() === 'ready') this.updates.check();
    });
    // The panel keeps its scroll position across routes, so start each page at the top.
    inject(Router).events
      .pipe(filter(e => e instanceof NavigationEnd), takeUntilDestroyed())
      .subscribe(() => scrollPageToTop('instant'));
  }

  ngOnInit(): void {
    this.session.start();
    const hasLoadedThisSession = this.cacheService.getSessionItem<boolean>(APP_LOADED_KEY);
    if (hasLoadedThisSession) {
      this.isAppLoading.set(false);
      this.isInitializing.set(false);
    } else {
      setTimeout(() => {
        this.isAppLoading.set(false);
        this.cacheService.setSessionItem(APP_LOADED_KEY, true);
      }, 800);
      setTimeout(() => this.isInitializing.set(false), 400);
    }
  }

  toggleSidebar(): void {
    this.isSidebarOpen.update(isOpen => !isOpen);
  }

  expandSidebar(): void {
    this.isSidebarOpen.set(true);
  }

  @HostListener('window:resize')
  onResize(): void {
    const mobile = window.innerWidth < MOBILE_BREAKPOINT;
    if (mobile === this.isMobile()) return;
    this.isMobile.set(mobile);
    this.isSidebarOpen.set(mobile ? false : (this.cacheService.getItem<boolean>(SIDEBAR_STATE_KEY) ?? true));
  }

  /** Cmd+B (Mac) / Ctrl+B collapses or expands the navigation, like Shopify. */
  @HostListener('document:keydown', ['$event'])
  onKeydown(event: KeyboardEvent): void {
    if (!this.isShellVisible() || event.altKey || event.shiftKey) return;
    if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'b') {
      event.preventDefault();
      this.toggleSidebar();
    }
  }

  closeSidebar(): void {
    this.isSidebarOpen.set(false);
  }
}
