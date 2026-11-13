<script setup lang="ts">
import { computed } from 'vue';
import Icon from '@/components/Icon.vue';
import { useRoomStore } from '@/stores/room';
import { usePlayerStore } from '@/stores/player';
import { useUiStore } from '@/stores/ui';
import { formatTime } from '@/utils/lyric';
import { icpForHost } from '@/utils/site';
const room = useRoomStore();
const player = usePlayerStore();
const ui = useUiStore();
const icp = icpForHost();
const liked = computed(
  () => !!room.current && room.likedIds.includes(room.current.queueId || ''),
);
async function like() {
  const cur = room.current;
  if (!cur) return;
  try {
    await room.goodTrack(cur.id, cur.queueId);
    ui.ok('已点赞');
  } catch (e) {
    ui.warn(e instanceof Error ? e.message : '点赞失败');
  }
}
const duration = computed(() => player.duration || room.current?.duration || 0);
const progress = computed(() =>
  duration.value ? Math.min(100, (player.time / duration.value) * 100) : 0,
);
</script>
<template>
  <footer class="player-bar" :class="{ 'has-icp': icp }" aria-label="播放控制">
    <div class="bar-track">
      <div class="bar-cover">
        <img
          v-if="room.current?.cover"
          :src="room.current.cover"
          alt=""
          @error="($event.target as HTMLImageElement).style.display = 'none'"
        /><Icon v-else name="music" :size="20" />
      </div>
      <div class="bar-track-info">
        <b>{{ room.current?.name || '还没有歌曲' }}</b
        ><span>{{ room.current?.artist || '点一首喜欢的歌吧' }}</span>
      </div>
      <button
        v-if="room.mode.goodMode"
        class="icon-btn like" :class="{ liked }"
        :disabled="!room.current || liked"
        aria-label="点赞当前歌曲"
        @click="like()"
      >
        <Icon name="heart" :size="18" /><span>{{ room.current?.likes || 0 }}</span>
      </button>
    </div>
    <div class="playback">
      <div class="playback-buttons">
        <span class="sync-label"
          ><i class="online-dot" />{{
            player.loading ? '缓冲中' : player.playing ? '同步收听中' : '本地已暂停'
          }}</span
        ><button
          class="play-button"
          :disabled="!room.current"
          :aria-label="player.playing ? '暂停收听' : '开始同步收听'"
          @click="player.toggle()"
        >
          <Icon :name="player.playing ? 'pause' : 'play'" :size="22" /></button
        ><button
          class="skip"
          :disabled="!room.current || room.mode.banSwitch"
          title="投票切换到下一首"
          @click="room.voteSkip()"
        >
          <Icon name="next" :size="18" /><span
            >切歌{{ room.votes ? ' ' + room.votes + '/' + room.voteNeed : '' }}</span
          >
        </button>
      </div>
      <div class="timeline">
        <span>{{ formatTime(player.time) }}</span>
        <div
          class="progress"
          role="progressbar"
          aria-label="房间同步播放进度"
          :aria-valuenow="Math.round(progress)"
          aria-valuemin="0"
          aria-valuemax="100"
        >
          <i :style="{ width: progress + '%' }" />
        </div>
        <span>{{ formatTime(duration) }}</span>
      </div>
    </div>
    <div class="bar-aside">
      <div class="volume-controls">
        <button
          class="icon-btn"
          :aria-label="player.muted ? '取消静音' : '静音'"
          @click="player.toggleMute()"
        >
          <Icon :name="player.muted ? 'mute' : 'volume'" :size="18" /></button
        ><input
          type="range"
          min="0"
          max="100"
          :value="Math.round(player.volume * 100)"
          aria-label="音量"
          @input="player.setVolume(Number(($event.target as HTMLInputElement).value) / 100)"
        /><span>{{ Math.round(player.volume * 100) }}%</span>
      </div>
      <a
        v-if="icp"
        class="beian"
        :href="icp.href"
        target="_blank"
        rel="noreferrer noopener"
      >{{ icp.text }}</a>
    </div>
  </footer>
</template>
<style scoped>
.player-bar {
  position: relative;
  z-index: 20;
  bottom: 0;
  left: 0;
  right: 0;
  min-height: 84px;
  padding: 10px 24px max(10px, env(safe-area-inset-bottom));
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(280px, 440px) minmax(0, 1fr);
  align-items: center;
  gap: 35px;
  background: #ffffffed;
  border-top: 1px solid var(--line);
  backdrop-filter: blur(18px);
}
.bar-track {
  display: flex;
  gap: 12px;
  align-items: center;
  min-width: 0;
}
.bar-cover {
  width: 49px;
  height: 49px;
  border-radius: 9px;
  background: var(--paper-2);
  display: grid;
  place-items: center;
  color: #616d5b;
  overflow: hidden;
  flex: none;
}
.bar-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.bar-track-info {
  min-width: 0;
}
.bar-track b,
.bar-track > div > span {
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.bar-track b {
  font-size: 14px;
  font-weight: 600;
}
.bar-track > div > span {
  font-size: 12px;
  color: var(--muted);
  margin-top: 3px;
}
.like {
  margin-left: 8px;
  color: var(--vermilion);
  gap: 3px;
  flex: none;
}
.like span {
  font-size: 12px;
}
.like.liked {
  opacity: 0.55;
  cursor: default;
}
.playback-buttons {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 24px;
}
.play-button {
  width: 39px;
  height: 39px;
  display: grid;
  place-items: center;
  border-radius: 50%;
  background: var(--green);
  color: white;
}
.play-button:hover {
  background: var(--green-2);
}
.sync-label {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 6px;
  color: var(--muted);
  font-size: 11px;
  width: 94px;
}
.sync-label .online-dot {
  width: 4px;
  height: 4px;
  box-shadow: none;
}
.skip {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--ink-soft);
  width: 94px;
}
.skip:hover {
  color: var(--green);
}
.timeline {
  display: flex;
  align-items: center;
  gap: 10px;
  margin-top: 9px;
  font-size: 11px;
  font-variant-numeric: tabular-nums;
  color: var(--muted);
}
.progress {
  height: 3px;
  border-radius: 3px;
  flex: 1;
  overflow: hidden;
  background: #e6ecdf;
}
.progress i {
  display: block;
  height: 100%;
  background: #8daa77;
  border-radius: 3px;
}
.bar-aside {
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 4px;
  min-width: 0;
}
.beian {
  color: var(--muted);
  font-size: 10px;
  line-height: 1.4;
  white-space: nowrap;
}
.beian:hover {
  color: var(--green);
  text-decoration: underline;
}
.volume-controls {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 8px;
}
.volume-controls input {
  width: 85px;
  height: 3px;
  cursor: pointer;
}
.volume-controls > span {
  font-size: 12px;
  color: var(--muted);
  width: 30px;
}
@media (max-width: 1100px) {
  .player-bar {
    gap: 18px;
    padding-left: 24px;
    padding-right: 24px;
    grid-template-columns: minmax(0, 1fr) 310px 140px;
  }
  .volume-controls input {
    width: 55px;
  }
  .volume-controls > span {
    display: none;
  }
  /* 窄屏不要隐藏点赞按钮和数字：原来这里 display:none，
     导致 1100px 以下窗口的普通听众完全看不到点赞 */
  .like {
    margin-left: 4px;
    padding: 0 6px;
    min-width: 34px;
    justify-content: center;
  }
  .like span {
    font-size: 11px;
    display: inline;
  }
}
@media (max-width: 900px) {
  .player-bar {
    min-height: 68px;
    display: flex;
    flex-wrap: wrap;
    gap: 4px 12px;
    padding: 10px 19px max(10px, env(safe-area-inset-bottom));
  }
  .bar-track {
    flex: 1;
    gap: 10px;
  }
  .bar-aside {
    display: none;
  }
  .has-icp .bar-aside {
    display: flex;
    flex-basis: 100%;
  }
  .beian {
    font-size: 9px;
  }
  .bar-cover {
    width: 42px;
    height: 42px;
  }
  .bar-track b {
    font-size: 13px;
  }
  .bar-track > div > span {
    font-size: 11px;
  }
  .playback {
    flex: none;
  }
  .playback-buttons {
    gap: 12px;
  }
  .play-button {
    width: 36px;
    height: 36px;
  }
  .sync-label,
  .volume-controls,
  .skip span {
    display: none;
  }
  .skip {
    width: 30px;
    justify-content: center;
  }
  .timeline {
    position: absolute;
    top: -1px;
    left: 0;
    right: 0;
    margin: 0;
  }
  .timeline > span {
    display: none;
  }
  .progress {
    height: 2px;
    border-radius: 0;
  }
}
</style>
