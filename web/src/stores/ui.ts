import { defineStore } from 'pinia';

export type ToastKind = 'info' | 'ok' | 'warn' | 'error';

export interface Toast {
  id: number;
  text: string;
  kind: ToastKind;
}

let seq = 0;

export const useUiStore = defineStore('ui', {
  state: () => ({
    toasts: [] as Toast[]
  }),
  actions: {
    toast(text: string, kind: ToastKind = 'info', ms = 2800) {
      const id = ++seq;
      this.toasts.push({ id, text, kind });
      window.setTimeout(() => this.dismiss(id), ms);
      return id;
    },
    ok(text: string) {
      return this.toast(text, 'ok');
    },
    warn(text: string) {
      return this.toast(text, 'warn', 3400);
    },
    error(text: string) {
      return this.toast(text, 'error', 4000);
    },
    dismiss(id: number) {
      this.toasts = this.toasts.filter((t) => t.id !== id);
    }
  }
});
