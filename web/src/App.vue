<script setup lang="ts">
import ToastHost from '@/components/ToastHost.vue';
</script>

<template>
  <router-view v-slot="{ Component }">
    <component :is="Component" />
  </router-view>
  <ToastHost />
</template>
