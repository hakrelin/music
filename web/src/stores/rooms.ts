import { defineStore } from 'pinia';
import { http, type CreateRoomBody } from '@/api/http';
import { IS_MOCK, mockConfig, mockRooms } from '@/mock';
import type { RoomSummary, SiteConfig } from '@/types';

export const useRoomsStore = defineStore('rooms', {
  state: () => ({
    rooms: [] as RoomSummary[],
    config: null as SiteConfig | null,
    loading: false,
    error: '',
    mock: IS_MOCK,
  }),
  getters: {
    radioRooms: (s) => s.rooms.filter((r) => r.radio),
    normalRooms: (s) => s.rooms.filter((r) => !r.radio),
    totalOnline: (s) => s.rooms.reduce((n, r) => n + (r.online || 0), 0),
    siteName: (s) => s.config?.site.name || '茶与金平糖',
    tagline: (s) => s.config?.site.tagline || '和大家一起听歌',
  },
  actions: {
    async loadConfig() {
      if (this.mock) {
        this.config = mockConfig;
        return;
      }
      try {
        this.config = await http.config();
      } catch {
        this.config = mockConfig;
      }
    },
    async loadRooms() {
      this.loading = true;
      try {
        if (this.mock) {
          this.rooms = mockRooms.map((r) => ({ ...r }));
          this.error = '';
          return;
        }
        const res = await http.rooms();
        this.rooms = res.rooms || [];
        this.error = '';
      } catch (e) {
        this.error = e instanceof Error ? e.message : '房间列表拉取失败';
      } finally {
        this.loading = false;
      }
    },
    async createRoom(body: CreateRoomBody): Promise<string> {
      if (this.mock) {
        const id = `mock-${Date.now()}`;
        this.rooms.unshift({
          id,
          name: body.name,
          desc: body.desc,
          radio: false,
          needPwd: body.needPwd,
          online: 1,
          builtin: false,
        });
        return id;
      }
      const res = await http.createRoom(body);
      await this.loadRooms();
      return res.id;
    },
  },
});
