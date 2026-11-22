/** 与 docs/architecture.md §2 一致的共享类型。 */

export interface Track {
  queueId?: string;
  removeVotes?: number;
  removeNeed?: number;
  id: string;
  source: string;
  name: string;
  artist: string;
  album: string;
  cover: string;
  duration: number;
  fee: number;
  url: string;
  nick?: string;
  likes?: number;
  /** 只有「当前播放」的曲目带 LRC 原文。 */
  lyric?: string;
  /** 与 lyric 同时间轴的翻译（可能没有）。 */
  lyricTrans?: string;
}

export interface MusicUser {
  id: string;
  nick: string;
  avatar: string;
}

export interface Member {
  nick: string;
  role: 'guest' | 'admin' | 'root' | string;
}

export interface Playlist {
  id: string;
  name: string;
  cover: string;
  creator: string;
  count: number;
}

export type ChatType = 'chat' | 'notice' | 'system';

export interface ChatMsg {
  type: ChatType;
  nick: string;
  content: string;
  images?: string[];
  ts: number;
  session?: string;
}

export interface NowPlaying {
  name: string;
  artist: string;
  cover: string;
}

export interface RoomSummary {
  id: string;
  name: string;
  desc: string;
  radio: boolean;
  needPwd: boolean;
  online: number;
  builtin: boolean;
  now?: NowPlaying;
}

export interface SiteConfig {
  site: { name: string; tagline: string };
  sources: { id: string; label: string }[];
  radioPresets: { id: string; label: string; playlists: string[] }[];
}

export type Role = 'user' | 'admin' | 'root';

export interface SelfInfo {
  session: string;
  nick: string;
  role: Role;
}

export interface RoomMode {
  loop: boolean;
  random: boolean;
  radio: boolean;
  goodMode: boolean;
  banChoose: boolean;
  banSwitch: boolean;
  volume: number;
}

/** 服务器 → 客户端消息：t 区分类型。 */
export interface ServerMsg {
  t: string;
  [key: string]: unknown;
}

/** 前端内部：搜索/歌单面板的一次请求结果。 */
export interface SearchResult {
  tracks: Track[];
  total: number;
}

export interface PlaylistSearchResult {
  playlists: Playlist[];
  total: number;
}

export interface AdminSessionInfo {
  authenticated: boolean;
  version: string;
  secretsFile?: string;
  secretsWritable?: boolean;
  configFiles?: string[];
  envOverrides?: string[];
}

export interface AdminStorageAccount {
  index: number;
  label: string;
  bucket: string;
  publicBase: string;
  accessKeyMasked: string;
  hasAccessKey: boolean;
  secretKeyMasked: string;
  hasSecretKey: boolean;
}

export interface AdminCustomKey {
  key: string;
  masked: string;
  length: number;
}

export interface AdminCacheStats {
  enabled: boolean;
  items?: number;
  hits?: number;
  inflight?: number;
  accounts?: { label: string; bucket: string; items: number; bytes: number }[];
}

export interface AdminConfigSnapshot {
  version: string;
  secretsFile: string;
  secretsExists: boolean;
  secretsWritable: boolean;
  configFiles: string[];
  envOverrides: string[];
  providerMode: string;
  primaryProvider: string;
  sources: { id: string; label: string }[];
  auth: {
    rootPasswordSet: boolean;
    adminPasswordSet: boolean;
  };
  netease: {
    configured: boolean;
    cookieMasked: string;
    cookieKeys: string[];
  };
  bilibili: {
    enabled: boolean;
    useLogin: boolean;
    configured: boolean;
    cookieMasked: string;
    cookieKeys: string[];
  };
  storage: {
    enabled: boolean;
    maxMBPerAccount: number;
    evictAfterDays: number;
    minHits: number;
    accounts: AdminStorageAccount[];
  };
  keys: AdminCustomKey[];
  cacheStats: AdminCacheStats;
}

export interface NeteaseAccountStatus {
  configured: boolean;
  loggedIn: boolean;
  uid?: string;
  nickname?: string;
  avatar?: string;
  vipType?: number;
  message?: string;
}

export interface BiliAccountStatus {
  configured: boolean;
  useLogin: boolean;
  loggedIn: boolean;
  uid?: string;
  username?: string;
  face?: string;
  vipStatus?: number;
  vipLabel?: string;
  message?: string;
}

export interface QRCreateResp {
  key: string;
  url: string;
  qrSvgDataUrl: string;
}

export interface QRPollResp {
  status: 'waiting' | 'scanned' | 'confirmed' | 'expired';
  code: number;
  message: string;
  nickname?: string;
  avatar?: string;
  saved?: boolean;
  config?: AdminConfigSnapshot;
}
