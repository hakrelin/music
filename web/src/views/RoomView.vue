<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import Icon from '@/components/Icon.vue';
import ChatPanel from '@/components/room/ChatPanel.vue';
import PlayerPanel from '@/components/room/PlayerPanel.vue';
import QueueList from '@/components/room/QueueList.vue';
import SearchPanel from '@/components/room/SearchPanel.vue';
import SettingsDialog from '@/components/room/SettingsDialog.vue';
import { usePlayerStore } from '@/stores/player';
import { useRoomStore } from '@/stores/room';

const route = useRoute();
const room = useRoomStore();
const player = usePlayerStore();

const settingsOpen = ref(false);
const gatePwd = ref('');
const gateMsg = ref('');
const mobileTab = ref<'play' | 'queue' | 'chat' | 'search'>('play');
const isMobile = ref(false);

let mq: MediaQueryList | null = null;
let syncTimer = 0;

const needGate = computed(() => !room.joined && !!room.fatal);
const loadingRoom = computed(() => !room.joined && !room.fatal);
const roomTags = computed(() => {
  const tags: string[] = [];
  if (room.room?.builtin) tags.push('常驻');
  if (room.room?.radio) tags.push('电台');
  if (room.room?.needPwd) tags.push('加密');
  return tags;
});

function onMobileChange() {
  isMobile.value = mq ? mq.matches : window.innerWidth < 900;
}

function submitGate() {
  if (!gatePwd.value.trim()) {
    gateMsg.value = '请输入房间密码';
    return;
  }
  room.connect(String(route.params.id), gatePwd.value.trim());
}

function onKey(ev: KeyboardEvent) {
  const el = ev.target as HTMLElement | null;
  if (el && /^(INPUT|TEXTAREA|SELECT)$/.test(el.tagName)) return;
  if (ev.code === 'Space') {
    ev.preventDefault();
    player.toggle();
  } else if (ev.code === 'ArrowRight') {
    room.voteSkip();
  }
}

onMounted(() => {
  const id = String(route.params.id || '');
  const pwd = String(route.query.pwd || '');
  room.connect(id, pwd);
  player.init();

  mq = window.matchMedia('(max-width: 900px)');
  onMobileChange();
  mq.addEventListener('change', onMobileChange);

  syncTimer = window.setInterval(() => {
    if (room.joined && room.current && player.playing) player.sync(room.clockOffset);
  }, 5000);

  window.addEventListener('keydown', onKey);
});

onBeforeUnmount(() => {
  room.leave();
  player.pause();
  window.clearInterval(syncTimer);
  mq?.removeEventListener('change', onMobileChange);
  window.removeEventListener('keydown', onKey);
});

watch(
  () => room.fatal,
  (msg) => {
    if (msg) {
      gateMsg.value = '';
      if (/密码/.test(msg)) gateMsg.value = '';
    }
  }
);

watch(
  [() => room.current?.id, () => room.current?.url, () => room.startedAt, () => room.clockOffset],
  () => {
    const track = room.current;
    if (!track) return;
    player.load(track, room.startedAt || Date.now(), room.clockOffset, true);
  }
);
</script>

<template>
  <div class="room-shell">
    <div class="room-bg" aria-hidden="true">
      <img src="/art/night.jpg" alt="" />
      <div class="bg-veil"></div>
    </div>

    <header class="top">
      <a class="back" href="/">
        <Icon name="home" :size="16" />
        <span>大厅</span>
      </a>

      <div class="room-title">
        <h1>{{ room.room?.name || '正在进门…' }}</h1>
        <div class="tags">
          <span v-for="t in roomTags" :key="t" class="tag night">{{ t }}</span>
          <span class="conn" :class="{ on: room.connected }">
            <i></i>{{ room.connected ? '已连接' : '连接中…' }}
          </span>
        </div>
      </div>

      <div class="top-right">
        <span class="online"><Icon name="users" :size="15" />{{ room.online }} 人在听</span>
        <button class="icon-btn night" type="button" aria-label="房间设置" @click="settingsOpen = true">
          <Icon name="settings" :size="18" />
        </button>
      </div>
    </header>

    <div class="wrap">
      <nav v-if="isMobile" class="mobile-tabs" aria-label="房间面板">
        <button :class="{ on: mobileTab === 'play' }" @click="mobileTab = 'play'"><Icon name="music" :size="16" />播放</button>
        <button :class="{ on: mobileTab === 'queue' }" @click="mobileTab = 'queue'"><Icon name="list" :size="16" />队列</button>
        <button :class="{ on: mobileTab === 'chat' }" @click="mobileTab = 'chat'"><Icon name="chat" :size="16" />聊天</button>
        <button :class="{ on: mobileTab === 'search' }" @click="mobileTab = 'search'"><Icon name="search" :size="16" />点歌</button>
      </nav>

      <div class="grid">
        <div class="col-main">
          <div v-show="!isMobile || mobileTab === 'play'">
            <PlayerPanel />
          </div>
          <div v-show="!isMobile || mobileTab === 'queue'" class="queue-slot">
            <QueueList />
          </div>
        </div>

        <aside class="col-side">
          <div v-show="!isMobile || mobileTab === 'search'">
            <SearchPanel />
          </div>
          <div v-show="!isMobile || mobileTab === 'chat'">
            <ChatPanel />
          </div>
        </aside>
      </div>
    </div>

    <!-- 进房加载 -->
    <transition name="fade">
      <div v-if="loadingRoom" class="curtain">
        <div class="curtain-inner">
          <div class="rings"><span></span><span></span><span></span></div>
          <p>正在推开茶室的门…</p>
        </div>
      </div>
    </transition>

    <!-- 密码房 -->
    <transition name="fade">
      <div v-if="needGate" class="curtain">
        <div class="gate">
          <Icon name="lock" :size="26" />
          <p class="eyebrow light">需要房间钥匙</p>
          <h2>这间房上了锁</h2>
          <p class="gate-msg">{{ room.fatal }}</p>
          <form class="gate-form" @submit.prevent="submitGate">
            <input v-model="gatePwd" class="input night" type="password" placeholder="房间密码" autofocus />
            <button class="btn btn-lime" type="submit">推门进去</button>
          </form>
          <p v-if="gateMsg" class="gate-err">{{ gateMsg }}</p>
          <a class="gate-back" href="/">回到大厅</a>
        </div>
      </div>
    </transition>

    <SettingsDialog :open="settingsOpen" @close="settingsOpen = false" />
  </div>
</template>

<style scoped>
.room-shell {
  min-height: 100vh;
  background: var(--night);
  color: #e9efe1;
  position: relative;
  isolation: isolate;
}
.room-bg {
  position: fixed;
  inset: 0;
  z-index: -1;
  overflow: hidden;
}
.room-bg img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: 70% 40%;
  opacity: 0.26;
  filter: saturate(0.8);
}
.bg-veil {
  position: absolute;
  inset: 0;
  background:
    radial-gradient(120% 90% at 70% 0%, rgba(226, 237, 186, 0.08), transparent 55%),
    linear-gradient(180deg, rgba(9, 29, 24, 0.86) 0%, rgba(9, 29, 24, 0.94) 40%, #0a201b 100%);
}

/* 顶栏 */
.top {
  max-width: 1520px;
  margin: 0 auto;
  padding: 18px 4% 14px;
  display: flex;
  align-items: center;
  gap: clamp(14px, 2.4vw, 30px);
}
.back {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  height: 36px;
  padding: 0 14px;
  border-radius: 999px;
  border: 1px solid var(--line-night-strong);
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.7);
  flex: none;
  transition: all 0.2s ease;
}
.back:hover {
  color: var(--lime);
  border-color: var(--lime);
}
.room-title {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 7px;
}
.room-title h1 {
  font-size: clamp(19px, 2vw, 26px);
  color: #f2f6e9;
  letter-spacing: 0.1em;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tags {
  display: flex;
  align-items: center;
  gap: 8px;
  flex-wrap: wrap;
}
.conn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.4);
}
.conn i {
  width: 5px;
  height: 5px;
  border-radius: 50%;
  background: #c9a15a;
}
.conn.on {
  color: rgba(232, 239, 220, 0.55);
}
.conn.on i {
  background: #9fc06a;
  box-shadow: 0 0 0 3px rgba(159, 192, 106, 0.14);
}
.top-right {
  display: flex;
  align-items: center;
  gap: 12px;
  flex: none;
}
.online {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.55);
  white-space: nowrap;
}

/* 主体 */
.wrap {
  max-width: 1520px;
  margin: 0 auto;
  padding: 8px 4% 60px;
}
.grid {
  display: grid;
  grid-template-columns: minmax(0, 1fr) minmax(320px, 380px);
  gap: clamp(16px, 1.6vw, 24px);
  align-items: stretch;
}
.col-main,
.col-side {
  display: flex;
  flex-direction: column;
  gap: clamp(16px, 1.6vw, 24px);
  min-width: 0;
}
.col-side > div {
  display: flex;
  flex-direction: column;
}
.col-side > div:last-child {
  flex: 1;
}
.col-side > div:last-child > :deep(.card) {
  height: 100%;
}

/* 移动端 tab */
.mobile-tabs {
  display: flex;
  gap: 4px;
  padding: 4px;
  background: rgba(255, 255, 255, 0.05);
  border: 1px solid var(--line-night);
  border-radius: 999px;
  margin-bottom: 14px;
}
.mobile-tabs button {
  flex: 1;
  height: 38px;
  border-radius: 999px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 6px;
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.55);
}
.mobile-tabs button.on {
  background: rgba(226, 237, 186, 0.14);
  color: var(--lime);
}

/* 幕布 */
.curtain {
  position: fixed;
  inset: 0;
  z-index: 70;
  display: grid;
  place-items: center;
  background: rgba(7, 24, 20, 0.86);
  backdrop-filter: blur(8px);
  padding: 20px;
}
.curtain-inner {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 18px;
  color: rgba(232, 239, 220, 0.6);
  font-size: 12.5px;
  letter-spacing: 0.16em;
}
.rings {
  display: flex;
  gap: 6px;
}
.rings span {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--lime);
  opacity: 0.3;
  animation: pulse 1.2s ease-in-out infinite;
}
.rings span:nth-child(2) {
  animation-delay: 0.18s;
}
.rings span:nth-child(3) {
  animation-delay: 0.36s;
}
@keyframes pulse {
  0%,
  100% {
    opacity: 0.25;
    transform: translateY(0);
  }
  50% {
    opacity: 1;
    transform: translateY(-4px);
  }
}

/* 密码门 */
.gate {
  width: min(420px, 100%);
  background: rgba(16, 46, 39, 0.92);
  border: 1px solid var(--line-night-strong);
  border-radius: var(--r-l);
  padding: 34px 32px 28px;
  display: flex;
  flex-direction: column;
  align-items: center;
  text-align: center;
  color: var(--lime);
  box-shadow: var(--shadow-night);
}
.eyebrow.light {
  margin-top: 16px;
  color: rgba(232, 239, 220, 0.5);
}
.eyebrow.light::before {
  color: rgba(232, 239, 220, 0.25);
}
.gate h2 {
  margin-top: 10px;
  font-size: 24px;
  color: #f2f6e9;
  letter-spacing: 0.1em;
}
.gate-msg {
  margin-top: 10px;
  font-size: 12px;
  color: rgba(232, 239, 220, 0.55);
  line-height: 1.9;
}
.gate-form {
  width: 100%;
  margin-top: 20px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.gate-err {
  margin-top: 10px;
  font-size: 11.5px;
  color: #f0a08f;
}
.gate-back {
  margin-top: 18px;
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.45);
  border-bottom: 1px solid rgba(232, 239, 220, 0.2);
  padding-bottom: 2px;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.25s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}

@media (max-width: 900px) {
  .top {
    padding: 14px 20px 10px;
    flex-wrap: wrap;
    gap: 10px;
  }
  .room-title h1 {
    font-size: 19px;
  }
  .online {
    display: none;
  }
  .wrap {
    padding: 6px 16px 40px;
  }
  .grid {
    grid-template-columns: 1fr;
  }
}
</style>
