/**
 * 东方电台 24h 常驻机器人
 *
 * 作用：
 *   1. 自己开一个「东方电台」房间（房间没人 20 秒会被回收，所以必须一直挂着）
 *   2. 把东方歌单装进默认列表，开循环 + 随机
 *   3. 断线自动重连、重进房间
 *
 * 用法：
 *   npm i                     # 首次
 *   node touhou-radio.js http://127.0.0.1:8080
 */
const SockJS = require('sockjs-client');
const Stomp = require('stompjs');

const BASE = process.argv[2] || 'http://127.0.0.1:8080';
const ROOM_NAME = process.env.ROOM_NAME || '东方电台';
const ROOM_DESC = process.env.ROOM_DESC || '24 小时东方 Project，不间断';

// 实测有效、曲目量大的东方歌单（网易云）
const TOUHOU = [
  '2416955681', // 东方Project-原曲（1398 首）
  '406607901',  // 東方Project - 正作原曲合集（275）
  '977204775',  // 东方project人气歌曲精选（281）
  '434256234',  // 东方Project［经典的幻想乡神曲］（302）
  '829520066',  // 评论超过5000的东方project神曲（84）
  '2824632945'  // 东方同人音乐（4038）
];

function log(...a) {
  console.log(new Date().toISOString().slice(11, 19), ...a);
}

async function api(path, body) {
  const r = await fetch(BASE + path, {
    method: body === undefined ? 'GET' : 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body)
  });
  return r.json();
}

/** 房间不存在就建一个，返回 houseId */
async function ensureRoom() {
  const r = await api('/house/search');
  const list = (r && r.data) || [];
  const hit = list.find((h) => h.name === ROOM_NAME);
  if (hit) return hit.id;
  const c = await api('/house/add', { name: ROOM_NAME, desc: ROOM_DESC });
  if (c && c.code === '20000' && c.data) {
    log('已创建房间', ROOM_NAME, c.data);
    return c.data;
  }
  log('创建房间失败：', c && c.message);
  return null;
}

let current = null;

function join(houseId) {
  const url = `${BASE}/server?houseId=${encodeURIComponent(houseId)}`;
  log('连接', url);
  const socket = new SockJS(url);
  const stomp = Stomp.over(socket);
  stomp.debug = () => {};
  stomp.heartbeat = { outgoing: 10000, incoming: 10000 };

  stomp.connect({}, () => {
    log('已连接，开始装载东方歌单');
    current = stomp;
    const nick = JSON.stringify({ name: ROOM_NAME, sendTime: Date.now() });

    stomp.send('/setting/name', {}, nick);
    // 房主口令（沿用后端默认，没改过就不用动）
    stomp.send('/auth/root', {}, JSON.stringify({ password: process.env.ROOT_PWD || '654321' }));

    setTimeout(() => {
      stomp.send('/music/setDefaultPlaylist', {}, JSON.stringify({ id: TOUHOU.join(','), source: 'netease' }));
      stomp.send('/music/musiclistmodel/true', {}, '');
      stomp.send('/music/randommodel/true', {}, '');
      log('已提交', TOUHOU.length, '个歌单 + 循环 + 随机');
    }, 1200);

    stomp.subscribe('/topic/chat', () => {});
    stomp.subscribe('/topic/music/order', () => {});
  }, (err) => {
    log('连接失败：', err && err.message);
    current = null;
    setTimeout(main, 5000);
  });

  socket.onclose = () => {
    log('连接断开，5 秒后重连');
    current = null;
    setTimeout(main, 5000);
  };
}

async function main() {
  try {
    const id = await ensureRoom();
    if (!id) return setTimeout(main, 8000);
    join(id);
  } catch (e) {
    log('异常：', e.message);
    setTimeout(main, 8000);
  }
}

process.on('SIGINT', () => {
  if (current) current.disconnect();
  process.exit(0);
});

main();
