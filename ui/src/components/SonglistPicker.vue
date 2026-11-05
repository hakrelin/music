<template>
  <div class="sp">
    <div class="sp-head">
      <span>导入歌单</span>
      <button class="sp-x" @click="$emit('close')">收起</button>
    </div>

    <!-- 1. 按用户找歌单 -->
    <div class="sp-step">
      <label class="sp-label">① 用户昵称 / 用户ID（网易云）</label>
      <div class="sp-row">
        <input class="sp-input" v-model="keyword" placeholder="如 坏北风 或 8615085728" @keyup.enter="searchUser">
        <button class="sp-btn" @click="searchUser">搜用户</button>
      </div>
      <div class="sp-list" v-if="users.length">
        <button v-for="u in users" :key="u.id || u.userId" class="sp-item" @click="pickUser(u)">
          <img v-if="u.avatarUrl || u.avatar" :src="u.avatarUrl || u.avatar" class="sp-ava">
          <span class="sp-name">{{ u.nickname || u.name || u.userName }}</span>
          <span class="sp-meta">ID {{ u.id || u.userId }}</span>
        </button>
      </div>
    </div>

    <!-- 2. 选歌单 -->
    <div class="sp-step" v-if="songlists.length">
      <label class="sp-label">② 选一个歌单</label>
      <div class="sp-list">
        <button v-for="s in songlists" :key="s.id" class="sp-item" @click="pickSonglist(s)">
          <img v-if="s.coverImgUrl || s.picUrl" :src="s.coverImgUrl || s.picUrl" class="sp-ava">
          <span class="sp-name">{{ s.name }}</span>
          <span class="sp-meta">{{ s.trackCount || s.songCount || '?' }} 首</span>
        </button>
      </div>
    </div>

    <!-- 直接填歌单 ID -->
    <div class="sp-step">
      <label class="sp-label">或者直接填歌单 ID</label>
      <div class="sp-row">
        <input class="sp-input" v-model="directIds" placeholder="多个用逗号隔开">
        <button class="sp-btn sp-primary" @click="applyDirect">按这个放</button>
      </div>
    </div>

    <!-- 东方电台预设 -->
    <div class="sp-step sp-preset">
      <label class="sp-label">东方电台</label>
      <p class="sp-hint">{{ touhou.length }} 个东方歌单已内置，一次装载后开循环随机即可 24h 连播。</p>
      <button class="sp-btn sp-primary" @click="loadTouhou">装载东方歌单</button>
    </div>

    <p class="sp-status" v-if="status">{{ status }}</p>
  </div>
</template>

<script>
const TOUHOU_PLAYLISTS = [
  '2416955681', // 东方Project-原曲
  '406607901',  // 東方Project - 正作原曲合集
  '977204775',  // 东方project人气歌曲精选
  '434256234',  // 东方Project［经典的幻想乡神曲］
  '829520066',  // 评论超过5000的东方project神曲
  '2824632945'  // 东方同人音乐
];

export default {
  name: 'SonglistPicker',
  data() {
    return { keyword: '', users: [], songlists: [], directIds: '', status: '', touhou: TOUHOU_PLAYLISTS };
  },
  computed: {
    stomp() { return this.$store.getters.getStompClient; },
    storeUsers() { return this.$store.getters.getSearchUser || []; },
    storeSonglists() { return this.$store.getters.getSearchSonglist || []; }
  },
  watch: {
    storeUsers(v) { if (v && v.length) { this.users = v; this.status = `找到 ${v.length} 个用户`; } },
    storeSonglists(v) { if (v && v.length) { this.songlists = v; this.status = `找到 ${v.length} 个歌单`; } }
  },
  methods: {
    send(dest, body) {
      const c = this.stomp;
      if (!c || !c.connected) { this.status = '还没连上房间'; return; }
      c.send(dest, {}, body === undefined ? '' : JSON.stringify(body));
    },
    searchUser() {
      const k = (this.keyword || '').trim();
      if (!k) { this.status = '先填昵称或用户ID'; return; }
      this.users = []; this.songlists = [];
      // 纯数字当作用户ID，直接查他的歌单
      if (/^\d{4,}$/.test(k)) {
        this.status = '按用户ID查歌单…';
        this.send('/music/searchsonglist', { name: k, source: 'wy_user' });
        return;
      }
      this.status = '搜用户中…';
      this.send('/music/searchuser', { nickname: k, source: 'netease' });
    },
    pickUser(u) {
      const id = u.id || u.userId;
      if (!id) { this.status = '这个用户没返回 ID'; return; }
      this.status = '查该用户的歌单…';
      this.send('/music/searchsonglist', { name: String(id), source: 'wy_user' });
    },
    pickSonglist(s) {
      if (!s || !s.id) return;
      this.status = `正在按歌单「${s.name}」播放…`;
      this.send('/music/setDefaultPlaylist', { id: String(s.id), source: 'netease' });
      // 打开循环 + 随机，队列空也一直放
      this.send('/music/musiclistmodel/true');
      this.send('/music/randommodel/true');
    },
    applyDirect() {
      const ids = (this.directIds || '').replace(/[，\s]+/g, ',').replace(/,+/g, ',').replace(/^,|,$/g, '');
      if (!ids) { this.status = '先填歌单 ID'; return; }
      this.send('/music/setDefaultPlaylist', { id: ids, source: 'netease' });
      this.send('/music/musiclistmodel/true');
      this.send('/music/randommodel/true');
      this.status = '已提交，等后端解析歌单';
    },
    loadTouhou() {
      this.send('/music/setDefaultPlaylist', { id: TOUHOU_PLAYLISTS.join(','), source: 'netease' });
      this.send('/music/musiclistmodel/true');
      this.send('/music/randommodel/true');
      this.status = `已提交 ${TOUHOU_PLAYLISTS.length} 个东方歌单`;
    }
  }
};
</script>

<style scoped>
.sp { background: #26333b; border: 1px solid #354550; border-radius: 10px; padding: 12px 14px; font-size: 13px; color: #dfe8ee; }
.sp-head { display: flex; align-items: center; margin-bottom: 8px; font-size: 13px; color: #9fb3c2; }
.sp-x { margin-left: auto; background: none; border: 0; color: #7f95a5; cursor: pointer; font-size: 12px; }
.sp-step { padding: 8px 0; border-top: 1px dashed #354550; }
.sp-step:first-of-type { border-top: 0; }
.sp-label { display: block; font-size: 12px; color: #8ba0b2; margin-bottom: 6px; }
.sp-row { display: flex; gap: 6px; }
.sp-input { flex: 1; min-width: 0; background: #1c262d; border: 1px solid #354550; border-radius: 6px; padding: 6px 8px; color: #e5eef5; font-size: 12.5px; outline: none; }
.sp-input:focus { border-color: #2fb8a6; }
.sp-btn { background: #2f3d47; border: 1px solid #3d5060; color: #dfe8ee; border-radius: 6px; padding: 5px 10px; font-size: 12.5px; cursor: pointer; white-space: nowrap; }
.sp-btn:hover { border-color: #2fb8a6; color: #8ff0e2; }
.sp-primary { background: #1f6f66; border-color: #2fb8a6; color: #d9fff9; }
.sp-list { margin-top: 8px; max-height: 190px; overflow-y: auto; display: flex; flex-direction: column; gap: 4px; }
.sp-item { display: flex; align-items: center; gap: 8px; background: #1f2a31; border: 1px solid #33424d; border-radius: 6px; padding: 5px 8px; cursor: pointer; color: #dfe8ee; text-align: left; }
.sp-item:hover { border-color: #2fb8a6; }
.sp-ava { width: 22px; height: 22px; border-radius: 4px; object-fit: cover; }
.sp-name { flex: 1; font-size: 12.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sp-meta { font-size: 11px; color: #7f95a5; }
.sp-hint { font-size: 11.5px; color: #7f95a5; margin: 0 0 8px; line-height: 1.5; }
.sp-preset { background: rgba(47,184,166,.07); border-radius: 8px; padding: 8px; margin-top: 6px; }
.sp-status { font-size: 12px; color: #8ff0e2; margin: 8px 0 0; }
</style>
