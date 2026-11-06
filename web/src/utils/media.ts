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
