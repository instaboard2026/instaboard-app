import { Injectable, computed, effect, inject, signal, untracked } from '@angular/core';
import { HttpClient, HttpErrorResponse, HttpHeaders } from '@angular/common/http';
import { Observable, catchError, forkJoin, map, of, throwError } from 'rxjs';
import { environment } from 'environments';
import { AuthTokenService } from './auth-token.service';
import { CacheService } from './cache.service';
import { UserContextService } from './user-context.service';

const SCHEDULE_KEY = 'calendar_schedule_v1';
const NOTES_KEY = 'calendar_notes_v1';
const DATE_RE = /^\d{4}-\d{2}-\d{2}$/;
/** Wait until typing pauses before writing a day note. */
const NOTE_SAVE_MS = 400;

interface ApiWrapper<T> {
  status: string;
  data?: T;
  message?: string;
}

interface CalendarPayload {
  schedule?: { save_id: number; date: string }[];
  notes?: { date: string; body: string }[];
}

/**
 * Calendar days and notes for the signed-in user, stored in the backend SQLite file so every browser
 * on this computer sees the same schedule.
 */
@Injectable({ providedIn: 'root' })
export class CalendarScheduleService {
  private http = inject(HttpClient);
  private authToken = inject(AuthTokenService);
  private cache = inject(CacheService);
  private userContext = inject(UserContextService);

  /** saveId → YYYY-MM-DD. Read from templates for OnPush reactivity. */
  readonly bySave = signal<ReadonlyMap<number, string>>(new Map());
  private readonly notes = signal<ReadonlyMap<string, string>>(new Map());
  /** Last note text confirmed by the API, used to undo a failed write. */
  private noteSaved = new Map<string, string>();
  /** Saves and notes edited locally while a load is in flight. */
  private dirtySaves = new Set<number>();
  private dirtyNotes = new Set<string>();
  private noteTimers = new Map<string, ReturnType<typeof setTimeout>>();
  private loadSeq = 0;

  readonly scheduledCount = computed(() => this.bySave().size);

  constructor() {
    effect(() => {
      if (!this.userContext.isLoggedIn()) {
        untracked(() => this.clear());
        return;
      }
      untracked(() => this.load());
    });
  }

  isScheduled(saveId: number): boolean {
    return this.bySave().has(saveId);
  }

  dateFor(saveId: number): string | null {
    return this.bySave().get(saveId) ?? null;
  }

  /** Save ids scheduled on a given YYYY-MM-DD. */
  savesOn(dateKey: string): number[] {
    const out: number[] = [];
    for (const [id, date] of this.bySave()) {
      if (date === dateKey) out.push(id);
    }
    return out;
  }

  schedule(saveId: number, dateKey: string): void {
    const prev = this.bySave().get(saveId) ?? null;
    this.dirtySaves.add(saveId);
    this.bySave.update((m) => {
      const next = new Map(m);
      next.set(saveId, dateKey);
      return next;
    });
    this.putSchedule(saveId, dateKey).subscribe({
      next: () => this.dirtySaves.delete(saveId),
      error: () => {
        this.dirtySaves.delete(saveId);
        if (this.bySave().get(saveId) !== dateKey) return;
        this.bySave.update((m) => {
          const next = new Map(m);
          if (prev === null) next.delete(saveId);
          else next.set(saveId, prev);
          return next;
        });
      },
    });
  }

  unschedule(saveId: number): void {
    const prev = this.bySave().get(saveId);
    if (prev === undefined) return;
    this.dirtySaves.add(saveId);
    this.bySave.update((m) => {
      if (!m.has(saveId)) return m;
      const next = new Map(m);
      next.delete(saveId);
      return next;
    });
    this.deleteSchedule(saveId).subscribe({
      next: () => this.dirtySaves.delete(saveId),
      error: () => {
        this.dirtySaves.delete(saveId);
        if (this.bySave().has(saveId)) return;
        this.bySave.update((m) => {
          const next = new Map(m);
          next.set(saveId, prev);
          return next;
        });
      },
    });
  }

  noteFor(dateKey: string): string {
    return this.notes().get(dateKey) ?? '';
  }

  setNote(dateKey: string, value: string): void {
    this.dirtyNotes.add(dateKey);
    this.notes.update((m) => {
      const next = new Map(m);
      if (value.trim()) next.set(dateKey, value);
      else next.delete(dateKey);
      return next;
    });
    const pending = this.noteTimers.get(dateKey);
    if (pending) clearTimeout(pending);
    this.noteTimers.set(
      dateKey,
      setTimeout(() => {
        this.noteTimers.delete(dateKey);
        const raw = this.notes().get(dateKey) ?? '';
        this.persistNote(dateKey, raw);
      }, NOTE_SAVE_MS)
    );
  }

  private load(): void {
    const headers = this.authHeaders();
    if (!headers) {
      this.clear();
      return;
    }
    const seq = ++this.loadSeq;
    const legacy = this.readLegacy();
    this.http.get<ApiWrapper<CalendarPayload>>(`${environment.apiUrl}/api/calendar`, { headers }).subscribe({
      next: (res) => {
        if (seq !== this.loadSeq || res?.status !== 'SUCCESS' || !res.data) return;
        this.applyLoaded(res.data, legacy);
        if (legacy.present) this.importLegacy(legacy);
      },
      error: () => {},
    });
  }

  private applyLoaded(
    data: CalendarPayload,
    legacy: { schedule: Map<number, string>; notes: Map<string, string> }
  ): void {
    const schedule = slotsToMap(data.schedule);
    const serverNotes = notesToMap(data.notes);
    for (const [id, date] of legacy.schedule) {
      if (!this.dirtySaves.has(id)) schedule.set(id, date);
    }
    for (const id of this.dirtySaves) {
      const cur = this.bySave().get(id);
      if (cur) schedule.set(id, cur);
      else schedule.delete(id);
    }
    const baseNotes = new Map(serverNotes);
    for (const [date, body] of legacy.notes) {
      if (!this.dirtyNotes.has(date)) baseNotes.set(date, body);
    }
    const viewNotes = new Map(baseNotes);
    for (const date of this.dirtyNotes) {
      const cur = this.notes().get(date);
      if (cur) viewNotes.set(date, cur);
      else viewNotes.delete(date);
    }
    this.noteSaved = baseNotes;
    this.bySave.set(schedule);
    this.notes.set(viewNotes);
  }

  /** Copies a browser-local schedule into SQLite once, then drops those keys. */
  private importLegacy(legacy: { schedule: Map<number, string>; notes: Map<string, string> }): void {
    const reqs: Observable<unknown>[] = [];
    for (const [id, date] of legacy.schedule) {
      if (this.dirtySaves.has(id)) continue;
      reqs.push(this.putSchedule(id, date).pipe(catchError(skipGone)));
    }
    for (const [date, body] of legacy.notes) {
      if (this.dirtyNotes.has(date)) continue;
      reqs.push(this.putNote(date, body.trim()).pipe(catchError(skipGone)));
    }
    const finish = () => {
      this.cache.removeItem(SCHEDULE_KEY);
      this.cache.removeItem(NOTES_KEY);
    };
    if (!reqs.length) {
      finish();
      return;
    }
    forkJoin(reqs).subscribe({ next: finish, error: () => {} });
  }

  private putSchedule(saveId: number, date: string): Observable<void> {
    const headers = this.authHeaders();
    if (!headers) return throwError(() => new Error('Please sign in.'));
    return this.http
      .put<ApiWrapper<unknown>>(`${environment.apiUrl}/api/calendar/saves/${saveId}`, { date }, { headers })
      .pipe(map((res) => assertSuccess(res, 'Could not schedule.')));
  }

  private deleteSchedule(saveId: number): Observable<void> {
    const headers = this.authHeaders();
    if (!headers) return throwError(() => new Error('Please sign in.'));
    return this.http
      .delete<ApiWrapper<unknown>>(`${environment.apiUrl}/api/calendar/saves/${saveId}`, { headers })
      .pipe(map((res) => assertSuccess(res, 'Could not remove from this day.')));
  }

  private persistNote(dateKey: string, raw: string): void {
    const body = raw.trim();
    this.putNote(dateKey, body).subscribe({
      next: () => {
        this.dirtyNotes.delete(dateKey);
        if (body) this.noteSaved.set(dateKey, raw);
        else this.noteSaved.delete(dateKey);
      },
      error: () => {
        this.dirtyNotes.delete(dateKey);
        const current = this.notes().get(dateKey) ?? '';
        if (current !== raw) return;
        const saved = this.noteSaved.get(dateKey);
        this.notes.update((m) => {
          const next = new Map(m);
          if (saved) next.set(dateKey, saved);
          else next.delete(dateKey);
          return next;
        });
      },
    });
  }

  private putNote(date: string, body: string): Observable<void> {
    const headers = this.authHeaders();
    if (!headers) return throwError(() => new Error('Please sign in.'));
    return this.http
      .put<ApiWrapper<unknown>>(
        `${environment.apiUrl}/api/calendar/notes/${encodeURIComponent(date)}`,
        { body },
        { headers }
      )
      .pipe(map((res) => assertSuccess(res, 'Could not save the note.')));
  }

  private readLegacy(): { present: boolean; schedule: Map<number, string>; notes: Map<string, string> } {
    const scheduleRaw = this.cache.getItem<Record<string, string>>(SCHEDULE_KEY);
    const notesRaw = this.cache.getItem<Record<string, string>>(NOTES_KEY);
    const schedule = new Map<number, string>();
    if (scheduleRaw) {
      for (const [k, v] of Object.entries(scheduleRaw)) {
        const id = Number(k);
        if (Number.isInteger(id) && id > 0 && typeof v === 'string' && DATE_RE.test(v)) {
          schedule.set(id, v);
        }
      }
    }
    const notes = new Map<string, string>();
    if (notesRaw) {
      for (const [k, v] of Object.entries(notesRaw)) {
        if (DATE_RE.test(k) && typeof v === 'string' && v.trim()) notes.set(k, v);
      }
    }
    return { present: scheduleRaw !== null || notesRaw !== null, schedule, notes };
  }

  private clear(): void {
    this.loadSeq++;
    for (const timer of this.noteTimers.values()) clearTimeout(timer);
    this.noteTimers.clear();
    this.dirtySaves.clear();
    this.dirtyNotes.clear();
    this.noteSaved.clear();
    this.bySave.set(new Map());
    this.notes.set(new Map());
  }

  private authHeaders(): HttpHeaders | null {
    const token = this.authToken.getToken();
    return token ? new HttpHeaders({ Authorization: `Bearer ${token}` }) : null;
  }
}

function slotsToMap(slots: { save_id: number; date: string }[] | undefined): Map<number, string> {
  const map = new Map<number, string>();
  for (const slot of slots ?? []) {
    if (Number.isInteger(slot.save_id) && slot.save_id > 0 && DATE_RE.test(slot.date)) {
      map.set(slot.save_id, slot.date);
    }
  }
  return map;
}

function notesToMap(notes: { date: string; body: string }[] | undefined): Map<string, string> {
  const map = new Map<string, string>();
  for (const note of notes ?? []) {
    if (DATE_RE.test(note.date) && note.body?.trim()) map.set(note.date, note.body);
  }
  return map;
}

/** A save or day that no longer exists should not block the rest of the one-time import. */
function skipGone(err: unknown): Observable<null> {
  if (err instanceof HttpErrorResponse && (err.status === 404 || err.status === 400)) return of(null);
  return throwError(() => err);
}

function assertSuccess<T>(res: ApiWrapper<T> | null, fallback: string): void {
  if (res?.status !== 'SUCCESS') {
    throw new Error(res?.message || fallback);
  }
}
