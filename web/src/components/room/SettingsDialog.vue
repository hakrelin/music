<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import { usePlayerStore } from '@/stores/player';
import { useRoomStore } from '@/stores/room';
import { useUiStore } from '@/stores/ui';

const props = defineProps<{ open: boolean }>();
const emit = defineEmits<{ (e: 'close'): void }>();

const room = useRoomStore();
const player = usePlayerStore();
const ui = useUiStore();

const role = ref<'root' | 'admin'>('root');
const password = ref('');
const volume = ref(room.mode.volume);

watch(
  () => props.open,
  (open) => {
    if (open) {
      volume.value = room.mode.volume;
      password.value = '';
    }
  }
);

const inviteLink = computed(() => {
  if (!room.room) return '';
  return `${location.origin}/room/${room.room.id}`;
});

function auth() {
  if (!password.value.trim()) return ui.warn('口令还没填');
  room.auth(role.value, password.value.trim());
  password.value = '';
}

async function copyLink() {
  try {
    await navigator.clipboard.writeText(inviteLink.value);
    ui.ok('邀请链接已复制');
  } catch {
    ui.warn('复制失败，手动复制地址栏吧');
  }
}

function onVolume(ev: Event) {
  const v = Number((ev.target as HTMLInputElement).value);
  volume.value = v;
  room.setVolume(v);
  player.setVolume(v / 100);
}

function toggle(key: 'loop' | 'random' | 'radio') {
  room.setMode({ [key]: !room.mode[key] });
}
</script>

<template>
  <teleport to="body">
    <transition name="fade">
      <div v-if="open" class="mask" @click.self="emit('close')">
        <div class="sheet" role="dialog" aria-modal="true" aria-label="房间设置">
          <header class="s-head">
            <div>
              <p class="eyebrow light">房间设置</p>
              <h2>{{ room.room?.name || '未命名房间' }}</h2>
            </div>
            <button class="close" type="button" aria-label="关闭" @click="emit('close')">
              <Icon name="close" :size="18" />
            </button>
          </header>

          <div class="s-body">
            <!-- 身份 -->
            <section class="block">
              <h3>我的身份</h3>
              <p class="role">
                当前：<b>{{ room.roleLabel }}</b>
                <span class="muted">· {{ room.self?.nick }}</span>
              </p>
              <div class="auth-row">
                <select v-model="role" class="input night select">
                  <option value="root">房主</option>
                  <option value="admin">管理员</option>
                </select>
                <input
                  v-model="password"
                  class="input night"
                  type="password"
                  placeholder="口令"
                  @keydown.enter="auth"
                />
                <button class="btn btn-lime btn-sm" type="button" @click="auth">认证</button>
              </div>
              <p class="tip">认证只对这次连接有效；断线重连后需要重新认证。</p>
            </section>

            <!-- 播放模式 -->
            <section class="block">
              <h3>播放模式</h3>
              <div class="switches">
                <button class="switch" :class="{ on: room.mode.loop }" :disabled="!room.isAdmin" @click="toggle('loop')">
                  <span>循环播放</span><i></i>
                </button>
                <button class="switch" :class="{ on: room.mode.random }" :disabled="!room.isAdmin" @click="toggle('random')">
                  <span>随机切歌</span><i></i>
                </button>
                <button class="switch" :class="{ on: room.mode.radio }" :disabled="!room.isAdmin" @click="toggle('radio')">
                  <span>电台补队</span><i></i>
                </button>
                <button
                  class="switch"
                  :class="{ on: room.mode.goodMode }"
                  :disabled="!room.isAdmin"
                  @click="room.setGoodMode(!room.mode.goodMode)"
                >
                  <span>点赞模式</span><i></i>
                </button>
                <button
                  class="switch"
                  :class="{ on: room.mode.banChoose }"
                  :disabled="!room.isAdmin"
                  @click="room.setBan({ choose: !room.mode.banChoose })"
                >
                  <span>禁止点歌</span><i></i>
                </button>
                <button
                  class="switch"
                  :class="{ on: room.mode.banSwitch }"
                  :disabled="!room.isAdmin"
                  @click="room.setBan({ switch: !room.mode.banSwitch })"
                >
                  <span>禁止投票切歌</span><i></i>
                </button>
              </div>
              <div class="vol-row">
                <Icon name="volume" :size="16" />
                <input
                  class="range"
                  type="range"
                  min="0"
                  max="100"
                  :value="volume"
                  :disabled="!room.isAdmin"
                  @input="onVolume"
                />
                <span class="vol-num">{{ volume }}</span>
              </div>
            </section>

            <!-- 管理 -->
            <section class="block">
              <h3>管理操作</h3>
              <div class="ops">
                <button class="op" type="button" :disabled="!room.isAdmin" @click="room.next()">
                  <Icon name="next" :size="15" />直接下一首
                </button>
                <button class="op danger" type="button" :disabled="!room.isAdmin" @click="room.queueClear()">
                  <Icon name="trash" :size="15" />清空队列
                </button>
              </div>
            </section>

            <!-- 房间信息 -->
            <section class="block">
              <h3>房间信息</h3>
              <p class="info-line">
                <span class="muted">房间号</span>
                <code>{{ room.room?.id }}</code>
                <span class="tag night" v-if="room.room?.builtin">常驻</span>
                <span class="tag night" v-if="room.room?.radio">电台</span>
              </p>
              <button class="op" type="button" @click="copyLink">
                <Icon name="link" :size="15" />复制邀请链接
              </button>
            </section>
          </div>
        </div>
      </div>
    </transition>
  </teleport>
</template>

<style scoped>
.mask {
  position: fixed;
  inset: 0;
  z-index: 80;
  background: rgba(4, 14, 12, 0.62);
  backdrop-filter: blur(6px);
  display: flex;
  align-items: center;
  justify-content: center;
  padding: 20px;
}
.sheet {
  width: min(560px, 100%);
  max-height: 88vh;
  overflow: auto;
  background: #0f2b24;
  border: 1px solid var(--line-night-strong);
  border-radius: var(--r-l);
  box-shadow: 0 40px 120px rgba(0, 0, 0, 0.5);
  color: #e9efe1;
}
.s-head {
  display: flex;
  align-items: flex-start;
  justify-content: space-between;
  gap: 16px;
  padding: 24px 26px 18px;
  border-bottom: 1px solid var(--line-night);
}
.eyebrow.light {
  color: rgba(232, 239, 220, 0.5);
}
.eyebrow.light::before {
  color: rgba(232, 239, 220, 0.25);
}
.s-head h2 {
  margin-top: 10px;
  font-size: 22px;
  color: #f2f6e9;
  letter-spacing: 0.08em;
}
.close {
  width: 34px;
  height: 34px;
  border-radius: 50%;
  display: grid;
  place-items: center;
  border: 1px solid var(--line-night-strong);
  color: rgba(232, 239, 220, 0.7);
}
.close:hover {
  color: var(--lime);
  border-color: var(--lime);
}
.s-body {
  padding: 8px 26px 26px;
}
.block {
  padding: 18px 0;
  border-bottom: 1px solid var(--line-night);
}
.block:last-child {
  border-bottom: 0;
  padding-bottom: 4px;
}
.block h3 {
  font-size: 12px;
  letter-spacing: 0.22em;
  color: rgba(232, 239, 220, 0.55);
  margin-bottom: 12px;
  font-family: var(--sans);
  font-weight: 400;
}
.role {
  font-size: 13px;
  margin-bottom: 12px;
}
.role b {
  color: var(--lime);
}
.muted {
  color: rgba(232, 239, 220, 0.45);
  font-size: 11.5px;
}
.auth-row {
  display: flex;
  gap: 8px;
}
.auth-row .input {
  height: 38px;
  font-size: 12.5px;
}
.select {
  width: 96px;
  flex: none;
  appearance: none;
  text-align: center;
}
.auth-row .btn {
  flex: none;
  height: 38px;
}
.tip {
  margin-top: 10px;
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.35);
}
.switches {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 8px 12px;
}
.switch {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 10px;
  height: 38px;
  padding: 0 4px 0 12px;
  border: 1px solid var(--line-night);
  border-radius: var(--r-s);
  font-size: 12px;
  color: rgba(232, 239, 220, 0.72);
}
.switch i {
  width: 30px;
  height: 17px;
  border-radius: 999px;
  background: rgba(232, 239, 220, 0.16);
  position: relative;
  transition: background 0.2s ease;
  flex: none;
}
.switch i::after {
  content: "";
  position: absolute;
  top: 2.5px;
  left: 3px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: rgba(232, 239, 220, 0.6);
  transition: transform 0.2s ease, background 0.2s ease;
}
.switch.on i {
  background: rgba(226, 237, 186, 0.4);
}
.switch.on i::after {
  transform: translateX(12px);
  background: var(--lime);
}
.switch:disabled {
  opacity: 0.45;
  cursor: default;
}
.vol-row {
  margin-top: 14px;
  display: flex;
  align-items: center;
  gap: 12px;
  color: rgba(232, 239, 220, 0.6);
}
.range {
  flex: 1;
  accent-color: var(--lime);
}
.vol-num {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.45);
  width: 26px;
  text-align: right;
}
.ops {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.op {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  height: 34px;
  padding: 0 14px;
  border: 1px solid var(--line-night-strong);
  border-radius: 999px;
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.75);
  transition: all 0.2s ease;
}
.op:hover:not(:disabled) {
  border-color: var(--lime);
  color: var(--lime);
}
.op:disabled {
  opacity: 0.4;
  cursor: default;
}
.op.danger:hover:not(:disabled) {
  border-color: #e59b8a;
  color: #f0a08f;
}
.info-line {
  display: flex;
  align-items: center;
  gap: 10px;
  font-size: 12px;
  margin-bottom: 12px;
  flex-wrap: wrap;
}
.info-line code {
  font-family: var(--mono);
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.75);
  background: rgba(0, 0, 0, 0.22);
  padding: 3px 8px;
  border-radius: 5px;
}
.fade-enter-active,
.fade-leave-active {
  transition: opacity 0.2s ease;
}
.fade-enter-from,
.fade-leave-to {
  opacity: 0;
}
@media (max-width: 560px) {
  .switches {
    grid-template-columns: 1fr;
  }
  .s-head,
  .s-body {
    padding-left: 20px;
    padding-right: 20px;
  }
}
</style>
