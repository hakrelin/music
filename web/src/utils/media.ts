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

/**
 * 同源兜底地址：服务端会把「已经在对象存储里」的曲目 302 到 COS 预签名地址，
 * 没缓存就 302 到它自己挑的健康 CDN 节点。浏览器只跟自己的域名打交道，
 * 因此不受网易云节点风控、签名过期、Referer 策略影响。
 */
export function streamFallbackUrl(track: { id: string; source?: string } | null): string {
  if (!track?.id) return '';
  const source = track.source || 'wy';
  return `/api/stream/${encodeURIComponent(track.id)}?source=${encodeURIComponent(source)}`;
}
