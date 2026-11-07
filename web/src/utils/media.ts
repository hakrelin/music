/** Keep the signed path intact; retry only equivalent nodes in NetEase's jdymusic pool. */
export function fallbackMediaUrl(src: string): string | null {
  try {
    const url = new URL(src);
    if (url.protocol !== 'https:' || (url.port && url.port !== '443')) return null;
    if (!/^m(?:7|8)0[14]\.music\.126\.net$/.test(url.hostname)) return null;
    if (!url.pathname.includes('/jdymusic/')) return null;
    const primary = url.hostname.startsWith('m8') ? 'm801.music.126.net' : 'm701.music.126.net';
    url.hostname =
      url.hostname === primary
        ? primary === 'm701.music.126.net'
          ? 'm801.music.126.net'
          : 'm701.music.126.net'
        : primary;
    return url.toString();
  } catch {
    return null;
  }
}

// Reuse only an exact signed URL whose alternate has actually reached canplay.
// Never infer that the same host substitution works for another path or song.
const verified = new Map<string, { url: string; expires: number }>();
export function preferredMediaUrl(original: string): string {
  const entry = verified.get(original);
  if (entry && entry.expires > Date.now()) return entry.url;
  verified.delete(original);
  return original;
}
export function rememberMediaSuccess(original: string, actual: string) {
  if (!original || !actual || original === actual) return;
  let url = original;
  let allowed = false;
  for (let i = 0; i < 2; i++) {
    const next = fallbackMediaUrl(url);
    if (!next) break;
    if (next === actual) allowed = true;
    url = next;
  }
  if (!allowed) return;
  if (verified.size >= 32) verified.delete(verified.keys().next().value!);
  verified.set(original, { url: actual, expires: Date.now() + 120000 });
}
export function forgetMediaUrl(original: string) {
  verified.delete(original);
}
