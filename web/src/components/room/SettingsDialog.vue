<script setup lang="ts">
import { computed, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import AppDialog from '@/components/AppDialog.vue';
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
const ownerPassword = ref('');
const volume = ref(room.mode.volume);

watch(
  () => props.open,
  (open) => {
    if (open) {
      volume.value = room.mode.volume;
      password.value = '';
      ownerPassword.value = '';
    }
  },
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
  <AppDialog :open="open" title="房间设置" wide @close="emit('close')"
    ><div class="settings-body">
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
        <p class="tip">
          房主可使用本房间管理密码认证；服务器房主 / 管理员口令可管理所有房间和电台。
        </p>
        <p class="tip">认证只对这次连接有效；断线重连后需要重新认证。</p>
      </section>

      <section v-if="room.isRoot" class="block">
        <h3>本房间管理密码</h3>
        <div class="auth-row">
          <input
            v-model="ownerPassword"
            class="input"
            type="password"
            maxlength="64"
            placeholder="新的房间管理密码"
            autocomplete="new-password"
          />
          <button
            class="btn btn-lime btn-sm"
            :disabled="!ownerPassword"
            @click="
              room.setOwnerPassword(ownerPassword);
              ownerPassword = '';
            "
          >
            修改密码
          </button>
        </div>
        <p class="tip">只影响当前普通房间；服务器口令和入房密码独立。</p>
      </section>
      <!-- 播放模式 -->
      <section class="block">
        <h3>播放模式</h3>
        <div class="switches">
          <button
            class="switch"
            :class="{ on: room.mode.loop }"
            :disabled="!room.isAdmin"
            @click="toggle('loop')"
          >
            <span>循环播放</span><i></i>
          </button>
          <button
            class="switch"
            :class="{ on: room.mode.random }"
            :disabled="!room.isAdmin"
            @click="toggle('random')"
          >
            <span>随机切歌</span><i></i>
          </button>
          <button
            class="switch"
            :class="{ on: room.mode.radio }"
            :disabled="!room.isAdmin"
            @click="toggle('radio')"
          >
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
          <button
            class="op danger"
            type="button"
            :disabled="!room.isAdmin"
            @click="room.queueClear()"
          >
            <Icon name="trash" :size="15" />清空待播
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
    </div></AppDialog
  >
</template>
<style scoped>
.settings-body {
  display: grid;
  gap: 24px;
}
.block + .block {
  border-top: 1px solid var(--line);
  padding-top: 20px;
}
h3 {
  font-size: 14px;
  margin-bottom: 12px;
}
.role {
  font-size: 12px;
  color: var(--ink-soft);
}
.auth-row {
  display: flex;
  gap: 8px;
  margin-top: 12px;
}
.select {
  width: 105px;
  flex: none;
}
.auth-row .input {
  height: 36px;
  font-size: 12px;
  min-width: 0;
}
.tip {
  font-size: 10px;
  color: var(--muted);
  margin-top: 8px;
}
.switches {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px 20px;
}
.switch {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 12px;
  color: var(--ink-soft);
  padding: 5px 0;
  gap: 8px;
}
.switch i {
  width: 29px;
  height: 17px;
  border-radius: 20px;
  background: #dfe6da;
  position: relative;
  flex: none;
}
.switch i:after {
  content: '';
  position: absolute;
  width: 11px;
  height: 11px;
  left: 3px;
  top: 3px;
  border-radius: 50%;
  background: #fff;
  transition: transform 0.2s;
}
.switch.on i {
  background: #7fa273;
}
.switch.on i:after {
  transform: translateX(12px);
}
.vol-row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 20px;
  color: var(--muted);
}
.range {
  flex: 1;
  height: 4px;
}
.vol-num {
  font-size: 11px;
}
.ops {
  display: flex;
  gap: 12px;
  flex-wrap: wrap;
}
.op {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  padding: 8px 12px;
  background: var(--paper-2);
  border-radius: 8px;
  font-size: 11px;
  color: var(--green);
}
.op.danger {
  background: #fbefea;
  color: #b66e60;
}
.info-line {
  display: flex;
  align-items: center;
  flex-wrap: wrap;
  gap: 10px;
  font-size: 11px;
  margin-bottom: 13px;
}
.info-line code {
  overflow-wrap: anywhere;
  max-width: 100%;
}
@media (max-width: 450px) {
  .auth-row {
    flex-wrap: wrap;
  }
  .auth-row > .input:nth-child(2) {
    flex: 1;
  }
  .switches {
    gap: 12px;
  }
  .switch {
    font-size: 11px;
  }
}
</style>
