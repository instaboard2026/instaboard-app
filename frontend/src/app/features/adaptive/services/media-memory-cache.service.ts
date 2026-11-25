import { Injectable, computed, signal } from '@angular/core';

interface CacheEntry {
  objectUrl: string;
  bytes: number;
  lastUsed: number;
}

/** Soft cap so full camera videos cannot exhaust RAM unnoticed. */
const MAX_BYTES = 512 * 1024 * 1024;

/**
 * Session-only blob cache for signed media URLs. Keyed by pathname so refreshed
 * query signatures still reuse the same file. Cleared on explicit Clear or full reload.
 */
@Injectable({ providedIn: 'root' })
export class MediaMemoryCacheService {
  private readonly entries = new Map<string, CacheEntry>();
  private readonly inflight = new Map<string, Promise<string>>();

  readonly bytes = signal(0);
  readonly count = signal(0);
  /** Video length (ms) reported by thumbnail responses, keyed like entries. */
  private readonly durations = signal<ReadonlyMap<string, number>>(new Map());

  /** Duration the server sent with this thumbnail (X-Duration-Ms), or null. */
  durationMs(url: string | null | undefined): number | null {
    if (!url) return null;
    return this.durations().get(this.keyFromUrl(url)) ?? null;
  }
  readonly label = computed(() => formatBytes(this.bytes()));

  /** Stable key across HMAC query rotation. */
  keyFromUrl(url: string): string {
    try {
      const u = new URL(url, typeof location !== 'undefined' ? location.origin : 'http://localhost');
      return u.pathname;
    } catch {
      return url.split('?')[0] || url;
    }
  }

  peek(url: string): string | null {
    const key = this.keyFromUrl(url);
    const e = this.entries.get(key);
    if (!e) return null;
    e.lastUsed = Date.now();
    return e.objectUrl;
  }

  /** Fetch once into memory; return a blob: URL for img/video src. */
  ensure(url: string): Promise<string> {
    const key = this.keyFromUrl(url);
    const hit = this.entries.get(key);
    if (hit) {
      hit.lastUsed = Date.now();
      return Promise.resolve(hit.objectUrl);
    }
    const pending = this.inflight.get(key);
    if (pending) return pending;

    const job = (async () => {
      const res = await fetch(url);
      if (!res.ok) throw new Error(`media cache fetch ${res.status}`);
      const dur = Number(res.headers.get('X-Duration-Ms'));
      if (Number.isFinite(dur) && dur > 0) {
        this.durations.update((m) => new Map(m).set(key, dur));
      }
      const blob = await res.blob();
      const objectUrl = URL.createObjectURL(blob);
      this.entries.set(key, { objectUrl, bytes: blob.size, lastUsed: Date.now() });
      this.inflight.delete(key);
      this.evictIfNeeded();
      this.publish();
      return objectUrl;
    })().catch((err) => {
      this.inflight.delete(key);
      throw err;
    });

    this.inflight.set(key, job);
    return job;
  }

  clear(): void {
    for (const e of this.entries.values()) {
      URL.revokeObjectURL(e.objectUrl);
    }
    this.entries.clear();
    this.inflight.clear();
    this.publish();
  }

  private evictIfNeeded(): void {
    let total = 0;
    for (const e of this.entries.values()) total += e.bytes;
    if (total <= MAX_BYTES) return;

    const ordered = [...this.entries.entries()].sort((a, b) => a[1].lastUsed - b[1].lastUsed);
    for (const [key, e] of ordered) {
      if (total <= MAX_BYTES) break;
      URL.revokeObjectURL(e.objectUrl);
      this.entries.delete(key);
      total -= e.bytes;
    }
  }

  private publish(): void {
    let total = 0;
    for (const e of this.entries.values()) total += e.bytes;
    this.bytes.set(total);
    this.count.set(this.entries.size);
  }
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1024 * 1024) return `${(n / 1024).toFixed(n < 10 * 1024 ? 1 : 0)} KB`;
  if (n < 1024 * 1024 * 1024) return `${(n / (1024 * 1024)).toFixed(n < 10 * 1024 * 1024 ? 1 : 0)} MB`;
  return `${(n / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}
