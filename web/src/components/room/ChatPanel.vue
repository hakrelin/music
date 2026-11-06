<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue';
import Icon from '@/components/Icon.vue';
import { useRoomStore } from '@/stores/room';
const room = useRoomStore();
const draft = ref('');
const box = ref<HTMLElement | null>(null);
const unread = ref(false);
function bottom() {
  if (box.value) {
    box.value.scrollTop = box.value.scrollHeight;
    unread.value = false;
  }
}
watch(
  () => room.chat.length,
  async () => {
    const el = box.value;
    const stick = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 70;
    await nextTick();
    if (stick) bottom();
    else unread.value = true;
  },
);
onMounted(() => nextTick(bottom));
function send() {
  if (!draft.value.trim()) return;
  room.chatSend(draft.value);
  draft.value = '';
  void nextTick(bottom);
}
function color(name: string) {
  return ['sage', 'rose', 'gold', 'blue'][
    Array.from(name).reduce((n, c) => n + c.charCodeAt(0), 0) % 4
  ];
}
</script>
<template>
  <section class="chat-panel" aria-label="房间聊天">
    <header>
      <h2 class="panel-title"><Icon name="chat" :size="17" />一起聊聊</h2>
      <span><i class="online-dot" />{{ room.online }} 人在听</span>
    </header>
    <div
      ref="box"
      class="messages"
      tabindex="0"
      aria-label="聊天记录"
      @scroll="box && box.scrollHeight - box.scrollTop - box.clientHeight < 30 && (unread = false)"
    >
      <p v-if="!room.chat.length" class="empty">还没有人说话，打个招呼吧。</p>
      <template v-for="(m, i) in room.chat" :key="m.ts + '-' + i"
        ><p v-if="m.type === 'notice' || m.type === 'system'" class="notice">{{ m.content }}</p>
        <div v-else class="message" :class="{ self: m.nick === room.nick }">
          <span class="chat-avatar" :class="color(m.nick)">{{ m.nick.slice(0, 1) }}</span>
          <div class="message-main">
            <div class="message-meta">
              <b>{{ m.nick }}</b
              ><time>{{
                new Date(m.ts).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
              }}</time>
            </div>
            <p class="bubble">{{ m.content }}</p>
            <a
              v-for="(img, k) in m.images || []"
              :key="k"
              :href="img"
              target="_blank"
              rel="noreferrer"
              ><img :src="img" alt="聊天图片" class="chat-image" loading="lazy"
            /></a>
          </div></div
      ></template>
    </div>
    <button v-if="unread" class="unread" @click="bottom">
      有新消息 <Icon name="down" :size="12" />
    </button>
    <form class="composer" @submit.prevent="send">
      <label class="visually-hidden" for="chat-draft">聊天消息</label
      ><textarea
        id="chat-draft"
        v-model="draft"
        maxlength="200"
        placeholder="聊点什么？"
        :disabled="!room.joined"
        rows="2"
        @keydown.enter.exact.prevent="send"
      />
      <div class="composer-bottom">
        <span>Enter 发送 · Shift + Enter 换行</span
        ><button :disabled="!room.joined || !draft.trim()" aria-label="发送消息">
          <Icon name="arrow" :size="16" />
        </button>
      </div>
    </form>
  </section>
</template>
<style scoped>
.chat-panel {
  height: 100%;
  padding: 22px 20px 18px;
  display: flex;
  flex-direction: column;
  min-height: 0;
  position: relative;
}
header {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding-bottom: 20px;
  gap: 10px;
}
header h2 {
  font-size: 14px;
}
header > span {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 11px;
  color: var(--muted);
}
header .online-dot {
  width: 4px;
  height: 4px;
  box-shadow: none;
}
.messages {
  flex: 1;
  min-height: 0;
  overflow: auto;
  display: flex;
  flex-direction: column;
  gap: 20px;
  padding-right: 3px;
}
.notice {
  text-align: center;
  font-size: 11px;
  color: #676d60;
  padding: 0 10px;
  overflow-wrap: anywhere;
}
.message {
  display: flex;
  align-items: flex-start;
  gap: 9px;
}
.chat-avatar {
  width: 29px;
  height: 29px;
  display: grid;
  place-items: center;
  border-radius: 10px;
  font-size: 13px;
  flex: none;
  background: #eaf0df;
  color: #5e704f;
}
.rose {
  background: #f5e4df;
  color: #8a6058;
}
.gold {
  background: #f5ecd7;
  color: #7b683a;
}
.blue {
  background: #e5edf0;
  color: #566c75;
}
.message-main {
  min-width: 0;
  flex: 1;
}
.message-meta {
  display: flex;
  align-items: center;
  gap: 9px;
  margin-bottom: 5px;
  min-width: 0;
}
.message-meta b {
  font-size: 12px;
  font-weight: 500;
  color: var(--ink-soft);
  overflow: hidden;
  white-space: nowrap;
  text-overflow: ellipsis;
}
.message-meta time {
  font-size: 10px;
  color: #686c64;
  flex: none;
}
.bubble {
  display: inline-block;
  padding: 9px 11px;
  background: #f2f4ed;
  border-radius: 0 10px 10px 10px;
  font-size: 13px;
  line-height: 1.8;
  color: var(--ink-soft);
  overflow-wrap: anywhere;
  white-space: pre-wrap;
}
.self .bubble {
  background: #eaf2e5;
}
.chat-image {
  margin-top: 7px;
  max-width: 100%;
  max-height: 250px;
  object-fit: contain;
  border-radius: 8px;
}
.composer {
  margin-top: 20px;
  border: 1px solid var(--line);
  border-radius: 12px;
  padding: 11px;
  background: #fafbf8;
  flex: none;
}
.composer:focus-within {
  border-color: #abc2a1;
}
.composer textarea {
  width: 100%;
  border: 0;
  outline: none;
  background: none;
  resize: none;
  font-size: 13px;
  line-height: 1.8;
  min-height: 43px;
}
.composer textarea::placeholder {
  color: #686c61;
}
.composer-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 5px;
  margin-top: 3px;
}
.composer-bottom span {
  font-size: 10px;
  color: #646b60;
}
.composer-bottom button {
  height: 27px;
  width: 30px;
  display: grid;
  place-items: center;
  border-radius: 7px;
  background: #dfeacd;
  color: #507448;
}
.composer-bottom button:disabled {
  opacity: 0.5;
}
.unread {
  position: absolute;
  bottom: 132px;
  left: 50%;
  transform: translateX(-50%);
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 5px 11px;
  border-radius: 20px;
  background: var(--green);
  color: #fff;
  font-size: 12px;
  box-shadow: var(--shadow-card);
}
@media (max-width: 900px) {
  .chat-panel {
    height: calc(100dvh - 245px);
    min-height: 350px;
  }
  .bubble {
    font-size: 13px;
  }
  .message-meta b {
    font-size: 13px;
  }
  .message-meta time {
    font-size: 11px;
  }
  .composer textarea {
    font-size: 13px;
  }
  .composer-bottom span {
    font-size: 11px;
  }
  .chat-avatar {
    width: 32px;
    height: 32px;
    font-size: 14px;
  }
  .notice {
    font-size: 12px;
  }
}
</style>
