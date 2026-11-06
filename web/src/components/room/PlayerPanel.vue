<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import LyricScroller from './LyricScroller.vue';
import { useRoomStore } from '@/stores/room';
import { usePlayerStore } from '@/stores/player';
import { parseLrc } from '@/utils/lyric';
const room = useRoomStore();
const player = usePlayerStore();
const track = computed(() => room.current);
const lines = computed(() => parseLrc(track.value?.lyric));
const broken = ref(false);
watch(
  () => track.value?.cover,
  () => (broken.value = false),
);
</script>
<template>
  <section class="listening" aria-label="正在播放">
    <div class="listening-label">
      <span class="playing-state"
        ><i class="online-dot" />{{ track ? '正在播放' : '等待点歌' }}</span
      ><span v-if="room.mode.radio" class="auto-label"
        ><Icon name="radio" :size="12" />电台自动补歌</span
      >
    </div>
    <div class="album-stage">
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
      <span v-if="track?.nick" class="picked-by">{{ track.nick }} 点的歌 <span>♪</span></span>
    </div>
    <div class="song-info">
      <h1>{{ track?.name || '第一首歌，听你的' }}</h1>
      <p>{{ track?.artist || '点一首喜欢的歌，和大家一起听。' }}</p>
      <small v-if="track?.album">{{ track.album }}</small>
    </div>
    <div v-if="lines.length" class="lyric-box">
      <LyricScroller :lines="lines" :time="player.time" />
    </div>
    <div v-else class="instrumental">
      <span class="lyric-dot">♪</span>
      <p>{{ track ? '没有歌词，就听旋律。' : '从左侧点歌，或导入一份歌单。' }}</p>
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
  padding: 24px 35px 20px;
  align-items: center;
  overflow: auto;
}
.listening-label {
  display: flex;
  align-items: center;
  justify-content: space-between;
  align-self: stretch;
  font-size: 10px;
  color: var(--muted);
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
.album-stage {
  position: relative;
  margin: 32px 0 27px;
  flex-shrink: 0;
}
.album {
  position: relative;
  z-index: 1;
  width: clamp(170px, 19vw, 270px);
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
.picked-by {
  position: absolute;
  left: 50%;
  bottom: -17px;
  z-index: 2;
  transform: translateX(-50%);
  padding: 4px 12px;
  white-space: nowrap;
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 20px;
  font-size: 9px;
  color: var(--muted);
  box-shadow: 0 3px 10px #29473106;
}
.picked-by span {
  color: var(--vermilion);
  margin-left: 5px;
}
.song-info {
  text-align: center;
  max-width: 100%;
  flex-shrink: 0;
}
.song-info h1 {
  font-size: clamp(19px, 1.8vw, 25px);
  line-height: 1.45;
  overflow-wrap: anywhere;
  letter-spacing: -0.6px;
}
.song-info p {
  color: var(--ink-soft);
  font-size: 12px;
  margin-top: 7px;
}
.song-info small {
  display: block;
  color: var(--muted);
  font-size: 10px;
  margin-top: 5px;
}
.lyric-box {
  min-height: 115px;
  height: 180px;
  flex: 1;
  max-height: 260px;
  width: 100%;
  text-align: center;
  margin-top: 23px;
}
.instrumental {
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 10px;
  margin: 22px 0 5px;
  flex: 1;
  min-height: 80px;
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
@media (max-height: 800px) and (min-width: 901px) {
  .album {
    width: 180px;
  }
  .album-stage {
    margin: 23px 0 26px;
  }
  .listening {
    padding-top: 18px;
  }
  .song-info h1 {
    font-size: 20px;
  }
  .lyric-box {
    margin-top: 14px;
  }
}
@media (max-width: 900px) {
  .listening {
    padding: 22px 24px;
    min-height: calc(100dvh - 250px);
  }
  .album {
    width: clamp(180px, 50vw, 270px);
  }
  .album-stage {
    margin: 27px 0;
  }
  .song-info h1 {
    font-size: 22px;
  }
  .lyric-box {
    height: 180px;
    flex: none;
  }
  .instrumental {
    min-height: 90px;
  }
}
</style>
