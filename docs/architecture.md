# 茶与金平糖 · 架构契约 v1

> 本站是「一起听歌吧 / Jusic」的彻底重写：Go 单二进制后端 + Vue3 SPA 前端。
> **不再依赖** Java jar、Docker、SockJS/STOMP、Node 音乐服务。
> 本文件是前后端共同遵守的接口契约，改动必须先改这里。

## 0. 仓库布局

```
music/
├─ web/                     Vue3 + TS + Vite 前端（唯一真源）
│  └─ dist/                 vite build 产物（gitignore）
├─ server/                  Go 后端（module github.com/hakrelin/music）
│  ├─ cmd/musicd/main.go    入口
│  ├─ internal/
│  │  ├─ config/            JSON 配置 + env 覆盖
│  │  ├─ model/             Track/Room/Message 等共享类型
│  │  ├─ provider/          音乐源接口 + registry
│  │  │  ├─ netease/        网易云原生实现（weapi，纯标准库）
│  │  │  └─ demo/           本地演示数据 + 程序合成音频
│  │  ├─ hub/               房间状态机、WS 会话、时钟推进、电台 DJ
│  │  ├─ httpapi/           HTTP 路由 + WS 升级 + 静态资源
│  │  └─ webui/             //go:embed dist  （构建前由脚本从 web/dist 拷入）
│  ├─ config.example.json
│  └─ config.local.json     gitignore，放 cookie
├─ deploy/                   systemd unit、nginx 示例、构建脚本（无 Docker）
├─ docs/                     架构与协议
├─ ui/ plus/ bot/            旧版实现，仅作参考，不再部署
└─ README.md
```

构建链：
1. `npm --prefix web run build` → `web/dist`
2. `node scripts/copy-dist.mjs` → `server/internal/webui/dist`
3. `go build -o bin/musicd ./server/cmd/musicd`

一条命令：`make build`（Windows 另给 `build.ps1`）。

## 1. 产品语义

- **房间**是唯一的收听单元：有人进房即开始同步播放，所有人听同一首、进度一致。
- **队列模型**：`queue[0]` 就是当前播放曲目，`startedAt` 是该曲目开始播放的服务器时间（毫秒）。
  新歌点进来追加到队尾；管理员可置顶 / 删除 / 清空。
- **播放推进由服务器负责**：按曲目时长 + `startedAt` 定时切下一首并广播，客户端只做时钟对齐与音频播放。
- **电台房**：房间带 `radio=true` 时，队列不足 3 首自动从 `defaultPlaylists` 补到 8 首（服务器侧 DJ，替代旧的常驻 bot）。
- **内置房间**：配置里声明的电台房（默认「东方电台」）在服务器启动时保证存在，空房不回收。
- **普通房间**：主动创建；房主离开后空置 10 分钟回收。
- **权限**：`root` / `admin` 两级口令，服务端校验，按 WS 会话授予。普通用户可点歌、聊天、投票切歌。

## 2. 数据模型（JSON 字段名前后端一致）

```jsonc
// Track —— 队列项就是 Track
{
  "id": "1901371647",
  "source": "wy",            // wy | demo（v1 只有这两种；qq/mg 预留）
  "name": "夜雀の歌声",
  "artist": "上海アリス幻樂団",
  "album": "東方永夜抄",
  "cover": "https://p1.music.126.net/....jpg",  // 可为空串
  "duration": 245,           // 秒
  "fee": 0,                  // 0 免费 / 1 VIP / 4 付费 / 8 低音质可播；前端据此显示标记
  "url": "",                 // 服务端解析后填充，客户端不要缓存
  "nick": "游客1024",         // 点歌人（服务端填）
  "likes": 0,
  "lyric": ""                // 仅当前播放的曲目带 LRC 原文；snapshot/current 里出现
}
```

```jsonc
// Playlist（歌单搜索结果 / 导入结果）
{ "id": "2416955681", "name": "东方Project-原曲", "cover": "...", "creator": "xxx", "count": 1398 }
```

```jsonc
// Room（房间列表里给前端的公开信息）
{
  "id": "r_xxxxx",
  "name": "东方电台",
  "desc": "24 小时东方 Project，不间断",
  "radio": true,
  "needPwd": false,
  "online": 3,
  "builtin": true,
  "now": { "name": "...", "artist": "...", "cover": "..." }   // 可空
}
```

```jsonc
// ChatMsg
{ "type": "chat" | "notice" | "system", "nick": "游客1024", "content": "…",
  "images": ["https://…"], "ts": 1762339200000, "session": "s_ab12" }
```

## 3. HTTP API（前缀 `/api`，全部 JSON，UTF-8）

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/api/healthz` | `{ok, version, provider, rooms}` |
| GET | `/api/config` | `{site:{name, tagline}, sources:[{id,label}], radioPresets:[{id,label,playlists}]}` |
| GET | `/api/rooms` | `{rooms:[Room]}`（按内置/电台/在线排序） |
| POST | `/api/rooms` | body `{name, desc, needPwd, password}` → `{id}`；校验：name 1–33 字，desc ≤133 |
| POST | `/api/rooms/{id}/auth` | body `{password}` → `{ok:true}`；错误 403 |
| GET | `/api/demo/audio/{id}.wav` | 仅 demo provider：程序合成音频（见 §7） |

错误统一：`{"error":{"code":"room_not_found","message":"房间不存在"}}` + 合适状态码。
密码类错误不区分「无此房/密码错」，避免枚举。

## 4. WebSocket 协议

入口：`GET /api/ws?room={roomId}&nick={nick}`（nick 可省略）。升级后 10 秒内必须发 `hello`，否则断开。
帧格式：单个 JSON 对象，字段 `t` 为类型。心跳：客户端每 20s 发 `{"t":"ping"}`，服务器回 `{"t":"pong","now":<serverMs>}`。

### 4.1 客户端 → 服务器

```jsonc
{"t":"hello","nick":"游客1024","pwd":"房间密码或空"}
{"t":"ping"}
{"t":"chat","content":"晚上好"}
{"t":"pick","id":"1901371647","source":"wy","name":"夜雀の歌声"}   // 有 id 用 id，无 id 走搜索点歌
{"t":"search","req":"q1","q":"东方","source":"wy","page":1,"size":10}
{"t":"playlist.search","req":"q2","q":"东方","page":1,"size":10}
{"t":"playlist.get","req":"q3","id":"2416955681"}
{"t":"user.playlists","req":"q4","uid":"12345"}                    // 只读，用于「按用户 ID 导入」
{"t":"queue.remove","id":"1901371647"}                             // admin/root 或点歌人本人
{"t":"queue.top","id":"1901371647"}                                // admin/root
{"t":"queue.clear"}                                                // admin/root
{"t":"skip.vote"}                                                  // 所有人；达阈值切歌
{"t":"auth","role":"root","password":"…"}
{"t":"radio.load","preset":"touhou"}                               // 把预设歌单装进 defaultPlaylists 并补队
{"t":"next"}                                                       // admin/root 强切
{"t":"mode","loop":true,"random":false,"radio":true}               // admin/root，缺省字段不改
{"t":"ban","choose":true,"switch":false}                           // admin/root，禁止点歌/投票切歌，缺省字段不改
{"t":"goodmode","on":true}                                         // admin/root，点赞模式
{"t":"volume","value":80}                                          // admin/root，值 0–100
{"t":"good","id":"1901371647"}                                     // 点赞
{"t":"chat.black","session":"s_ab12"}                              // admin/root
{"t":"chat.unblack","session":"s_ab12"}                            // admin/root
```

### 4.2 服务器 → 客户端

连接成功后按序发送：`hello` → `state` → `snapshot`。

```jsonc
{"t":"hello","now":1762339200000,"self":{"session":"s_7f3a","nick":"游客1024","role":"user"},
 "room":{"id":"r_x","name":"东方电台","radio":true,"needPwd":false}}
{"t":"state","loop":true,"random":true,"radio":true,"goodMode":false,
 "banChoose":false,"banSwitch":false,"volume":80}
{"t":"snapshot","online":3,"queue":["…Track…"],"startedAt":1762339190000,"likes":{"id":2},
 "chat":[["ChatMsg…"]]}
{"t":"presence","online":4}
{"t":"queue","queue":["…Track…"]}                    // 队列任何变化都整包重发
{"t":"current","track":{…Track…},"startedAt":1762339190000}
{"t":"chat","msg":{…ChatMsg…}}
{"t":"notice","content":"东方电台 已开始播放","level":"info"}
{"t":"search","req":"q1","tracks":["…Track…"],"total":280}
{"t":"playlist.search","req":"q2","playlists":["…Playlist…"],"total":12}
{"t":"playlist.get","req":"q3","playlist":{…Playlist…},"tracks":["…Track…"]}
{"t":"user.playlists","req":"q4","playlists":["…Playlist…"]}
{"t":"auth","role":"root","ok":true,"message":"房主认证成功"}
{"t":"error","code":"bad_password","message":"口令不对","req":"q1"}   // req 可省略
```

约定：
- 队列/当前播放/在线人数的任何变化都必须广播；`current` 与 `queue` 一起变化时先 `current` 后 `queue` 或反之都可，前端以最后一条为准。
- 服务器时间基准：`hello.now` / `pong.now` 用于估算客户端时钟偏移。
- 断线重连：客户端带 `room` 重新连，服务器补发 `hello/state/snapshot`，前端无缝恢复。
- 昵称持久化在客户端 localStorage（`yue.nick`），连接时通过 `hello.nick` 带上；服务器做 1–16 字裁剪与去重后缀。

## 5. 音乐源 Provider 接口（Go）

```go
package provider

type Track struct { /* 见 §2；URL 由 Resolve 填充 */ }

type Provider interface {
    Name() string
    Search(ctx context.Context, q string, page, size int) ([]Track, int, error)
    Playlist(ctx context.Context, id string) (Playlist, []Track, error)
    UserPlaylists(ctx context.Context, uid string) ([]Playlist, error)
    Resolve(ctx context.Context, src *Track) error   // 填 URL（+ 必要时 cover/lyric）
    Lyric(ctx context.Context, id string) (string, error)
}
```

- 注册表按 `source` 前缀路由（`wy` → netease，`demo` → demo）。
- 服务器对 `Resolve` 结果做进程内 LRU 缓存（URL 有效期短，缓存 ≤ 5 分钟）。
- `Resolve` 失败：跳过该曲目并广播 notice，不阻塞队列。
- 搜索/歌单结果里的 Track **不带 URL**，点歌或开始播放时才解析（省请求）。
- 网易云实现：纯标准库 weapi（AES-128-CBC + RSA），cookie 来自配置；无 cookie 时仍可搜索，URL 可能低码率/试听。

## 6. 配置

`server/config.example.json`（提交）；`server/config.local.json`（gitignore）覆盖同名键；env 覆盖两者（`MUSIC_HTTP_ADDR`、`MUSIC_NETEASE_COOKIE`、`MUSIC_DATA_DIR`…）。

```jsonc
{
  "httpAddr": ":8080",
  "provider": "auto",              // auto: 有 cookie 用 wy，否则 demo
  "neteaseCookie": "",
  "site": { "name": "茶与金平糖", "tagline": "和大家一起听歌" },
  "rootPassword": "654321",
  "adminPassword": "123456",
  "radioRooms": [
    { "name": "东方电台", "desc": "24 小时东方 Project，不间断",
      "playlists": ["2416955681","406607901","977204775","434256234","829520066","2824632945"] }
  ],
  "roomIdleMinutes": 10,
  "voteSkipRatio": 0.3,
  "logLevel": "info"
}
```

安全：示例配置里的口令必须改；`config.local.json` 与带 cookie 的文件永不入库。

## 7. Demo provider（本地无 cookie 也能完整跑）

- 内置约 24 首虚构曲目（中文名，标注 demo），封面用 `/assets/demo/cover-N.svg`（前端静态或后端生成渐变 SVG，二选一，后端生成更省事）。
- 音频：`/api/demo/audio/{id}.wav` 由服务器**实时合成**（44.1kHz 16bit 单声道，30–45s 柔和和弦垫 + 轻微噪声），不同 id 不同调式，够验证播放/切歌/同步。
- Demo 下 `playlist.get` 返回内置「东方电台（演示）」歌单；`radio.load` 直接用 demo 队列。
- 目的：任何人 `go run` 起来就能看完整 UI 与播放逻辑；真实部署换成 wy。

## 8. 前端（web/）

- 栈：Vue 3 + TypeScript + Vite + Pinia + vue-router。**不引入 UI 组件库**，组件自写，样式手写 CSS（设计 token 见 `web/src/styles/tokens.css`）。
- 路由：`/` 大厅、`/room/:id` 房间、`/about` 说明（可选）。
- 状态：`stores/room.ts`（WS 连接、快照、队列）、`stores/player.ts`（音频元素、进度、音量、歌词索引）、`stores/ui.ts`（弹窗/toast）。
- WS 客户端：`api/ws.ts` 负责重连（指数退避 0.5s→8s）、心跳、请求-响应（req id ↔ Promise）。
- 音频播放：`<audio>` 单例，`player.ts` 统一控制；同步逻辑 = 服务器 `startedAt` + 本机时钟偏移；偏差 > 1.5s 时 seek。
- 设计：深绿/米白/浅茶绿，宋体大标题，留白，细线；房间页是「夜间」暗色变体。首屏插画位：`web/src/components/hero/HeroScene.vue`（当前 SVG 夜景，用户提供插画后换成 `<img>`，布局不变）。
- 响应式：≥1100 双栏，≤760 单栏；键盘可访问；`prefers-reduced-motion` 降级。

## 9. 部署（无 Docker）

```
/usr/local/bin/musicd
/etc/musicd/config.json        # 含 cookie，chmod 600
musicd.service                 # systemd，User=musicd，WorkingDirectory=/var/lib/musicd
nginx: server_name → proxy_pass http://127.0.0.1:8080;  # 含 WS Upgrade
```

nginx 关键片段（WebSocket 必须）：
```nginx
location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection "upgrade";
    proxy_set_header Host $host;
    proxy_read_timeout 3600s;
}
```

## 10. 验收清单

- [ ] `make build` 产出单个 `bin/musicd`，无网络依赖即可启动（demo 模式）。
- [ ] 浏览器开 `/` 能看到大厅、开房、进房；进房 3 秒内出画面。
- [ ] 两个浏览器进同一房间：一方点歌，两方队列一致；刷新后进度对齐（偏差 ≤1.5s）。
- [ ] 歌切完自动下一首；投票切歌达阈值切歌。
- [ ] 聊天、昵称、在线人数实时；root 口令后可置顶/删除/清空/切歌。
- [ ] 搜索 → 点歌全链路（wy 无 cookie 也不崩；demo 完整可播）。
- [ ] 东方电台房间空置不回收，队列自动补。
- [ ] 移动端 390px 布局正常；桌面 1440px 无横向滚动。
- [ ] 页面无 console error；首屏 LCP 资源 < 1MB（不含音频/封面）。

## 点歌反馈、独立房主与投票删除（2026-11-07）

- 带 `req` 的成功写操作返回 `{t:"ok",req,message}`；失败仍为 `error`。点歌界面等待服务端入队确认后提示成功，保留歌曲时长、歌手和封面；批量点歌按确认结果计数，遇错停止。
- `user.search {q,page,size}` 返回 `{t:"user.search",req,users:[{id,nick,avatar}],total}`；用户选择账号后通过 `user.playlists {uid}` 打开公开歌单，再通过 `playlist.get` 选择单曲。同名用户由 ID 区分。
- 创建房间可传 `ownerPassword`（1–64 字；留空时随机生成）。创建响应额外返回 `ownerPassword`，只返回给创建者，公开房间列表、快照均不包含它。创建者标签页在 sessionStorage 保存本房管理密码并自动认证。房间为内存状态，随空置回收或服务重启消失。
- `auth {role:"root",password}` 接受本房管理密码或服务器 root 口令。服务器 admin 口令保持原有权限。普通房间的 root 可用 `room.password {password}` 修改本房管理密码；内置电台拒绝此操作。改密后现有已认证连接保持权限，新认证使用新密码；服务器配置不变。
- 每个队列项带唯一 `queueId`；同歌多次入队也各自计票。`queue.remove.vote {id:<queueId>}` 对待播项投票，同一连接限一票，达到 `ceil(在线人数 × voteSkipRatio)` 即删除。广播中的待播项含 `removeVotes/removeNeed`。当前歌曲继续使用 `skip.vote`；禁切歌同时禁止普通听众投票删除。
- 删除/清空/开始播放后丢弃该项删除票，断线时移除对应连接的票。人数变化更新阈值，在下次有效投票时判断通过。
- 播放器在用户希望播放但超过 12 秒无进度时重载音源，沿用等价 CDN 重试，最多自动恢复两次；手动暂停和浏览器禁止自动播放时不触发。单个客户端的卡顿不自动切换整个房间。周期同步仅向已缓冲位置纠偏；重新载入和手动恢复允许强制对齐。
