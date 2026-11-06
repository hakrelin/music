<script setup lang="ts">
import { computed, ref } from 'vue';
import Icon from '@/components/Icon.vue';
import { useRoomStore } from '@/stores/room';
import { useRoomsStore } from '@/stores/rooms';
import { useUiStore } from '@/stores/ui';
import type { Playlist, Track } from '@/types';

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

async function doSearch(reset = true) {
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

function pick(track: Track) {
  room.pick({ id: track.id, source: track.source, name: track.name });
  ui.ok(`已点歌：${track.name}`);
}

/* ——— 导入歌单 ——— */
const plKeyword = ref('');
const plMode = ref<'search' | 'id'>('search');
const plLoading = ref(false);
const plList = ref<Playlist[]>([]);
const plSearched = ref(false);
const current = ref<Playlist | null>(null);
const currentTracks = ref<Track[]>([]);
const loadingTracks = ref(false);
const picked = ref<Set<string>>(new Set());

async function searchPlaylists() {
  const q = plKeyword.value.trim();
  if (!q) return ui.warn('输入歌单关键词或歌单 ID');
  plLoading.value = true;
  current.value = null;
  currentTracks.value = [];
  try {
    if (plMode.value === 'id') {
      const one = await room.getPlaylist(q);
      current.value = one.playlist;
      currentTracks.value = one.tracks;
    } else {
      const res = await room.searchPlaylists(q, 1, 10);
      plList.value = res.playlists;
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
    currentTracks.value = one.tracks.slice(0, 60);
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '歌单曲目加载失败');
  } finally {
    loadingTracks.value = false;
  }
}

function pickAll() {
  const rest = currentTracks.value.filter((t) => !picked.value.has(t.id)).slice(0, 20);
  if (!rest.length) return ui.warn('没有可以加入的曲目');
  rest.forEach((t) => {
    room.pick({ id: t.id, source: t.source, name: t.name });
    picked.value.add(t.id);
  });
  ui.ok(`已加入 ${rest.length} 首（单次最多 20 首）`);
}

function pickOne(t: Track) {
  if (picked.value.has(t.id)) return;
  room.pick({ id: t.id, source: t.source, name: t.name });
  picked.value.add(t.id);
  ui.ok(`已点歌：${t.name}`);
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
    <div class="tabs" role="tablist">
      <button :class="{ on: tab === 'search' }" role="tab" @click="tab = 'search'">搜索点歌</button>
      <button :class="{ on: tab === 'playlist' }" role="tab" @click="tab = 'playlist'">导入歌单</button>
      <button :class="{ on: tab === 'radio' }" role="tab" @click="tab = 'radio'">东方电台</button>
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
        <button class="go" type="button" :disabled="searching" @click="doSearch()">
          <Icon name="search" :size="17" />
        </button>
      </div>

      <div v-if="searching" class="state">正在搜索…</div>
      <div v-else-if="searched && !results.length" class="state">没有找到，换个关键词试试。</div>

      <ul v-else-if="results.length" class="tracks">
        <li v-for="t in results" :key="t.id" class="track">
          <div class="t-main">
            <span class="t-name">{{ t.name }}</span>
            <span class="t-sub">{{ t.artist }}<template v-if="t.album"> · 《{{ t.album }}》</template></span>
          </div>
          <span v-if="t.fee > 0" class="tag gold tiny">VIP</span>
          <button class="add" type="button" @click="pick(t)">点歌</button>
        </li>
      </ul>

      <div v-if="total > pageSize" class="pager">
        <button type="button" :disabled="page <= 1" @click="turnPage(-1)">上一页</button>
        <span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span>
        <button type="button" :disabled="page >= Math.ceil(total / pageSize)" @click="turnPage(1)">下一页</button>
      </div>
    </div>

    <!-- 导入歌单 -->
    <div v-show="tab === 'playlist'" class="pane">
      <div class="mode-switch">
        <button :class="{ on: plMode === 'search' }" @click="plMode = 'search'">搜歌单</button>
        <button :class="{ on: plMode === 'id' }" @click="plMode = 'id'">按 ID / 链接</button>
      </div>
      <div class="row">
        <input
          v-model="plKeyword"
          class="input night"
          :placeholder="plMode === 'search' ? '歌单关键词，例如：东方原曲' : '歌单 ID 或 分享链接'"
          @keydown.enter="searchPlaylists()"
        />
        <button class="go" type="button" :disabled="plLoading" @click="searchPlaylists()">
          <Icon name="search" :size="17" />
        </button>
      </div>

      <p v-if="plLoading" class="state">正在打开歌单…</p>

      <!-- 歌单列表 -->
      <ul v-if="!current && plList.length" class="playlists">
        <li v-for="p in plList" :key="p.id" class="pl" @click="openPlaylist(p)">
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
            <small>{{ current.creator }} · 共 {{ current.count }} 首，这里显示前 60 首</small>
          </div>
          <button class="add-all" type="button" @click="pickAll">前 20 首加入队列</button>
        </div>
        <p v-if="loadingTracks" class="state">正在取曲目…</p>
        <ul v-else class="tracks">
          <li v-for="t in currentTracks" :key="t.id" class="track">
            <div class="t-main">
              <span class="t-name">{{ t.name }}</span>
              <span class="t-sub">{{ t.artist }}</span>
            </div>
            <button class="add" type="button" :disabled="picked.has(t.id)" @click="pickOne(t)">
              {{ picked.has(t.id) ? '已加入' : '点歌' }}
            </button>
          </li>
        </ul>
      </template>

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
.card {
  background: rgba(255, 255, 255, 0.035);
  border: 1px solid var(--line-night);
  border-radius: var(--r-l);
  backdrop-filter: blur(10px);
}
.search {
  padding: clamp(16px, 1.8vw, 22px);
}
.tabs {
  display: flex;
  gap: 4px;
  padding: 3px;
  background: rgba(0, 0, 0, 0.18);
  border-radius: 999px;
  margin-bottom: 14px;
}
.tabs button {
  flex: 1;
  height: 32px;
  border-radius: 999px;
  font-size: 11.5px;
  letter-spacing: 0.08em;
  color: rgba(232, 239, 220, 0.55);
  transition: background 0.2s ease, color 0.2s ease;
}
.tabs button.on {
  background: rgba(226, 237, 186, 0.14);
  color: var(--lime);
}
.pane {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.row {
  display: flex;
  gap: 8px;
}
.row .input {
  height: 40px;
  font-size: 12.5px;
}
.go {
  width: 40px;
  height: 40px;
  border-radius: var(--r-m);
  background: var(--lime);
  color: var(--deep);
  display: grid;
  place-items: center;
  flex: none;
}
.go:disabled {
  opacity: 0.5;
}
.state {
  font-size: 11.5px;
  color: rgba(232, 239, 220, 0.45);
  padding: 10px 2px;
  line-height: 1.8;
}
.tracks {
  display: flex;
  flex-direction: column;
  max-height: 292px;
  overflow: auto;
}
.track {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 0;
  border-top: 1px solid var(--line-night);
}
.track:first-child {
  border-top: 0;
}
.t-main {
  min-width: 0;
  flex: 1;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.t-name {
  font-size: 12.5px;
  color: #e7eeda;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.t-sub {
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.4);
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.tag.tiny {
  font-size: 9px;
  height: 18px;
  padding: 0 7px;
}
.add {
  flex: none;
  height: 27px;
  padding: 0 12px;
  border-radius: 999px;
  border: 1px solid var(--line-night-strong);
  color: rgba(232, 239, 220, 0.75);
  font-size: 11px;
  transition: all 0.2s ease;
}
.add:hover:not(:disabled) {
  border-color: var(--lime);
  color: var(--lime);
}
.add:disabled {
  opacity: 0.4;
  cursor: default;
}
.pager {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: rgba(232, 239, 220, 0.45);
}
.pager button {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.6);
  padding: 4px 8px;
}
.pager button:disabled {
  opacity: 0.3;
}
.mode-switch {
  display: flex;
  gap: 6px;
}
.mode-switch button {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.45);
  padding: 4px 10px;
  border-radius: 999px;
  border: 1px solid transparent;
}
.mode-switch button.on {
  color: var(--lime);
  border-color: var(--line-night-strong);
}
.playlists {
  display: flex;
  flex-direction: column;
  gap: 8px;
  max-height: 300px;
  overflow: auto;
}
.pl {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px;
  border-radius: var(--r-s);
  background: rgba(255, 255, 255, 0.03);
  cursor: pointer;
  transition: background 0.2s ease;
}
.pl:hover {
  background: rgba(226, 237, 186, 0.08);
}
.pl img {
  width: 38px;
  height: 38px;
  border-radius: var(--r-s);
  object-fit: cover;
}
.pl-main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.pl-name {
  font-size: 12px;
  color: #e7eeda;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.pl-sub {
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.4);
}
.pl-head {
  display: flex;
  align-items: center;
  gap: 10px;
  flex-wrap: wrap;
}
.back {
  font-size: 11px;
  color: rgba(232, 239, 220, 0.55);
}
.pl-title {
  flex: 1;
  min-width: 140px;
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.pl-title b {
  font-size: 12.5px;
  color: var(--lime);
}
.pl-title small {
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.4);
}
.add-all {
  height: 30px;
  padding: 0 12px;
  border-radius: 999px;
  background: rgba(226, 237, 186, 0.14);
  color: var(--lime);
  font-size: 11px;
}
.radio-desc {
  font-size: 11.5px;
  line-height: 1.9;
  color: rgba(232, 239, 220, 0.5);
}
.presets {
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.preset {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  padding: 11px 12px;
  border: 1px solid var(--line-night);
  border-radius: var(--r-m);
}
.preset b {
  display: block;
  font-size: 12.5px;
  color: #e7eeda;
  margin-bottom: 2px;
}
.preset small {
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.4);
}
.tip {
  font-size: 10.5px;
  color: rgba(232, 239, 220, 0.32);
  line-height: 1.8;
}
</style>
