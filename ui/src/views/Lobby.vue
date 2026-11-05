<template>
  <div class="lobby">
    <header class="lb-head">
      <div class="lb-brand" @click="$router.push('/')">一起听歌吧</div>
      <div class="lb-nick" @click="editNick" title="点击修改昵称">
        <span class="lb-dot"></span>{{ nick }}
      </div>
    </header>

    <main class="lb-main">
      <section class="lb-hero">
        <h1>选个房间进去听</h1>
        <p>点歌房想听什么点什么；电台房是别人整理好的歌单，一直放着。</p>
      </section>

      <!-- 房间列表 -->
      <section class="lb-sec">
        <div class="lb-sec-head">
          <h2>房间</h2>
          <button class="lb-mini" @click="load" :class="{ spin: loading }">刷新</button>
        </div>
        <div v-if="error" class="lb-err">{{ error }}</div>
        <div class="lb-grid">
          <article v-for="h in houses" :key="h.id" class="lb-card" :class="{ radio: isRadio(h) }">
            <div class="lb-card-top">
              <span class="lb-tag" v-if="isRadio(h)">电台</span>
              <span class="lb-tag lb-tag-lock" v-if="h.needPwd">加密</span>
              <span class="lb-pop">{{ h.population || 0 }} 人在线</span>
            </div>
            <h3>{{ h.name }}</h3>
            <p>{{ h.desc || '这个房间还没有写介绍' }}</p>
            <button class="lb-btn lb-primary" @click="enter(h)">进入</button>
          </article>
          <article v-if="!houses.length && !loading" class="lb-card lb-empty">
            <h3>还没有房间</h3>
            <p>下面创建一个，或者等别人开。</p>
          </article>
        </div>
      </section>

      <!-- 创建房间 -->
      <section class="lb-sec">
        <div class="lb-sec-head">
          <h2>开个房间</h2>
          <div class="lb-presets">
            <button class="lb-mini" @click="preset('normal')">普通点歌房</button>
            <button class="lb-mini" @click="preset('radio')">电台房</button>
            <button class="lb-mini" @click="preset('touhou')">东方电台</button>
          </div>
        </div>
        <div class="lb-form">
          <label>房间名<input v-model="form.name" maxlength="33" placeholder="最多 33 字"></label>
          <label>介绍<input v-model="form.desc" maxlength="133" placeholder="随便写一句"></label>
          <label class="lb-check">
            <input type="checkbox" v-model="form.needPwd"> 设密码
            <input v-if="form.needPwd" v-model="form.password" placeholder="房间密码" class="lb-pwd">
          </label>
          <div class="lb-actions">
            <button class="lb-btn lb-primary" @click="create" :disabled="creating">
              {{ creating ? '创建中…' : '创建并进入' }}
            </button>
            <span class="lb-tip" v-if="msg">{{ msg }}</span>
          </div>
        </div>
        <p class="lb-note">电台房会直接在房间里打开「导入歌单」；东方电台选好后要有人在里面点一次「装载东方歌单」。</p>
      </section>
    </main>
  </div>
</template>

<script>
import { searchHouses, addHouse, enterHouse } from '../services/houseApi';
import { getNick, setNick } from '../services/nick';

export default {
  name: 'Lobby',
  data() {
    return {
      nick: getNick(),
      houses: [],
      loading: false,
      creating: false,
      error: '',
      msg: '',
      form: { name: '', desc: '', needPwd: false, password: '' },
      timer: null
    };
  },
  mounted() {
    this.load();
    this.timer = setInterval(this.load, 10000);
  },
  beforeDestroy() {
    if (this.timer) clearInterval(this.timer);
  },
  methods: {
    isRadio(h) {
      return /电台|radio/i.test(h.name || '') || /电台/.test(h.desc || '');
    },
    async load() {
      this.loading = true;
      try {
        const r = await searchHouses();
        const list = (r && r.data) || [];
        // 电台房排前面
        this.houses = list.slice().sort((a, b) => (this.isRadio(b) ? 1 : 0) - (this.isRadio(a) ? 1 : 0));
        this.error = '';
      } catch (e) {
        this.error = '房间列表拉取失败（' + e.message + '），可直接进默认房间';
        if (!this.houses.length) {
          this.houses = [{ id: 'DEFAULT', name: '默认点歌房', desc: '拉不到房间列表时的兜底入口', population: 0 }];
        }
      } finally {
        this.loading = false;
      }
    },
    editNick() {
      const n = window.prompt('改个昵称（其它房间也用它）', this.nick);
      if (n && n.trim()) {
        this.nick = n.trim().slice(0, 16);
        setNick(this.nick);
      }
    },
    preset(kind) {
      if (kind === 'radio') {
        this.form.name = '电台房';
        this.form.desc = '导入一份歌单，整晚不间断';
      } else if (kind === 'touhou') {
        this.form.name = '东方电台';
        this.form.desc = '24 小时东方 Project，不间断';
      } else {
        this.form.name = '点歌房';
        this.form.desc = '想听什么点什么';
      }
    },
    async create() {
      if (!this.form.name.trim()) { this.msg = '先写个房间名'; return; }
      if (this.form.needPwd && !this.form.password.trim()) { this.msg = '密码没填'; return; }
      this.creating = true;
      this.msg = '';
      try {
        const r = await addHouse({
          name: this.form.name.trim(),
          desc: this.form.desc.trim(),
          needPwd: this.form.needPwd,
          password: this.form.needPwd ? this.form.password.trim() : null
        });
        if (r && r.code === '20000' && r.data) {
          this.msg = '创建成功，正在进入…';
          this.go(r.data, this.form.needPwd ? this.form.password.trim() : '');
        } else {
          this.msg = (r && r.message) || '创建失败';
        }
      } catch (e) {
        this.msg = '创建失败：' + e.message;
      } finally {
        this.creating = false;
      }
    },
    async enter(h) {
      let pwd = '';
      if (h.needPwd) {
        pwd = window.prompt('房间「' + h.name + '」需要密码') || '';
        if (!pwd) return;
        const r = await enterHouse(h.id, pwd);
        if (!r || r.code !== '20000') { this.error = (r && r.message) || '密码不对'; return; }
      }
      this.go(h.id, pwd);
    },
    go(id, pwd) {
      this.$router.push({ path: '/room/' + id, query: pwd ? { pwd } : {} });
    }
  }
};
</script>

<style scoped>
.lobby { min-height: 100vh; background: #222b32; color: #e3ebf2; }
.lb-head {
  display: flex; align-items: center; justify-content: space-between;
  padding: 14px 22px; background: #009688; color: #fff;
}
.lb-brand { font-size: 18px; letter-spacing: .5px; cursor: pointer; }
.lb-nick { font-size: 13px; background: rgba(0,0,0,.18); padding: 4px 10px; border-radius: 14px; cursor: pointer; }
.lb-dot { display: inline-block; width: 7px; height: 7px; border-radius: 50%; background: #8effe0; margin-right: 6px; }
.lb-main { max-width: 1040px; margin: 0 auto; padding: 26px 22px 60px; }
.lb-hero h1 { font-size: 22px; margin: 0 0 6px; font-weight: 500; }
.lb-hero p { margin: 0 0 24px; color: #93a6b5; font-size: 13.5px; }
.lb-sec { margin-bottom: 30px; }
.lb-sec-head { display: flex; align-items: center; gap: 12px; margin-bottom: 12px; }
.lb-sec-head h2 { font-size: 15px; color: #9fb3c2; font-weight: 500; margin: 0; }
.lb-presets { margin-left: auto; display: flex; gap: 8px; }
.lb-grid { display: grid; gap: 14px; grid-template-columns: repeat(auto-fill, minmax(260px, 1fr)); }
.lb-card {
  background: #2b353d; border: 1px solid #38454f; border-radius: 10px; padding: 14px 16px 16px;
  display: flex; flex-direction: column;
}
.lb-card.radio { border-color: #2fb8a6; box-shadow: inset 0 0 0 1px rgba(47,184,166,.18); }
.lb-card h3 { margin: 6px 0 4px; font-size: 16px; font-weight: 500; }
.lb-card p { margin: 0 0 12px; color: #8fa3b2; font-size: 12.5px; min-height: 32px; }
.lb-card-top { display: flex; align-items: center; gap: 8px; }
.lb-tag { font-size: 11px; padding: 1px 7px; border-radius: 9px; background: #1f6f66; color: #9cf3e2; }
.lb-tag-lock { background: #5a4222; color: #ffd08a; }
.lb-pop { margin-left: auto; font-size: 11.5px; color: #7e93a3; }
.lb-empty { opacity: .75; }
.lb-btn {
  border: 1px solid #3d4c57; background: #313d46; color: #dce7ef; border-radius: 7px;
  padding: 7px 14px; font-size: 13px; cursor: pointer; align-self: flex-start;
}
.lb-btn:hover { border-color: #2fb8a6; color: #8ff0e2; }
.lb-primary { background: #1f6f66; border-color: #2fb8a6; color: #d9fff9; }
.lb-mini {
  border: 1px solid #3d4c57; background: transparent; color: #9fb3c2; border-radius: 6px;
  padding: 4px 10px; font-size: 12px; cursor: pointer;
}
.lb-mini:hover { color: #8ff0e2; border-color: #2fb8a6; }
.lb-form { background: #2b353d; border: 1px solid #38454f; border-radius: 10px; padding: 16px; }
.lb-form label { display: block; font-size: 12.5px; color: #9fb3c2; margin-bottom: 10px; }
.lb-form input:not([type=checkbox]) {
  display: block; width: 100%; margin-top: 5px; background: #202a31; border: 1px solid #3d4c57;
  border-radius: 6px; padding: 7px 9px; color: #e3ebf2; font-size: 13px; outline: none;
}
.lb-form input:focus { border-color: #2fb8a6; }
.lb-check { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
.lb-pwd { max-width: 180px; }
.lb-actions { display: flex; align-items: center; gap: 12px; margin-top: 4px; }
.lb-tip { font-size: 12.5px; color: #ffcf8a; }
.lb-note { font-size: 12px; color: #7e93a3; margin-top: 10px; }
.lb-err { background: #4a2222; color: #ffb3b3; padding: 8px 12px; border-radius: 7px; font-size: 12.5px; margin-bottom: 10px; }
</style>
