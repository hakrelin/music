<script setup lang="ts">
import { useUiStore } from '@/stores/ui';
const ui = useUiStore();
</script>
<template>
  <Teleport to="body"
    ><div class="toasts" aria-live="polite">
      <TransitionGroup name="toast"
        ><div v-for="t in ui.toasts" :key="t.id" class="toast" :class="t.kind">
          {{ t.text }}
        </div></TransitionGroup
      >
    </div></Teleport
  >
</template>
<style scoped>
.toasts {
  position: fixed;
  right: 24px;
  bottom: 125px;
  z-index: 100;
  display: flex;
  flex-direction: column;
  gap: 8px;
  pointer-events: none;
  max-width: calc(100vw - 48px);
}
.toast {
  padding: 11px 18px;
  background: #fff;
  border: 1px solid var(--line-strong);
  border-radius: 12px;
  box-shadow: var(--shadow-card);
  color: var(--green);
  font-size: 12px;
  overflow-wrap: anywhere;
}
.toast.error {
  color: #b06555;
  border-color: #eed1c5;
}
.toast.warn {
  color: #a38336;
}
.toast-enter-active,
.toast-leave-active {
  transition:
    opacity 0.2s,
    transform 0.2s;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(8px);
}
@media (max-width: 900px) {
  .toasts {
    bottom: 98px;
    right: 18px;
    max-width: calc(100vw - 36px);
  }
}
</style>
