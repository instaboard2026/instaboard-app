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

  return dedupeUrls(found);
}

/** One detected link for the import-review popup (matches CSV columns when present). */
export interface ImportLinkRow {
  slNo: number;
  url: string;
  selected: boolean;
}

const URL_HEADER_RE = /^(post\s*url|url|link|links|post\s*link)$/i;
const SL_HEADER_RE = /^(sl\s*no\.?|s\.?\s*no\.?|no\.?|serial|index|#)$/i;

/**
 * Turn file or paste text into review rows. Prefers CSV headers like
 * `Sl No,Post URL`; otherwise falls back to extractPasteLinks.
 */
export function parseImportRows(raw: string): ImportLinkRow[] {
  const text = raw.trim();
  if (!text) return [];

  const csvRows = parseCsvImport(text);
  if (csvRows) return csvRows;

  return extractPasteLinks(text).map((url, i) => ({
    slNo: i + 1,
    url,
    selected: true,
  }));
}

function parseCsvImport(text: string): ImportLinkRow[] | null {
  const lines = text.split(/\r?\n/).map((l) => l.trim()).filter(Boolean);
  if (lines.length < 2) return null;

  const headers = splitCsvLine(lines[0]);
  if (headers.length < 2) return null;

  let urlIdx = -1;
  let slIdx = -1;
  for (let i = 0; i < headers.length; i++) {
    const h = headers[i].trim();
    if (urlIdx < 0 && URL_HEADER_RE.test(h)) urlIdx = i;
    if (slIdx < 0 && SL_HEADER_RE.test(h)) slIdx = i;
  }
  if (urlIdx < 0) return null;

  const rows: ImportLinkRow[] = [];
  const seen = new Set<string>();
  let auto = 1;

  for (let li = 1; li < lines.length; li++) {
    const cols = splitCsvLine(lines[li]);
    const cell = (cols[urlIdx] ?? '').trim();
    if (!cell) continue;

    const urls = extractPasteLinks(cell);
    if (urls.length === 0) continue;

    let slNo = auto;
    if (slIdx >= 0) {
      const n = Number.parseInt((cols[slIdx] ?? '').trim(), 10);
      if (Number.isFinite(n) && n > 0) slNo = n;
    }

    for (const url of urls) {
      const key = url.toLowerCase();
      if (seen.has(key)) continue;
      seen.add(key);
      rows.push({ slNo, url, selected: true });
      auto = Math.max(auto, slNo) + 1;
      slNo = auto;
    }
  }

  return rows.length > 0 ? rows : null;
}

/** Simple CSV split that respects double-quoted fields. */
function splitCsvLine(line: string): string[] {
  const out: string[] = [];
  let cur = '';
  let inQuotes = false;
  for (let i = 0; i < line.length; i++) {
    const ch = line[i];
    if (inQuotes) {
      if (ch === '"') {
        if (line[i + 1] === '"') {
          cur += '"';
          i++;
        } else {
          inQuotes = false;
        }
      } else {
        cur += ch;
      }
      continue;
    }
    if (ch === '"') {
      inQuotes = true;
      continue;
    }
    if (ch === ',') {
      out.push(cur);
      cur = '';
      continue;
    }
    cur += ch;
  }
  out.push(cur);
  return out;
}

function dedupeUrls(found: string[]): string[] {
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
