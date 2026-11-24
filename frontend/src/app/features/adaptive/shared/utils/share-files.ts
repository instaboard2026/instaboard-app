/** Soft cap so multi-select share cannot load unbounded camera/folder video into RAM. */
export const SHARE_MAX_BYTES = 1.5 * 1024 * 1024 * 1024;

export interface ShareFileInput {
  name: string;
  url: string;
  byteSize: number;
}

export type ShareFilesResult =
  | { kind: 'empty' }
  | { kind: 'too_large'; bytes: number }
  | { kind: 'shared' }
  | { kind: 'downloaded' }
  | { kind: 'cancelled' }
  | { kind: 'error'; message: string };

export interface ShareFilesOptions {
  /** Called before each fetch (1-based index). */
  onProgress?: (current: number, total: number) => void;
}

/**
 * Fetch signed media URLs into Files, then open the system share sheet (AirDrop on Safari)
 * or fall back to downloads when file Web Share is unavailable.
 */
export async function shareFilesFromUrls(
  items: ShareFileInput[],
  options: ShareFilesOptions = {},
): Promise<ShareFilesResult> {
  if (!items.length) return { kind: 'empty' };

  let totalBytes = 0;
  for (const it of items) {
    const n = it.byteSize;
    if (Number.isFinite(n) && n > 0) totalBytes += n;
  }
  if (totalBytes > SHARE_MAX_BYTES) {
    return { kind: 'too_large', bytes: totalBytes };
  }

  const files: File[] = [];
  try {
    for (let i = 0; i < items.length; i++) {
      const item = items[i];
      options.onProgress?.(i + 1, items.length);
      const res = await fetch(item.url);
      if (!res.ok) {
        return { kind: 'error', message: `Could not load ${item.name}.` };
      }
      const blob = await res.blob();
      const type = blob.type || mimeFromName(item.name);
      files.push(new File([blob], item.name, { type }));
    }
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Could not prepare files.';
    return { kind: 'error', message: message.charAt(0).toUpperCase() + message.slice(1) };
  }

  const canShareFiles =
    typeof navigator !== 'undefined' &&
    typeof navigator.share === 'function' &&
    (typeof navigator.canShare !== 'function' || navigator.canShare({ files }));

  if (canShareFiles) {
    try {
      await navigator.share({ files });
      return { kind: 'shared' };
    } catch (err) {
      if (isShareCancel(err)) return { kind: 'cancelled' };
      // Lost user activation or batch rejected — fall through to downloads.
      if (!isNotAllowed(err) && !isShareReject(err)) {
        const message = err instanceof Error ? err.message : 'Share failed.';
        return { kind: 'error', message: message.charAt(0).toUpperCase() + message.slice(1) };
      }
    }
  }

  try {
    downloadFiles(files);
    return { kind: 'downloaded' };
  } catch (err) {
    const message = err instanceof Error ? err.message : 'Download failed.';
    return { kind: 'error', message: message.charAt(0).toUpperCase() + message.slice(1) };
  }
}

/** User-facing notice for a helper result (null = clear / silent). */
export function shareFilesNotice(result: ShareFilesResult): string | null {
  switch (result.kind) {
    case 'empty':
      return 'Nothing to share.';
    case 'too_large':
      return `Selection is too large to share in the browser (${formatShareBytes(result.bytes)}). Select less than ${formatShareBytes(SHARE_MAX_BYTES)}.`;
    case 'shared':
      return null;
    case 'downloaded':
      return 'Downloaded — open Finder and use AirDrop, or use Safari for Share.';
    case 'cancelled':
      return null;
    case 'error':
      return result.message;
  }
}

export function formatShareBytes(bytes: number): string {
  if (!Number.isFinite(bytes) || bytes < 0) return '—';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'] as const;
  let n = bytes;
  let i = 0;
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024;
    i++;
  }
  const digits = i === 0 ? 0 : n >= 10 ? 0 : 1;
  return `${n.toFixed(digits)} ${units[i]}`;
}

function mimeFromName(name: string): string {
  const ext = name.includes('.') ? name.slice(name.lastIndexOf('.') + 1).toLowerCase() : '';
  switch (ext) {
    case 'mp4':
      return 'video/mp4';
    case 'mov':
      return 'video/quicktime';
    case 'm4v':
      return 'video/x-m4v';
    case 'webm':
      return 'video/webm';
    case 'jpg':
    case 'jpeg':
      return 'image/jpeg';
    case 'png':
      return 'image/png';
    case 'gif':
      return 'image/gif';
    case 'webp':
      return 'image/webp';
    case 'heic':
      return 'image/heic';
    case 'heif':
      return 'image/heif';
    default:
      return 'application/octet-stream';
  }
}

function downloadFiles(files: File[]): void {
  for (const file of files) {
    const objectUrl = URL.createObjectURL(file);
    const a = document.createElement('a');
    a.href = objectUrl;
    a.download = file.name;
    a.rel = 'noopener';
    a.style.display = 'none';
    document.body.appendChild(a);
    a.click();
    a.remove();
    // Defer revoke so the browser can start the download.
    window.setTimeout(() => URL.revokeObjectURL(objectUrl), 60_000);
  }
}

function isShareCancel(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false;
  const name = (err as { name?: string }).name;
  return name === 'AbortError';
}

function isNotAllowed(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false;
  return (err as { name?: string }).name === 'NotAllowedError';
}

function isShareReject(err: unknown): boolean {
  if (!err || typeof err !== 'object') return false;
  const name = (err as { name?: string }).name;
  // InvalidStateError / TypeError / DataError often mean the payload was rejected.
  return name === 'InvalidStateError' || name === 'TypeError' || name === 'DataError';
}
