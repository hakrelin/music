import { defineStore } from 'pinia';
import type { Track } from '@/types';

const audio = new Audio();
audio.preload = 'auto';

const STORE_VOLUME = 'yue.volume';

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
  getters: {
    /** 服务器时间轴上的应播位置（秒）。 */
    targetTime(s): number {
      if (!s.startedAt) return 0;
      return Math.max(0, (Date.now() + s.offset - s.startedAt) / 1000);
    },
  },
  actions: {
    init() {
      if (this._inited) return;
      this._inited = true;
      audio.volume = this.volume;
      audio.muted = this.muted;
      audio.addEventListener('timeupdate', () => {
        this.time = audio.currentTime;
      });
      audio.addEventListener('loadedmetadata', () => {
        this.duration = Number.isFinite(audio.duration) ? audio.duration : 0;
        this.seekToTarget();
      });
      audio.addEventListener('play', () => {
        this.playing = true;
        this.blocked = false;
      });
      audio.addEventListener('pause', () => {
        this.playing = false;
      });
      audio.addEventListener('error', () => {
        if (!audio.src) return;
        if (this._retries < 2) {
          this._retries += 1;
          window.setTimeout(() => void this.play(), 600);
        } else {
          this.failed = true;
          this.loading = false;
        }
      });
      audio.addEventListener('waiting', () => {
        this.loading = true;
      });
      audio.addEventListener('playing', () => {
        this.loading = false;
      });
      audio.addEventListener('canplay', () => {
        this.ready = true;
        this.loading = false;
      });
      this.ready = audio.readyState >= 1;
    },
    load(track: Track | null, startedAt: number, offset: number, autoPlay = true) {
      this.init();
      this.startedAt = startedAt;
      this.offset = offset;
      this._retries = 0;
      this.failed = false;
      this.time = 0;
      this.duration = track?.duration || 0;
      if (!track || !track.url) {
        audio.pause();
        audio.removeAttribute('src');
        audio.load();
        this.trackId = '';
        this.playing = false;
        this.loading = false;
        this.blocked = false;
        return;
      }
      if (track.id === this.trackId && audio.src) {
        this.seekToTarget();
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
      this.init();
      if (!audio.src) return;
      try {
        await audio.play();
        this.blocked = false;
      } catch {
        this.blocked = true;
      }
    },
    retry() {
      this._retries = 0;
      this.failed = false;
      audio.load();
      void this.play();
    },
    pause() {
      audio.pause();
    },
    toggle() {
      if (this.playing) this.pause();
      else {
        this.seekToTarget();
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
    seekToTarget() {
      if (!audio.src || !Number.isFinite(audio.duration) || audio.duration <= 0) return;
      const target = Math.min(this.targetTime, audio.duration - 0.5);
      if (target < 0) return;
      if (Math.abs(audio.currentTime - target) > 1.5) {
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
