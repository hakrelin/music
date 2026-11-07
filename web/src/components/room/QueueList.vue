<script setup lang="ts">
import Icon from '@/components/Icon.vue';
import { useRoomStore } from '@/stores/room';
import { formatTime } from '@/utils/lyric';
const room = useRoomStore();
defineEmits<{ search: [] }>();
</script>
<template>
  <section class="queue-panel" aria-label="点歌队列">
    <header>
      <h2 class="panel-title">
        <Icon name="list" :size="17" />待播队列
        <span>{{ room.upcoming.length }}</span>
      </h2>
      <button class="icon-btn" aria-label="添加歌曲" @click="$emit('search')">
        <Icon name="plus" :size="17" />
      </button>
    </header>
    <button class="add-music" @click="$emit('search')">
      <Icon name="search" :size="15" />搜索音乐 / 导入歌单
      <Icon name="plus" :size="13" />
    </button>
    <div class="queue-scroll" tabindex="0" aria-label="歌曲列表">
      <p v-if="!room.queue.length" class="empty">队列空空的，来点第一首吧。</p>
      <ol>
        <li v-for="(t, i) in room.queue" :key="t.id + '-' + i" :class="{ current: i === 0 }">
          <span class="index"
            ><Icon v-if="i === 0" name="music" :size="15" /><template v-else>{{
              String(i).padStart(2, '0')
            }}</template></span
          >
          <div class="track-info">
            <b :title="t.name">{{ t.name }}</b
            ><span>{{ t.artist }}</span
            ><small v-if="t.nick">{{ t.nick }} 点的歌</small>
          </div>
          <div class="track-end">
            <span>{{ formatTime(t.duration) }}</span>
            <div v-if="room.isAdmin && i > 0" class="track-actions">
              <button aria-label="置顶歌曲" @click="room.queueTop(t.id)">
                <Icon name="up" :size="13" /></button
              ><button aria-label="移除歌曲" @click="room.queueRemove(t.id)">
                <Icon name="close" :size="13" />
              </button>
            </div>
            <button
              v-if="i > 0 && t.queueId"
              class="vote-remove"
              :disabled="room.mode.banSwitch && !room.isAdmin"
              @click="room.voteRemove(t.queueId)"
            >
              投票删除 {{ t.removeVotes || 0 }}/{{ t.removeNeed || 1 }}
            </button>
            <span v-else-if="i === 0" class="now-label">当前</span>
          </div>
        </li>
      </ol>
    </div>
    <div class="queue-foot">
      <Icon name="radio" :size="13" /><span>{{
        room.mode.radio ? '电台会在队列不足时自动补歌' : '大家点的歌会按顺序播放'
      }}</span>
    </div>
  </section>
</template>
<style scoped>
.queue-panel {
  height: 100%;
  display: flex;
  flex-direction: column;
  padding: 21px 16px 17px;
  min-height: 0;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 0 5px 12px;
}
.panel-title {
  font-size: 14px;
}
.panel-title > span {
  font-size: 11px;
  background: var(--paper-2);
  padding: 1px 6px;
  border-radius: 4px;
  color: var(--muted);
  margin-left: 1px;
}
.add-music {
  background: #edf3e7;
  color: #56734f;
  min-height: 39px;
  display: flex;
  align-items: center;
  gap: 7px;
  border-radius: 9px;
  padding: 0 11px;
  font-size: 12px;
  flex: none;
}
.add-music > .icon:last-child {
  margin-left: auto;
}
.add-music:hover {
  background: #e2eddb;
}
.queue-scroll {
  overflow: auto;
  min-height: 0;
  flex: 1;
  margin: 17px -5px 0;
  padding: 0 5px;
}
.queue-scroll li {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 14px 8px;
  border-radius: 11px;
  min-width: 0;
}
.queue-scroll li + li {
  margin-top: 2px;
}
.queue-scroll li.current {
  background: #edf3e7;
}
.index {
  font-size: 12px;
  min-width: 17px;
  display: flex;
  justify-content: center;
  color: #676c64;
}
.current .index {
  color: var(--green);
}
.track-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.track-info b {
  font-size: 13px;
  font-weight: 500;
  color: var(--ink-soft);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.current .track-info b {
  color: var(--green);
  font-weight: 600;
}
.track-info > span {
  font-size: 11px;
  color: var(--muted);
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.track-info small {
  font-size: 10px;
  color: #676d60;
}
.track-end {
  flex: none;
  display: flex;
  flex-direction: column;
  align-items: flex-end;
  gap: 3px;
  font-size: 11px;
  color: #656e5e;
}
.now-label {
  font-size: 10px;
  color: #547046;
}
.vote-remove {
  font-size: 11px;
  color: var(--green);
  padding: 4px;
}
.track-actions {
  display: flex;
  gap: 2px;
}
.track-actions button {
  padding: 4px;
  display: grid;
  place-items: center;
  border-radius: 4px;
}
.track-actions button:hover {
  background: var(--paper-3);
}
.queue-foot {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: #626d59;
  border-top: 1px solid var(--line);
  padding: 14px 4px 0;
  margin-top: 12px;
}
.queue-foot .icon {
  flex: none;
}
.empty {
  font-size: 13px;
  padding: 20px 10px;
}
@media (max-width: 900px) {
  .queue-panel {
    min-height: calc(100dvh - 250px);
    padding: 20px;
  }
  .queue-scroll {
    max-height: none;
  }
  .track-info b {
    font-size: 13px;
  }
  .track-info > span {
    font-size: 13px;
  }
  .track-info small {
    font-size: 12px;
  }
  .queue-scroll li {
    padding: 16px 10px;
  }
  .add-music {
    font-size: 14px;
  }
}
</style>
