import { defineStore } from 'pinia';
import { markRaw } from 'vue';
import type { Track } from '@/types';
import {
  fallbackMediaUrl,
  preferredMediaUrl,
  rememberMediaSuccess,
  forgetMediaUrl,
} from '@/utils/media';

let audio = new Audio();
audio.preload = 'auto';
const STORE_VOLUME = 'yue.volume';
let retryTimer = 0;
let loadVersion = 0;
let recovering = false;
let lastProgressAt = Date.now();
let lastProgressTime = 0;
let wantsPlayback = false;
type RefreshMedia = (track: Track, failedUrl: string, lower: boolean) => Promise<Track>;
let refreshMedia: RefreshMedia | null = null;
let sourceURL = '';
let prepared: {
  track: Track;
  media: HTMLAudioElement;
  timer: number;
  retries: number;
  refreshing: boolean;
  resolvedURL: string;
} | null = null;
const sameEntry = (a: Track | null, b: Track | null) =>
  !!a && !!b && (a.queueId ? a.queueId === b.queueId : a.source === b.source && a.id === b.id);

function clearPrepared() {
  const old = prepared;
  prepared = null;
  if (!old) return;
  window.clearTimeout(old.timer);
  old.media.pause();
  old.media.removeAttribute('src');
  old.media.load();
}

export const usePlayerStore = defineStore('player', {
  state: () => ({
    // The active element changes only when a prepared next track is promoted.
    media: markRaw(audio),
    track: null as Track | null,
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
    startedAt: 0,
    offset: 0,
    _inited: false,
    _retries: 0,
  }),
  actions: {
    targetTime(): number {
      return this.startedAt ? Math.max(0, (Date.now() + this.offset - this.startedAt) / 1000) : 0;
    },
    setRefreshHandler(handler: RefreshMedia | null) {
      refreshMedia = handler;
    },
    bindMedia(el: HTMLAudioElement) {
      const on = (name: string, handler: () => void) =>
        el.addEventListener(name, () => {
          if (el === audio) handler();
        });
      on('timeupdate', () => {
        this.time = el.currentTime;
        if (Math.abs(el.currentTime - lastProgressTime) > 0.05) {
          lastProgressAt = Date.now();
          lastProgressTime = el.currentTime;
        }
      });
      on('loadedmetadata', () => {
        this.duration = Number.isFinite(el.duration) ? el.duration : 0;
        this.seekToTarget(true);
      });
      on('play', () => {
        this.playing = true;
        this.blocked = false;
      });
      on('pause', () => {
        this.playing = false;
      });
      on('error', () => {
        if (wantsPlayback) this.recover();
      });
      on('waiting', () => {
        this.loading = true;
      });
      on('playing', () => {
        this.loading = false;
        this.failed = false;
      });
      on('canplay', () => {
        this.ready = true;
        this.loading = false;
        if (this.track) rememberMediaSuccess(this.track.url, el.getAttribute('src') || '');
      });
    },
    init() {
      if (this._inited) return;
      this._inited = true;
      this.bindMedia(audio);
      audio.volume = this.volume;
      audio.muted = this.muted;
      window.setInterval(() => {
        if (
          wantsPlayback &&
          !this.blocked &&
          !this.failed &&
          !audio.ended &&
          audio.getAttribute('src') &&
          Date.now() - lastProgressAt >
            (audio.readyState < HTMLMediaElement.HAVE_CURRENT_DATA ? 5000 : 12000)
        )
          this.recover();
      }, 1000);
    },
    prepareNext(track: Track | null) {
      this.init();
      if (sameEntry(prepared?.track || null, track) && prepared?.track.url === track?.url) return;
      clearPrepared();
      if (!track?.url || sameEntry(this.track, track)) return;
      const next = new Audio();
      next.preload = 'auto';
      this.bindMedia(next);
      const candidate = {
        track: { ...track },
        media: next,
        timer: 0,
        retries: 0,
        refreshing: false,
        resolvedURL: track.url,
      };
      prepared = candidate;
      const fallback = async () => {
        if (prepared !== candidate || next === audio || candidate.refreshing) return;
        window.clearTimeout(candidate.timer);
        if (candidate.retries >= 2) return;
        candidate.retries++;
        let url = fallbackMediaUrl(next.src);
        if (!url && refreshMedia && track.queueId) {
          candidate.refreshing = true;
          try {
            const fresh = await refreshMedia(track, candidate.resolvedURL, candidate.retries > 1);
            candidate.resolvedURL = fresh.url;
            url = fresh.url;
          } catch {
            /* Current playback can still request a fresh source later. */
          }
          candidate.refreshing = false;
        }
        if (prepared !== candidate || next === audio || !url) return;
        next.src = url;
        next.load();
        candidate.timer = window.setTimeout(() => {
          if (next.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) void fallback();
        }, 3500);
      };
      next.addEventListener('error', () => {
        window.clearTimeout(candidate.timer);
        candidate.timer = window.setTimeout(() => void fallback(), candidate.retries ? 2200 : 0);
      });
      next.addEventListener('canplay', () => {
        window.clearTimeout(candidate.timer);
        rememberMediaSuccess(candidate.track.url, next.getAttribute('src') || '');
      });
      next.src = preferredMediaUrl(track.url);
      next.load();
      candidate.timer = window.setTimeout(() => {
        if (next.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) fallback();
      }, 3500);
    },
    recover() {
      if (retryTimer || recovering || !wantsPlayback || !audio.getAttribute('src')) return;
      if (this._retries >= 2) {
        this.failed = true;
        this.loading = false;
        return;
      }
      this._retries++;
      const attempt = this._retries;
      const version = loadVersion;
      const track = this.track;
      if (track) forgetMediaUrl(track.url);
      retryTimer = window.setTimeout(
        async () => {
          retryTimer = 0;
          if (version !== loadVersion || !wantsPlayback) return;
          recovering = true;
          let refreshed: Track | null = null;
          try {
            if (refreshMedia && track?.queueId)
              refreshed = await refreshMedia(track, sourceURL || track.url, attempt > 1);
          } catch {
            /* A local CDN fallback remains available when the API is temporarily unavailable. */
          }
          if (version !== loadVersion || !wantsPlayback) return;
          recovering = false;
          const oldUrl = audio.getAttribute('src') || '';
          let url = refreshed?.url || '';
          if (!url || url === oldUrl) url = fallbackMediaUrl(oldUrl) || url || oldUrl;
          if (refreshed) sourceURL = refreshed.url;
          lastProgressAt = Date.now();
          lastProgressTime = 0;
          this.loading = true;
          this.blocked = false;
          audio.src = url;
          audio.load();
          void this.play();
        },
        attempt === 1 ? 400 : 2200,
      );
    },
    load(track: Track | null, startedAt: number, offset: number, autoPlay = true) {
      this.init();
      const unchanged =
        sameEntry(this.track, track) &&
        this.startedAt === startedAt &&
        this.track?.url === track?.url;
      this.offset = offset;
      if (unchanged) return;
      window.clearTimeout(retryTimer);
      retryTimer = 0;
      recovering = false;
      loadVersion++;
      wantsPlayback = autoPlay;
      lastProgressAt = Date.now();
      lastProgressTime = 0;
      this.startedAt = startedAt;
      this._retries = 0;
      this.failed = false;
      this.blocked = false;
      this.track = track ? { ...track } : null;
      sourceURL = track?.url || '';
      this.time = 0;
      this.duration = track?.duration || 0;
      this.trackId = track?.id || '';
      if (!track?.url) {
        wantsPlayback = false;
        if (!track) clearPrepared();
        audio.pause();
        audio.removeAttribute('src');
        audio.load();
        this.playing = false;
        this.loading = false;
        this.ready = false;
        return;
      }
      if (
        sameEntry(prepared?.track || null, track) &&
        prepared?.track.url === track.url &&
        !prepared.media.error
      ) {
        const old = audio;
        const next = prepared!;
        prepared = null;
        window.clearTimeout(next.timer);
        audio = next.media;
        sourceURL = next.resolvedURL;
        this.media = markRaw(audio);
        old.pause();
        old.removeAttribute('src');
        old.load();
        audio.volume = this.volume;
        audio.muted = this.muted;
        this.ready = audio.readyState >= HTMLMediaElement.HAVE_METADATA;
        this.loading = audio.readyState < HTMLMediaElement.HAVE_FUTURE_DATA;
        this.duration = Number.isFinite(audio.duration) ? audio.duration : track.duration;
        this.seekToTarget(true);
      } else {
        if (sameEntry(prepared?.track || null, track)) clearPrepared();
        this.ready = false;
        this.loading = true;
        if (audio.getAttribute('src') === track.url) this.seekToTarget(true);
        else {
          audio.src = preferredMediaUrl(track.url);
          audio.load();
        }
      }
      if (autoPlay) void this.play();
    },
    async play(): Promise<void> {
      this.init();
      if (!wantsPlayback) lastProgressAt = Date.now();
      wantsPlayback = true;
      if (!audio.getAttribute('src')) return;
      const version = loadVersion;
      try {
        await audio.play();
        if (version === loadVersion) this.blocked = false;
      } catch (e) {
        if (version !== loadVersion) return;
        const name = e instanceof Error ? e.name : '';
        if (name === 'AbortError') return;
        if (name === 'NotAllowedError') {
          this.blocked = true;
          this.loading = false;
        } else this.recover();
      }
    },
    retry() {
      window.clearTimeout(retryTimer);
      retryTimer = 0;
      recovering = false;
      loadVersion++;
      this._retries = 0;
      this.failed = false;
      wantsPlayback = true;
      this.recover();
    },
    pause() {
      wantsPlayback = false;
      loadVersion++;
      recovering = false;
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
      this.volume = Math.max(0, Math.min(1, v));
      audio.volume = this.volume;
      localStorage.setItem(STORE_VOLUME, String(this.volume));
      if (this.volume > 0 && this.muted) this.toggleMute();
    },
    toggleMute() {
      this.muted = !this.muted;
      audio.muted = this.muted;
    },
    seekTo(sec: number) {
      if (!audio.getAttribute('src')) return;
      try {
        audio.currentTime = Math.max(0, Math.min(sec, audio.duration || sec));
      } catch {
        /* metadata not ready */
      }
    },
    seekToTarget(force = false) {
      if (
        !audio.getAttribute('src') ||
        !Number.isFinite(audio.duration) ||
        audio.duration <= 0 ||
        audio.seeking
      )
        return;
      if (!force && audio.readyState < HTMLMediaElement.HAVE_FUTURE_DATA) return;
      const target = Math.min(this.targetTime(), audio.duration - 0.5);
      if (target < 0) return;
      if (!force) {
        let buffered = false;
        for (let i = 0; i < audio.buffered.length; i++)
          if (audio.buffered.start(i) <= target && target < audio.buffered.end(i)) buffered = true;
        if (!buffered) return;
      }
      if (force || target - audio.currentTime > 1.5) this.seekTo(target);
    },
    sync(offset: number) {
      this.offset = offset;
      this.seekToTarget();
    },
  },
});
