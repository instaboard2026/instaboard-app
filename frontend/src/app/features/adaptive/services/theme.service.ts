import { Injectable, signal, computed, inject } from '@angular/core';
import { CacheService } from './cache.service';

type Theme = 'light' | 'dark' | 'system';
const THEME_CACHE_KEY = 'user_theme';

@Injectable({ providedIn: 'root' })
export class ThemeService {
  private cacheService = inject(CacheService);
  private _theme = signal<Theme>('system');
  public readonly theme = this._theme.asReadonly();

  isDarkMode = computed(() => {
    const currentTheme = this._theme();
    if (currentTheme === 'system') {
      if (typeof window !== 'undefined') {
        return window.matchMedia('(prefers-color-scheme: dark)').matches;
      }
      return false;
    }
    return currentTheme === 'dark';
  });

  constructor() {
    const savedTheme = this.cacheService.getItem<Theme>(THEME_CACHE_KEY);
    if (savedTheme) this._theme.set(savedTheme);
  }

  setTheme(newTheme: Theme): void {
    this._theme.set(newTheme);
    this.cacheService.setItem(THEME_CACHE_KEY, newTheme);
  }

  toggleTheme(): void {
    const newTheme = this.isDarkMode() ? 'light' : 'dark';
    this.setTheme(newTheme);
  }
}
