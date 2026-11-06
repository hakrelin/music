<script setup lang="ts">
import { computed } from 'vue';
import Icon from '@/components/Icon.vue';
import type { RoomSummary } from '@/types';

const props = defineProps<{ room: RoomSummary }>();
const emit = defineEmits<{ (e: 'enter', room: RoomSummary): void }>();

const arts = ['/art/night.jpg', '/art/hero.jpg', '/art/walk.jpg'];
const art = computed(() => {
  let h = 0;
  for (const ch of props.room.id) h = (h * 31 + ch.charCodeAt(0)) % 997;
  return arts[h % arts.length];
});
const hasPwd = computed(() => props.room.needPwd);
</script>

<template>
  <article class="room-card" @click="emit('enter', room)">
    <div class="media">
      <img :src="art" :alt="room.name + ' 的封面插画'" loading="lazy" />
      <div class="media-veil"></div>
      <div class="media-tags">
        <span v-if="room.radio" class="tag night"><Icon name="radio" :size="13" />电台</span>
        <span v-if="hasPwd" class="tag night"><Icon name="lock" :size="12" />加密</span>
      </div>
      <div v-if="room.now" class="media-now">
        <span class="now-label">正在播放</span>
        <b class="now-name">{{ room.now.name }}</b>
        <span class="now-artist">{{ room.now.artist }}</span>
      </div>
    </div>

    <div class="body">
      <h3>{{ room.name }}</h3>
      <p class="desc">{{ room.desc || '这间房还没有写介绍。' }}</p>
      <div class="foot">
        <span class="online"><i></i>{{ room.online || 0 }} 人在听</span>
        <button class="enter" type="button" @click.stop="emit('enter', room)">
          {{ hasPwd ? '输入密码' : '入席' }} <Icon name="arrow" :size="15" />
        </button>
      </div>
    </div>
  </article>
</template>

<style scoped>
.room-card {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: var(--r-l);
  overflow: hidden;
  cursor: pointer;
  display: flex;
  flex-direction: column;
  transition: transform 0.35s ease, box-shadow 0.35s ease, border-color 0.35s ease;
}
.room-card:hover {
  transform: translateY(-4px);
  box-shadow: var(--shadow-card);
  border-color: var(--line-strong);
}
.media {
  position: relative;
  aspect-ratio: 16 / 9;
  overflow: hidden;
  background: var(--deep);
}
.media img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  transform: scale(1.02);
  transition: transform 0.8s ease;
}
.room-card:hover .media img {
  transform: scale(1.06);
}
.media-veil {
  position: absolute;
  inset: 0;
  background: linear-gradient(180deg, rgba(12, 36, 31, 0.05) 0%, rgba(12, 36, 31, 0.15) 45%, rgba(12, 36, 31, 0.86) 100%);
}
.media-tags {
  position: absolute;
  top: 12px;
  left: 12px;
  display: flex;
  gap: 6px;
}
.media-tags .tag {
  background: rgba(12, 36, 31, 0.55);
  color: #e8efdc;
  backdrop-filter: blur(6px);
}
.media-now {
  position: absolute;
  left: 16px;
  right: 16px;
  bottom: 13px;
  color: #f2f6e9;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.now-label {
  font-size: 9.5px;
  letter-spacing: 0.3em;
  color: rgba(232, 239, 220, 0.62);
}
.now-name {
  font-family: var(--serif);
  font-size: 15px;
  font-weight: 600;
  letter-spacing: 0.04em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.now-artist {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.7);
}
.body {
  padding: 18px 20px 20px;
  display: flex;
  flex-direction: column;
  gap: 9px;
  flex: 1;
}
.body h3 {
  font-size: 21px;
  letter-spacing: 0.08em;
}
.desc {
  color: var(--muted);
  font-size: 12.5px;
  line-height: 1.85;
  min-height: 46px;
  display: -webkit-box;
  -webkit-line-clamp: 2;
  -webkit-box-orient: vertical;
  overflow: hidden;
}
.foot {
  margin-top: auto;
  padding-top: 12px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.online {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 11.5px;
  color: var(--muted);
}
.online i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #7ea05a;
  box-shadow: 0 0 0 3px rgba(126, 160, 90, 0.16);
}
.enter {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 12px;
  letter-spacing: 0.1em;
  color: var(--green);
  padding: 6px 0 6px 12px;
  transition: gap 0.2s ease, color 0.2s ease;
}
.enter:hover {
  gap: 10px;
  color: var(--vermilion);
}
</style>
