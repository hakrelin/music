import type { Track } from '@/types';

const actions: MediaSessionAction[] = [
  'play',
  'pause',
  'stop',
  'nexttrack',
  'previoustrack',
  'seekbackward',
  'seekforward',
  'seekto',
];
let metadataKey = '';

export function setMediaActions(handlers: Partial<Record<MediaSessionAction, () => void>>) {
  const session = navigator.mediaSession;
  if (!session) return;
  for (const action of actions) {
    try {
      session.setActionHandler(action, handlers[action] || null);
    } catch {
      /* Actions vary between browsers. */
    }
  }
}

export function configureAudioSession(playback: boolean) {
  const session = (navigator as Navigator & { audioSession?: { type: string } }).audioSession;
  if (!session) return;
  try {
    session.type = playback ? 'playback' : 'auto';
  } catch {
    /* Optional API. */
  }
}

export function updateMediaSession(
  track: Track | null,
  playing: boolean,
  position: number,
  duration: number,
) {
  const session = navigator.mediaSession;
  if (!session) return;
  try {
    if (!track) {
      metadataKey = '';
      session.metadata = null;
      session.playbackState = 'none';
      session.setPositionState?.();
      return;
    }
    const key = JSON.stringify([track.id, track.name, track.artist, track.album, track.cover]);
    if (key !== metadataKey && typeof MediaMetadata !== 'undefined') {
      let artwork: MediaImage[] = [];
      if (track.cover) {
        try {
          const src = new URL(track.cover, location.origin);
          if (src.protocol === 'https:' || src.protocol === 'http:') artwork = [{ src: src.href }];
        } catch {
          /* A bad cover must not break playback. */
        }
      }
      session.metadata = new MediaMetadata({
        title: track.name,
        artist: track.artist,
        album: track.album,
        artwork,
      });
      metadataKey = key;
    }
    session.playbackState = playing ? 'playing' : 'paused';
    if (Number.isFinite(duration) && duration > 0 && Number.isFinite(position)) {
      session.setPositionState?.({
        duration,
        position: Math.max(0, Math.min(position, duration)),
        playbackRate: 1,
      });
    } else session.setPositionState?.();
  } catch {
    /* System integration must never interrupt the audio element. */
  }
}
