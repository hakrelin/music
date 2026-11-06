<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import { activeLyricIndex, type LyricLine } from '@/utils/lyric';

const props = defineProps<{ lines: LyricLine[]; time: number }>();
const box = ref<HTMLElement | null>(null);
const active = computed(() => activeLyricIndex(props.lines, props.time));

watch(active, async (idx) => {
  if (idx < 0 || !box.value) return;
  const el = box.value.querySelector<HTMLElement>(`[data-line="${idx}"]`);
  if (!el) return;
  await nextTick();
  box.value.scrollTo({ top: el.offsetTop - box.value.clientHeight / 2 + el.clientHeight / 2, behavior: 'smooth' });
});
</script>

<template>
  <div ref="box" class="lyrics" aria-label="歌词">
    <p v-if="!lines.length" class="empty">这一首暂时没有歌词，就听旋律吧。</p>
    <p
      v-for="(line, i) in lines"
      :key="i"
      :data-line="i"
      class="line"
      :class="{ active: i === active, passed: i < active }"
    >
      {{ line.text }}
    </p>
  </div>
</template>

<style scoped>
.lyrics {
  height: 100%;
  overflow: hidden;
  position: relative;
  mask-image: linear-gradient(180deg, transparent 0%, #000 18%, #000 82%, transparent 100%);
  scrollbar-width: none;
}
.lyrics::-webkit-scrollbar {
  display: none;
}
.empty {
  padding: 18px 0;
  font-size: 12.5px;
  color: rgba(232, 239, 220, 0.5);
}
.line {
  padding: 7px 0;
  font-size: 13.5px;
  line-height: 1.7;
  color: rgba(232, 239, 220, 0.42);
  transition: color 0.35s ease, transform 0.35s ease;
}
.line.active {
  color: var(--lime);
  font-family: var(--serif);
  font-size: 16px;
  letter-spacing: 0.04em;
  transform: translateX(4px);
}
.line.passed {
  color: rgba(232, 239, 220, 0.28);
}
</style>
