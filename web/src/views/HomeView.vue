<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import SiteHeader from '@/components/SiteHeader.vue';
import SiteFooter from '@/components/SiteFooter.vue';
import RoomCard from '@/components/RoomCard.vue';
import AppDialog from '@/components/AppDialog.vue';
import Icon from '@/components/Icon.vue';
import { useRoomsStore } from '@/stores/rooms';
import { useUiStore } from '@/stores/ui';
import type { RoomSummary } from '@/types';
const rooms = useRoomsStore();
const ui = useUiStore();
const router = useRouter();
const creating = ref(false);
const createOpen = ref(false);
const aboutOpen = ref(false);
const filter = ref('all');
const query = ref('');
const form = reactive({
  name: '',
  desc: '',
  needPwd: false,
  password: '',
  ownerPassword: '',
});
const featured = computed(
  () => rooms.rooms.find((r) => r.builtin && r.radio) || rooms.radioRooms[0],
);
const filtered = computed(() =>
  rooms.rooms.filter(
    (r) =>
      (filter.value !== 'radio' || r.radio) &&
      (filter.value !== 'normal' || !r.radio) &&
      (!query.value ||
        [r.name, r.desc, r.now?.name].some((x) =>
          x?.toLowerCase().includes(query.value.toLowerCase()),
        )),
  ),
);
let timer = 0;
onMounted(() => {
  void rooms.loadRooms();
  void rooms.loadConfig();
  timer = window.setInterval(() => {
    if (!document.hidden) void rooms.loadRooms();
  }, 15000);
});
onBeforeUnmount(() => clearInterval(timer));
function enter(r: RoomSummary) {
  void router.push({ name: 'room', params: { id: r.id } });
}
async function createRoom() {
  if (!form.name.trim()) return ui.warn('请输入房间名');
  if (form.needPwd && !form.password.trim()) return ui.warn('请输入房间密码');
  creating.value = true;
  try {
    const id = await rooms.createRoom({
      name: form.name.trim(),
      desc: form.desc.trim(),
      ownerPassword: form.ownerPassword,
      needPwd: form.needPwd,
      password: form.needPwd ? form.password.trim() : undefined,
    });
    createOpen.value = false;
    await router.push({
      name: 'room',
      params: { id },
      state: form.needPwd ? { roomPassword: form.password.trim() } : undefined,
    });
  } catch (e) {
    ui.error(e instanceof Error ? e.message : '创建失败');
  } finally {
    creating.value = false;
  }
}
</script>
<template>
  <div class="home">
    <SiteHeader @create="createOpen = true" />
    <main class="home-main">
      <section class="welcome-row" aria-label="欢迎与常驻电台">
        <div class="welcome">
          <div class="welcome-copy">
            <span class="welcome-note"><i />茶与金平糖汉化组 · 一起听歌</span>
            <h1>今天也<br />一起听<span>。</span></h1>
            <p>放一首喜欢的，和大家待一会儿。</p>
            <a href="#rooms" class="welcome-link"
              >看看大家在听什么 <Icon name="arrow" :size="15"
            /></a>
          </div>
          <img
            class="welcome-art"
            src="/art/reimari.webp"
            alt="灵梦与魔理沙靠在一起微笑，minusT 绘"
            fetchpriority="high"
          /><span class="art-credit">ILLUST. minusT</span
          ><span class="spark spark-one" aria-hidden="true">✦</span
          ><span class="spark spark-two" aria-hidden="true">✧</span>
        </div>
        <aside class="featured">
          <div class="feature-head">
            <span><Icon name="radio" :size="15" />常驻电台</span><span class="live"><i />LIVE</span>
          </div>
          <template v-if="featured"
            ><h2>{{ featured.name }}</h2>
            <p class="feature-desc">随时进来，总有歌在放。</p>
            <div class="feature-track">
              <div class="feature-cover">
                <img
                  v-if="featured.now?.cover"
                  :src="featured.now.cover"
                  alt="当前歌曲封面"
                  @error="($event.target as HTMLImageElement).style.display = 'none'"
                /><Icon v-else name="music" :size="24" />
              </div>
              <div>
                <span>正在播放</span><b>{{ featured.now?.name || '等待下一首歌曲' }}</b
                ><small>{{ featured.now?.artist || '点一首歌，加入队列' }}</small>
              </div>
            </div>
            <div class="feature-bottom">
              <span><Icon name="users" :size="14" />{{ featured.online }} 人在听</span
              ><button class="btn" @click="enter(featured)">
                <Icon name="play" :size="14" />加入收听
              </button>
            </div></template
          ><template v-else
            ><h2>一起听东方</h2>
            <p class="feature-desc">
              {{ rooms.loading ? '正在获取电台…' : '电台暂未开放，先看看其他房间吧。' }}
            </p>
            <Icon class="feature-empty" name="radio" :size="56"
          /></template>
        </aside>
      </section>
      <section id="rooms" class="rooms-section">
        <div class="rooms-heading">
          <div>
            <h2>
              听歌大厅 <span>{{ rooms.rooms.length }}</span>
            </h2>
            <p>
              <i class="online-dot" />{{ rooms.totalOnline }}
              人正在分享同一段旋律
            </p>
          </div>
          <button
            class="refresh"
            :disabled="rooms.loading"
            aria-label="刷新房间列表"
            @click="rooms.loadRooms()"
          >
            <Icon name="refresh" :size="15" />刷新
          </button>
        </div>
        <div class="room-tools">
          <div class="filters" aria-label="房间分类">
            <button
              v-for="f in [
                { id: 'all', label: '全部房间' },
                { id: 'radio', label: '电台' },
                { id: 'normal', label: '点歌房' },
              ]"
              :key="f.id"
              :class="{ active: filter === f.id }"
              :aria-pressed="filter === f.id"
              @click="filter = f.id"
            >
              {{ f.label }}
            </button>
          </div>
          <label class="room-search"
            ><Icon name="search" :size="15" /><input
              v-model="query"
              placeholder="找个房间或正在放的歌"
              aria-label="搜索房间"
          /></label>
        </div>
        <div v-if="rooms.error" class="error-note" role="alert">
          {{ rooms.error }} <button @click="rooms.loadRooms()">重试</button>
        </div>
        <div class="room-grid">
          <RoomCard v-for="r in filtered" :key="r.id" :room="r" @enter="enter" /><template
            v-if="rooms.loading && !rooms.rooms.length"
            ><div v-for="i in 3" :key="i" class="skeleton room-skeleton" /></template
          ><button v-if="!query && filter === 'all'" class="create-card" @click="createOpen = true">
            <span class="create-plus"><Icon name="plus" :size="23" /></span><b>开个自己的房间</b
            ><span>邀请朋友，第一首歌你来选</span>
          </button>
        </div>
        <p v-if="!rooms.loading && !filtered.length" class="no-rooms">
          {{ query ? '没有找到这个房间，换个关键词试试。' : '还没有房间，来放第一首歌吧。' }}
        </p>
      </section>
      <SiteFooter
        ><button class="about-link" @click="aboutOpen = true">关于本站</button></SiteFooter
      >
    </main>
    <AppDialog :open="createOpen" title="创建房间" @close="!creating && (createOpen = false)"
      ><form class="create-form" @submit.prevent="createRoom">
        <label class="field"
          ><span>房间名</span
          ><input
            v-model="form.name"
            class="input"
            maxlength="33"
            placeholder="例如：修图的时候听什么"
            required /></label
        ><label class="field"
          ><span>介绍 <small>选填</small></span
          ><input
            v-model="form.desc"
            class="input"
            maxlength="133"
            placeholder="告诉大家，你想放些什么" /></label
        ><label class="password-toggle"
          ><input v-model="form.needPwd" type="checkbox" />设置房间密码</label
        ><label v-if="form.needPwd" class="field"
          ><span>密码</span
          ><input
            v-model="form.password"
            class="input"
            type="password"
            maxlength="32"
            autocomplete="new-password"
            required
        /></label>
        <label class="field"
          ><span>房间管理密码 <small>选填，留空自动生成</small></span>
          <input
            v-model="form.ownerPassword"
            class="input"
            type="password"
            maxlength="64"
            autocomplete="new-password"
          />
        </label>
        <p>创建者自动获得房主权限，可在设置中修改管理密码。</p>
        <p>创建后直接进入。普通房间空置 10 分钟后自动关闭。</p>
        <button class="btn btn-primary" :disabled="creating">
          {{ creating ? '正在创建…' : '创建并进入' }}<Icon name="arrow" :size="16" />
        </button></form></AppDialog
    ><AppDialog :open="aboutOpen" title="关于这个听歌房" @close="aboutOpen = false"
      ><div class="about-copy">
        <h3>茶与金平糖 · 一起听</h3>
        <p>茶与金平糖汉化组的同步听歌网站。选个房间，点歌、聊天，和大家听同一首歌。</p>
        <p>
          搜索音乐或导入网易云歌单即可点歌；不喜欢当前歌曲，可以投票切歌。常驻电台会自动补充队列。
        </p>
        <p>
          主视觉：<a href="https://www.pixiv.net/artworks/65515001" target="_blank" rel="noreferrer"
            >minusT《天空璋レイマリ》↗</a
          >。插画版权归原作者所有。
        </p>
        <p>东方 Project 非官方同人站点。歌曲、封面及歌词归各自权利人所有。</p>
        <p class="domain">music.tea-kompeito.com</p>
      </div></AppDialog
    >
  </div>
</template>
<style scoped>
.home-main {
  max-width: 1400px;
  padding: 16px 40px 0;
  margin: auto;
}
.welcome-row {
  display: grid;
  grid-template-columns: minmax(0, 1fr) 320px;
  gap: 22px;
}
.welcome {
  position: relative;
  overflow: hidden;
  min-height: 300px;
  border: 1px solid #ebece2;
  background: #f1f3e8;
  border-radius: 24px;
  isolation: isolate;
}
.welcome-copy {
  position: relative;
  z-index: 2;
  padding: 33px 35px;
  pointer-events: none;
}
.welcome-note {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 10px;
  letter-spacing: 0.5px;
  color: #627153;
}
.welcome-note i {
  height: 5px;
  width: 5px;
  border-radius: 50%;
  background: #a6b68a;
}
h1 {
  font-size: 43px;
  line-height: 1.27;
  letter-spacing: -2px;
  margin: 19px 0 13px;
  color: #314e3c;
}
h1 span {
  color: #cc8d75;
}
.welcome-copy p {
  font-size: 12px;
  color: #647052;
}
.welcome-link {
  display: inline-flex;
  align-items: center;
  gap: 12px;
  font-size: 11px;
  color: #4e775c;
  margin-top: 20px;
  pointer-events: auto;
}
.welcome-art {
  position: absolute;
  right: 0;
  bottom: 0;
  width: 65%;
  height: 100%;
  object-fit: cover;
  object-position: 55% 38%;
  z-index: 0;
  mask-image: linear-gradient(90deg, transparent, #000 21%);
}
.art-credit {
  position: absolute;
  right: 14px;
  bottom: 10px;
  z-index: 2;
  font-size: 7px;
  letter-spacing: 1px;
  color: #6a665db0;
}
.spark {
  position: absolute;
  color: #dbb659;
  font-size: 24px;
  z-index: 1;
}
.spark-one {
  left: 38%;
  top: 28px;
}
.spark-two {
  left: 35%;
  bottom: 36px;
  font-size: 15px;
}
.featured {
  padding: 25px;
  background: #2e6049;
  color: #f7faef;
  border-radius: 24px;
  display: flex;
  flex-direction: column;
  min-width: 0;
  position: relative;
  overflow: hidden;
}
.featured:after {
  content: '';
  position: absolute;
  width: 190px;
  height: 190px;
  right: -110px;
  top: 50px;
  border: 1px solid #ffffff0e;
  border-radius: 50%;
  box-shadow: 0 0 0 35px #ffffff04;
  pointer-events: none;
}
.feature-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  font-size: 11px;
  color: #d0e0c8;
}
.feature-head > span {
  display: flex;
  align-items: center;
  gap: 7px;
}
.live {
  font-size: 8px;
  letter-spacing: 1px;
}
.live i {
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: #c5dd9a;
}
.featured h2 {
  font-size: 25px;
  margin-top: 23px;
  letter-spacing: -0.6px;
  color: #fff;
}
.feature-desc {
  font-size: 11px;
  color: #c5d6be;
  margin-top: 4px;
}
.feature-track {
  display: flex;
  gap: 12px;
  align-items: center;
  margin: 23px 0;
}
.feature-cover {
  height: 52px;
  width: 52px;
  display: grid;
  place-items: center;
  border-radius: 9px;
  background: #ffffff14;
  color: #d0ddc2;
  overflow: hidden;
  flex: none;
}
.feature-cover img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.feature-track > div:last-child {
  display: flex;
  flex-direction: column;
  min-width: 0;
  gap: 2px;
}
.feature-track span {
  font-size: 9px;
  color: #b9ccae;
}
.feature-track b {
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.feature-track small {
  font-size: 9px;
  color: #bfd0b7;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.feature-bottom {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin-top: auto;
  gap: 8px;
}
.feature-bottom > span {
  display: flex;
  align-items: center;
  gap: 6px;
  font-size: 10px;
  color: #c5d6be;
}
.feature-bottom .btn {
  background: #e6edcb;
  color: #30503d;
  font-size: 11px;
  min-height: 35px;
  padding: 0 13px;
  border-radius: 9px;
}
.feature-empty {
  margin: 30px auto;
  color: #8eac82;
}
.rooms-section {
  padding-top: 36px;
  scroll-margin-top: 20px;
}
.rooms-heading {
  display: flex;
  align-items: center;
  justify-content: space-between;
}
.rooms-heading h2 {
  font-size: 21px;
  display: flex;
  align-items: center;
  gap: 9px;
}
.rooms-heading h2 span {
  font-size: 10px;
  background: var(--paper-3);
  color: var(--muted);
  padding: 2px 7px;
  border-radius: 5px;
  font-weight: 500;
}
.rooms-heading p {
  display: flex;
  align-items: center;
  gap: 7px;
  margin-top: 6px;
  font-size: 11px;
  color: var(--muted);
}
.rooms-heading .online-dot {
  width: 5px;
  height: 5px;
  box-shadow: none;
}
.refresh {
  display: flex;
  align-items: center;
  gap: 7px;
  color: var(--muted);
  font-size: 11px;
  padding: 8px;
}
.room-tools {
  margin: 23px 0 18px;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 16px;
}
.filters {
  display: flex;
  gap: 6px;
}
.filters button {
  padding: 7px 14px;
  border-radius: 8px;
  font-size: 12px;
  color: var(--muted);
}
.filters button.active {
  background: #e9f0e1;
  color: var(--green);
  font-weight: 600;
}
.room-search {
  display: flex;
  align-items: center;
  gap: 8px;
  color: #68775f;
  border: 1px solid var(--line);
  background: #fff;
  border-radius: 9px;
  padding: 8px 11px;
}
.room-search input {
  border: 0;
  outline: none;
  width: 165px;
  background: none;
  font-size: 11px;
}
.room-grid {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  gap: 18px;
}
.create-card {
  border: 1px dashed #d4ddcc;
  border-radius: 18px;
  min-height: 195px;
  display: flex;
  align-items: center;
  justify-content: center;
  flex-direction: column;
  gap: 8px;
  background: #f3f6ee;
  color: var(--green);
  transition: background 0.2s;
}
.create-card:hover {
  background: #eaf1e3;
}
.create-plus {
  height: 42px;
  width: 42px;
  display: grid;
  place-items: center;
  background: #e6eddd;
  border-radius: 50%;
  margin-bottom: 2px;
}
.create-card b {
  font-size: 12px;
  font-weight: 500;
}
.create-card > span:last-child {
  font-size: 10px;
  color: var(--muted);
}
.room-skeleton {
  height: 195px;
}
.no-rooms {
  padding: 20px 0;
  color: var(--muted);
  font-size: 13px;
}
.create-form {
  display: grid;
  gap: 19px;
}
.create-form small {
  font-weight: 400;
  color: var(--muted);
  margin-left: 5px;
}
.password-toggle {
  display: flex;
  align-items: center;
  gap: 8px;
  font-size: 12px;
  accent-color: var(--green);
}
.create-form p {
  font-size: 11px;
  color: var(--muted);
}
.create-form > .btn {
  justify-self: end;
}
.error-note {
  padding: 12px;
  background: #fcf0e8;
  margin-bottom: 15px;
  border-radius: 10px;
  font-size: 12px;
  color: #a3654b;
}
.error-note button {
  font-weight: 600;
  margin-left: 12px;
}
.about-link {
  font-size: 11px;
}
.about-copy {
  display: grid;
  gap: 16px;
  font-size: 13px;
  color: var(--ink-soft);
}
.about-copy h3 {
  color: var(--ink);
}
.about-copy a {
  color: var(--green);
  text-decoration: underline;
  text-underline-offset: 3px;
}
.domain {
  font-size: 11px;
  letter-spacing: 0.5px;
  color: var(--muted);
}
@media (min-width: 1500px) {
  .home-main {
    padding-top: 26px;
  }
  .welcome {
    min-height: 322px;
  }
  .welcome-copy {
    padding-top: 40px;
  }
}
@media (max-width: 1050px) {
  .home-main {
    padding: 12px 24px 0;
  }
  .welcome-row {
    grid-template-columns: minmax(0, 1fr) 285px;
    gap: 16px;
  }
  .welcome-copy {
    padding: 30px 25px;
  }
  .welcome-art {
    width: 62%;
  }
  .welcome-note {
    font-size: 9px;
  }
  .room-grid {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
}
@media (max-width: 760px) {
  .welcome-row {
    grid-template-columns: 1fr;
  }
  .welcome {
    min-height: 270px;
  }
  .welcome-art {
    width: 62%;
  }
  .featured {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 0 18px;
    padding: 19px 23px;
    border-radius: 18px;
  }
  .feature-head {
    grid-column: 1/-1;
  }
  .featured h2 {
    font-size: 20px;
    margin-top: 12px;
  }
  .feature-desc {
    grid-column: 1;
  }
  .feature-track {
    grid-column: 1;
    margin: 14px 0 0;
  }
  .feature-bottom {
    grid-column: 2;
    grid-row: 2/5;
    flex-direction: column;
    justify-content: flex-end;
    align-items: flex-end;
    gap: 10px;
  }
  .feature-empty {
    margin: 10px;
  }
  .room-tools {
    gap: 8px;
  }
  .room-search input {
    width: 130px;
  }
}
@media (max-width: 500px) {
  .home-main {
    padding: 8px 18px 0;
  }
  .welcome {
    min-height: 245px;
    border-radius: 20px;
  }
  .welcome-copy {
    padding: 25px 21px;
  }
  .welcome-note {
    font-size: 8px;
    max-width: 170px;
  }
  h1 {
    font-size: 34px;
    margin-top: 18px;
  }
  .welcome-copy p {
    max-width: 132px;
    font-size: 10px;
  }
  .welcome-link {
    font-size: 9px;
    gap: 5px;
    margin-top: 17px;
  }
  .welcome-art {
    width: 66%;
    object-position: 50% 50%;
    mask-image: linear-gradient(90deg, transparent, #000 24%);
  }
  .spark-one {
    left: 41%;
    font-size: 17px;
  }
  .spark-two {
    display: none;
  }
  .art-credit {
    font-size: 6px;
  }
  .rooms-section {
    padding-top: 27px;
  }
  .rooms-heading h2 {
    font-size: 19px;
  }
  .room-tools {
    flex-wrap: wrap;
    margin-top: 17px;
  }
  .filters button {
    font-size: 11px;
    padding: 6px 11px;
  }
  .room-search {
    width: 100%;
    order: 2;
  }
  .room-search input {
    width: 100%;
  }
  .room-grid {
    grid-template-columns: 1fr;
    gap: 12px;
  }
  .create-card {
    min-height: 110px;
    position: relative;
    align-items: flex-start;
    padding-left: 80px;
    gap: 3px;
  }
  .create-plus {
    position: absolute;
    left: 24px;
  }
  .feature-track {
    gap: 9px;
  }
  .feature-cover {
    width: 44px;
    height: 44px;
  }
  .featured {
    padding: 17px 19px;
    gap: 0 10px;
  }
  .feature-bottom .btn {
    padding: 0 10px;
  }
  .feature-track b {
    font-size: 11px;
  }
  .rooms-heading p {
    font-size: 10px;
  }
}

@media (max-width: 500px) {
  .welcome-copy p {
    max-width: 118px;
  }
  .welcome-art {
    width: 70%;
    height: 90%;
    right: -7%;
    object-position: 50% 50%;
  }
}
</style>
