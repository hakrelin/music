<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';
import Icon from '@/components/Icon.vue';
import RoomCard from '@/components/RoomCard.vue';
import SiteFooter from '@/components/SiteFooter.vue';
import SiteHeader from '@/components/SiteHeader.vue';
import { useRoomsStore } from '@/stores/rooms';
import { useUiStore } from '@/stores/ui';
import type { RoomSummary } from '@/types';

const router = useRouter();
const rooms = useRoomsStore();
const ui = useUiStore();

const creating = ref(false);
const form = reactive({ name: '', desc: '', needPwd: false, password: '' });

const steps = [
  {
    title: '点一首歌',
    text: '搜网易云的曲名、歌手或歌单，一键加入点歌队列；也可以直接贴歌单链接。'
  },
  {
    title: '和大家同步',
    text: '播放进度由服务器校准，所有人耳机里的同一句歌词，误差在一秒半以内。'
  },
  {
    title: '投票切歌',
    text: '遇上不喜欢的曲子，投一票就好；超过三成在听的人投票，就会换下一首。'
  },
  {
    title: '电台常驻',
    text: '东方电台 24 小时自动补队，房间没人也照样放着，随时推门就能听。'
  }
];

const presets = {
  normal: { name: '博丽茶席', desc: '一壶茶，想听什么点什么。' },
  radio: { name: '电台房', desc: '整晚放同一份歌单，不间断。' },
  touhou: { name: '东方电台', desc: '24 小时东方 Project，不间断。' }
} as const;

onMounted(() => {
  void rooms.loadRooms();
  if (!rooms.config) void rooms.loadConfig();
});

function preset(kind: keyof typeof presets) {
  form.name = presets[kind].name;
  form.desc = presets[kind].desc;
}

function enter(room: RoomSummary) {
  void router.push({ name: 'room', params: { id: room.id } });
}

async function createRoom() {
  if (!form.name.trim()) return ui.warn('先给房间起个名字');
  if (form.needPwd && !form.password.trim()) return ui.warn('密码还没填');
  creating.value = true;
  try {
    const id = await rooms.createRoom({
      name: form.name.trim(),
      desc: form.desc.trim(),
      needPwd: form.needPwd,
      password: form.needPwd ? form.password.trim() : undefined
    });
    await router.push({
      name: 'room',
      params: { id },
      query: form.needPwd ? { pwd: form.password.trim() } : {}
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
    <SiteHeader />

    <main>
      <!-- 首屏 -->
      <section class="hero-sec">
        <div class="hero">
          <img class="hero-art" src="/art/hero.jpg" alt="东方 Project 同人插画：雪林中的博丽灵梦" />
          <div class="hero-tint"></div>
          <div class="hero-shade"></div>

          <div class="hero-copy">
            <p class="eyebrow light">东方 Project · 同步点歌 · 24h 电台</p>
            <h1>在幻想乡，<br />一起听一首歌。</h1>
            <p class="hero-desc">
              茶与月色是一间开在博丽神社旁的线上茶室。<br />
              点一首歌，所有人的耳机里就同时响起同一段旋律。
            </p>
            <div class="hero-actions">
              <a class="btn btn-lime" href="#rooms">挑一间房间 <span aria-hidden="true">↗</span></a>
              <a class="hero-link" href="#how">怎么玩</a>
            </div>
          </div>

          <p class="hero-vertical">茶香渐起 · 夜色未央</p>

          <div class="hero-foot">
            <span>TEA &amp; MOONLIGHT / 博丽神社 · 雪</span>
            <a class="scroll-cue" href="#intro">往下看 <i aria-hidden="true">↓</i></a>
          </div>
        </div>
      </section>

      <!-- 引子 -->
      <section id="intro" class="intro">
        <p class="intro-text">
          一壶茶，一张唱片，<br />
          一群把同一首歌听到副歌的人。
        </p>
        <div class="intro-stats">
          <div class="stat"><strong>{{ rooms.totalOnline }}</strong><span>此刻在听</span></div>
          <div class="stat"><strong>{{ rooms.rooms.length }}</strong><span>开放房间</span></div>
          <div class="stat"><strong>1.5<em>s</em></strong><span>同步误差以内</span></div>
        </div>
      </section>

      <!-- 房间 -->
      <section id="rooms" class="section">
        <div class="section-head">
          <div>
            <p class="eyebrow">今夜开席</p>
            <h2>挑一间房间，坐下听</h2>
          </div>
          <button class="refresh" type="button" :disabled="rooms.loading" @click="rooms.loadRooms()">
            <Icon name="refresh" :size="15" :class="{ spin: rooms.loading }" />
            刷新
          </button>
        </div>

        <p v-if="rooms.error" class="error-note">{{ rooms.error }}</p>

        <div class="room-grid">
          <RoomCard v-for="r in rooms.rooms" :key="r.id" :room="r" @enter="enter" />
          <template v-if="rooms.loading && !rooms.rooms.length">
            <div v-for="i in 3" :key="i" class="skeleton-card skeleton"></div>
          </template>
        </div>

        <p v-if="!rooms.loading && !rooms.rooms.length" class="empty-note">
          还没有房间。开一间，第一首歌你来放。
        </p>

        <p class="section-note">房间空无一人 10 分钟后会自动收起来；「东方电台」是常驻的，什么时候来都在。</p>
      </section>

      <!-- 开房 -->
      <section id="create" class="create-band">
        <div class="create-inner">
          <div class="create-copy">
            <p class="eyebrow">开一间房</p>
            <h2>把茶烧上，<br />等人来听。</h2>
            <p class="create-desc">
              房间名和介绍会出现在大厅里。设了密码，就只有拿到钥匙的人能推门进来。
            </p>
            <ul class="preset-list">
              <li><b>普通点歌房</b>想听什么就点什么</li>
              <li><b>电台房</b>整晚放同一份歌单</li>
              <li><b>东方电台</b>内置 24 小时曲库（常驻）</li>
            </ul>
          </div>

          <form class="create-form" @submit.prevent="createRoom">
            <label class="field">
              <span>房间名</span>
              <input v-model="form.name" class="input" maxlength="33" placeholder="例如：博丽茶席" />
            </label>
            <label class="field">
              <span>介绍</span>
              <input
                v-model="form.desc"
                class="input"
                maxlength="133"
                placeholder="随便写一句，让路过的人知道该带什么心情进来"
              />
            </label>
            <div class="form-row">
              <label class="check">
                <input v-model="form.needPwd" type="checkbox" />
                设置房间密码
              </label>
              <input
                v-if="form.needPwd"
                v-model="form.password"
                class="input pwd"
                maxlength="32"
                placeholder="房间密码"
              />
            </div>
            <div class="preset-btns">
              <button type="button" class="chip" @click="preset('normal')">普通点歌房</button>
              <button type="button" class="chip" @click="preset('radio')">电台房</button>
              <button type="button" class="chip" @click="preset('touhou')">东方电台</button>
            </div>
            <button class="btn btn-primary submit" type="submit" :disabled="creating">
              {{ creating ? '正在备茶…' : '创建并进入' }}
            </button>
            <p class="form-tip">创建后直接进入；房间空置 10 分钟会自动回收。</p>
          </form>
        </div>
      </section>

      <!-- 玩法 -->
      <section id="how" class="section how">
        <div class="section-head">
          <div>
            <p class="eyebrow">怎么玩</p>
            <h2>四件事，就够了</h2>
          </div>
        </div>
        <ol class="steps">
          <li v-for="(s, i) in steps" :key="s.title">
            <span class="num">0{{ i + 1 }}</span>
            <h3>{{ s.title }}</h3>
            <p>{{ s.text }}</p>
          </li>
        </ol>
      </section>

      <!-- 引语 -->
      <section class="quote-band">
        <img src="/art/walk.jpg" alt="东方 Project 同人插画：暮色中并肩行走的两人" loading="lazy" />
        <div class="quote-veil"></div>
        <p class="quote">「茶凉了可以再添，<br />喜欢的歌要一起听完。」</p>
        <span class="quote-src">—— 茶与月色 · 入席须知</span>
      </section>
    </main>

    <SiteFooter />
  </div>
</template>

<style scoped>
.home {
  min-height: 100vh;
}

/* ——— 首屏 ——— */
.hero-sec {
  padding: 6px clamp(12px, 2.2vw, 34px) 0;
}
.hero {
  position: relative;
  height: clamp(560px, calc(100svh - 122px), 860px);
  border-radius: 8px;
  overflow: hidden;
  isolation: isolate;
  background: var(--deep);
}
.hero-art {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: 72% 42%;
  z-index: -3;
}
.hero-tint {
  position: absolute;
  inset: 0;
  z-index: -2;
  background: #1d4a3b;
  mix-blend-mode: multiply;
  opacity: 0.3;
}
.hero-shade {
  position: absolute;
  inset: 0;
  z-index: -1;
  background:
    linear-gradient(90deg, rgba(9, 31, 26, 0.97) 0%, rgba(9, 31, 26, 0.9) 30%, rgba(9, 31, 26, 0.42) 56%, rgba(9, 31, 26, 0.04) 78%),
    linear-gradient(0deg, rgba(9, 31, 26, 0.85) 0%, rgba(9, 31, 26, 0) 34%);
}
.hero-copy {
  position: relative;
  max-width: 640px;
  padding: clamp(56px, 9vh, 120px) 0 0 clamp(24px, 7%, 100px);
  color: #f2f6e9;
}
.eyebrow.light {
  color: rgba(232, 239, 220, 0.72);
}
.eyebrow.light::before {
  color: rgba(232, 239, 220, 0.35);
}
.hero h1 {
  margin: 26px 0 22px;
  font-size: clamp(38px, 4.8vw, 66px);
  line-height: 1.42;
  letter-spacing: 0.04em;
  color: #f6f8ef;
  font-weight: 600;
  text-shadow: 0 6px 40px rgba(4, 18, 15, 0.45);
}
.hero-desc {
  font-size: 13.5px;
  line-height: 2.1;
  color: rgba(240, 245, 230, 0.78);
  letter-spacing: 0.02em;
}
.hero-actions {
  margin-top: 34px;
  display: flex;
  align-items: center;
  gap: 26px;
}
.hero-link {
  font-size: 12.5px;
  letter-spacing: 0.14em;
  color: rgba(240, 245, 230, 0.82);
  border-bottom: 1px solid rgba(240, 245, 230, 0.32);
  padding-bottom: 4px;
  transition: color 0.2s ease, border-color 0.2s ease;
}
.hero-link:hover {
  color: var(--lime);
  border-color: var(--lime);
}
.hero-vertical {
  position: absolute;
  right: clamp(18px, 2.4vw, 30px);
  top: 108px;
  writing-mode: vertical-rl;
  font-family: var(--serif);
  font-size: 12px;
  letter-spacing: 0.5em;
  color: rgba(240, 245, 230, 0.55);
}
.hero-foot {
  position: absolute;
  left: clamp(24px, 7%, 100px);
  right: clamp(24px, 3vw, 40px);
  bottom: clamp(20px, 3.4vh, 34px);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 20px;
  font-size: 10px;
  letter-spacing: 0.22em;
  color: rgba(240, 245, 230, 0.56);
}
.scroll-cue {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  letter-spacing: 0.2em;
  transition: color 0.2s ease;
}
.scroll-cue i {
  font-style: normal;
  animation: bob 2.4s ease-in-out infinite;
}
.scroll-cue:hover {
  color: var(--lime);
}
@keyframes bob {
  0%,
  100% {
    transform: translateY(0);
  }
  50% {
    transform: translateY(4px);
  }
}

/* ——— 引子 ——— */
.intro {
  max-width: var(--container);
  margin: 0 auto;
  padding: clamp(30px, 4vw, 52px) 5%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: clamp(24px, 5vw, 80px);
  border-bottom: 1px solid var(--line);
}
.intro-text {
  font-family: var(--serif);
  font-size: clamp(16px, 1.5vw, 21px);
  line-height: 2;
  letter-spacing: 0.08em;
  color: var(--ink);
}
.intro-stats {
  display: flex;
  gap: clamp(28px, 4vw, 64px);
  flex: none;
}
.stat {
  display: flex;
  align-items: baseline;
  gap: 8px;
}
.stat strong {
  font-family: var(--serif);
  font-size: clamp(28px, 2.6vw, 38px);
  font-weight: 500;
  letter-spacing: 0.02em;
  color: var(--green);
  line-height: 1;
}
.stat strong em {
  font-style: normal;
  font-size: 0.45em;
  margin-left: 2px;
}
.stat span {
  font-size: 11px;
  letter-spacing: 0.14em;
  color: var(--muted);
  white-space: nowrap;
}

/* ——— 通用 section ——— */
.section {
  max-width: var(--container);
  margin: 0 auto;
  padding: clamp(46px, 7vw, 92px) 5% 0;
}
.section-head {
  display: flex;
  align-items: flex-end;
  justify-content: space-between;
  gap: 20px;
  margin-bottom: clamp(24px, 3vw, 40px);
}
.section-head h2 {
  margin-top: 16px;
  font-size: var(--fs-h2);
  letter-spacing: 0.08em;
}
.refresh {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  padding: 9px 18px;
  border: 1px solid var(--line-strong);
  border-radius: 999px;
  font-size: 12px;
  letter-spacing: 0.12em;
  color: var(--ink-soft);
  transition: border-color 0.2s ease, color 0.2s ease;
}
.refresh:hover {
  border-color: var(--green);
  color: var(--green);
}
.refresh:disabled {
  opacity: 0.5;
  cursor: default;
}
.spin {
  animation: spin 0.9s linear infinite;
}
@keyframes spin {
  to {
    transform: rotate(360deg);
  }
}
.room-grid {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(300px, 1fr));
  gap: clamp(16px, 1.8vw, 24px);
}
.skeleton-card {
  height: 380px;
  border-radius: var(--r-l);
}
.empty-note {
  padding: 40px 0;
  color: var(--muted);
  font-size: 13px;
  text-align: center;
}
.section-note {
  margin-top: 22px;
  font-size: 11.5px;
  color: var(--muted);
  letter-spacing: 0.04em;
}
.error-note {
  margin-bottom: 16px;
  padding: 12px 16px;
  border-radius: var(--r-m);
  background: var(--vermilion-soft);
  color: var(--vermilion);
  font-size: 12.5px;
}

/* ——— 开房 ——— */
.create-band {
  margin-top: clamp(56px, 8vw, 110px);
  background: var(--sage);
  padding: clamp(46px, 6vw, 84px) 5%;
}
.create-inner {
  max-width: var(--container);
  margin: 0 auto;
  display: grid;
  grid-template-columns: minmax(0, 0.9fr) minmax(0, 1.1fr);
  gap: clamp(30px, 6vw, 90px);
  align-items: start;
}
.create-copy h2 {
  margin: 18px 0 20px;
  font-size: var(--fs-h2);
  line-height: 1.5;
}
.create-desc {
  color: var(--ink-soft);
  font-size: 13.5px;
  line-height: 2.1;
  max-width: 400px;
}
.preset-list {
  margin-top: 30px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  font-size: 12.5px;
  color: var(--muted);
}
.preset-list li {
  display: flex;
  gap: 14px;
  align-items: baseline;
}
.preset-list b {
  color: var(--green);
  font-weight: 600;
  letter-spacing: 0.08em;
  min-width: 76px;
}
.create-form {
  background: var(--paper);
  border: 1px solid var(--line);
  border-radius: var(--r-l);
  padding: clamp(22px, 2.6vw, 34px);
  display: flex;
  flex-direction: column;
  gap: 18px;
  box-shadow: var(--shadow-card);
}
.form-row {
  display: flex;
  align-items: center;
  gap: 14px;
  flex-wrap: wrap;
}
.check {
  display: inline-flex;
  align-items: center;
  gap: 9px;
  font-size: 12.5px;
  color: var(--ink-soft);
  cursor: pointer;
}
.check input {
  width: 15px;
  height: 15px;
  accent-color: var(--green);
}
.pwd {
  flex: 1;
  min-width: 150px;
  max-width: 220px;
  height: 40px;
}
.preset-btns {
  display: flex;
  gap: 8px;
  flex-wrap: wrap;
}
.chip {
  padding: 7px 14px;
  border: 1px solid var(--line-strong);
  border-radius: 999px;
  font-size: 11.5px;
  color: var(--ink-soft);
  transition: all 0.2s ease;
}
.chip:hover {
  border-color: var(--green);
  color: var(--green);
  background: var(--sage);
}
.submit {
  width: 100%;
  margin-top: 2px;
}
.form-tip {
  font-size: 11px;
  color: var(--muted);
  text-align: center;
}

/* ——— 玩法 ——— */
.steps {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: clamp(16px, 2vw, 26px);
}
.steps li {
  border-top: 1px solid var(--line-strong);
  padding-top: 20px;
}
.steps .num {
  font-family: var(--serif);
  font-size: 12px;
  letter-spacing: 0.24em;
  color: var(--gold);
}
.steps h3 {
  margin: 12px 0 10px;
  font-size: 19px;
  letter-spacing: 0.1em;
}
.steps p {
  font-size: 12.5px;
  line-height: 2;
  color: var(--muted);
}

/* ——— 引语 ——— */
.quote-band {
  position: relative;
  margin: clamp(56px, 8vw, 110px) clamp(12px, 2.2vw, 34px) 0;
  height: clamp(280px, 34vw, 420px);
  border-radius: 8px;
  overflow: hidden;
  isolation: isolate;
}
.quote-band img {
  position: absolute;
  inset: 0;
  width: 100%;
  height: 100%;
  object-fit: cover;
  object-position: 60% 55%;
  z-index: -2;
}
.quote-veil {
  position: absolute;
  inset: 0;
  z-index: -1;
  background:
    linear-gradient(90deg, rgba(10, 34, 28, 0.82) 0%, rgba(10, 34, 28, 0.5) 45%, rgba(10, 34, 28, 0.2) 100%),
    linear-gradient(0deg, rgba(10, 34, 28, 0.55), rgba(10, 34, 28, 0.05));
}
.quote {
  position: absolute;
  left: clamp(24px, 6%, 90px);
  bottom: clamp(60px, 9%, 96px);
  font-family: var(--serif);
  font-size: clamp(22px, 2.6vw, 40px);
  line-height: 1.9;
  letter-spacing: 0.1em;
  color: #f4f7ec;
}
.quote-src {
  position: absolute;
  left: clamp(24px, 6%, 90px);
  bottom: clamp(28px, 4.5%, 46px);
  font-size: 10.5px;
  letter-spacing: 0.24em;
  color: rgba(240, 245, 230, 0.6);
}

@media (max-width: 1100px) {
  .steps {
    grid-template-columns: repeat(2, minmax(0, 1fr));
  }
  .create-inner {
    grid-template-columns: 1fr;
    gap: 34px;
  }
}
@media (max-width: 760px) {
  .hero {
    height: auto;
    min-height: calc(100svh - 96px);
  }
  .hero-art {
    object-position: 86% 22%;
  }
  .hero .eyebrow {
    font-size: 9px;
    letter-spacing: 0.16em;
  }
  .hero-shade {
    background:
      linear-gradient(180deg, rgba(9, 31, 26, 0.9) 0%, rgba(9, 31, 26, 0.72) 38%, rgba(9, 31, 26, 0.18) 62%, rgba(9, 31, 26, 0.72) 100%);
  }
  .hero-copy {
    padding: 42px 24px 150px;
    max-width: none;
  }
  .hero h1 {
    font-size: clamp(32px, 9vw, 42px);
    line-height: 1.55;
    margin: 20px 0 16px;
  }
  .hero-desc {
    font-size: 12px;
    line-height: 2;
  }
  .hero-actions {
    margin-top: 26px;
    gap: 18px;
  }
  .hero-vertical {
    display: none;
  }
  .hero-foot {
    left: 24px;
    right: 20px;
    bottom: 20px;
    font-size: 8.5px;
    letter-spacing: 0.14em;
  }
  .intro {
    flex-direction: column;
    align-items: flex-start;
    gap: 22px;
    padding: 26px 22px;
  }
  .intro-text {
    font-size: 15px;
  }
  .intro-stats {
    width: 100%;
    justify-content: space-between;
    gap: 12px;
  }
  .stat {
    flex-direction: column;
    gap: 4px;
  }
  .stat strong {
    font-size: 26px;
  }
  .section {
    padding: 40px 22px 0;
  }
  .section-head h2 {
    font-size: 24px;
  }
  .steps {
    grid-template-columns: 1fr;
    gap: 18px;
  }
  .create-band {
    padding: 40px 22px;
  }
  .quote-band {
    margin: 40px 10px 0;
    border-radius: 5px;
  }
  .quote-band img {
    object-position: 66% 50%;
  }
}
</style>
