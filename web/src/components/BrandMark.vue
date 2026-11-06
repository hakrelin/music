<script setup lang="ts">
withDefaults(defineProps<{ size?: number | string }>(), { size: 42 });
</script>

<template>
  <svg class="brand-mark" :width="size" :height="size" viewBox="0 0 48 48" fill="none" aria-hidden="true">
    <path d="M31.5 7.5a15.2 15.2 0 1 0 8.2 27.9A16.8 16.8 0 0 1 31.5 7.5Z" fill="currentColor" opacity=".9" />
    <path d="M11.5 23.5h16v6.6a6.6 6.6 0 0 1-13.2 0v-6.6Z" fill="#faf9f5" />
    <path d="M27.5 24.8h2.2a3.2 3.2 0 1 1 0 6.4h-2.6" stroke="#faf9f5" stroke-width="1.7" stroke-linecap="round" />
    <path d="m16 19.6 1-3.4m3.7 3.4 1-3.4" stroke="#faf9f5" stroke-width="1.5" stroke-linecap="round" />
    <path d="M9 34.6h21" stroke="currentColor" stroke-width="1.4" stroke-linecap="round" opacity=".55" />
  </svg>
</template>

<style scoped>
.brand-mark {
  display: block;
  transform: rotate(-6deg);
  flex: none;
}
</style>
