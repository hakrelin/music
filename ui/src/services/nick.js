/**
 * 昵称：全局、自动、可改
 *  - 首次进入自动生成（游客-XXXX），写入 localStorage
 *  - 连上房间后自动 /setting/name 同步给后端
 */
import { baseUrl } from '../config/environment';

const KEY = 'JUSIC_NICK';

export function getNick() {
    let n = '';
    try { n = localStorage.getItem(KEY) || localStorage.getItem('USER_NAME') || ''; } catch (e) { n = ''; }
    if (!n) {
        n = '游客' + Math.floor(1000 + Math.random() * 9000);
        setNick(n);
    }
    return n;
}

export function setNick(n) {
    try {
        localStorage.setItem(KEY, n);
        localStorage.setItem('USER_NAME', n); // 老代码读这个 key
    } catch (e) { /* ignore */ }
}

export function origin() {
    return baseUrl;
}
