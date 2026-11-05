/*!
 * jusic-plus —— 在官方旧版 UI 上加功能（不改打包产物，运行时注入）
 *   ① 昵称：全局自动（首次生成、自动上报、顶栏可改）
 *   ② 导入歌单：用户昵称/ID → 该用户的歌单 → 一键按歌单播放
 *   ③ 电台：循环 + 随机，队列空了也一直放；内置东方电台歌单
 *   ④ 快捷键：空格 / → 切歌，R 重载当前
 *   ⑤ 播放自愈：http 音源升 https、出错重试、卡住自动切歌
 */
(function () {
  'use strict';
  if (window.__JUSIC_PLUS__) return;
  window.__JUSIC_PLUS__ = true;

  var NICK_KEY = 'JUSIC_NICK';
  var TOUHOU = ['2416955681', '406607901', '977204775', '434256234', '829520066', '2824632945'];

  function store() {
    var app = document.querySelector('#app');
    return app && app.__vue__ ? app.__vue__.$store : null;
  }
  function get(k) {
    var s = store();
    if (!s) return null;
    try { return s.getters[k]; } catch (e) { return null; }
  }
  function stomp() {
    var c = get('getStompClient');
    return c && c.connected ? c : null;
  }
  function send(dest, body) {
    var c = stomp();
    if (!c) return false;
    c.send(dest, {}, body === undefined ? '' : JSON.stringify(body));
    return true;
  }
  function getNick() {
    var n = '';
    try { n = localStorage.getItem(NICK_KEY) || localStorage.getItem('USER_NAME') || ''; } catch (e) {}
    if (!n || n === 'null') {
      n = '游客' + Math.floor(1000 + Math.random() * 9000);
      setNick(n);
    }
    return n;
  }
  function setNick(n) {
    try {
      localStorage.setItem(NICK_KEY, n);
      localStorage.setItem('USER_NAME', n);
    } catch (e) {}
  }

  /* ---------------- 样式 ---------------- */
  var css = ''
    + '.jp-r{display:inline-flex;gap:6px;align-items:center;margin-right:6px}'
    + '.jp-chip{display:inline-flex;align-items:center;gap:5px;background:rgba(255,255,255,.16);border:0;border-radius:14px;color:#fff;font-size:12px;padding:4px 10px;cursor:pointer;line-height:1.6}'
    + '.jp-chip:hover{background:rgba(255,255,255,.28)}'
    + '.jp-chip.on{background:rgba(255,255,255,.34)}'
    + '.jp-mask{position:fixed;inset:0;background:rgba(0,0,0,.55);z-index:9998;display:none}'
    + '.jp-mask.on{display:block}'
    + '.jp-dlg{position:fixed;left:50%;top:50%;transform:translate(-50%,-50%);width:min(520px,92vw);max-height:86vh;overflow:auto;'
    + 'background:#22303a;color:#e6eef4;border-radius:12px;z-index:9999;box-shadow:0 18px 50px rgba(0,0,0,.5);display:none;font-size:13px}'
    + '.jp-dlg.on{display:block}'
    + '.jp-hd{display:flex;align-items:center;padding:12px 16px;border-bottom:1px solid #33454f;font-size:14px}'
    + '.jp-x{margin-left:auto;background:none;border:0;color:#8aa2b2;cursor:pointer;font-size:18px;line-height:1}'
    + '.jp-bd{padding:12px 16px 16px}'
    + '.jp-sec{padding:10px 0;border-bottom:1px dashed #33454f}'
    + '.jp-sec:last-child{border-bottom:0}'
    + '.jp-lb{display:block;font-size:12px;color:#8aa2b2;margin-bottom:6px}'
    + '.jp-row{display:flex;gap:8px;align-items:center}'
    + '.jp-in{flex:1;min-width:0;background:#1a242c;border:1px solid #33454f;border-radius:6px;padding:7px 9px;color:#e6eef4;font-size:12.5px;outline:none}'
    + '.jp-in:focus{border-color:#00bfa5}'
    + '.jp-b{background:#2b3b45;border:1px solid #3d505c;color:#e6eef4;border-radius:6px;padding:7px 12px;font-size:12.5px;cursor:pointer;white-space:nowrap}'
    + '.jp-b:hover{border-color:#00bfa5;color:#7ef0dd}'
    + '.jp-b.p{background:#00796b;border-color:#00bfa5;color:#e8fffb}'
    + '.jp-auth{background:#1c2a22;border:1px solid #2f5a45;border-radius:8px;padding:10px 12px}'
    + '.jp-auth .st{font-size:12px;color:#7ef0dd}'
    + '.jp-auth .st.no{color:#ffb2b2}'
    + '.jp-list{margin-top:8px;max-height:190px;overflow:auto;display:flex;flex-direction:column;gap:4px}'
    + '.jp-it{display:flex;align-items:center;gap:8px;width:100%;background:#1f2a32;border:1px solid #33454f;border-radius:6px;'
    + 'padding:6px 9px;color:#e6eef4;cursor:pointer;text-align:left;font-size:12.5px}'
    + '.jp-it:hover{border-color:#00bfa5}'
    + '.jp-nm{flex:1;overflow:hidden;text-overflow:ellipsis;white-space:nowrap}'
    + '.jp-mt{font-size:11px;color:#88a0b0}'
    + '.jp-tip{font-size:12px;color:#7ef0dd;margin-top:8px;min-height:16px;line-height:1.5}'
    + '.jp-mt2{font-size:11.5px;color:#8aa2b2;margin:4px 0 0;line-height:1.5}';

  /* ---------------- 弹窗 ---------------- */
  var dlg, tip, userBox, listBox, keyword, directId;

  function buildDialog() {
    var mask = document.createElement('div');
    mask.className = 'jp-mask';
    mask.innerHTML = ''
      + '<div class="jp-dlg" id="jp-dlg">'
      + '  <div class="jp-hd"><span>导入歌单 · 电台</span><button class="jp-x" id="jp-close">×</button></div>'
      + '  <div class="jp-bd">'
      + '    <div class="jp-sec jp-auth">'
      + '      <label class="jp-lb">权限（导入歌单/电台需要房主或管理员）</label>'
      + '      <div class="jp-row"><input class="jp-in" id="jp-pwd" placeholder="房主口令，或管理员口令">'
      + '        <button class="jp-b p" id="jp-root">认证房主</button>'
      + '        <button class="jp-b" id="jp-admin">认证管理员</button></div>'
      + '      <p class="st no" id="jp-authst">当前没有房主权限</p>'
      + '    </div>'
      + '    <div class="jp-sec">'
      + '      <label class="jp-lb">① 按关键词搜歌单（最快）</label>'
      + '      <div class="jp-row"><input class="jp-in" id="jp-kw2" placeholder="如 东方Project">'
      + '        <button class="jp-b p" id="jp-find2">搜歌单</button></div>'
      + '    </div>'
      + '    <div class="jp-sec">'
      + '      <label class="jp-lb">② 按用户查歌单（用户昵称 或 用户ID）</label>'
      + '      <div class="jp-row"><input class="jp-in" id="jp-kw" placeholder="如 坏北风 或 8615085728">'
      + '        <button class="jp-b" id="jp-find">搜用户</button></div>'
      + '      <div class="jp-list" id="jp-users"></div>'
      + '    </div>'
      + '    <div class="jp-sec">'
      + '      <label class="jp-lb">② 选歌单 → 曲目加进<b>点歌队列</b></label>'
      + '      <div class="jp-list" id="jp-lists"></div>'
      + '    </div>'
      + '    <div class="jp-sec">'
      + '      <label class="jp-lb">或者直接填歌单 ID（多个用逗号隔开）</label>'
      + '      <div class="jp-row"><input class="jp-in" id="jp-ids" placeholder="如 2416955681">'
      + '        <button class="jp-b p" id="jp-apply">加入队列</button></div>'
      + '    </div>'
      + '    <div class="jp-sec">'
      + '      <label class="jp-lb">电台 / 队列</label>'
      + '      <div class="jp-row">'
      + '        <button class="jp-b p" id="jp-touhou">东方歌单加进队列</button>'
      + '        <button class="jp-b" id="jp-rand">看队列首数</button>'
      + '        <button class="jp-b" id="jp-loop">清空队列</button>'
      + '      </div>'
      + '      <button class="jp-b" id="jp-size" style="display:none"></button>'
      + '      <button class="jp-b" id="jp-clear" style="display:none"></button>'
      + '      <p class="jp-mt2">一次最多加 20 首（后端队列上限 23），按顺序播放。</p>'
      + '    </div>'
      + '    <p class="jp-tip" id="jp-tip"></p>'
      + '  </div>'
      + '</div>';
    document.body.appendChild(mask);
    document.getElementById('jp-close').onclick = function () { mask.classList.remove('on'); };
    mask.onclick = function (e) { if (e.target === mask) mask.classList.remove('on'); };
    dlg = document.getElementById('jp-dlg');
    tip = document.getElementById('jp-tip');
    userBox = document.getElementById('jp-users');
    listBox = document.getElementById('jp-lists');
    keyword = document.getElementById('jp-kw');
    directId = document.getElementById('jp-ids');

    document.getElementById('jp-find').onclick = findUser;
    document.getElementById('jp-root').onclick = function () {
      var p = (document.getElementById('jp-pwd').value || '').trim();
      if (!p) { say('先填房主口令'); return; }
      send('/auth/root', { password: p });
      say('正在认证房主…');
    };
    document.getElementById('jp-admin').onclick = function () {
      var p = (document.getElementById('jp-pwd').value || '').trim();
      if (!p) { say('先填管理员口令'); return; }
      send('/auth/admin', { password: p });
      say('正在认证管理员…');
    };
    var kw2 = document.getElementById('jp-kw2');
    document.getElementById('jp-find2').onclick = function () {
      var k = (kw2.value || '').trim();
      if (!k) { say('先填关键词'); return; }
      listBox.innerHTML = '';
      say('搜歌单中（后端要查音乐库，稍等几秒）…');
      send('/music/searchsonglist', { name: k, source: 'wy', pageIndex: 1, pageSize: 10 });
      pollResult('songlist', renderSonglists);
    };
    kw2.addEventListener('keyup', function (e) { if (e.key === 'Enter') document.getElementById('jp-find2').click(); });
    document.getElementById('jp-apply').onclick = function () {
      var ids = (directId.value || '').replace(/[，\s]+/g, ',').replace(/,+/g, ',').replace(/^,|,$/g, '');
      if (!ids) { say('先填歌单 ID'); return; }
      addToQueue(ids.split(','), '（填写的歌单）');
    };
    document.getElementById('jp-touhou').onclick = function () {
      addToQueue(TOUHOU, '（东方歌单）');
    };
    document.getElementById('jp-loop').onclick = function () { clearQueue(); };
    document.getElementById('jp-rand').onclick = function () {
      // 队列剩余首数（后端没有单独接口，用点歌历史条数兜底显示）
      var pick = get('getPlayerPick') || [];
      say('当前队列 ' + pick.length + ' 首（后端上限 ' + QUEUE_MAX + '）');
    };
    document.getElementById('jp-clear').onclick = clearQueue;
    keyword.addEventListener('keyup', function (e) { if (e.key === 'Enter') findUser(); });
  }

  function say(msg) { if (tip) tip.textContent = msg; }

  // 旧版 store 里已经存了搜索结果（getSearchDataGd / getSearchDataUser），
  // 直接轮询它，比钩 onmessage 稳（应用会反复包 onmessage）
  function pollResult(kind, done) {
    var t0 = Date.now();
    var key = kind === 'user' ? 'getSearchDataUser' : 'getSearchDataGd';
    var first = get(key);
    var timer = setInterval(function () {
      var arr = get(key) || [];
      if (arr.length && arr !== first) { clearInterval(timer); done(arr); return; }
      if (Date.now() - t0 > 20000) { clearInterval(timer); say('后端没返回结果（可能查得太慢，重试一次）'); }
    }, 400);
  }

  function findUser() {
    var k = (keyword.value || '').trim();
    if (!k) { say('先填昵称或用户ID'); return; }
    userBox.innerHTML = ''; listBox.innerHTML = '';
    if (/^\d{4,}$/.test(k)) {
      say('按用户ID查歌单…');
      send('/music/searchsonglist', { name: k, source: 'wy_user', pageIndex: 1, pageSize: 10 });
      pollResult('songlist', renderSonglists);
      return;
    }
    say('搜用户中…');
    send('/music/searchuser', { nickname: k, source: 'wy', pageIndex: 1, pageSize: 10 });
    pollResult('user', renderUsers);
  }

  function renderUsers(list) {
    userBox.innerHTML = '';
    if (!list.length) { say('没搜到用户'); return; }
    say('找到 ' + list.length + ' 个用户');
    list.slice(0, 12).forEach(function (u) {
      var b = document.createElement('button');
      b.className = 'jp-it';
      b.innerHTML = '<span class="jp-nm">' + esc(u.nickname || u.name || u.userName || '未知') + '</span>'
        + '<span class="jp-mt">ID ' + esc(String(u.id || u.userId || '')) + '</span>';
      b.onclick = function () {
        var id = u.id || u.userId;
        if (!id) { say('这个用户没返回 ID'); return; }
        listBox.innerHTML = '';
        say('查该用户的歌单…');
        send('/music/searchsonglist', { name: String(id), source: 'wy_user', pageIndex: 1, pageSize: 10 });
        pollResult('songlist', renderSonglists);
      };
      userBox.appendChild(b);
    });
  }

  function renderSonglists(list) {
    listBox.innerHTML = '';
    if (!list.length) { say('没搜到歌单'); return; }
    say('找到 ' + list.length + ' 个歌单，点一个就把曲目加进点歌队列');
    list.slice(0, 20).forEach(function (s) {
      var b = document.createElement('button');
      b.className = 'jp-it';
      b.innerHTML = '<span class="jp-nm">' + esc(s.name || '未命名') + '</span>'
        + '<span class="jp-mt">' + (s.trackCount || s.songCount || '?') + ' 首</span>';
      b.onclick = function () { addToQueue([String(s.id)], '歌单「' + (s.name || '') + '」'); };
      listBox.appendChild(b);
    });
  }

  var QUEUE_MAX = 23;     // 后端 PlaylistSize=23，队列超过就拒
  var sleep = function (ms) { return new Promise(function (r) { setTimeout(r, ms); }); };

  /**
   * 把歌单曲目逐首 /music/pick 进点歌队列（不是默认列表）
   * 多个歌单时平均分配名额，避免第一张就吃满上限
   */
  function addToQueue(ids, label) {
    var total = Math.min(QUEUE_MAX - 2, 20);          // 留 2 个余量给人手动点
    var per = Math.max(1, Math.floor(total / ids.length));
    var added = 0;
    say('正在取曲目…0/' + total);

    (async function () {
      for (var i = 0; i < ids.length && added < total; i++) {
        var r, j;
        try {
          r = await fetch('/plus-api/tracks?id=' + encodeURIComponent(ids[i]) + '&limit=' + per,
                          { credentials: 'same-origin' });
          if (!r.ok) {
            say('歌单接口返回 ' + r.status + '（这个域名的 /plus-api/ 没配反代）');
            return;
          }
          j = await r.json();
        } catch (e) {
          say('取曲目失败：' + e.message);
          continue;
        }
        var list = (j && j.tracks) || [];
        if (!list.length) continue;
        for (var k = 0; k < list.length && added < total; k++) {
          var t = list[k];
          send('/music/pick', {
            name: t.name, id: String(t.id), source: 'wy',
            quality: '320k', sendTime: Date.now()
          });
          added++;
          say('正在点歌 ' + added + '/' + total + '：' + t.name);
          await sleep(260);
        }
      }
      say('已把 ' + added + ' 首' + label + '加入点歌队列');
    })();
  }

  /* 清空点歌队列 */
  function clearQueue() {
    if (!get('isSocketRoot') && !get('isSocketAdmin')) { say('没有权限：先在上面认证'); return; }
    send('/music/clear');
    say('已清空点歌队列');
  }

  /* 权限状态 + 后端回执 */
  var lastChatLen = -1;
  function pollAuth() {
    var el = document.getElementById('jp-authst');
    if (!el) return;
    if (get('isSocketRoot')) { el.textContent = '房主 ✓ 可以导入歌单、开电台'; el.className = 'st'; }
    else if (get('isSocketAdmin')) { el.textContent = '管理员 ✓ 可以导入歌单、开电台'; el.className = 'st'; }
    else { el.textContent = '当前没有房主权限（导入歌单会被后端拒绝）'; el.className = 'st no'; }
  }
  function pollChat() {
    var d = get('getChatData') || [];
    if (lastChatLen < 0) { lastChatLen = d.length; return; }
    if (d.length > lastChatLen) {
      var last = d[d.length - 1] || {};
      lastChatLen = d.length;
      var c = String(last.content || '');
      if (c && /权限|歌单|列表|播放|成功|失败|口令/.test(c)) say(c);
    }
  }

  function esc(s) {
    return String(s == null ? '' : s).replace(/[&<>"]/g, function (c) {
      return { '&': '&amp;', '<': '&lt;', '>': '&gt;', '"': '&quot;' }[c];
    });
  }

  /* ---------------- 顶栏注入 ---------------- */
  function injectTopbar() {
    var bar = document.querySelector('.mu-appbar');
    if (!bar || bar.querySelector('.jp-r')) return;
    var right = bar.querySelector('.mu-appbar-right');
    var box = document.createElement('div');
    box.className = 'jp-r';
    var nickBtn = document.createElement('button');
    nickBtn.className = 'jp-chip';
    nickBtn.id = 'jp-nick';
    nickBtn.textContent = getNick();
    nickBtn.title = '点击修改昵称（所有房间通用）';
    nickBtn.onclick = function () {
      var n = window.prompt('改个昵称', getNick());
      if (!n || !n.trim()) return;
      n = n.trim().slice(0, 16);
      setNick(n);
      nickBtn.textContent = n;
      send('/setting/name', { name: n, sendTime: Date.now() });
    };
    var impBtn = document.createElement('button');
    impBtn.className = 'jp-chip';
    impBtn.textContent = '导入歌单';
    impBtn.onclick = function () {
      if (!dlg) buildDialog();
      document.querySelector('.jp-mask').classList.add('on');
      dlg.classList.add('on');
      say('');
    };
    box.appendChild(nickBtn);
    box.appendChild(impBtn);
    // 插进原生右侧容器，排在「分享」左边，避免绝对定位压住原生按钮
    if (right) {
      right.insertBefore(box, right.firstChild);
    } else {
      bar.appendChild(box);
    }
  }

  /* ---------------- WebSocket 消息钩子 ---------------- */
  function hookSocket() {
    var sock = get('getSocketClient');
    if (!sock || typeof sock.onmessage !== 'function') return;
    // 应用会在连接回调里再包一层 onmessage，把我们的钩子顶掉；
    // 所以只要发现栈顶不是我们的包装，就重新包一次。
    if (sock.onmessage && sock.onmessage.__jp) return;
    var prev = sock.onmessage;
    var wrapper = function (m) {
      try {
        var d = m && m.data;
        if (typeof d === 'string' && d.indexOf('\n') > 0) {
          var type = d.split('\n')[0];
          if (type === 'SEARCH_USER' || type === 'SEARCH_SONGLIST') {
            var payload = JSON.parse(d.slice(d.indexOf('\n') + 1));
            var body = payload && payload.data ? payload.data : {};
            var arr = body.data || [];
            if (type === 'SEARCH_USER') renderUsers(arr); else renderSonglists(arr);
          }
        }
      } catch (e) {}
      return prev.apply(this, arguments);
    };
    wrapper.__jp = true;
    sock.onmessage = wrapper;
  }

  /* ---------------- 昵称自动上报 ---------------- */
  var lastSent = '';
  function autoNick() {
    var c = stomp();
    if (!c) return;
    var n = getNick();
    if (lastSent === n) return;
    c.send('/setting/name', {}, JSON.stringify({ name: n, sendTime: Date.now() }));
    lastSent = n;
    var el = document.getElementById('jp-nick');
    if (el) el.textContent = n;
  }

  /* ---------------- 播放自愈 ---------------- */
  function httpsify(u) { return String(u || '').replace(/^http:\/\//, 'https://'); }
  var audio = null, retried = 0, stallTimer = null, preloadEl = null, lastPre = '';

  function bindAudio() {
    var a = document.getElementById('music');
    if (!a || a.__jpHooked) return;
    audio = a;
    ['error', 'stalled', 'abort'].forEach(function (ev) {
      a.addEventListener(ev, function () { heal(ev); }, true);
    });
    a.addEventListener('timeupdate', watchStall);
    a.addEventListener('ended', function () { clearStall(); retried = 0; });
    a.__jpHooked = true;
  }

  function watchStall() {
    clearStall();
    if (!audio || audio.paused) return;
    var before = audio.currentTime;
    stallTimer = setTimeout(function () {
      if (!audio || audio.paused) return;
      if (Math.abs(audio.currentTime - before) < 0.3) heal('卡住不动');
    }, 9000);
  }
  function clearStall() { if (stallTimer) { clearTimeout(stallTimer); stallTimer = null; } }

  function heal(reason) {
    var m = get('getPlayerMusic') || {};
    var url = httpsify(m.url);
    if (!url) return;
    if (retried >= 2) { send('/music/skip/vote'); retried = 0; return; }
    retried++;
    if (!audio) return;
    audio.src = url + (url.indexOf('?') > -1 ? '&' : '?') + '_r=' + Date.now();
    audio.load();
    var p = audio.play();
    if (p && p.catch) p.catch(function () {});
  }

  function watchMusic() {
    var s = store();
    if (!s || s.__jpWatching) return;
    s.watch(function (st) { return (st.player.music && st.player.music.url) || ''; }, function (v) {
      retried = 0;
      clearStall();
      if (!v) return;
      if (String(v).indexOf('http://') === 0 && audio) {
        audio.src = httpsify(v);
        audio.load();
        var p = audio.play();
        if (p && p.catch) p.catch(function () {});
      }
      watchStall();
    });
    s.watch(function (st) { return (st.player.pick || []).length; }, function () {
      var pick = get('getPlayerPick') || [];
      var next = pick.length > 1 ? pick[1] : null;
      if (!next || !next.url) return;
      var u = httpsify(next.url);
      if (u === lastPre) return;
      lastPre = u;
      try { preloadEl = new Audio(); preloadEl.preload = 'auto'; preloadEl.src = u; } catch (e) {}
    });
    s.__jpWatching = true;
  }

  /* ---------------- 快捷键 ---------------- */
  document.addEventListener('keydown', function (e) {
    var t = (e.target && e.target.tagName) || '';
    if (t === 'INPUT' || t === 'TEXTAREA' || t === 'SELECT') return;
    if (e.code === 'Space' || e.key === 'ArrowRight') {
      e.preventDefault();
      send('/music/skip/vote');
    } else if (e.key === 'r' || e.key === 'R') {
      e.preventDefault();
      var m = get('getPlayerMusic') || {};
      if (m && m.id) {
        send('/music/pick', { music: m, name: m.name, id: m.id, source: m.source });
      }
    }
  });

  /* ---------------- 启动 ---------------- */
  function boot() {
    var s = store();
    if (!s) return setTimeout(boot, 400);
    var style = document.createElement('style');
    style.textContent = css;
    document.head.appendChild(style);

    setInterval(function () {
      injectTopbar();
      hookSocket();
      bindAudio();
      watchMusic();
      autoNick();
      pollAuth();
      pollChat();
    }, 1000);

    injectTopbar();
    hookSocket();
    bindAudio();
    watchMusic();
    autoNick();
  }

  if (document.readyState === 'loading') {
    document.addEventListener('DOMContentLoaded', boot);
  } else {
    boot();
  }
})();
