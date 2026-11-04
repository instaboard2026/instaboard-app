import { Injectable, inject } from '@angular/core';
import { HttpClient, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, map, of } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';

/** GET /api/profile (matches backend ProfileResponse). */
export interface Profile {
  name: string;
  email?: string;
  member_since?: string;
}

interface ProfileApiWrapper {
  status: string;
  data?: Profile;
}

@Injectable({ providedIn: 'root' })
export class ProfileService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);

  /** Fetches the current user's profile. Returns null on error. */
  getProfile(): Observable<Profile | null> {
    const token = this.authToken.getToken();
    if (!token) return of(null);
    const headers = new HttpHeaders({ Authorization: `Bearer ${token}` });
    return this.http.get<ProfileApiWrapper>(`${environment.apiUrl}/api/profile`, { headers }).pipe(
      map((res) => (res?.status === 'SUCCESS' && res.data ? res.data : null)),
      catchError(() => of(null)),
    );
  }
}
