<script setup lang="ts">
import { nextTick, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import { useRoomStore } from '@/stores/room';

const room = useRoomStore();
const draft = ref('');
const box = ref<HTMLElement | null>(null);

watch(
  () => room.chat.length,
  async () => {
    await nextTick();
    if (box.value) box.value.scrollTop = box.value.scrollHeight;
  }
);

function send() {
  const text = draft.value.trim();
  if (!text) return;
  room.chatSend(text);
  draft.value = '';
}

function isImage(url: string) {
  return /\.(png|jpe?g|gif|webp|avif)(\?|#|$)/i.test(url);
}
</script>

<template>
  <section class="chat card" aria-label="房间聊天">
    <header class="c-head">
      <div>
        <p class="eyebrow light">房间聊天</p>
        <h2>茶桌上的话</h2>
      </div>
      <span class="online"><i></i>{{ room.online }} 人在听</span>
    </header>

    <div ref="box" class="msgs">
      <p v-if="!room.chat.length" class="empty">还没有人说话。打个招呼，或者先点一首歌。</p>
      <template v-for="(m, i) in room.chat" :key="i">
        <p v-if="m.type === 'notice'" class="notice">{{ m.content }}</p>
        <div v-else class="msg">
          <div class="msg-head">
            <span class="nick">{{ m.nick }}</span>
            <span class="time">{{ new Date(m.ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' }) }}</span>
          </div>
          <p class="text">{{ m.content }}</p>
          <template v-if="m.images && m.images.length">
            <a v-for="(img, k) in m.images" :key="k" :href="img" target="_blank" rel="noreferrer">
              <img v-if="isImage(img)" :src="img" alt="聊天图片" class="pic" loading="lazy" />
            </a>
          </template>
        </div>
      </template>
    </div>

    <form class="composer" @submit.prevent="send">
      <input
        v-model="draft"
        class="input night"
        maxlength="300"
        placeholder="说点什么……（图片链接会自动展开）"
        :disabled="!room.joined"
      />
      <button class="send" type="submit" :disabled="!room.joined || !draft.trim()" aria-label="发送">
        <Icon name="arrow" :size="17" />
      </button>
    </form>
    <p class="hint">点歌与导入歌单请用「点歌」面板；聊天只用来聊天。</p>
  </section>
</template>

<style scoped>
.card {
  background: rgba(255, 255, 255, 0.035);
  border: 1px solid var(--line-night);
  border-radius: var(--r-l);
  backdrop-filter: blur(10px);
}
.chat {
  padding: clamp(18px, 2vw, 24px);
  display: flex;
  flex-direction: column;
  min-height: 0;
}
.c-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 12px;
  margin-bottom: 12px;
}
.eyebrow.light {
  color: rgba(232, 239, 220, 0.5);
}
.eyebrow.light::before {
  color: rgba(232, 239, 220, 0.25);
}
.c-head h2 {
  margin-top: 10px;
  font-size: 20px;
  color: #eef3e4;
  letter-spacing: 0.1em;
}
.online {
  display: inline-flex;
  align-items: center;
  gap: 7px;
  font-size: 11px;
  color: rgba(232, 239, 220, 0.55);
  white-space: nowrap;
}
.online i {
  width: 6px;
  height: 6px;
  border-radius: 50%;
  background: #9fc06a;
  box-shadow: 0 0 0 3px rgba(159, 192, 106, 0.14);
}
.msgs {
  flex: 1;
  min-height: 180px;
  max-height: 420px;
  overflow: auto;
  padding-right: 4px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.empty {
  color: rgba(232, 239, 220, 0.4);
  font-size: 12.5px;
  padding: 14px 0;
}
.notice {
  align-self: center;
  text-align: center;
  font-size: 11px;
  color: rgba(226, 237, 186, 0.5);
  letter-spacing: 0.04em;
  line-height: 1.8;
  max-width: 92%;
}
.msg {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
.msg-head {
  display: flex;
  align-items: baseline;
  gap: 10px;
}
.nick {
  font-size: 12px;
  color: var(--lime);
  letter-spacing: 0.04em;
}
.time {
  font-size: 10px;
  color: rgba(232, 239, 220, 0.3);
}
.text {
  font-size: 13px;
  line-height: 1.8;
  color: rgba(240, 245, 232, 0.86);
  word-break: break-word;
  white-space: pre-wrap;
}
.pic {
  margin-top: 6px;
  max-width: 100%;
  border-radius: var(--r-s);
}
.composer {
  margin-top: 14px;
  display: flex;
  gap: 8px;
}
.composer .input {
  height: 42px;
  font-size: 13px;
}
.send {
  width: 42px;
  height: 42px;
  border-radius: var(--r-m);
  background: var(--lime);
  color: var(--deep);
  display: grid;
  place-items: center;
  flex: none;
  transition: background 0.2s ease;
}
.send:hover {
  background: #eaf3c9;
}
.send:disabled {
  opacity: 0.4;
  cursor: default;
}
.hint {
  margin-top: 10px;
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.3);
  line-height: 1.7;
}
</style>
