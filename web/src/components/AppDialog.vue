<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import Icon from './Icon.vue';
const props = withDefaults(
  defineProps<{ open: boolean; title: string; wide?: boolean }>(),
  {
    wide: false,
  },
);
const emit = defineEmits<{ close: [] }>();
const dialog = ref<HTMLDialogElement | null>(null);
function update() {
  if (props.open && !dialog.value?.open) dialog.value?.showModal();
  else if (!props.open && dialog.value?.open) dialog.value.close();
}
watch(() => props.open, update);
onMounted(update);
function outside(e: MouseEvent) {
  const el = dialog.value;
  if (!el || e.target !== el) return;
  const r = el.getBoundingClientRect();
  if (
    e.clientX < r.left ||
    e.clientX > r.right ||
    e.clientY < r.top ||
    e.clientY > r.bottom
  )
    emit('close');
}
function trapFocus(event: KeyboardEvent) {
  if (event.key !== 'Tab' || !dialog.value) return;
  const items = Array.from(
    dialog.value.querySelectorAll<HTMLElement>(
      'button:not([disabled]),input:not([disabled]),textarea:not([disabled]),select:not([disabled]),a[href],[tabindex]:not([tabindex="-1"])',
    ),
  ).filter((el) => el.getClientRects().length > 0);
  const first = items[0];
  const last = items[items.length - 1];
  if (!first) {
    event.preventDefault();
    return;
  }
  if (event.shiftKey && document.activeElement === first) {
    event.preventDefault();
    last.focus();
  } else if (!event.shiftKey && document.activeElement === last) {
    event.preventDefault();
    first.focus();
  }
}
</script>
<template>
  <Teleport to="body"
    ><dialog
      ref="dialog"
      :class="{ wide }"
      :aria-label="title"
      @close="emit('close')"
      @click="outside"
      @keydown="trapFocus"
    >
      <header>
        <h2>{{ title }}</h2>
        <button class="icon-btn" aria-label="关闭" @click="emit('close')">
          <Icon name="close" :size="20" />
        </button>
      </header>
      <div class="dialog-content"><slot /></div></dialog
  ></Teleport>
</template>
<style scoped>
dialog {
  width: min(460px, calc(100% - 28px));
  max-height: calc(100dvh - 40px);
  padding: 0;
  border: 1px solid var(--line);
  border-radius: 22px;
  background: #fff;
  color: var(--ink);
  box-shadow: var(--shadow-night);
}
dialog.wide {
  width: min(660px, calc(100% - 28px));
}
dialog::backdrop {
  background: #203c3447;
  backdrop-filter: blur(6px);
}
header {
  position: sticky;
  top: 0;
  z-index: 2;
  background: #fffffff5;
  display: flex;
  justify-content: space-between;
  align-items: center;
  padding: 19px 24px;
  border-bottom: 1px solid var(--line);
}
h2 {
  font-size: 18px;
}
.dialog-content {
  padding: 24px;
}
@media (max-width: 500px) {
  header {
    padding: 16px 19px;
  }
  .dialog-content {
    padding: 19px;
  }
}
</style>
