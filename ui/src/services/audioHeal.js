/**
 * 播放自愈（无界面，挂在 Music 页上）
 *   1. 音源 http:// → https://（https 页面播 http 音频会被浏览器拦）
 *   2. audio error / stalled / abort → 重试，连续失败自动请求切歌
 *   3. 播放中每 9 秒看 currentTime 有没有走，卡住就当失败
 *   4. 提前预热下一首
 */
export function installAudioHeal(vm) {
    const store = vm.$store;
    let audio = null;
    let stallTimer = null;
    let retried = 0;
    let lastUrl = '';
    let preloadEl = null;
    let healCount = 0;

    const log = (msg) => {
        // 只在聊天区留一行，方便排查（不刷屏）
        if (healCount <= 3) {
            store.commit('pushChatData', { content: '播放器：' + msg, type: 'notice' });
        }
        healCount += 1;
    };

    const httpsify = (u) => String(u || '').replace(/^http:\/\//, 'https://');

    const clearStall = () => {
        if (stallTimer) { clearTimeout(stallTimer); stallTimer = null; }
    };

    const watch = () => {
        clearStall();
        if (!audio || audio.paused) return;
        const before = audio.currentTime;
        stallTimer = setTimeout(() => {
            if (!audio || audio.paused) return;
            if (Math.abs(audio.currentTime - before) < 0.3) heal('卡住不动');
        }, 9000);
    };

    const send = (dest, body) => {
        const c = store.getters.getStompClient;
        if (c && c.connected) c.send(dest, {}, body === undefined ? '' : JSON.stringify(body));
    };

    const heal = (reason) => {
        const music = store.getters.getPlayerMusic || {};
        const url = httpsify(music.url);
        if (!url) return;
        if (retried >= 2) {
            log(`连续失败（${reason}），自动切歌`);
            send('/music/skip/vote');
            retried = 0;
            return;
        }
        retried += 1;
        log(`检测到 ${reason}，重试第 ${retried} 次`);
        if (!audio) return;
        audio.src = url + (url.includes('?') ? '&' : '?') + '_r=' + Date.now();
        audio.load();
        const p = audio.play();
        if (p && p.catch) p.catch(() => {});
    };

    const bind = () => {
        audio = document.getElementById('music');
        if (!audio) { setTimeout(bind, 1200); return; }
        ['error', 'stalled', 'abort'].forEach((ev) =>
            audio.addEventListener(ev, () => heal(ev), true));
        audio.addEventListener('timeupdate', watch);
        audio.addEventListener('ended', () => { clearStall(); retried = 0; });
    };

    // 监听当前歌曲变化
    store.watch(
        (s) => (s.player.music && s.player.music.url) || '',
        (v) => {
            if (!v) return;
            retried = 0;
            clearStall();
            const fixed = httpsify(v);
            if (v.startsWith('http://') && audio && audio.src !== fixed) {
                audio.src = fixed;
                audio.load();
                const p = audio.play();
                if (p && p.catch) p.catch(() => {});
                log('已把 http 音源升级为 https');
            }
            watch();
        }
    );

    // 预热下一首
    store.watch(
        (s) => (s.player.pick || []).length,
        () => {
            const pick = store.getters.getPlayerPick || [];
            const next = pick.length > 1 ? pick[1] : null;
            if (!next || !next.url) return;
            const u = httpsify(next.url);
            if (u === lastUrl) return;
            lastUrl = u;
            try {
                preloadEl = new Audio();
                preloadEl.preload = 'auto';
                preloadEl.src = u;
            } catch (e) { /* ignore */ }
        }
    );

    bind();
}
