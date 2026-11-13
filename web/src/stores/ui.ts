import { defineStore } from 'pinia';

export type ToastKind = 'info' | 'ok' | 'warn' | 'error';

export interface Toast {
  id: number;
  text: string;
  kind: ToastKind;
}

let seq = 0;

type RoomDisplay = 'coverVisible' | 'queueVisible' | 'chatVisible';
function savedDisplay(key: RoomDisplay): boolean {
  try {
    return localStorage.getItem(`yue.${key}`) !== 'false';
  } catch {
    return true;
  }
}

export const useUiStore = defineStore('ui', {
  state: () => ({
    toasts: [] as Toast[],
    coverVisible: savedDisplay('coverVisible'),
    queueVisible: savedDisplay('queueVisible'),
    chatVisible: savedDisplay('chatVisible'),
  }),
  actions: {
    toggleRoomDisplay(key: RoomDisplay) {
      this[key] = !this[key];
      try {
        localStorage.setItem(`yue.${key}`, String(this[key]));
      } catch {
        // 当前页面仍可切换；浏览器不允许存储时不影响听歌。
      }
    },
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
