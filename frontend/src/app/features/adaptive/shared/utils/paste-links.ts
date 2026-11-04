/**
 * Pull Instagram / YouTube / Pinterest URLs out of a messy paste
 * (newlines, commas, spaces, or no separators).
 */
export function extractPasteLinks(raw: string): string[] {
  const text = raw.trim();
  if (!text) return [];

  const found: string[] = [];
  const httpRe = /https?:\/\/[^\s<>"'`]+/gi;
  let m: RegExpExecArray | null;
  while ((m = httpRe.exec(text)) !== null) {
    found.push(cleanUrlToken(m[0]));
  }

  for (const part of text.split(/[\s,;|]+/)) {
    const t = cleanUrlToken(part);
    if (!t || /^https?:\/\//i.test(t)) continue;
    if (isSupportedHostPath(t)) {
      found.push(t.startsWith('//') ? `https:${t}` : `https://${t}`);
    }
  }

  const seen = new Set<string>();
  const out: string[] = [];
  for (const u of found) {
    const key = u.toLowerCase();
    if (seen.has(key)) continue;
    seen.add(key);
    out.push(u);
  }
  return out;
}

function cleanUrlToken(s: string): string {
  return s.trim().replace(/^["'<(]+/, '').replace(/[.,;:!?)"'>]+$/g, '');
}

function isSupportedHostPath(t: string): boolean {
  const lower = t.toLowerCase();
  return (
    lower.includes('instagram.com/') ||
    lower.includes('youtube.com/') ||
    lower.includes('youtu.be/') ||
    lower.includes('pin.it/') ||
    /pinterest\.[a-z.]+\/+/i.test(lower)
  );
}
