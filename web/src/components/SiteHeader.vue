<script setup lang="ts">
import { ref } from 'vue';
import { useRoomStore } from '@/stores/room';
import BrandMark from './BrandMark.vue';
import Icon from './Icon.vue';
import AppDialog from './AppDialog.vue';
withDefaults(defineProps<{ compact?: boolean }>(), { compact: false });
defineEmits<{ create: [] }>();
const room = useRoomStore();
const editing = ref(false);
const nick = ref('');
function edit() {
  nick.value = room.nick;
  editing.value = true;
}
function save() {
  room.setNick(nick.value);
  if (room.joined) room.connect(room.roomId, room.pwd);
  editing.value = false;
}
</script>
<template>
  <header class="site-header" :class="{ compact }">
    <RouterLink to="/" class="brand" aria-label="茶与金平糖 · 听歌大厅"
      ><BrandMark /><span
        ><b>茶与金平糖<span class="brand-dot">.</span></b
        ><small>TEA &amp; KOMPEITO <i>/ 一起听歌</i></small></span
      ></RouterLink
    >
    <nav v-if="!compact" aria-label="主导航">
      <a class="active" href="#rooms">听歌大厅</a
      ><a href="https://tea-kompeito.com" target="_blank" rel="noreferrer">汉化组 <span>↗</span></a>
    </nav>
    <div class="header-actions">
      <button class="nickname" @click="edit">
        <span class="avatar">{{ room.nick.slice(0, 1) }}</span
        ><span>{{ room.nick }}</span
        ><Icon name="chevron" :size="14" /></button
      ><button v-if="!compact" class="btn btn-primary" @click="$emit('create')">
        <Icon name="plus" :size="16" /><span>创建房间</span></button
      ><slot />
    </div>
  </header>
  <AppDialog :open="editing" title="怎么称呼你？" @close="editing = false"
    ><form class="nickname-form" @submit.prevent="save">
      <label class="field"
        ><span>昵称</span
        ><input v-model="nick" class="input" maxlength="16" required placeholder="输入你的昵称"
      /></label>
      <p>昵称会用于所有房间，保存后会重新加入当前房间。</p>
      <button class="btn btn-primary" type="submit">保存</button>
    </form></AppDialog
  >
</template>
<style scoped>
.site-header {
  height: 84px;
  max-width: 1400px;
  padding: 0 40px;
  margin: auto;
  display: flex;
  align-items: center;
  gap: 58px;
}
.brand {
  display: flex;
  align-items: center;
  gap: 11px;
  flex-shrink: 0;
}
.brand b {
  font-size: 20px;
  letter-spacing: -0.7px;
  line-height: 1.3;
}
.brand-dot {
  color: var(--vermilion);
  padding-left: 3px;
}
.brand small {
  display: block;
  font-size: 8px;
  font-weight: 600;
  letter-spacing: 1.65px;
  color: var(--muted);
}
.brand small i {
  font-style: normal;
  letter-spacing: 0.7px;
  padding-left: 3px;
}
nav {
  display: flex;
  gap: 27px;
  align-self: stretch;
  font-size: 13px;
  color: var(--muted);
}
nav a {
  display: flex;
  align-items: center;
  position: relative;
  gap: 6px;
}
nav a.active {
  color: var(--green);
  font-weight: 600;
}
nav a.active:after {
  content: '';
  height: 3px;
  width: 18px;
  background: var(--green);
  border-radius: 3px;
  position: absolute;
  bottom: 16px;
  left: calc(50% - 9px);
}
nav a span {
  font-size: 12px;
}
.header-actions {
  margin-left: auto;
  display: flex;
  align-items: center;
  gap: 21px;
}
.nickname {
  display: flex;
  align-items: center;
  gap: 9px;
  font-size: 12px;
  color: var(--ink-soft);
}
.avatar {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 50%;
  background: #f1e9d8;
  color: #9a7b44;
  font-size: 12px;
}
.nickname-form {
  display: grid;
  gap: 18px;
}
.nickname-form p {
  color: var(--muted);
  font-size: 12px;
}
.nickname-form .btn {
  justify-self: end;
}
.site-header.compact {
  height: 56px;
  width: 100%;
  max-width: 1600px;
  padding: 0 24px;
}
.compact .brand b {
  font-size: 17px;
}
.compact .brand small {
  display: none;
}
.compact .brand :deep(svg) {
  width: 30px;
  height: 30px;
}
@media (max-width: 900px) {
  .site-header {
    padding: 0 24px;
    gap: 30px;
  }
  .header-actions {
    gap: 12px;
  }
}
@media (max-width: 600px) {
  .site-header.compact {
    height: 48px;
    padding: 0 12px;
  }
  .site-header {
    height: 74px;
    padding: 0 18px;
    gap: 12px;
  }
  .brand b {
    font-size: 17px;
  }
  .brand small {
    font-size: 6.5px;
    letter-spacing: 1px;
  }
  .brand small i {
    display: none;
  }
  nav {
    display: none;
  }
  .header-actions > .btn {
    width: 36px;
    min-height: 36px;
    padding: 0;
  }
  .header-actions > .btn span {
    display: none;
  }
  .nickname > span:not(.avatar),
  .nickname > .icon {
    display: none;
  }
  .header-actions {
    gap: 8px;
  }
}
</style>
