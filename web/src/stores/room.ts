import { defineStore } from 'pinia';
import { markRaw } from 'vue';
import { RoomSocket } from '@/api/ws';
import { IS_MOCK, mockChat, mockLyric, mockPlaylists, mockRooms, mockTracks } from '@/mock';
import { usePlayerStore } from '@/stores/player';
import { useUiStore } from '@/stores/ui';
import type {
  ChatMsg,
  Member,
  MusicUser,
  Playlist,
  PlaylistSearchResult,
  Role,
  RoomMode,
  SearchResult,
  SelfInfo,
  ServerMsg,
  Track,
} from '@/types';

const NICK_KEY = 'yue.nick';

function defaultNick(): string {
  const saved = localStorage.getItem(NICK_KEY);
  if (saved) return saved;
  const nick = `游客${Math.floor(1000 + Math.random() * 9000)}`;
  localStorage.setItem(NICK_KEY, nick);
  return nick;
}

function wsUrl(roomId: string): string {
  const proto = location.protocol === 'https:' ? 'wss' : 'ws';
  return `${proto}://${location.host}/api/ws?room=${encodeURIComponent(roomId)}`;
}

const defaultMode = (): RoomMode => ({
  loop: false,
  random: false,
  radio: false,
  goodMode: false,
  banChoose: false,
  banSwitch: false,
  volume: 70,
});

export const useRoomStore = defineStore('room', {
  state: () => ({
    roomId: '',
    pwd: '',
    nick: defaultNick(),
    socket: null as RoomSocket | null,
    connected: false,
    joined: false,
    fatal: '',
    mock: IS_MOCK,
    room: null as {
      id: string;
      name: string;
      radio: boolean;
      needPwd: boolean;
      builtin?: boolean;
    } | null,
    self: null as SelfInfo | null,
    online: 0,
    members: [] as Member[],
    queue: [] as Track[],
    startedAt: 0,
    clockOffset: 0,
    chat: [] as ChatMsg[],
    mode: defaultMode(),
    votes: 0,
    voteNeed: 0,
  }),
  getters: {
    current: (s): Track | null => s.queue[0] || null,
    upcoming: (s): Track[] => s.queue.slice(1),
    isRoot: (s) => s.self?.role === 'root',
    isAdmin: (s) => s.self?.role === 'admin' || s.self?.role === 'root',
    roleLabel: (s) =>
      s.self?.role === 'root' ? '房主' : s.self?.role === 'admin' ? '管理员' : '听众',
    serverNow: (s) => Date.now() + s.clockOffset,
  },
  actions: {
    setNick(nick: string) {
      const clean = nick.trim().slice(0, 16);
      if (!clean) return;
      this.nick = clean;
      localStorage.setItem(NICK_KEY, clean);
    },
    connect(roomId: string, pwd = '') {
      this.leave();
      usePlayerStore().setRefreshHandler((track, failedUrl, lower) =>
        this.refreshMedia(track, failedUrl, lower),
      );
      usePlayerStore().beginSession(() => {
        if (this.joined && !this.mode.banSwitch) this.voteSkip();
      });
      this.roomId = roomId;
      this.pwd = pwd;
      this.fatal = '';
      if (this.mock) {
        this.applyMock(roomId);
        return;
      }
      const socket = new RoomSocket({
        url: wsUrl(roomId),
        onOpen: () => {
          if (this.socket === socket) this.sendHello();
        },
        onStatus: (connected) => {
          if (this.socket === socket) this.connected = connected;
        },
        onEvent: (msg) => {
          if (this.socket === socket) this.handleEvent(msg);
        },
      });
      this.socket = markRaw(socket);
      socket.connect();
    },
    sendHello() {
      this.socket?.send({ t: 'hello', nick: this.nick, pwd: this.pwd });
    },
    leave() {
      usePlayerStore().endSession();
      usePlayerStore().prepareNext(null);
      usePlayerStore().setRefreshHandler(null);
      this.socket?.close();
      this.socket = null;
      this.connected = false;
      this.joined = false;
      this.room = null;
      this.self = null;
      this.queue = [];
      this.startedAt = 0;
      this.online = 0;
      this.members = [];
      this.chat = [];
      this.mode = defaultMode();
      this.votes = 0;
    },
    handleEvent(msg: ServerMsg) {
      const ui = useUiStore();
      switch (msg.t) {
        case 'hello': {
          this.self = msg.self as SelfInfo;
          this.room = msg.room as typeof this.room;
          this.clockOffset = Number(msg.now || Date.now()) - Date.now();
          this.joined = true;
          const ownerPassword = sessionStorage.getItem(`music.owner.${this.roomId}`);
          if (ownerPassword) void this.auth('root', ownerPassword);
          break;
        }
        case 'state': {
          this.mode = {
            loop: Boolean(msg.loop),
            random: Boolean(msg.random),
            radio: Boolean(msg.radio),
            goodMode: Boolean(msg.goodMode),
            banChoose: Boolean(msg.banChoose),
            banSwitch: Boolean(msg.banSwitch),
            volume: Number(msg.volume ?? 70),
          };
          break;
        }
        case 'snapshot': {
          this.queue = (msg.queue as Track[]) || [];
          this.startedAt = Number(msg.startedAt || 0);
          this.members = (msg.members as Member[]) || [];
          this.online = Number(msg.online || 0);
          this.chat = ((msg.chat as ChatMsg[]) || []).slice(-200);
          usePlayerStore().prepareNext((msg.next as Track) || null);
          break;
        }
        case 'presence': {
          this.members = (msg.members as Member[]) || [];
          this.online = Number(msg.online || 0);
          break;
        }
        case 'queue': {
          this.queue = (msg.queue as Track[]) || [];
          if (this.queue[0] && this.queue[0].id === this.current?.id) {
            /* 队列刷新保留当前曲目对象 */
          }
          break;
        }
        case 'current': {
          const track = msg.track as Track;
          const prev = this.queue[0];
          this.startedAt = Number(msg.startedAt || 0);
          if (!track) {
            this.queue = [];
            break;
          }
          const same = (t: Track) =>
            track.queueId ? t.queueId === track.queueId : t.id === track.id;
          this.queue =
            prev && same(prev)
              ? [{ ...prev, ...track }, ...this.queue.slice(1)]
              : [track, ...this.queue.filter((t) => !same(t))];
          break;
        }
        case 'prepare': {
          usePlayerStore().prepareNext((msg.track as Track) || null);
          break;
        }
        case 'lyric': {
          const track = this.current;
          if (track && track.queueId === msg.queueId && this.startedAt === Number(msg.startedAt)) {
            track.lyric = String(msg.lyric || '');
            track.lyricTrans = String(msg.lyricTrans || '');
          }
          break;
        }
        case 'chat': {
          const chat = msg.msg as ChatMsg;
          if (!chat) break;
          this.pushChat(chat);
          break;
        }
        case 'notice': {
          this.pushChat({
            type: 'notice',
            nick: '',
            content: String(msg.content || ''),
            ts: Date.now(),
          });
          break;
        }
        case 'auth': {
          if (this.self && msg.ok) {
            this.self.role = String(msg.role) as Role;
            ui.ok(String(msg.message || '认证成功'));
          } else {
            ui.warn(String(msg.message || '认证失败'));
          }
          break;
        }
        case 'vote': {
          this.votes = Number(msg.votes || 0);
          this.voteNeed = Number(msg.need || 0);
          if (msg.message) ui.toast(String(msg.message));
          break;
        }
        case 'error': {
          if (!this.joined) {
            this.fatal = String(msg.message || '进不去这间房');
          } else {
            ui.error(String(msg.message || '操作失败'));
          }
          break;
        }
        default:
          break;
      }
    },
    pushChat(msg: ChatMsg) {
      this.chat.push(msg);
      if (this.chat.length > 200) this.chat.splice(0, this.chat.length - 200);
    },
    /* ——— 发送动作 ——— */
    chatSend(content: string) {
      const text = content.trim();
      if (!text) return;
      if (this.mock) {
        this.pushChat({
          type: 'chat',
          nick: this.nick,
          content: text,
          ts: Date.now(),
        });
        return;
      }
      this.socket?.send({ t: 'chat', content: text });
    },
    async pick(
      track: Pick<
        Track,
        'id' | 'source' | 'name' | 'artist' | 'album' | 'cover' | 'duration' | 'fee'
      >,
    ) {
      if (this.mock) {
        this.queue.push({
          ...track,
          album: '',
          artist: '上海アリス幻樂団',
          cover: '',
          duration: 200,
          fee: 0,
          url: '',
          nick: this.nick,
        });
        return;
      }
      if (!this.socket) throw new Error('尚未连接');
      await this.socket.request({ ...track, t: 'pick' });
    },
    voteSkip() {
      if (this.mock) return;
      this.socket?.send({ t: 'skip.vote' });
    },
    async search(q: string, page = 1, size = 10): Promise<SearchResult> {
      if (this.mock) {
        return { tracks: mockTracks.slice(0, 6), total: 6 };
      }
      const r = await this.socket!.request({ t: 'search', q, page, size });
      return {
        tracks: (r.tracks as Track[]) || [],
        total: Number(r.total || 0),
      };
    },
    async searchPlaylists(q: string, page = 1, size = 10): Promise<PlaylistSearchResult> {
      if (this.mock) {
        return { playlists: mockPlaylists, total: mockPlaylists.length };
      }
      const r = await this.socket!.request({
        t: 'playlist.search',
        q,
        page,
        size,
      });
      return {
        playlists: (r.playlists as Playlist[]) || [],
        total: Number(r.total || 0),
      };
    },
    async getPlaylist(id: string): Promise<{ playlist: Playlist; tracks: Track[] }> {
      if (this.mock) {
        return { playlist: mockPlaylists[0], tracks: mockTracks };
      }
      const r = await this.socket!.request({ t: 'playlist.get', id }, 20000);
      return {
        playlist: r.playlist as Playlist,
        tracks: (r.tracks as Track[]) || [],
      };
    },
    async searchUsers(q: string, page = 1): Promise<{ users: MusicUser[]; total: number }> {
      if (this.mock)
        return {
          users: [{ id: 'demo-user', nick: '演示听众', avatar: '' }],
          total: 1,
        };
      const r = await this.socket!.request({
        t: 'user.search',
        q,
        page,
        size: 10,
      });
      return {
        users: (r.users as MusicUser[]) || [],
        total: Number(r.total || 0),
      };
    },
    async userPlaylists(uid: string): Promise<Playlist[]> {
      if (this.mock) return mockPlaylists;
      const r = await this.socket!.request({ t: 'user.playlists', uid }, 15000);
      return (r.playlists as Playlist[]) || [];
    },
    async auth(role: 'root' | 'admin', password: string) {
      if (this.mock) {
        if (this.self) this.self.role = role;
        useUiStore().ok(`已认证为${role === 'root' ? '房主' : '管理员'}（预览模式）`);
        return;
      }
      this.socket?.send({ t: 'auth', role, password });
    },
    async refreshMedia(track: Track, failedUrl: string, lower: boolean): Promise<Track> {
      if (!this.socket || !this.joined) throw new Error('尚未连接');
      const res = await this.socket.request(
        { t: 'media.refresh', id: track.queueId, failedUrl, lower },
        10000,
      );
      return res.track as Track;
    },
    async voteRemove(id: string) {
      try {
        if (!this.socket) throw new Error('尚未连接');
        const r = await this.socket.request({ t: 'queue.remove.vote', id });
        useUiStore().ok(String(r.message || '投票已提交'));
      } catch (e) {
        useUiStore().error(e instanceof Error ? e.message : '投票失败');
      }
    },
    async setOwnerPassword(password: string) {
      try {
        if (!this.socket) throw new Error('尚未连接');
        await this.socket.request({ t: 'room.password', password });
        sessionStorage.setItem(`music.owner.${this.roomId}`, password);
        useUiStore().ok('房间管理密码已修改');
      } catch (e) {
        useUiStore().error(e instanceof Error ? e.message : '修改失败');
      }
    },
    queueRemove(id: string) {
      this.socket?.send({ t: 'queue.remove', id });
    },
    queueTop(id: string) {
      this.socket?.send({ t: 'queue.top', id });
    },
    queueClear() {
      this.socket?.send({ t: 'queue.clear' });
    },
    next() {
      this.socket?.send({ t: 'next' });
    },
    setMode(partial: Partial<Pick<RoomMode, 'loop' | 'random' | 'radio'>>) {
      this.socket?.send({ t: 'mode', ...partial });
    },
    setVolume(value: number) {
      this.socket?.send({ t: 'volume', value });
    },
    setBan(partial: { choose?: boolean; switch?: boolean }) {
      this.socket?.send({ t: 'ban', ...partial });
    },
    setGoodMode(on: boolean) {
      this.socket?.send({ t: 'goodmode', on });
    },
    radioLoad(preset: string) {
      this.socket?.send({ t: 'radio.load', preset });
    },
    good(id: string) {
      this.socket?.send({ t: 'good', id });
    },
    blackSession(session: string) {
      this.socket?.send({ t: 'chat.black', session });
    },
    /* ——— 预览模式 ——— */
    applyMock(roomId: string) {
      const info = mockRooms.find((r) => r.id === roomId) || mockRooms[0];
      this.room = {
        id: info.id,
        name: info.name,
        radio: info.radio,
        needPwd: info.needPwd,
        builtin: info.builtin,
      };
      this.self = { session: 'mock', nick: this.nick, role: 'root' };
      this.connected = true;
      this.joined = true;
      this.online = info.online;
      this.chat = mockChat.slice();
      this.mode = {
        ...defaultMode(),
        loop: true,
        random: true,
        radio: info.radio,
      };
      const first: Track = {
        ...mockTracks[0],
        nick: '游客1024',
        lyric: mockLyric,
        url: '',
      };
      this.queue = [first, ...mockTracks.slice(1).map((t) => ({ ...t }))];
      this.startedAt = Date.now() - 61000;
      this.clockOffset = 0;
      usePlayerStore().load(null, 0, 0);
    },
  },
});
