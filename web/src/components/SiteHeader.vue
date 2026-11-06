<script setup lang="ts">
import { onMounted, ref } from 'vue';
import BrandMark from '@/components/BrandMark.vue';
import { useRoomsStore } from '@/stores/rooms';

const rooms = useRoomsStore();
const open = ref(false);
const links = [
  { href: '#rooms', label: '房间' },
  { href: '#create', label: '开一间房' },
  { href: '#how', label: '怎么玩' },
  { href: '#about', label: '关于' }
];

onMounted(() => {
  void rooms.loadConfig();
});
</script>

<template>
  <header class="site-header">
    <a class="brand" href="/" aria-label="茶与月色首页">
      <BrandMark :size="42" />
      <span class="brand-text">
        茶与月色
        <small>TEA &amp; MOONLIGHT</small>
      </span>
    </a>

    <nav class="nav" :class="{ open }" aria-label="主导航">
      <a v-for="l in links" :key="l.href" :href="l.href" @click="open = false">{{ l.label }}</a>
    </nav>

    <div class="header-cta">
      <a class="btn btn-ghost btn-sm" href="#create">开一间房</a>
    </div>

    <button class="menu-toggle" :aria-expanded="open" aria-label="展开导航" @click="open = !open">
      <span></span><span></span>
    </button>
  </header>
</template>

<style scoped>
.site-header {
  height: var(--header-h);
  max-width: var(--container);
  margin: 0 auto;
  padding: 0 5%;
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 28px;
  position: relative;
  z-index: 20;
}
.brand {
  display: flex;
  align-items: center;
  gap: 12px;
  color: var(--green);
  white-space: nowrap;
}
.brand-text {
  font-family: var(--serif);
  font-size: 23px;
  font-weight: 700;
  letter-spacing: 0.14em;
  line-height: 1.2;
}
.brand-text small {
  display: block;
  margin-top: 5px;
  font-family: var(--sans);
  font-size: 8.5px;
  font-weight: 400;
  letter-spacing: 0.32em;
  color: var(--muted);
}
.nav {
  display: flex;
  align-items: center;
  gap: 34px;
}
.nav a {
  position: relative;
  padding: 14px 0;
  font-size: 13px;
  letter-spacing: 0.12em;
  color: var(--muted);
  transition: color 0.2s ease;
}
.nav a:hover {
  color: var(--green);
}
.nav a::after {
  content: "";
  position: absolute;
  left: 50%;
  bottom: 5px;
  width: 4px;
  height: 4px;
  border-radius: 50%;
  background: var(--green);
  opacity: 0;
  transform: translateX(-50%) scale(0.4);
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.nav a:hover::after {
  opacity: 1;
  transform: translateX(-50%) scale(1);
}
.menu-toggle {
  display: none;
}
@media (max-width: 960px) {
  .nav {
    gap: 22px;
  }
  .header-cta {
    display: none;
  }
}
@media (max-width: 760px) {
  .site-header {
    height: 76px;
    padding: 0 22px;
  }
  .brand-text {
    font-size: 20px;
  }
  .brand-text small {
    font-size: 8px;
  }
  .menu-toggle {
    display: flex;
    flex-direction: column;
    justify-content: center;
    gap: 6px;
    width: 40px;
    height: 40px;
    padding: 0 11px;
    border: 1px solid var(--line-strong);
    border-radius: 50%;
  }
  .menu-toggle span {
    display: block;
    height: 1px;
    background: var(--ink);
  }
  .nav {
    display: none;
    position: absolute;
    top: 72px;
    left: 16px;
    right: 16px;
    background: var(--paper);
    border: 1px solid var(--line);
    border-radius: var(--r-m);
    box-shadow: var(--shadow-card);
    padding: 10px 24px;
    flex-direction: column;
    align-items: stretch;
    gap: 0;
  }
  .nav.open {
    display: flex;
  }
  .nav a {
    padding: 13px 0;
  }
  .nav a::after {
    display: none;
  }
}
</style>
