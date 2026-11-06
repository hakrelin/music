import type { ChatMsg, Playlist, RoomSummary, SiteConfig, Track } from '@/types';

/** VITE_MOCK=1 时前端不进后端，用内置数据渲染（只用于视觉走查）。 */
export const IS_MOCK = import.meta.env.VITE_MOCK === '1';

const cover = (n: number) => ['/art/night.jpg', '/art/hero.jpg', '/art/walk.jpg'][n % 3];

export const mockConfig: SiteConfig = {
  site: { name: '茶与月色', tagline: '在幻想乡，一起听一首歌' },
  sources: [{ id: 'wy', label: '网易云' }],
  radioPresets: [
    { id: 'touhou', label: '东方电台', playlists: [] }
  ]
};

export const mockTracks: Track[] = [
  { id: '1', source: 'wy', name: '夜雀の歌声 ～ Night Bird', artist: '上海アリス幻樂団', album: '東方永夜抄', cover: cover(0), duration: 214, fee: 0, url: '', nick: '游客1024', likes: 3 },
  { id: '2', source: 'wy', name: '少女綺想曲 ～ Dream Battle', artist: '上海アリス幻樂団', album: '東方永夜抄', cover: cover(1), duration: 189, fee: 0, url: '', nick: '博丽的茶壶', likes: 1 },
  { id: '3', source: 'wy', name: '恋色マスタースパーク', artist: '上海アリス幻樂団', album: '東方永夜抄', cover: cover(2), duration: 231, fee: 0, url: '', nick: '七色的人偶', likes: 0 },
  { id: '4', source: 'wy', name: '竹取飛翔 ～ Lunatic Princess', artist: '上海アリス幻樂団', album: '東方永夜抄', cover: cover(0), duration: 266, fee: 1, url: '', nick: '月面的兔子', likes: 2 },
  { id: '5', source: 'wy', name: '幻視の夜 ～ Ghostly Eyes', artist: '上海アリス幻樂団', album: '東方永夜抄', cover: cover(1), duration: 205, fee: 0, url: '', nick: '香霖堂常客', likes: 0 }
];

export const mockRooms: RoomSummary[] = [
  {
    id: 'radio',
    name: '东方电台',
    desc: '24 小时东方 Project，不间断。夜深了也有人在听。',
    radio: true,
    needPwd: false,
    online: 12,
    builtin: true,
    now: { name: '夜雀の歌声 ～ Night Bird', artist: '上海アリス幻樂団', cover: cover(0) }
  },
  {
    id: 'room-tea',
    name: '博丽茶席',
    desc: '一壶茶，几张碟，想听什么就点什么。',
    radio: false,
    needPwd: false,
    online: 4,
    builtin: false,
    now: { name: '少女綺想曲 ～ Dream Battle', artist: '上海アリス幻樂団', cover: cover(1) }
  },
  {
    id: 'room-night',
    name: '宵暗夜话',
    desc: '只放安静的曲子。进来的人请把声音调小一点。',
    radio: false,
    needPwd: true,
    online: 2,
    builtin: false,
    now: { name: '竹取飛翔 ～ Lunatic Princess', artist: '上海アリス幻樂団', cover: cover(2) }
  }
];

export const mockChat: ChatMsg[] = [
  { type: 'notice', nick: '', content: '欢迎来到茶与月色，点歌队列已经准备好了。', ts: Date.now() - 600000 },
  { type: 'chat', nick: '游客1024', content: '今晚放点永夜抄吧，夜雀那首很好听。', ts: Date.now() - 520000 },
  { type: 'chat', nick: '博丽的茶壶', content: '已经在队列里了，下一首就是。', ts: Date.now() - 480000 },
  { type: 'notice', nick: '', content: '东方电台 已开始播放', ts: Date.now() - 300000 },
  { type: 'chat', nick: '七色的人偶', content: '这间房的气氛真好。', ts: Date.now() - 120000 }
];

export const mockPlaylists: Playlist[] = [
  { id: '2416955681', name: '东方Project-原曲', cover: cover(0), creator: '被钓起来的鱼', count: 1398 },
  { id: '406607901', name: '東方Project - 正作原曲合集', cover: cover(1), creator: '幻想乡的居民', count: 275 },
  { id: '2824632945', name: '东方同人音乐', cover: cover(2), creator: '夜雀', count: 4038 }
];

export const mockLyric = `[00:00.00] 夜雀の歌声 ～ Night Bird
[00:12.40] 夜の闇に 溶けてゆく
[00:24.80] 小さな羽音 ひとつ
[00:37.20] 月の光が 道しるべ
[00:49.60] 迷い込んだ 竹林の奥
[01:02.00] 歌うように 鳴いている
[01:14.40] 夜雀の歌声
[01:26.80] この夜が 終わるまで
[01:39.20] そばにいて
[01:51.60] 茶が冷める前に
[02:04.00] もう一曲 どうぞ`;
