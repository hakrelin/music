<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import LyricScroller from './LyricScroller.vue';
import { useRoomStore } from '@/stores/room';
import { usePlayerStore } from '@/stores/player';
import { useUiStore } from '@/stores/ui';
import { parseLrc } from '@/utils/lyric';
const room = useRoomStore();
const player = usePlayerStore();
const ui = useUiStore();
const track = computed(() => room.current);
const lines = computed(() => parseLrc(track.value?.lyric));
const broken = ref(false);
watch(
  () => track.value?.cover,
  () => (broken.value = false),
);
</script>
<template>
  <section class="listening" :class="{ 'cover-hidden': !ui.coverVisible }" aria-label="正在播放">
    <div class="listening-label">
      <span class="playing-state"
        ><i class="online-dot" />{{ track ? '正在播放' : '等待点歌' }}</span
      ><span v-if="room.mode.radio" class="auto-label"
        ><Icon name="radio" :size="12" />电台自动补歌</span
      >
      <button
        class="cover-toggle"
        :aria-pressed="ui.coverVisible"
        @click="ui.toggleRoomDisplay('coverVisible')"
      >
        {{ ui.coverVisible ? '隐藏封面' : '显示封面' }}
      </button>
    </div>
    <div v-if="ui.coverVisible" class="album-stage">
      <div class="album-shadow" />
      <div class="album">
        <img
          v-if="track?.cover && !broken"
          :src="track.cover"
          :alt="track.album || track.name"
          @error="broken = true"
        />
        <div v-else class="album-placeholder">
          <Icon name="music" :size="65" /><span>TEA &amp; KOMPEITO</span>
        </div>
      </div>
    </div>
    <div class="song-info">
      <h1 :title="track?.name">{{ track?.name || '第一首歌，听你的' }}</h1>
      <p :title="track?.artist">{{ track?.artist || '点一首喜欢的歌，和大家一起听。' }}</p>
      <div class="song-details">
        <small v-if="track?.album" :title="track.album">{{ track.album }}</small>
        <small v-if="track?.nick" :title="`${track.nick} 点的歌`">{{ track.nick }} 点的歌</small>
      </div>
    </div>
    <div v-if="lines.length" class="lyric-box">
      <LyricScroller :lines="lines" :time="player.time" :trans-raw="room.current?.lyricTrans" />
    </div>
    <div v-else class="instrumental">
      <span class="lyric-dot">♪</span>
      <p>{{ track ? '没有歌词，就听旋律。' : '点一首喜欢的歌，或导入一份歌单。' }}</p>
      <span class="tiny-wave" :class="{ playing: player.playing }" aria-hidden="true"
        ><i /><i /><i /><i /><i
      /></span>
    </div>
  </section>
</template>
<style scoped>
.listening {
  height: 100%;
  min-height: 0;
  display: flex;
  flex-direction: column;
  padding: 12px 24px 16px;
  align-items: center;
  overflow: hidden;
}
.listening-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  align-self: stretch;
  font-size: 10px;
  color: var(--muted);
  gap: 12px;
  flex: none;
  min-height: 32px;
}
.playing-state,
.auto-label {
  display: flex;
  gap: 7px;
  align-items: center;
}
.playing-state .online-dot {
  width: 5px;
  height: 5px;
  box-shadow: none;
}
.auto-label {
  margin-left: auto;
}
.cover-toggle {
  margin-left: auto;
  padding: 6px 10px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: #ffffffb3;
  color: var(--ink-soft);
  font-size: 12px;
  white-space: nowrap;
}
.cover-toggle:hover {
  background: var(--sage);
  color: var(--green);
}
.album-stage {
  position: relative;
  margin: 12px 0 16px;
  flex-shrink: 0;
}
.album {
  position: relative;
  z-index: 1;
  width: clamp(72px, 21dvh, 200px);
  max-width: 100%;
  aspect-ratio: 1;
  border-radius: 15px;
  overflow: hidden;
  box-shadow: 0 12px 32px #26452d1a;
  border: 5px solid #fff;
  background: var(--paper-2);
}
.album img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.album-shadow {
  position: absolute;
  width: 95%;
  height: 95%;
  left: 10%;
  top: 5%;
  background: #e8ecdb;
  border-radius: 13px;
  transform: rotate(8deg);
}
.album-placeholder {
  height: 100%;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 18px;
  flex-direction: column;
  background: linear-gradient(135deg, #e4eddb, #d4e3d5);
  color: #596d5c;
}
.album-placeholder span {
  font-size: 7px;
  letter-spacing: 3px;
}
.song-info {
  text-align: center;
  max-width: 100%;
  width: 100%;
  flex-shrink: 0;
  margin-top: 8px;
}
.song-info h1 {
  font-size: clamp(19px, 1.8vw, 25px);
  line-height: 1.45;
  overflow-wrap: anywhere;
  letter-spacing: -0.6px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.song-info p {
  color: var(--ink-soft);
  font-size: 12px;
  margin-top: 7px;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.song-details {
  display: flex;
  justify-content: center;
  gap: 6px;
}
.song-info small {
  color: var(--muted);
  font-size: 10px;
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.song-info small + small::before {
  content: ' · ';
}
.lyric-box {
  min-height: 0;
  flex: 1;
  width: 100%;
  text-align: center;
  margin-top: 12px;
}
.instrumental {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 10px;
  margin: 22px 0 5px;
  flex: 1;
  min-height: 0;
  color: #636c5c;
}
.lyric-dot {
  font-size: 22px;
  color: #636a5a;
}
.instrumental p {
  font-size: 11px;
}
.tiny-wave {
  display: flex;
  align-items: center;
  gap: 3px;
  height: 20px;
  margin-top: 5px;
}
.tiny-wave i {
  width: 3px;
  height: 8px;
  border-radius: 3px;
  background: #c3d1b3;
}
.tiny-wave i:nth-child(2),
.tiny-wave i:nth-child(4) {
  height: 13px;
}
.tiny-wave i:nth-child(3) {
  height: 20px;
}
.playing i {
  animation: wave 1.3s ease-in-out infinite alternate;
}
.playing i:nth-child(2n) {
  animation-delay: -0.8s;
}
@keyframes wave {
  to {
    transform: scaleY(0.3);
  }
}
@media (max-width: 900px) {
  .listening {
    padding: 10px 18px 12px;
  }
  .song-info h1 {
    font-size: 20px;
  }
  .auto-label {
    font-size: 9px;
  }
}
@media (max-height: 600px) {
  .album-stage { margin: 4px 0; }
  .album { width: 64px; border-width: 3px; }
  .song-info h1 { font-size: 16px; }
  .song-info p { margin-top: 2px; }
  .song-info small { display: none; }
  .lyric-box { margin-top: 4px; }
}
@media (max-height: 500px) and (min-width: 600px) {
  .listening {
    display: grid;
    grid-template-columns: 64px minmax(100px, 1fr) minmax(0, 2fr);
    grid-template-rows: 32px minmax(0, 1fr);
    column-gap: 12px;
  }
  .listening-label { grid-column: 1 / -1; }
  .album-stage { grid-column: 1; grid-row: 2; margin: 0; }
  .song-info { grid-column: 2; grid-row: 2; margin: 0; }
  .lyric-box,
  .instrumental { grid-column: 3; grid-row: 2; height: 100%; margin: 0; }
  .cover-hidden { grid-template-columns: minmax(140px, 1fr) minmax(0, 2fr); }
  .cover-hidden .song-info { grid-column: 1; }
  .cover-hidden .lyric-box,
  .cover-hidden .instrumental { grid-column: 2; }
}
</style>
