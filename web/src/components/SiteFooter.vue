<script setup lang="ts">
import BrandMark from '@/components/BrandMark.vue';
const year = new Date().getFullYear();
</script>

<template>
  <footer id="about" class="site-footer">
    <div class="foot-inner">
      <div class="foot-main">
        <a class="foot-brand" href="/">
          <BrandMark :size="34" />
          <span>茶与月色 <i>✳</i></span>
        </a>
        <p class="foot-slogan">一杯茶，一首歌，<br />还有想一起听的人。</p>
      </div>

      <div class="foot-cols">
        <div class="foot-col">
          <span class="foot-title">听歌</span>
          <a href="/#rooms">房间</a>
          <a href="/#create">开一间房</a>
          <a href="/room/radio">东方电台</a>
        </div>
        <div class="foot-col">
          <span class="foot-title">关于</span>
          <a href="/#how">怎么玩</a>
          <a href="/#about">关于本站</a>
        </div>
        <div class="foot-col foot-note">
          <span class="foot-title">说明</span>
          <p>东方 Project 二次创作 · 非官方粉丝站<br />歌曲与封面来自网易云音乐，版权归各自权利人所有。</p>
        </div>
      </div>
    </div>

    <div class="foot-bottom">
      <small>© {{ year }} 茶与月色 · TEA &amp; MOONLIGHT</small>
      <small>首屏插画：うさほり / みるるん / みふる（Pixiv）· 版权归原作者</small>
    </div>
  </footer>
</template>

<style scoped>
.site-footer {
  margin-top: clamp(50px, 7vw, 100px);
  padding: 0 5% 34px;
  background: var(--paper);
}
.foot-inner {
  max-width: var(--container);
  margin: 0 auto;
  padding: clamp(38px, 5vw, 62px) 0 clamp(28px, 3vw, 40px);
  border-top: 1px solid var(--line);
  display: flex;
  justify-content: space-between;
  gap: 40px;
  flex-wrap: wrap;
}
.foot-brand {
  display: inline-flex;
  align-items: center;
  gap: 10px;
  color: var(--green);
  font-family: var(--serif);
  font-size: 22px;
  font-weight: 700;
  letter-spacing: 0.14em;
}
.foot-brand i {
  font-style: normal;
  color: var(--gold);
}
.foot-slogan {
  margin-top: 16px;
  color: var(--muted);
  font-size: 13px;
  line-height: 2;
}
.foot-cols {
  display: flex;
  gap: clamp(30px, 4vw, 70px);
  flex-wrap: wrap;
}
.foot-col {
  display: flex;
  flex-direction: column;
  gap: 9px;
  font-size: 13px;
  color: var(--ink-soft);
}
.foot-title {
  font-size: 11px;
  letter-spacing: 0.26em;
  color: var(--muted);
  margin-bottom: 6px;
}
.foot-col a {
  color: var(--ink-soft);
  transition: color 0.2s ease;
  width: fit-content;
}
.foot-col a:hover {
  color: var(--green);
}
.foot-note {
  max-width: 280px;
}
.foot-note p {
  color: var(--muted);
  font-size: 12px;
  line-height: 1.95;
}
.foot-bottom {
  max-width: var(--container);
  margin: 0 auto;
  padding-top: 22px;
  border-top: 1px solid var(--line);
  display: flex;
  justify-content: space-between;
  gap: 16px;
  flex-wrap: wrap;
  color: var(--muted);
  font-size: 11px;
  letter-spacing: 0.04em;
}
@media (max-width: 760px) {
  .foot-inner {
    flex-direction: column;
  }
  .foot-cols {
    flex-direction: column;
    gap: 24px;
  }
}
</style>
