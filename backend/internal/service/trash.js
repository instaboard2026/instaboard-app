ObjC.import('Foundation');
function run(argv) {
  const fm = $.NSFileManager.defaultManager;
  const out = [];
  for (const p of argv) {
    if (!fm.fileExistsAtPath(p)) { out.push('missing\t' + p); continue; }
    const ok = fm.trashItemAtURLResultingItemURLError($.NSURL.fileURLWithPath(p), null, null);
    out.push((ok ? 'ok' : 'err') + '\t' + p);
  }
  return out.join('\n');
}
