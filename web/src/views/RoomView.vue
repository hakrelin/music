<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import SiteHeader from '@/components/SiteHeader.vue';
import AppDialog from '@/components/AppDialog.vue';
import Icon from '@/components/Icon.vue';
import PlayerPanel from '@/components/room/PlayerPanel.vue';
import PlayerBar from '@/components/room/PlayerBar.vue';
import QueueList from '@/components/room/QueueList.vue';
import ChatPanel from '@/components/room/ChatPanel.vue';
import SearchPanel from '@/components/room/SearchPanel.vue';
import SettingsDialog from '@/components/room/SettingsDialog.vue';
import { useRoomStore } from '@/stores/room';
import { usePlayerStore } from '@/stores/player';
import { useRoomsStore } from '@/stores/rooms';
import { useUiStore } from '@/stores/ui';
import { icpForHost } from '@/utils/site';
const route = useRoute();
const room = useRoomStore();
const player = usePlayerStore();
const rooms = useRoomsStore();
const ui = useUiStore();
// 备案号跟着访问的域名走（茶与金平糖 -2 / tea-sugar -1），见 utils/site.ts
const icp = icpForHost();
const settingsOpen = ref(false);
const searchOpen = ref(false);
const mobileTab = ref('play');
const gatePwd = ref('');
let timer = 0;
const needPassword = computed(() => /密码|口令|password/i.test(room.fatal));
function connect() {
  room.connect(
    String(route.params.id),
    String(history.state.roomPassword || route.query.pwd || ''),
  );
}
function key(e: KeyboardEvent) {
  const el = e.target as HTMLElement | null;
  if (el?.closest('input,textarea,select,button,a,[contenteditable],dialog')) return;
  if (e.code === 'Space') {
    e.preventDefault();
    player.toggle();
  }
  // → 键以前是「投票切歌」，正在听歌的人随手按方向键就会切歌（在线 1 人时一票即过），
  // 现在只在按住 Alt 时才触发，正常切歌请点播放条上的按钮。
  if (e.code === 'ArrowRight' && e.altKey && !room.mode.banSwitch) room.voteSkip();
}
async function share() {
  try {
    await navigator.clipboard.writeText(
      location.origin + '/room/' + encodeURIComponent(String(route.params.id)),
    );
    ui.ok('房间链接已复制');
  } catch {
    ui.warn('可以复制地址栏，把房间分享给朋友');
  }
}
onMounted(() => {
  connect();
  player.init();
  void rooms.loadConfig();
  timer = window.setInterval(() => {
    if (!document.hidden && room.joined && room.current && player.playing)
      player.sync(room.clockOffset);
  }, 5000);
  window.addEventListener('keydown', key);
});
onBeforeUnmount(() => {
  room.leave();
  player.pause();
  clearInterval(timer);
  window.removeEventListener('keydown', key);
});
watch(
  () => route.params.id,
  (id, prev) => {
    if (id !== prev) connect();
  },
);
watch(
  [() => room.current?.id, () => room.current?.url, () => room.startedAt, () => room.clockOffset],
  () => {
    if (room.current)
      player.load(room.current, room.startedAt || Date.now(), room.clockOffset, true);
    else player.load(null, 0, 0);
  },
);
</script>
<template>
  <div class="room-page">
    <SiteHeader compact
      ><button class="icon-btn" aria-label="房间设置" @click="settingsOpen = true">
        <Icon name="settings" :size="19" /></button
    ></SiteHeader>
    <main class="room-main">
      <div class="room-top">
        <div class="room-heading">
          <RouterLink class="back" to="/" aria-label="返回听歌大厅"
            ><Icon name="prev" :size="17"
          /></RouterLink>
          <div>
            <div class="room-name">
              <h1>{{ room.room?.name || '正在连接房间' }}</h1>
              <span v-if="room.room?.builtin" class="tag">常驻</span
              ><span v-if="room.room?.needPwd" class="lock-label"
                ><Icon name="lock" :size="12"
              /></span>
            </div>
            <p>
              <span class="connection" :class="{ connected: room.connected }"
                ><i />{{ room.connected ? '已连接' : '正在重连…' }}</span
              ><span class="top-separator">·</span>{{ room.online }} 人一起听<span
                class="top-separator"
                >·</span
              >{{ room.queue.length }} 首歌曲
            </p>
          </div>
        </div>
        <button class="invite" @click="share">
          <Icon name="link" :size="15" /><span>邀请朋友</span>
        </button>
      </div>
      <div v-if="player.blocked" class="play-notice">
        <span><Icon name="volume" :size="15" />浏览器需要你点击一次，才能开始播放。</span
        ><button @click="player.play()">开始收听 <Icon name="play" :size="12" /></button>
      </div>
      <div v-else-if="player.interrupted" class="play-notice">
        <span>播放被系统中断，点击继续收听。</span
        ><button @click="player.toggle()">继续收听</button>
      </div>
      <div v-else-if="player.failed" class="play-notice failed" role="alert">
        <span>这首歌暂时无法播放，可以重试或投票切歌。</span
        ><button @click="player.retry()">重试</button>
      </div>
      <nav class="mobile-tabs" aria-label="房间面板">
        <button
          v-for="t in [
            { id: 'play', label: '正在听', icon: 'music' },
            { id: 'queue', label: '队列', icon: 'list' },
            { id: 'chat', label: '聊天', icon: 'chat' },
          ]"
          :key="t.id"
          :class="{ active: mobileTab === t.id }"
          :aria-pressed="mobileTab === t.id"
          @click="mobileTab = t.id"
        >
          <Icon :name="t.icon" :size="15" />{{ t.label }}</button
        ><button @click="searchOpen = true"><Icon name="plus" :size="15" />点歌</button>
      </nav>
      <div class="room-layout">
        <aside class="queue-slot" :class="{ mobileActive: mobileTab === 'queue' }">
          <QueueList @search="searchOpen = true" />
        </aside>
        <div class="player-slot" :class="{ mobileActive: mobileTab === 'play' }">
          <PlayerPanel />
        </div>
        <aside class="chat-slot" :class="{ mobileActive: mobileTab === 'chat' }">
          <ChatPanel />
        </aside>
      </div>
      <div class="room-bottom">
        <span><span class="mini-star">✦</span> TEA &amp; KOMPEITO</span
        ><a
          v-if="icp"
          class="beian"
          :href="icp.href"
          target="_blank"
          rel="noreferrer noopener"
          >{{ icp.text }}</a
        ><span>听歌的时候，也可以聊聊天。</span
        ><button @click="searchOpen = true"><Icon name="plus" :size="12" />点一首歌</button>
      </div>
    </main>
    <PlayerBar /><AppDialog :open="searchOpen" title="点一首歌" wide @close="searchOpen = false"
      ><SearchPanel /></AppDialog
    ><SettingsDialog :open="settingsOpen" @close="settingsOpen = false" />
    <div v-if="!room.joined" class="room-gate">
      <div class="gate-inner">
        <template v-if="!room.fatal"
          ><span class="loading-note"><Icon name="radio" :size="30" /></span>
          <h2>正在连接房间</h2>
          <p>马上就能和大家一起听。</p></template
        ><template v-else
          ><Icon :name="needPassword ? 'lock' : 'info'" :size="30" />
          <h2>{{ needPassword ? '这个房间需要密码' : '暂时无法进入房间' }}</h2>
          <p role="alert">{{ room.fatal }}</p>
          <form
            v-if="needPassword"
            class="gate-form"
            @submit.prevent="room.connect(String(route.params.id), gatePwd.trim())"
          >
            <label class="visually-hidden" for="room-password">房间密码</label
            ><input
              id="room-password"
              v-model="gatePwd"
              class="input"
              type="password"
              placeholder="输入房间密码"
              required
            /><button class="btn btn-primary" type="submit">进入房间</button>
          </form>
          <button v-else class="btn btn-primary" @click="connect">重新连接</button></template
        ><RouterLink class="gate-back" to="/">← 返回听歌大厅</RouterLink>
      </div>
    </div>
  </div>
</template>
<style scoped>
.room-page {
  height: 100dvh;
  overflow: hidden;
  padding-bottom: 104px;
}
.room-main {
  max-width: 1400px;
  margin: auto;
  padding: 3px 32px 0;
  height: calc(100% - 84px);
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.room-top {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  padding: 12px 3px 20px;
  flex: none;
}
.room-heading {
  display: flex;
  align-items: center;
  gap: 15px;
  min-width: 0;
}
.room-heading > div {
  min-width: 0;
}
.back {
  width: 30px;
  height: 36px;
  display: grid;
  place-items: center;
  color: #626b5a;
  flex: none;
}
.back:hover {
  color: var(--green);
}
.room-name {
  display: flex;
  align-items: center;
  gap: 10px;
}
.room-name h1 {
  font-size: 22px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
  letter-spacing: -0.6px;
}
.room-name .tag {
  font-size: 11px;
  min-height: 19px;
  padding: 1px 6px;
}
.lock-label {
  color: var(--muted);
}
.room-heading p {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  color: #646d5d;
  margin-top: 5px;
}
.connection {
  display: flex;
  gap: 5px;
  align-items: center;
}
.connection i {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #d6b56b;
}
.connection.connected i {
  background: #8cab78;
}
.top-separator {
  color: #666a60;
}
.invite {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  color: var(--ink-soft);
  padding: 8px 13px;
  border-radius: 9px;
  background: #fff;
  border: 1px solid var(--line);
  flex: none;
}
.invite:hover {
  background: var(--paper-2);
}
.room-layout {
  display: grid;
  grid-template-columns: 270px minmax(0, 1fr) 310px;
  border: 1px solid var(--line);
  border-radius: 21px;
  overflow: hidden;
  background: #fff;
  min-height: 0;
  flex: 1;
}
.queue-slot {
  border-right: 1px solid var(--line);
  min-height: 0;
}
.player-slot {
  min-width: 0;
  min-height: 0;
  background: radial-gradient(ellipse at 50% 30%, #f1f5e8, #fafbf7 75%);
}
.chat-slot {
  border-left: 1px solid var(--line);
  min-height: 0;
}
.room-bottom {
  height: 39px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  font-size: 11px;
  color: #676c60;
  flex: none;
  padding: 0 5px;
}
.room-bottom > span:first-child {
  font-size: 10px;
  letter-spacing: 1.4px;
}
.mini-star {
  color: #776638;
  font-size: 14px;
  margin-right: 6px;
}
/* 备案号：房间页底部也要有（法规要求每个页面可见） */
.beian {
  font-size: 10px;
  color: #676c60;
  text-decoration: none;
  white-space: nowrap;
}
.beian:hover {
  color: var(--green);
  text-decoration: underline;
}
.room-bottom button {
  display: flex;
  align-items: center;
  gap: 5px;
  color: #5e6c54;
}
.play-notice {
  padding: 9px 15px;
  background: #edf3df;
  border: 1px solid #dce8cd;
  color: #557348;
  border-radius: 10px;
  margin: 0 0 13px;
  font-size: 13px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 14px;
}
.play-notice > span,
.play-notice button {
  display: flex;
  align-items: center;
  gap: 7px;
}
.play-notice button {
  font-weight: 600;
  white-space: nowrap;
}
.play-notice.failed {
  background: #fff0e7;
  color: #8c6046;
  border-color: #f0decf;
}
.mobile-tabs {
  display: none;
}
.room-gate {
  position: fixed;
  inset: 0;
  z-index: 40;
  display: grid;
  place-items: center;
  background: #f5f8f0e8;
  backdrop-filter: blur(10px);
  padding: 20px;
}
.gate-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 16px;
  max-width: 390px;
  text-align: center;
}
.gate-inner > .icon {
  color: var(--green);
}
.gate-inner h2 {
  font-size: 21px;
}
.gate-inner p {
  font-size: 14px;
  color: var(--muted);
}
.gate-form {
  display: flex;
  gap: 8px;
  width: 100%;
}
.gate-form .input {
  min-width: 0;
}
.gate-back {
  font-size: 14px;
  color: var(--green);
  margin-top: 7px;
}
.loading-note {
  color: var(--green);
  animation: pulse 1.6s infinite alternate;
}
@keyframes pulse {
  to {
    opacity: 0.3;
  }
}
@media (max-width: 1200px) {
  .room-layout {
    grid-template-columns: 235px minmax(0, 1fr) 280px;
  }
  .room-main {
    padding-left: 24px;
    padding-right: 24px;
  }
}
@media (max-width: 1000px) and (min-width: 901px) {
  .room-layout {
    grid-template-columns: 205px minmax(0, 1fr) 255px;
  }
  .room-main {
    padding-left: 15px;
    padding-right: 15px;
  }
}
@media (max-width: 900px) {
  .room-page {
    height: auto;
    min-height: 100dvh;
    overflow: visible;
    padding-bottom: 85px;
  }
  .room-main {
    height: auto;
    padding: 0 18px;
  }
  .room-top {
    padding: 7px 0 17px;
    gap: 10px;
  }
  .room-heading {
    gap: 5px;
  }
  .room-name h1 {
    font-size: 19px;
  }
  .room-heading p {
    font-size: 11px;
    gap: 5px;
  }
  .invite {
    padding: 7px 9px;
    font-size: 12px;
  }
  .mobile-tabs {
    display: flex;
    align-items: center;
    margin-bottom: 15px;
    border-bottom: 1px solid var(--line);
    gap: 4px;
  }
  .mobile-tabs button {
    flex: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    padding: 10px 0 13px;
    font-size: 14px;
    color: var(--muted);
    position: relative;
  }
  .mobile-tabs button.active {
    color: var(--green);
    font-weight: 600;
  }
  .mobile-tabs button.active:after {
    content: '';
    position: absolute;
    bottom: -1px;
    left: 25%;
    right: 25%;
    height: 3px;
    background: var(--green);
    border-radius: 3px;
  }
  .room-layout {
    display: block;
    border-radius: 17px;
  }
  .queue-slot,
  .player-slot,
  .chat-slot {
    display: none;
    border: 0;
  }
  .mobileActive {
    display: block;
  }
  .room-bottom {
    font-size: 10px;
    height: 32px;
    gap: 5px;
  }
  .room-bottom > span:nth-child(2) {
    display: none;
  }
  .room-bottom > span:first-child {
    font-size: 7px;
  }
  .play-notice {
    font-size: 12px;
    align-items: flex-start;
  }
  .play-notice > span {
    align-items: flex-start;
  }
  .play-notice > span > .icon {
    margin-top: 2px;
    flex: none;
  }
}
@media (max-width: 380px) {
  .room-main {
    padding: 0 13px;
  }
  .invite span {
    display: none;
  }
  .room-name h1 {
    font-size: 17px;
  }
}
</style>
