<script setup lang="ts">
import { computed, ref } from 'vue';
import Icon from '@/components/Icon.vue';
import { useRoomStore } from '@/stores/room';
import { useRoomsStore } from '@/stores/rooms';
import { useUiStore } from '@/stores/ui';
import type { Playlist, Track, MusicUser } from '@/types';

const room = useRoomStore();
const rooms = useRoomsStore();
const ui = useUiStore();

type Tab = 'search' | 'playlist' | 'radio';
const tab = ref<Tab>('search');

/* ——— 搜索点歌 ——— */
const keyword = ref('');
const searching = ref(false);
const results = ref<Track[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = 8;
const searched = ref(false);
const pending = ref(new Set<string>());

async function doSearch(reset = true) {
  if (searching.value) return;
  const q = keyword.value.trim();
  if (!q) return ui.warn('先输入歌名或歌手');
  if (reset) page.value = 1;
  searching.value = true;
  try {
    const res = await room.search(q, page.value, pageSize);
    results.value = res.tracks;
    total.value = res.total;
    searched.value = true;
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '搜索失败');
  } finally {
    searching.value = false;
  }
}

function turnPage(delta: number) {
  const max = Math.max(1, Math.ceil(total.value / pageSize));
  const next = Math.min(max, Math.max(1, page.value + delta));
  if (next === page.value) return;
  page.value = next;
  void doSearch(false);
}

async function pick(track: Track) {
  if (pending.value.has(track.id)) return;
  pending.value.add(track.id);
  try {
    await room.pick({
      id: track.id,
      source: track.source,
      name: track.name,
      artist: track.artist,
      album: track.album,
      cover: track.cover,
      duration: track.duration,
      fee: track.fee,
    });
    ui.ok(`点歌成功：${track.name}`);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '点歌失败');
  } finally {
    pending.value.delete(track.id);
  }
}

/* ——— 导入歌单 ——— */
const plKeyword = ref('');
const plMode = ref<'search' | 'id' | 'user'>('search');
const plLoading = ref(false);
const users = ref<MusicUser[]>([]);
const userPage = ref(1);
const userTotal = ref(0);
const selectedUser = ref<MusicUser | null>(null);
async function openUser(u: MusicUser) {
  plLoading.value = true;
  selectedUser.value = u;
  try {
    plList.value = await room.userPlaylists(u.id);
    plPage.value = 1;
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '用户歌单加载失败');
  } finally {
    plLoading.value = false;
  }
}
function changeMode(mode: 'search' | 'id' | 'user') {
  plMode.value = mode;
  users.value = [];
  plList.value = [];
  current.value = null;
  selectedUser.value = null;
  plSearched.value = false;
  userPage.value = 1;
}
async function userTurn(delta: number) {
  userPage.value += delta;
  await searchPlaylists(false);
}
const plList = ref<Playlist[]>([]);
// 歌单列表分页：用户歌单可能有上百个，一次只渲染 10 个
const PL_PAGE = 30;
const plPage = ref(1);
const plPages = computed(() => Math.max(1, Math.ceil(plList.value.length / PL_PAGE)));
const plPaged = computed(() =>
  plList.value.slice((plPage.value - 1) * PL_PAGE, plPage.value * PL_PAGE),
);
function plTurn(delta: number) {
  plPage.value = Math.min(plPages.value, Math.max(1, plPage.value + delta));
}
// 页码输入框：回车或点「跳转」直达该页
const plJump = ref('');
async function plSearchTurn(delta: number) {
  const next = Math.min(plSearchPages.value, Math.max(1, plSearchPage.value + delta));
  if (next === plSearchPage.value) return;
  plSearchPage.value = next;
  await searchPlaylists(false);
}
function plGoto() {
  const n = Math.floor(Number(plJump.value));
  if (!Number.isFinite(n) || n < 1) {
    plJump.value = '';
    return;
  }
  plPage.value = Math.min(plPages.value, n);
  plJump.value = '';
}
const plSearched = ref(false);
const current = ref<Playlist | null>(null);
const currentTracks = ref<Track[]>([]);
// 歌单曲目分页：1398 首的东方歌单也能翻完
const TRACK_PAGE = 50;     // 歌单曲目每页 50 首
const trackPage = ref(1);
// 关键词搜歌单走服务端分页
const plSearchPage = ref(1);
const plSearchTotal = ref(0);
const plSearchPages = computed(() => Math.max(1, Math.ceil(plSearchTotal.value / PL_PAGE)));
const trackPages = computed(() => Math.max(1, Math.ceil(currentTracks.value.length / TRACK_PAGE)));
const trackPaged = computed(() =>
  currentTracks.value.slice((trackPage.value - 1) * TRACK_PAGE, trackPage.value * TRACK_PAGE),
);
function trackTurn(delta: number) {
  trackPage.value = Math.min(trackPages.value, Math.max(1, trackPage.value + delta));
}const trackJump = ref('');
function trackGoto() {
  const n = Math.floor(Number(trackJump.value));
  if (!Number.isFinite(n) || n < 1) { trackJump.value = ''; return; }
  trackPage.value = Math.min(trackPages.value, n);
  trackJump.value = '';
}
const loadingTracks = ref(false);
const picked = ref<Set<string>>(new Set());

async function searchPlaylists(reset = true) {
  if (plLoading.value) return;
  const q = plKeyword.value.trim();
  if (!q) return ui.warn('输入歌单关键词、ID 或用户昵称');
  if (reset) userPage.value = 1;
  users.value = [];
  plList.value = [];
  selectedUser.value = null;
  plSearched.value = false;
  plLoading.value = true;
  current.value = null;
  currentTracks.value = [];
  try {
    if (plMode.value === 'user') {
      const res = await room.searchUsers(q, userPage.value);
      users.value = res.users;
      userTotal.value = res.total;
      plSearched.value = true;
    } else if (plMode.value === 'id') {
      const one = await room.getPlaylist(q);
      current.value = one.playlist;
      currentTracks.value = one.tracks;
    } else {
      const res = await room.searchPlaylists(q, plSearchPage.value, PL_PAGE);
      plList.value = res.playlists;
      plSearchTotal.value = res.total || res.playlists.length;
      plPage.value = 1;
      plSearched.value = true;
    }
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '歌单加载失败');
  } finally {
    plLoading.value = false;
  }
}

async function openPlaylist(p: Playlist) {
  current.value = p;
  currentTracks.value = [];
  loadingTracks.value = true;
  try {
    const one = await room.getPlaylist(p.id);
    current.value = one.playlist;
    currentTracks.value = one.tracks;
    trackPage.value = 1;
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '歌单曲目加载失败');
  } finally {
    loadingTracks.value = false;
  }
}

const batchPicking = ref(false);
async function pickAll() {
  if (batchPicking.value) return;
  const rest = currentTracks.value.filter((t) => !picked.value.has(t.id)).slice(0, 20);
  if (!rest.length) return ui.warn('没有可以加入的曲目');
  batchPicking.value = true;
  let count = 0;
  try {
    for (const t of rest) {
      await room.pick(t);
      picked.value.add(t.id);
      count++;
    }
    ui.ok(`已加入 ${count} 首`);
  } catch (e) {
    ui.error(`已加入 ${count} 首；${e instanceof Error ? e.message : '点歌失败'}`);
  } finally {
    batchPicking.value = false;
  }
}
async function pickOne(t: Track) {
  if (picked.value.has(t.id) || pending.value.has(t.id)) return;
  pending.value.add(t.id);
  try {
    await room.pick(t);
    picked.value.add(t.id);
    ui.ok(`点歌成功：${t.name}`);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '点歌失败');
  } finally {
    pending.value.delete(t.id);
  }
}

/* ——— 东方电台 ——— */
const presets = computed(() => {
  const list = rooms.config?.radioPresets || [];
  return list.length ? list : [{ id: 'touhou', label: '东方电台', playlists: [] }];
});

function loadRadio(id: string) {
  room.radioLoad(id);
  ui.ok('已请求装载电台歌单，稍等队列刷新');
}
</script>

<template>
  <section class="search card" aria-label="点歌与歌单">
    <div class="tabs" aria-label="点歌方式">
      <button :class="{ on: tab === 'search' }" type="button" @click="tab = 'search'">
        搜索点歌
      </button>
      <button :class="{ on: tab === 'playlist' }" type="button" @click="tab = 'playlist'">
        导入歌单
      </button>
      <button :class="{ on: tab === 'radio' }" type="button" @click="tab = 'radio'">
        东方电台
      </button>
    </div>

    <!-- 搜索点歌 -->
    <div v-show="tab === 'search'" class="pane">
      <div class="row">
        <input
          v-model="keyword"
          class="input night"
          placeholder="歌名 / 歌手 / 专辑"
          @keydown.enter="doSearch()"
        />
        <button
          class="go"
          type="button"
          aria-label="搜索音乐"
          :disabled="searching"
          @click="doSearch()"
        >
          <Icon name="search" :size="17" />
        </button>
      </div>

      <div v-if="searching" class="state">正在搜索…</div>
      <div v-else-if="searched && !results.length" class="state">没有找到，换个关键词试试。</div>

      <ul v-else-if="results.length" class="tracks">
        <li v-for="t in results" :key="t.id" class="track">
          <div class="t-main">
            <span class="t-name">{{ t.name }}</span>
            <span class="t-sub"
              >{{ t.artist }}<template v-if="t.album"> · 《{{ t.album }}》</template></span
            >
          </div>
          <span v-if="t.fee > 0" class="tag gold tiny">VIP</span>
          <button class="add" type="button" :disabled="pending.has(t.id)" @click="pick(t)">
            点歌
          </button>
        </li>
      </ul>

      <div v-if="total > pageSize" class="pager">
        <button type="button" :disabled="page <= 1" @click="turnPage(-1)">上一页</button>
        <span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span>
        <button type="button" :disabled="page >= Math.ceil(total / pageSize)" @click="turnPage(1)">
          下一页
        </button>
      </div>
    </div>

    <!-- 导入歌单 -->
    <div v-show="tab === 'playlist'" class="pane">
      <div class="mode-switch">
        <button
          :class="{ on: plMode === 'search' }"
          @click="changeMode('search')"
          :disabled="plLoading"
        >
          搜歌单
        </button>
        <button
          :class="{ on: plMode === 'user' }"
          @click="changeMode('user')"
          :disabled="plLoading"
        >
          搜用户昵称
        </button>
        <button :class="{ on: plMode === 'id' }" @click="changeMode('id')" :disabled="plLoading">
          按 ID / 链接
        </button>
      </div>
      <div class="row">
        <input
          v-model="plKeyword"
          class="input night"
          :placeholder="
            plMode === 'user'
              ? '网易云用户昵称'
              : plMode === 'search'
                ? '歌单关键词，例如：东方原曲'
                : '歌单 ID 或 分享链接'
          "
          @keydown.enter="searchPlaylists()"
        />
        <button
          class="go"
          type="button"
          aria-label="搜索歌单"
          :disabled="plLoading"
          @click="searchPlaylists()"
        >
          <Icon name="search" :size="17" />
        </button>
      </div>

      <p v-if="plLoading" class="state">正在打开歌单…</p>

      <template v-if="plMode === 'user' && !current && !plLoading">
        <template v-if="!selectedUser">
          <ul class="playlists">
            <li v-for="u in users" :key="u.id">
              <button class="pl" @click="openUser(u)">
                <img v-if="u.avatar" :src="u.avatar" alt="" /><span class="pl-main"
                  ><b>{{ u.nick }}</b
                  ><small class="pl-sub">用户 ID：{{ u.id }}</small></span
                >
              </button>
            </li>
          </ul>
          <div v-if="userTotal > 10" class="pager">
            <button :disabled="userPage <= 1" @click="userTurn(-1)">上一页</button
            ><span>{{ userPage }}</span
            ><button :disabled="userPage * 10 >= userTotal" @click="userTurn(1)">下一页</button>
          </div>
        </template>
        <p v-else class="tip">
          <button
            @click="
              selectedUser = null;
              plList = [];
            "
          >
            ← 返回用户
          </button>
          · {{ selectedUser.nick }} 的公开歌单
        </p>
      </template>
      <p
        v-if="
          !plLoading &&
          !current &&
          plSearched &&
          !plList.length &&
          (plMode !== 'user' || selectedUser || !users.length)
        "
        class="state"
      >
        没有找到公开结果，换个关键词试试。
      </p>
      <!-- 歌单列表 -->
      <ul v-if="!current && plList.length" class="playlists">
        <li v-for="p in plPaged" :key="p.id" class="pl" @click="openPlaylist(p)">
          <img v-if="p.cover" :src="p.cover" :alt="p.name" loading="lazy" />
          <div class="pl-main">
            <span class="pl-name">{{ p.name }}</span>
            <span class="pl-sub">{{ p.creator }} · {{ p.count }} 首</span>
          </div>
          <Icon name="arrow" :size="15" />
        </li>
      </ul>

      <!-- 歌单曲目 -->
      <template v-if="current">
        <div class="pl-head">
          <button class="back" type="button" @click="current = null">← 返回</button>
          <div class="pl-title">
            <b>{{ current.name }}</b>
            <small>{{ current.creator }} · 共 {{ current.count }} 首，显示 {{ currentTracks.length }} 首</small>
          </div>
          <button
            class="add-all"
            type="button"
            :disabled="batchPicking || loadingTracks"
            @click="pickAll"
          >
            前 20 首加入队列
          </button>
        </div>
        <p v-if="loadingTracks" class="state">正在取曲目…</p>
        <ul v-else class="tracks">
          <li v-for="t in trackPaged" :key="t.id" class="track">
            <div class="t-main">
              <span class="t-name">{{ t.name }}</span>
              <span class="t-sub">{{ t.artist }}</span>
            </div>
            <button
              class="add"
              type="button"
              :disabled="picked.has(t.id) || pending.has(t.id) || batchPicking"
              @click="pickOne(t)"
            >
              {{ picked.has(t.id) ? '已加入' : '点歌' }}
            </button>
          </li>
        </ul>
        <ul v-if="trackPages > 1" class="pager pl-pager">
          <li>
            <button type="button" :disabled="trackPage <= 1" @click="trackTurn(-1)">上一页</button>
            <span>{{ trackPage }} / {{ trackPages }}（共 {{ currentTracks.length }} 首）</span>
            <button type="button" :disabled="trackPage >= trackPages" @click="trackTurn(1)">下一页</button>            <input
              v-model="trackJump"
              class="pl-jump"
              type="number"
              min="1"
              :max="trackPages"
              placeholder="页码"
              @keydown.enter.prevent="trackGoto()"
            />
            <button type="button" @click="trackGoto()">跳转</button>

          </li>
        </ul>
      </template>

      <ul v-if="!current && plList.length > PL_PAGE" class="pager pl-pager">
        <li>
          <button type="button" :disabled="plPage <= 1" @click="plTurn(-1)">上一页</button>
          <span>{{ plPage }} / {{ plPages }}（共 {{ plList.length }} 个歌单）</span>
          <button type="button" :disabled="plPage >= plPages" @click="plTurn(1)">下一页</button>
          <input
            v-model="plJump"
            class="pl-jump"
            type="number"
            min="1"
            :max="plPages"
            placeholder="页码"
            @keydown.enter.prevent="plGoto()"
          />
          <button type="button" @click="plGoto()">跳转</button>
        </li>
      </ul>
      <ul v-if="!current && plMode !== 'user' && plList.length && plSearchPages > 1" class="pager pl-pager">
        <li>
          <button type="button" :disabled="plSearchPage <= 1" @click="plSearchTurn(-1)">上一页</button>
          <span>{{ plSearchPage }} / {{ plSearchPages }}（共约 {{ plSearchTotal }} 个歌单）</span>
          <button type="button" :disabled="plSearchPage >= plSearchPages" @click="plSearchTurn(1)">下一页</button>
        </li>
      </ul>
      <p v-if="!plLoading && !current && !plList.length && !plSearched" class="state">
        也可以切到「东方电台」，一键把整套东方歌单装进房间。
      </p>
    </div>

    <!-- 东方电台 -->
    <div v-show="tab === 'radio'" class="pane">
      <p class="radio-desc">
        选中一套预设，房间会把它装进默认歌单，队列不足时自动补歌——没人看着也一直放着。
      </p>
      <ul class="presets">
        <li v-for="p in presets" :key="p.id" class="preset">
          <div>
            <b>{{ p.label }}</b>
            <small>{{ p.playlists.length ? `${p.playlists.length} 份歌单` : '内置曲库' }}</small>
          </div>
          <button class="add" type="button" @click="loadRadio(p.id)">装载</button>
        </li>
      </ul>
      <p class="tip">提示：只有管理员 / 房主可以切换电台来源；普通听众直接点歌就好。</p>
    </div>
  </section>
</template>

<style scoped>
.search {
  color: var(--ink);
}
.tabs {
  display: flex;
  gap: 22px;
  border-bottom: 1px solid var(--line);
  margin-bottom: 20px;
}
.tabs button {
  padding: 0 0 12px;
  font-size: 13px;
  color: var(--muted);
  position: relative;
}
.tabs button.on {
  color: var(--green);
  font-weight: 600;
}
.tabs button.on:after {
  content: '';
  height: 3px;
  background: var(--green);
  position: absolute;
  bottom: -1px;
  left: 0;
  right: 0;
  border-radius: 3px;
}
.row {
  display: flex;
  gap: 8px;
}
.go {
  width: 44px;
  flex: none;
  display: grid;
  place-items: center;
  background: var(--green);
  color: #fff;
  border-radius: 10px;
}
.state {
  padding: 35px 6px;
  font-size: 12px;
  color: var(--muted);
}
.tracks {
  margin-top: 17px;
}
.track {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 14px 0;
  border-bottom: 1px solid var(--line);
}
.t-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
}
.t-name {
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.t-sub {
  font-size: 11px;
  color: var(--muted);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.add,
.add-all {
  padding: 5px 11px;
  background: var(--sage);
  color: var(--green);
  border-radius: 7px;
  font-size: 11px;
  flex: none;
}
.add:hover,
.add-all:hover {
  background: var(--sage-2);
}
.tiny {
  font-size: 9px;
  min-height: 18px;
  padding: 1px 5px;
}
.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: 18px;
  font-size: 11px;
  color: var(--muted);
}
.pager button {
  padding: 5px;
}
.mode-switch {
  display: flex;
  gap: 8px;
  margin-bottom: 13px;
}
.mode-switch button {
  font-size: 11px;
  color: var(--muted);
  padding: 4px 10px;
  border-radius: 6px;
}
.mode-switch .on {
  background: var(--paper-2);
  color: var(--green);
}
.playlists {
  margin-top: 20px;
}
.pl {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 0;
  border-bottom: 1px solid var(--line);
  cursor: pointer;
}
.pl img {
  width: 48px;
  height: 48px;
  border-radius: 8px;
  object-fit: cover;
}
.pl-main {
  flex: 1;
  min-width: 0;
}
.pl-name,
.pl-sub {
  display: block;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pl-name {
  font-size: 13px;
}
.pl-sub {
  font-size: 11px;
  color: var(--muted);
  margin-top: 4px;
}
.pl-head {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
  margin-top: 20px;
}
.back {
  font-size: 11px;
  color: var(--green);
}
.pl-title {
  min-width: 0;
  flex: 1;
}
.pl-title b {
  display: block;
  font-size: 13px;
}
.pl-title small {
  font-size: 10px;
  color: var(--muted);
}
.radio-desc {
  font-size: 12px;
  color: var(--ink-soft);
  line-height: 1.9;
}
.presets {
  margin-top: 20px;
}
.preset {
  display: flex;
  align-items: center;
  justify-content: space-between;
  padding: 14px;
  background: var(--paper-2);
  border-radius: 10px;
  margin-bottom: 10px;
}
.preset b {
  display: block;
  font-size: 13px;
}
.preset small {
  font-size: 10px;
  color: var(--muted);
}
.tip {
  font-size: 11px;
  color: var(--muted);
  margin-top: 15px;
}
@media (max-width: 500px) {
  .tabs {
    gap: 18px;
  }
  .tabs button {
    font-size: 12px;
  }
  .track {
    gap: 7px;
  }
  .t-name {
    font-size: 12px;
  }
  .pl-head {
    gap: 10px;
  }
  .add-all {
    margin-left: auto;
  }
}
</style>
