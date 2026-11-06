<script setup lang="ts">
import { ref, watch } from 'vue';
import Icon from './Icon.vue';
import type { RoomSummary } from '@/types';
const props = defineProps<{ room: RoomSummary }>();
defineEmits<{ enter: [room: RoomSummary] }>();
const broken = ref(false);
watch(
  () => props.room.now?.cover,
  () => (broken.value = false),
);
</script>
<template>
  <article class="room-card">
    <div class="room-card-top">
      <div class="room-cover">
        <img
          v-if="room.now?.cover && !broken"
          :src="room.now.cover"
          alt=""
          loading="lazy"
          @error="broken = true"
        /><Icon v-else :name="room.radio ? 'radio' : 'music'" :size="30" />
      </div>
      <div class="room-info">
        <div class="room-labels">
          <span class="room-kind">{{ room.radio ? '电台' : '点歌房' }}</span
          ><Icon v-if="room.needPwd" name="lock" :size="12" /><span
            v-if="room.builtin"
            class="resident"
            >常驻</span
          >
        </div>
        <h3>{{ room.name }}</h3>
        <span class="room-online"><i class="online-dot" />{{ room.online }} 人在听</span>
      </div>
      <button class="join" :aria-label="'加入' + room.name" @click="$emit('enter', room)">
        <Icon :name="room.needPwd ? 'lock' : 'arrow'" :size="20" />
      </button>
    </div>
    <p class="room-desc">{{ room.desc || '想听什么，就点什么。' }}</p>
    <div class="room-song">
      <Icon name="music" :size="13" /><span>{{ room.now?.name || '还没有歌曲，来点第一首吧' }}</span
      ><span v-if="room.now?.artist" class="artist">{{ room.now.artist }}</span>
    </div>
  </article>
</template>
<style scoped>
.room-card {
  background: #fff;
  border: 1px solid var(--line);
  border-radius: 18px;
  padding: 21px;
  transition:
    border-color 0.2s,
    box-shadow 0.2s;
}
.room-card:hover {
  border-color: #c3d5bf;
  box-shadow: var(--shadow-card);
}
.room-card-top {
  display: flex;
  align-items: center;
  gap: 13px;
}
.room-cover {
  height: 68px;
  width: 68px;
  border-radius: 13px;
  overflow: hidden;
  background: linear-gradient(135deg, #e8eedb, #dce9dd);
  display: grid;
  place-items: center;
  flex: none;
  color: #507059;
}
.room-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.room-info {
  min-width: 0;
  flex: 1;
}
.room-labels {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 11px;
}
.room-kind {
  color: var(--green);
  font-weight: 600;
}
.resident {
  font-size: 10px;
  padding: 0 4px;
  border-radius: 3px;
  background: #f4eedc;
  color: #7f6833;
}
h3 {
  font-size: 16px;
  margin: 3px 0;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.room-online {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--muted);
}
.room-online .online-dot {
  width: 4px;
  height: 4px;
  box-shadow: none;
}
.join {
  display: grid;
  place-items: center;
  width: 37px;
  height: 37px;
  background: var(--paper-2);
  color: var(--green);
  border-radius: 50%;
  flex: none;
}
.join:hover {
  background: var(--green);
  color: #fff;
}
.room-desc {
  font-size: 13px;
  color: var(--muted);
  margin: 17px 0 14px;
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.room-song {
  border-top: 1px solid var(--line);
  padding-top: 12px;
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 12px;
  color: var(--green);
  min-width: 0;
}
.room-song > span {
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.room-song .artist {
  color: var(--muted);
  margin-left: auto;
  font-size: 11px;
  max-width: 38%;
  flex: none;
}
@media (max-width: 600px) {
  .room-card {
    padding: 17px;
  }
  .room-cover {
    width: 59px;
    height: 59px;
  }
  .room-desc {
    margin-top: 13px;
  }
  .room-song .artist {
    display: none;
  }
}
</style>
