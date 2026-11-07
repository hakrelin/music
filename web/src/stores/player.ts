import { defineStore } from 'pinia';
import type { Track } from '@/types';
import { fallbackMediaUrl } from '@/utils/media';

const audio = new Audio();
audio.preload = 'auto';

const STORE_VOLUME = 'yue.volume';
let retryTimer = 0;
let loadVersion = 0;
let lastProgressAt = Date.now();
let lastProgressTime = 0;
let wantsPlayback = false;

export const usePlayerStore = defineStore('player', {
  state: () => ({
    ready: false,
    playing: false,
    loading: false,
    time: 0,
    duration: 0,
    volume: (() => {
      const saved = localStorage.getItem(STORE_VOLUME);
      const raw = saved === null ? NaN : Number(saved);
      return Number.isFinite(raw) && raw >= 0 ? Math.min(1, raw) : 0.7;
    })(),
    muted: false,
    blocked: false,
    failed: false,
    trackId: '',
    /** 当前曲目的服务器开始时间与时钟偏移，用于对齐进度。 */
    startedAt: 0,
    offset: 0,
    _inited: false,
    _retries: 0,
  }),
  actions: {
    /** Read wall-clock time on each call; a computed getter would cache Date.now(). */
    targetTime(): number {
      if (!this.startedAt) return 0;
      return Math.max(0, (Date.now() + this.offset - this.startedAt) / 1000);
    },
    init() {
      if (this._inited) return;
      this._inited = true;
      audio.volume = this.volume;
      audio.muted = this.muted;
      audio.addEventListener('timeupdate', () => {
        this.time = audio.currentTime;
        if (Math.abs(audio.currentTime - lastProgressTime) > 0.05) {
          lastProgressAt = Date.now();
          lastProgressTime = audio.currentTime;
        }
      });
      audio.addEventListener('loadedmetadata', () => {
        this.duration = Number.isFinite(audio.duration) ? audio.duration : 0;
        this.seekToTarget(true);
      });
      audio.addEventListener('play', () => {
        this.playing = true;
        this.blocked = false;
      });
      audio.addEventListener('pause', () => {
        this.playing = false;
      });
      audio.addEventListener('error', () => {
        if (wantsPlayback) this.recover();
      });
      window.setInterval(() => {
        if (
          !wantsPlayback ||
          this.blocked ||
          this.failed ||
          audio.ended ||
          !audio.getAttribute('src')
        )
          return;
        if (Date.now() - lastProgressAt > 12000) this.recover();
      }, 1000);
      audio.addEventListener('waiting', () => {
        this.loading = true;
      });
      audio.addEventListener('playing', () => {
        this.loading = false;
        this.failed = false;
      });
      audio.addEventListener('canplay', () => {
        this.ready = true;
        this.loading = false;
      });
      this.ready = audio.readyState >= 1;
    },
    recover() {
      if (retryTimer || !audio.getAttribute('src')) return;
      this.blocked = false;
      if (this._retries >= 2) {
        this.failed = true;
        this.loading = false;
        return;
      }
      this._retries++;
      const version = loadVersion;
      retryTimer = window.setTimeout(() => {
        retryTimer = 0;
        if (version !== loadVersion || !wantsPlayback) return;
        const fallback = fallbackMediaUrl(audio.src);
        if (fallback) audio.src = fallback;
        lastProgressAt = Date.now();
        lastProgressTime = 0;
        this.loading = true;
        audio.load();
        void this.play();
      }, 600 * this._retries);
    },
    load(track: Track | null, startedAt: number, offset: number, autoPlay = true) {
      this.init();
      window.clearTimeout(retryTimer);
      retryTimer = 0;
      loadVersion += 1;
      wantsPlayback = autoPlay;
      lastProgressAt = Date.now();
      lastProgressTime = 0;
      this.startedAt = startedAt;
      this.offset = offset;
      this._retries = 0;
      this.failed = false;
      this.time = 0;
      this.duration = track?.duration || 0;
      if (!track || !track.url) {
        wantsPlayback = false;
        audio.pause();
        audio.removeAttribute('src');
        audio.load();
        this.trackId = '';
        this.playing = false;
        this.loading = false;
        this.blocked = false;
        return;
      }
      if (track.id === this.trackId && audio.getAttribute('src') === track.url) {
        this.seekToTarget(true);
        if (autoPlay && audio.paused) void this.play();
        return;
      }
      this.trackId = track.id;
      this.loading = true;
      audio.src = track.url;
      audio.load();
      if (autoPlay) void this.play();
    },
    async play(): Promise<void> {
      if (!wantsPlayback) lastProgressAt = Date.now();
      wantsPlayback = true;
      this.init();
      if (!audio.getAttribute('src')) return;
      const version = loadVersion;
      try {
        await audio.play();
        if (version === loadVersion) this.blocked = false;
      } catch (error) {
        if (version !== loadVersion) return;
        const name = error instanceof Error ? error.name : '';
        if (name === 'AbortError') return;
        if (name === 'NotAllowedError') {
          this.blocked = true;
          this.loading = false;
        } else {
          this.recover();
        }
      }
    },
    retry() {
      wantsPlayback = true;
      lastProgressAt = Date.now();
      lastProgressTime = 0;
      window.clearTimeout(retryTimer);
      retryTimer = 0;
      loadVersion += 1;
      this._retries = 0;
      this.failed = false;
      audio.load();
      void this.play();
    },
    pause() {
      wantsPlayback = false;
      window.clearTimeout(retryTimer);
      retryTimer = 0;
      audio.pause();
    },
    toggle() {
      if (this.playing) this.pause();
      else {
        this.seekToTarget(true);
        void this.play();
      }
    },
    setVolume(v: number) {
      const vol = Math.max(0, Math.min(1, v));
      this.volume = vol;
      audio.volume = vol;
      localStorage.setItem(STORE_VOLUME, String(vol));
      if (vol > 0 && this.muted) this.toggleMute();
    },
    toggleMute() {
      this.muted = !this.muted;
      audio.muted = this.muted;
    },
    /** 手动跳转到某秒（进度条点击）。 */
    seekTo(sec: number) {
      if (!audio.src) return;
      try {
        audio.currentTime = Math.max(0, Math.min(sec, audio.duration || sec));
      } catch {
        /* 元数据未就绪时忽略 */
      }
    },
    /** 按服务器时间轴校正进度；偏差超过 1.5s 才 seek，避免频繁跳。 */
    seekToTarget(force = false) {
      if (!audio.src || !Number.isFinite(audio.duration) || audio.duration <= 0 || audio.seeking)
        return;
      // Do not interrupt an in-flight seek or chase the clock while the network is buffering.
      if (!force && audio.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) return;
      const target = Math.min(this.targetTime(), audio.duration - 0.5);
      if (target < 0) return;
      if (!force) {
        // Slow connections should keep playing downloaded audio instead of seeking into another request.
        let buffered = false;
        for (let i = 0; i < audio.buffered.length; i++) {
          if (audio.buffered.start(i) <= target && target < audio.buffered.end(i)) buffered = true;
        }
        if (!buffered) return;
      }
      // 只向前纠偏：目标落后于当前位置时不回拉，避免换歌瞬间 seek 触发二次缓冲。
      if (force || target - audio.currentTime > 1.5) {
        try {
          audio.currentTime = target;
        } catch {
          /* 元数据未就绪时忽略 */
        }
      }
    },
    /** 由 room store 在每次收到 startedAt 时调用。 */
    sync(offset: number) {
      this.offset = offset;
      this.seekToTarget();
    },
  },
});
