<template>
  <footer>
    <span>茶与金平糖汉化组 <span class="sep">·</span> 一起听歌的小地方</span
    ><span
      ><a href="https://tea-kompeito.com" target="_blank" rel="noreferrer">TEA &amp; KOMPEITO ↗</a
      ><slot
    /></span>
  </footer>
</template>
<style scoped>
footer {
  max-width: 1320px;
  margin: 30px auto 0;
  padding: 20px 0 25px;
  border-top: 1px solid var(--line);
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 15px;
  font-size: 11px;
  color: var(--muted);
}
.sep {
  padding: 0 7px;
}
footer > span:last-child {
  display: flex;
  align-items: center;
  gap: 20px;
}
a {
  font-size: 9px;
  letter-spacing: 1px;
}
@media (max-width: 600px) {
  footer {
    flex-wrap: wrap;
    margin-top: 24px;
    padding-bottom: 20px;
    gap: 9px;
  }
}
</style>
