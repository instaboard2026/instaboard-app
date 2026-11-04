import { Injectable } from '@angular/core';

const TOKEN_KEY = 'instaboard_token';

@Injectable({ providedIn: 'root' })
export class AuthTokenService {
  setToken(token: string): void {
    if (token) {
      localStorage.setItem(TOKEN_KEY, token);
    } else {
      localStorage.removeItem(TOKEN_KEY);
    }
  }

  getToken(): string | null {
    return localStorage.getItem(TOKEN_KEY);
  }

  clearToken(): void {
    localStorage.removeItem(TOKEN_KEY);
  }
}
