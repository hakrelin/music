<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import LyricScroller from '@/components/room/LyricScroller.vue';
import { usePlayerStore } from '@/stores/player';
import { useRoomStore } from '@/stores/room';
import { useUiStore } from '@/stores/ui';
import { formatTime, parseLrc } from '@/utils/lyric';

const room = useRoomStore();
const player = usePlayerStore();
const ui = useUiStore();

const track = computed(() => room.current);
const lines = computed(() => parseLrc(track.value?.lyric));
const cover = computed(() => track.value?.cover || '');
const progress = computed(() => {
  const dur = player.duration || track.value?.duration || 0;
  return dur > 0 ? Math.min(100, (player.time / dur) * 100) : 0;
});
const volumePct = ref(Math.round(player.volume * 100));

watch(
  () => player.volume,
  (v) => {
    volumePct.value = Math.round(v * 100);
  }
);

function onVolume(ev: Event) {
  const v = Number((ev.target as HTMLInputElement).value);
  volumePct.value = v;
  player.setVolume(v / 100);
}

function like() {
  if (!track.value) return;
  room.good(track.value.id);
  ui.ok('已点赞');
}

function seek(ev: MouseEvent) {
  const bar = ev.currentTarget as HTMLElement;
  const rect = bar.getBoundingClientRect();
  const ratio = Math.max(0, Math.min(1, (ev.clientX - rect.left) / rect.width));
  const dur = player.duration || track.value?.duration || 0;
  if (dur > 0) player.seekTo(dur * ratio);
}

// 移动端：播放被浏览器拦截时给一个显式入口
onMounted(() => {
  player.init();
});
onBeforeUnmount(() => {
  /* 音频由 player store 持有，离开房间时暂停 */
  player.pause();
});
</script>

<template>
  <section class="player card" aria-label="正在播放">
    <div class="disc-wrap">
      <div class="disc" :class="{ spinning: player.playing }">
        <img v-if="cover" :src="cover" :alt="track?.name || '唱片封面'" />
        <div v-else class="disc-blank"><Icon name="music" :size="30" /></div>
      </div>
      <div class="disc-ring"></div>
      <p class="disc-hint" v-if="!player.playing && !player.blocked">已暂停</p>
    </div>

    <div class="info">
      <div class="head">
        <p class="eyebrow light">正在播放</p>
        <span v-if="room.mode.radio" class="tag night">电台补队</span>
        <span v-if="room.mode.loop" class="tag night">循环</span>
        <span v-if="room.mode.random" class="tag night">随机</span>
      </div>

      <template v-if="track">
        <h1 class="title">{{ track.name }}</h1>
        <p class="meta">
          {{ track.artist }}
          <template v-if="track.album"> · 《{{ track.album }}》</template>
        </p>
      </template>
      <template v-else>
        <h1 class="title empty-title">还没有人在播放</h1>
        <p class="meta">点一首歌，让这间房响起来。</p>
      </template>

      <div class="lyric-box">
        <LyricScroller :lines="lines" :time="player.time" />
      </div>

      <div class="progress-row">
        <span class="t">{{ formatTime(player.time) }}</span>
        <div class="bar" role="slider" aria-label="播放进度" @click="seek">
          <i :style="{ width: progress + '%' }"></i>
        </div>
        <span class="t">{{ formatTime(player.duration || track?.duration || 0) }}</span>
      </div>

      <div class="controls">
        <div class="ctrl-left">
          <button class="play" type="button" :aria-label="player.playing ? '暂停' : '播放'" @click="player.toggle()">
            <Icon :name="player.playing ? 'pause' : 'play'" :size="20" />
          </button>
          <button class="vote" type="button" title="投票切歌（超过三成在听的人投票就换歌）" @click="room.voteSkip()">
            <Icon name="next" :size="16" />
            投票切歌
          </button>
          <button class="ghost-btn" type="button" :class="{ liked: false }" title="给这首歌点个赞" @click="like">
            <Icon name="heart" :size="16" />
            {{ track?.likes || 0 }}
          </button>
        </div>

        <div class="ctrl-right">
          <button class="icon-btn night" type="button" :aria-label="player.muted ? '取消静音' : '静音'" @click="player.toggleMute()">
            <Icon :name="player.muted ? 'mute' : 'volume'" :size="17" />
          </button>
          <input
            class="vol"
            type="range"
            min="0"
            max="100"
            :value="volumePct"
            aria-label="音量"
            @input="onVolume"
          />
        </div>
      </div>

      <div v-if="player.blocked" class="blocked">
        <button class="btn btn-lime btn-sm" type="button" @click="player.play()">点击开始同步收听</button>
        <span>浏览器需要一次点击才允许播放声音</span>
      </div>
    </div>
  </section>
</template>

<style scoped>
.card {
  background: rgba(255, 255, 255, 0.035);
  border: 1px solid var(--line-night);
  border-radius: var(--r-l);
  backdrop-filter: blur(10px);
}
.player {
  display: grid;
  grid-template-columns: 220px minmax(0, 1fr);
  gap: clamp(22px, 3vw, 40px);
  padding: clamp(22px, 2.6vw, 34px);
}
.disc-wrap {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 14px;
  padding-top: 6px;
}
.disc {
  width: 200px;
  height: 200px;
  border-radius: 50%;
  padding: 10px;
  background: radial-gradient(circle at 50% 50%, #0a1d19 0%, #06110f 62%, #000 100%);
  box-shadow: 0 0 0 1px rgba(226, 237, 186, 0.14), 0 24px 60px rgba(0, 0, 0, 0.5);
  position: relative;
  overflow: hidden;
}
.disc::after {
  content: "";
  position: absolute;
  inset: 10px;
  border-radius: 50%;
  pointer-events: none;
  background: radial-gradient(circle at 32% 26%, rgba(255, 255, 255, 0.16), transparent 46%);
}
.disc img {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  object-fit: cover;
  opacity: 0.92;
}
.disc-blank {
  width: 100%;
  height: 100%;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: radial-gradient(circle, #1b4036, #0a1d19);
  color: var(--lime);
}
.disc.spinning img,
.disc.spinning .disc-blank {
  animation: spin 26s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.disc-ring {
  position: absolute;
  top: 6px;
  width: 200px;
  height: 200px;
  border-radius: 50%;
  border: 1px dashed rgba(226, 237, 186, 0.16);
  pointer-events: none;
}
.disc-hint {
  font-size: 10.5px;
  letter-spacing: 0.24em;
  color: rgba(232, 239, 220, 0.4);
}
.head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.eyebrow.light {
  color: rgba(232, 239, 220, 0.5);
}
.eyebrow.light::before {
  color: rgba(232, 239, 220, 0.25);
}
.title {
  margin: 14px 0 8px;
  font-size: clamp(24px, 2.4vw, 34px);
  color: #f2f6e9;
  line-height: 1.4;
}
.empty-title {
  color: rgba(232, 239, 220, 0.6);
}
.meta {
  font-size: 12.5px;
  color: rgba(232, 239, 220, 0.6);
  letter-spacing: 0.04em;
}
.lyric-box {
  height: 132px;
  margin: 16px 0 6px;
  border-top: 1px solid var(--line-night);
  border-bottom: 1px solid var(--line-night);
  padding: 10px 0;
}
.progress-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin: 14px 0 4px;
}
.t {
  font-size: 10.5px;
  font-variant-numeric: tabular-nums;
  color: rgba(232, 239, 220, 0.45);
  letter-spacing: 0.06em;
}
.bar {
  flex: 1;
  height: 3px;
  border-radius: 3px;
  background: rgba(232, 239, 220, 0.16);
  cursor: pointer;
  position: relative;
}
.bar i {
  display: block;
  height: 100%;
  border-radius: 3px;
  background: linear-gradient(90deg, #b9d47c, var(--lime));
  transition: width 0.25s linear;
}
.controls {
  margin-top: 14px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
}
.ctrl-left {
  display: flex;
  align-items: center;
  gap: 12px;
}
.play {
  width: 46px;
  height: 46px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  background: var(--lime);
  color: var(--deep);
  transition: transform 0.2s ease, background 0.2s ease;
}
.play:hover {
  transform: scale(1.05);
  background: #eaf3c9;
}
.vote,
.ghost-btn {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 36px;
  padding: 0 15px;
  border-radius: 999px;
  border: 1px solid var(--line-night-strong);
  color: rgba(232, 239, 220, 0.78);
  font-size: 12px;
  letter-spacing: 0.06em;
  transition: border-color 0.2s ease, color 0.2s ease;
}
.vote:hover,
.ghost-btn:hover {
  border-color: var(--lime);
  color: var(--lime);
}
.ctrl-right {
  display: flex;
  align-items: center;
  gap: 10px;
}
.vol {
  width: 96px;
  accent-color: var(--lime);
  height: 3px;
}
.blocked {
  margin-top: 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  flex-wrap: wrap;
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.55);
}
@media (max-width: 900px) {
  .player {
    grid-template-columns: 1fr;
  }
  .disc-wrap {
    flex-direction: row;
    gap: 18px;
    align-items: center;
    justify-content: center;
  }
  .disc {
    width: 104px;
    height: 104px;
    padding: 6px;
    flex: none;
  }
  .disc-ring {
    display: none;
  }
  .lyric-box {
    height: 112px;
  }
}
</style>
