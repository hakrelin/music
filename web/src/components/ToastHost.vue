<script setup lang="ts">
import { ref, watch, nextTick } from 'vue';
import { useUiStore } from '@/stores/ui';
const ui = useUiStore();
const host = ref<HTMLElement>();
watch(
  () => ui.toasts.map((t) => t.id).join(','),
  async () => {
    await nextTick();
    host.value?.hidePopover();
    if (ui.toasts.length) host.value?.showPopover();
  },
);
</script>
<template>
  <Teleport to="body"
    ><div ref="host" popover="manual" class="toasts" aria-live="polite">
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
  inset: auto 24px 125px auto;
  margin: 0;
  padding: 0;
  border: 0;
  background: transparent;
  overflow: visible;
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
