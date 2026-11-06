<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{ name: string; size?: number | string; stroke?: number }>(), {
  size: 20,
  stroke: 1.7
});

/** 统一线性图标（stroke 风格，与宋体标题的细线条呼应）。 */
const paths: Record<string, string> = {
  play: '<path d="M8 5.4v13.2L19 12 8 5.4Z" fill="currentColor" stroke="none"/>',
  pause: '<path d="M8.5 5.5v13M15.5 5.5v13" stroke-linecap="round"/>',
  next: '<path d="M6 6.2v11.6L15 12 6 6.2Z" fill="currentColor" stroke="none"/><path d="M18.5 5.5v13" stroke-linecap="round"/>',
  prev: '<path d="M18 6.2v11.6L9 12l9-5.8Z" fill="currentColor" stroke="none"/><path d="M5.5 5.5v13" stroke-linecap="round"/>',
  volume: '<path d="M4 9.5h3.2L12 5.6v12.8L7.2 14.5H4v-5Z" stroke-linejoin="round"/><path d="M15.5 9a4.4 4.4 0 0 1 0 6M18 6.6a8 8 0 0 1 0 10.8" stroke-linecap="round"/>',
  mute: '<path d="M4 9.5h3.2L12 5.6v12.8L7.2 14.5H4v-5Z" stroke-linejoin="round"/><path d="m16 9.5 4 5m0-5-4 5" stroke-linecap="round"/>',
  search: '<circle cx="10.8" cy="10.8" r="6.2"/><path d="m15.6 15.6 4.3 4.3" stroke-linecap="round"/>',
  plus: '<path d="M12 5.5v13M5.5 12h13" stroke-linecap="round"/>',
  minus: '<path d="M5.5 12h13" stroke-linecap="round"/>',
  close: '<path d="m6 6 12 12M18 6 6 18" stroke-linecap="round"/>',
  arrow: '<path d="M4.5 12h14M13 6.5l5.5 5.5L13 17.5" stroke-linecap="round" stroke-linejoin="round"/>',
  up: '<path d="M12 19V5M6.5 10.5 12 5l5.5 5.5" stroke-linecap="round" stroke-linejoin="round"/>',
  down: '<path d="M12 5v14M6.5 13.5 12 19l5.5-5.5" stroke-linecap="round" stroke-linejoin="round"/>',
  check: '<path d="m5 12.5 4.5 4.5L19 6.5" stroke-linecap="round" stroke-linejoin="round"/>',
  lock: '<rect x="5" y="10.5" width="14" height="9" rx="2"/><path d="M8.2 10.5V8a3.8 3.8 0 0 1 7.6 0v2.5" stroke-linecap="round"/>',
  radio: '<circle cx="12" cy="12" r="2.2"/><path d="M8.6 8.6a4.8 4.8 0 0 0 0 6.8M15.4 8.6a4.8 4.8 0 0 1 0 6.8M5.8 5.8a8.8 8.8 0 0 0 0 12.4M18.2 5.8a8.8 8.8 0 0 1 0 12.4" stroke-linecap="round"/>',
  settings: '<path d="M4.5 7.5h15M4.5 16.5h15" stroke-linecap="round"/><circle cx="9" cy="7.5" r="2.1"/><circle cx="15" cy="16.5" r="2.1"/>',
  trash: '<path d="M5.5 7.5h13M9.5 7.5V5.8h5v1.7M7.5 7.5l.8 11.2h7.4l.8-11.2" stroke-linecap="round" stroke-linejoin="round"/>',
  pin: '<path d="M12 4v11M7.5 10.5 12 15l4.5-4.5" stroke-linecap="round" stroke-linejoin="round"/><path d="M6 20h12" stroke-linecap="round"/>',
  heart: '<path d="M12 19.5s-7-4.3-7-9.2A3.9 3.9 0 0 1 12 7.5a3.9 3.9 0 0 1 7 2.8c0 4.9-7 9.2-7 9.2Z" stroke-linejoin="round"/>',
  users: '<circle cx="9.5" cy="9" r="3.2"/><path d="M3.8 19c.6-3 2.9-4.6 5.7-4.6s5.1 1.6 5.7 4.6" stroke-linecap="round"/><path d="M15.5 6.2a3 3 0 0 1 0 5.6M17.2 14.7c2 .5 3.2 2 3.6 4.3" stroke-linecap="round"/>',
  music: '<path d="M9.5 18V6.6l9-1.6v11" stroke-linecap="round" stroke-linejoin="round"/><circle cx="7" cy="18" r="2.4"/><circle cx="16" cy="16.5" r="2.4"/>',
  link: '<path d="M10 14a3.4 3.4 0 0 0 4.8 0l2.6-2.6a3.4 3.4 0 0 0-4.8-4.8L11.2 8" stroke-linecap="round"/><path d="M14 10a3.4 3.4 0 0 0-4.8 0l-2.6 2.6a3.4 3.4 0 0 0 4.8 4.8l1.4-1.4" stroke-linecap="round"/>',
  refresh: '<path d="M19 12a7 7 0 1 1-2.2-5.1" stroke-linecap="round"/><path d="M19.2 4.8v4.4h-4.4" stroke-linecap="round" stroke-linejoin="round"/>',
  chevron: '<path d="m8 10 4 4 4-4" stroke-linecap="round" stroke-linejoin="round"/>',
  list: '<path d="M8.5 7h11M8.5 12h11M8.5 17h11" stroke-linecap="round"/><circle cx="4.6" cy="7" r="1" fill="currentColor" stroke="none"/><circle cx="4.6" cy="12" r="1" fill="currentColor" stroke="none"/><circle cx="4.6" cy="17" r="1" fill="currentColor" stroke="none"/>',
  chat: '<path d="M4.5 6.5h15v9.2l-3.6 3.3h-11.4V6.5Z" stroke-linejoin="round"/><path d="M8 10.5h8M8 13.5h5" stroke-linecap="round"/>',
  home: '<path d="m4.5 10.5 7.5-6 7.5 6V19h-5v-5.5h-5V19h-5v-8.5Z" stroke-linejoin="round"/>',
  tea: '<path d="M5.5 9.5h13v4.2a5.5 5.5 0 0 1-11 0V9.5Z" stroke-linejoin="round"/><path d="M18.5 10.8h1.4a2.4 2.4 0 1 1 0 4.8h-1.6" stroke-linecap="round"/><path d="M9.5 6.5 10 4.5m3.5 2 .5-2" stroke-linecap="round"/>',
  info: '<circle cx="12" cy="12" r="8.2"/><path d="M12 11v5.2" stroke-linecap="round"/><circle cx="12" cy="8" r="1" fill="currentColor" stroke="none"/>'
};

const html = computed(() => paths[props.name] || '');
</script>

<template>
  <svg
    class="icon"
    :width="size"
    :height="size"
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    :stroke-width="stroke"
    aria-hidden="true"
    v-html="html"
  />
</template>

<style scoped>
.icon {
  flex: none;
  display: block;
}
</style>
