<script setup lang="ts">
withDefaults(defineProps<{ size?: number }>(), { size: 36 });
</script>
<template>
  <svg :width="size" :height="size" viewBox="0 0 40 40" fill="none" aria-hidden="true">
    <rect width="40" height="40" rx="13" fill="#e9f0df" />
    <path d="M9 19h19v6a8 8 0 0 1-16 0v-6" fill="#34745c" />
    <path d="M28 20h2a4 4 0 0 1 0 8h-3" stroke="#34745c" stroke-width="2.5" />
    <path
      d="m20 6 2.1 3.5 4-.3-.9 3.9 2.8 2.9-3.8 1.4-1.2 3.9-3-2.7-3.9 1-.1-4-3.4-2.2 3.7-1.6Z"
      fill="#e9bc56"
    />
    <path d="M10 34h19" stroke="#34745c" stroke-width="2" stroke-linecap="round" />
  </svg>
</template>
