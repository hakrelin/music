export interface LyricLine {
  time: number;
  text: string;
}

const TAG = /\[(\d{1,2}):(\d{1,2})(?:[.:](\d{1,3}))?\]/g;

/** 解析 LRC：同一行支持多个时间标签；返回按时间升序的行。 */
export function parseLrc(raw: string | undefined | null): LyricLine[] {
  if (!raw) return [];
  const lines: LyricLine[] = [];
  for (const line of raw.split(/\r?\n/)) {
    const texts = line.replace(TAG, '').trim();
    let m: RegExpExecArray | null;
    TAG.lastIndex = 0;
    while ((m = TAG.exec(line))) {
      const min = Number(m[1]);
      const sec = Number(m[2]);
      const frac = m[3] ? Number(`0.${m[3].padEnd(3, '0')}`) : 0;
      const time = min * 60 + sec + frac;
      if (texts) lines.push({ time, text: texts });
    }
  }
  lines.sort((a, b) => a.time - b.time);
  return lines;
}

/** 当前应高亮的歌词下标；没有匹配返回 -1。 */
export function activeLyricIndex(lines: LyricLine[], time: number): number {
  if (!lines.length) return -1;
  let lo = 0;
  let hi = lines.length - 1;
  if (time < lines[0].time) return -1;
  while (lo <= hi) {
    const mid = (lo + hi) >> 1;
    if (lines[mid].time <= time) lo = mid + 1;
    else hi = mid - 1;
  }
  return hi;
}

export function formatTime(sec: number): string {
  if (!Number.isFinite(sec) || sec <= 0) return '00:00';
  const s = Math.floor(sec);
  const m = Math.floor(s / 60);
  const r = s % 60;
  return `${String(m).padStart(2, '0')}:${String(r).padStart(2, '0')}`;
}
