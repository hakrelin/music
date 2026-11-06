<script setup lang="ts">
import { computed } from 'vue';
import Icon from '@/components/Icon.vue';
import { usePlayerStore } from '@/stores/player';
import { useRoomStore } from '@/stores/room';

const room = useRoomStore();
const player = usePlayerStore();

const list = computed(() => room.queue);
const canManage = computed(() => room.isAdmin);
</script>

<template>
  <section class="queue card" aria-label="点歌队列">
    <header class="q-head">
      <div>
        <p class="eyebrow light">点歌队列</p>
        <h2>接下来听什么</h2>
      </div>
      <span class="count">{{ room.upcoming.length }} 首待播</span>
    </header>

    <p v-if="!list.length" class="empty">队列是空的。去搜一首歌，或者导入一份歌单。</p>

    <ol v-else class="rows">
      <li
        v-for="(track, i) in list"
        :key="track.id + '-' + i"
        class="row"
        :class="{ now: i === 0 }"
      >
        <span class="idx">
          <template v-if="i === 0">
            <span class="bars" :class="{ paused: !player.playing }"><i></i><i></i><i></i></span>
          </template>
          <template v-else>{{ String(i).padStart(2, '0') }}</template>
        </span>

        <span class="main">
          <span class="name" :title="track.name">{{ track.name }}</span>
          <span class="sub">
            {{ track.artist }}
            <template v-if="track.nick"> · {{ track.nick }} 点</template>
          </span>
        </span>

        <span class="dur">{{ Math.floor((track.duration || 0) / 60) }}:{{ String((track.duration || 0) % 60).padStart(2, '0') }}</span>

        <span class="acts">
          <button v-if="canManage && i > 0" class="mini" type="button" title="置顶" @click="room.queueTop(track.id)">
            <Icon name="pin" :size="14" />
          </button>
          <button v-if="canManage" class="mini danger" type="button" title="从队列移除" @click="room.queueRemove(track.id)">
            <Icon name="close" :size="14" />
          </button>
        </span>
      </li>
    </ol>
  </section>
</template>

<style scoped>
.card {
  background: rgba(255, 255, 255, 0.035);
  border: 1px solid var(--line-night);
  border-radius: var(--r-l);
  backdrop-filter: blur(10px);
}
.queue {
  padding: clamp(20px, 2.2vw, 28px);
}
.q-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 14px;
  margin-bottom: 16px;
}
.eyebrow.light {
  color: rgba(232, 239, 220, 0.5);
}
.eyebrow.light::before {
  color: rgba(232, 239, 220, 0.25);
}
.q-head h2 {
  margin-top: 10px;
  font-size: 20px;
  color: #eef3e4;
  letter-spacing: 0.1em;
}
.count {
  font-size: 11px;
  letter-spacing: 0.12em;
  color: rgba(232, 239, 220, 0.45);
  white-space: nowrap;
}
.empty {
  padding: 26px 0 10px;
  font-size: 12.5px;
  color: rgba(232, 239, 220, 0.45);
}
.rows {
  display: flex;
  flex-direction: column;
  max-height: 340px;
  overflow: auto;
  margin: 0 -6px;
  padding: 0 6px;
}
.row {
  display: grid;
  grid-template-columns: 34px minmax(0, 1fr) 46px 60px;
  align-items: center;
  gap: 10px;
  padding: 11px 0;
  border-top: 1px solid var(--line-night);
  position: relative;
}
.row:first-child {
  border-top: 0;
}
.row.now .name {
  color: var(--lime);
}
.row.now .idx {
  color: var(--lime);
}
.idx {
  font-family: var(--serif);
  font-size: 12px;
  color: rgba(232, 239, 220, 0.35);
  display: flex;
  align-items: center;
  justify-content: center;
  height: 22px;
}
.bars {
  display: inline-flex;
  align-items: flex-end;
  gap: 2px;
  height: 13px;
}
.bars i {
  width: 2.5px;
  background: var(--lime);
  border-radius: 2px;
  animation: eq 0.9s ease-in-out infinite;
}
.bars i:nth-child(1) {
  height: 60%;
  animation-delay: 0s;
}
.bars i:nth-child(2) {
  height: 100%;
  animation-delay: 0.18s;
}
.bars i:nth-child(3) {
  height: 45%;
  animation-delay: 0.36s;
}
.bars.paused i {
  animation-play-state: paused;
  opacity: 0.5;
}
@keyframes eq {
  0%,
  100% {
    transform: scaleY(0.45);
  }
  50% {
    transform: scaleY(1);
  }
}
.main {
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.name {
  font-size: 13.5px;
  color: #e7eeda;
  letter-spacing: 0.02em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.sub {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.42);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.dur {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.35);
  text-align: right;
  font-variant-numeric: tabular-nums;
}
.acts {
  display: flex;
  align-items: center;
  justify-content: flex-end;
  gap: 4px;
  opacity: 0;
  transition: opacity 0.2s ease;
}
.row:hover .acts {
  opacity: 1;
}
.mini {
  width: 26px;
  height: 26px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  color: rgba(232, 239, 220, 0.5);
  border: 1px solid transparent;
}
.mini:hover {
  color: var(--lime);
  border-color: var(--line-night-strong);
}
.mini.danger:hover {
  color: #f0a08f;
}
@media (max-width: 760px) {
  .acts {
    opacity: 1;
  }
}
</style>
