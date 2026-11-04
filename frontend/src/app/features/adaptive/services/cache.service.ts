import { Injectable, PLATFORM_ID, inject } from '@angular/core';
import { isPlatformBrowser } from '@angular/common';

const CACHE_PREFIX = 'instaboard_';

interface CacheItem<T> {
  data: T;
}

@Injectable({ providedIn: 'root' })
export class CacheService {
  private isBrowser = isPlatformBrowser(inject(PLATFORM_ID));

  setItem<T>(key: string, data: T): void {
    if (!this.isBrowser) return;
    const item: CacheItem<T> = { data };
    localStorage.setItem(this.buildKey(key), JSON.stringify(item));
  }

  getItem<T>(key: string): T | null {
    if (!this.isBrowser) return null;
    const itemString = localStorage.getItem(this.buildKey(key));
    if (!itemString) return null;
    try {
      const parsed = JSON.parse(itemString) as CacheItem<T>;
      return parsed?.data ?? null;
    } catch {
      return null;
    }
  }

  setSessionItem<T>(key: string, data: T): void {
    if (!this.isBrowser) return;
    sessionStorage.setItem(this.buildKey(key), JSON.stringify(data));
  }

  getSessionItem<T>(key: string): T | null {
    if (!this.isBrowser) return null;
    const s = sessionStorage.getItem(this.buildKey(key));
    if (!s) return null;
    try {
      return JSON.parse(s) as T;
    } catch {
      return null;
    }
  }

  private buildKey(key: string): string {
    return `${CACHE_PREFIX}${key}`;
  }
}
