<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { activeLyricIndex, parseLrc, type LyricLine } from '@/utils/lyric';

const props = defineProps<{ lines: LyricLine[]; time: number; transRaw?: string }>();
const transMap = computed(() => {
  const m = new Map<number, string>();
  for (const l of parseLrc(props.transRaw)) m.set(Math.round(l.time * 10), l.text);
  return m;
});
const box = ref<HTMLElement | null>(null);
const active = computed(() => activeLyricIndex(props.lines, props.time));

async function scrollActive() {
  await nextTick();
  const el = box.value?.querySelector<HTMLElement>('[data-line="' + active.value + '"]');
  if (!el || !box.value) return;
  box.value.scrollTo({
    top: el.offsetTop - box.value.clientHeight / 2 + el.clientHeight / 2,
    behavior: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 'instant' : 'smooth',
  });
}
watch([active, () => props.lines], scrollActive);
const observer = new ResizeObserver(() => void scrollActive());
onMounted(() => {
  if (box.value) observer.observe(box.value);
  void scrollActive();
});
onBeforeUnmount(() => observer.disconnect());
</script>

<template>
  <div ref="box" class="lyrics" tabindex="0" aria-label="歌词">
    <p v-if="!lines.length" class="empty">这一首暂时没有歌词，就听旋律吧。</p>
    <p
      v-for="(line, i) in lines"
      :key="i"
      :data-line="i"
      class="line"
      :class="{ active: i === active, passed: i < active }"
    >
      {{ line.text }}
      <span v-if="transMap.get(Math.round(line.time * 10))" class="trans">{{ transMap.get(Math.round(line.time * 10)) }}</span>
    </p>
  </div>
</template>

<style scoped>
.lyrics {
  height: 100%;
  overflow: auto;
  position: relative;
  scrollbar-width: none;
  mask-image: linear-gradient(180deg, transparent, #000 20%, #000 80%, transparent);
}
.lyrics::-webkit-scrollbar {
  display: none;
}
.line {
  padding: 8px 5px;
  font-size: 13px;
  color: #676d61;
  line-height: 1.8;
  transition: color 0.3s;
}
.line.active {
  color: var(--green);
  font-size: 16px;
  font-weight: 600;
}
.trans {
  display: block;
  font-size: 11.5px;
  font-weight: 400;
  color: #77806f;
  margin-top: 2px;
}
.line.passed {
  color: #666a63;
}
.empty {
  color: var(--muted);
  font-size: 12px;
  padding: 20px;
}
</style>
