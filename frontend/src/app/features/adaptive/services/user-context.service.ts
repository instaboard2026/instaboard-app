import { Injectable, signal, computed } from '@angular/core';

/** The local app's user (named after the computer account). */
export interface AppUser {
  id?: number;
  displayName: string;
  email: string;
}

@Injectable({ providedIn: 'root' })
export class UserContextService {
  private _user = signal<AppUser | null>(null);

  user = this._user.asReadonly();
  isLoggedIn = computed(() => this._user() !== null);
  displayName = computed(() => this._user()?.displayName ?? null);
  email = computed(() => this._user()?.email ?? null);

  /** Sets the user from the session token's claims. Returns false if the token can't be read. */
  setUserFromToken(token: string): boolean {
    const payload = decodeJwtPayload(token);
    if (!payload || payload.sub === undefined) return false;
    const name = typeof payload.name === 'string' ? payload.name : undefined;
    const email = typeof payload.email === 'string' ? payload.email : '';
    this._user.set({ id: parseSub(payload.sub), displayName: name ?? (email || 'You'), email });
    return true;
  }
}

/** Parse JWT sub (user id) as string or number from backend. */
function parseSub(sub: unknown): number | undefined {
  if (typeof sub === 'number' && Number.isSafeInteger(sub)) return sub;
  if (typeof sub === 'string') {
    const n = parseInt(sub, 10);
    return Number.isSafeInteger(n) ? n : undefined;
  }
  return undefined;
}

/** Decode JWT payload (middle part) without verification. For display only. Rejects expired tokens. */
function decodeJwtPayload(token: string): Record<string, unknown> | null {
  try {
    const parts = token.split('.');
    if (parts.length !== 3) return null;
    const base64 = parts[1].replace(/-/g, '+').replace(/_/g, '/');
    const padded = base64.padEnd(base64.length + (4 - base64.length % 4) % 4, '=');
    const json = atob(padded);
    const payload = JSON.parse(json) as Record<string, unknown>;
    const exp = payload.exp;
    if (typeof exp === 'number' && exp * 1000 < Date.now()) {
      return null;
    }
    return payload;
  } catch {
    return null;
  }
}
