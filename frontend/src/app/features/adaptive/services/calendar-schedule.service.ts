import { Injectable, computed, inject, signal } from '@angular/core';
import { CacheService } from './cache.service';

const SCHEDULE_KEY = 'calendar_schedule_v1';
const NOTES_KEY = 'calendar_notes_v1';

/** Local schedule: one calendar date (YYYY-MM-DD) per save id. Persisted until a backend exists. */
@Injectable({ providedIn: 'root' })
export class CalendarScheduleService {
  private cache = inject(CacheService);

  /** saveId → YYYY-MM-DD. Read from templates for OnPush reactivity. */
  readonly bySave = signal<ReadonlyMap<number, string>>(this.loadSchedule());
  private readonly notes = signal<ReadonlyMap<string, string>>(this.loadNotes());

  readonly scheduledCount = computed(() => this.bySave().size);

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
    this.bySave.update((m) => {
      const next = new Map(m);
      next.set(saveId, dateKey);
      return next;
    });
    this.persistSchedule();
  }

  unschedule(saveId: number): void {
    this.bySave.update((m) => {
      if (!m.has(saveId)) return m;
      const next = new Map(m);
      next.delete(saveId);
      return next;
    });
    this.persistSchedule();
  }

  noteFor(dateKey: string): string {
    return this.notes().get(dateKey) ?? '';
  }

  setNote(dateKey: string, value: string): void {
    this.notes.update((m) => {
      const next = new Map(m);
      if (value.trim()) next.set(dateKey, value);
      else next.delete(dateKey);
      return next;
    });
    this.persistNotes();
  }

  private loadSchedule(): Map<number, string> {
    const raw = this.cache.getItem<Record<string, string>>(SCHEDULE_KEY);
    const map = new Map<number, string>();
    if (!raw) return map;
    for (const [k, v] of Object.entries(raw)) {
      const id = Number(k);
      if (Number.isFinite(id) && typeof v === 'string' && /^\d{4}-\d{2}-\d{2}$/.test(v)) {
        map.set(id, v);
      }
    }
    return map;
  }

  private persistSchedule(): void {
    const obj: Record<string, string> = {};
    for (const [id, date] of this.bySave()) obj[String(id)] = date;
    this.cache.setItem(SCHEDULE_KEY, obj);
  }

  private loadNotes(): Map<string, string> {
    const raw = this.cache.getItem<Record<string, string>>(NOTES_KEY);
    return new Map(raw ? Object.entries(raw) : []);
  }

  private persistNotes(): void {
    const obj: Record<string, string> = {};
    for (const [k, v] of this.notes()) obj[k] = v;
    this.cache.setItem(NOTES_KEY, obj);
  }
}
