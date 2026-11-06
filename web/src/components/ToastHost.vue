<script setup lang="ts">
import { useUiStore } from '@/stores/ui';

const ui = useUiStore();
</script>

<template>
  <div class="toast-host" aria-live="polite">
    <transition-group name="toast">
      <div v-for="t in ui.toasts" :key="t.id" class="toast" :class="t.kind" @click="ui.dismiss(t.id)">
        {{ t.text }}
      </div>
    </transition-group>
  </div>
</template>

<style scoped>
.toast-host {
  position: fixed;
  left: 50%;
  bottom: 34px;
  transform: translateX(-50%);
  z-index: 90;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 10px;
  pointer-events: none;
}
.toast {
  pointer-events: auto;
  max-width: min(560px, 86vw);
  padding: 11px 20px;
  border-radius: 999px;
  background: rgba(20, 58, 49, 0.94);
  color: #f2f6e9;
  font-size: 13px;
  letter-spacing: 0.02em;
  box-shadow: 0 16px 40px rgba(12, 36, 31, 0.28);
  backdrop-filter: blur(8px);
}
.toast.ok {
  background: rgba(31, 75, 60, 0.95);
}
.toast.warn {
  background: rgba(138, 106, 44, 0.95);
}
.toast.error {
  background: rgba(150, 58, 46, 0.95);
}
.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(10px);
}
</style>
