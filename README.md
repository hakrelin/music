# 茶与月色 · TEA & MOONLIGHT

> 在幻想乡，一起听一首歌。

「茶与月色」是 **一起听歌吧 / Jusic** 的完全重写版：一间线上点歌房，房间里所有人同步听同一首歌。
前端从头重写为 Vue 3 SPA，后端换成 **Go 单二进制**（内嵌前端、原生 WebSocket），
**不再依赖**官方 Java jar、Docker、SockJS/STOMP、Node 音乐服务。

- 视觉：墨绿 / 米白 / 浅茶绿，宋体大标题，东方 Project 同人插画（见文末致谢）
- 部署：一个二进制 + 一份配置 + systemd，没有容器

![茶与月色 · 大厅](docs/screenshots/home.jpg)

| 房间（桌面） | 移动端 |
|---|---|
| ![房间](docs/screenshots/room.jpg) | ![移动端](docs/screenshots/room-mobile.jpg) |

---

## 功能

| 模块 | 内容 |
|---|---|
| 大厅 | 房间列表与在线人数、开房（可设密码）、常驻「东方电台」 |
| 房间 | 同步播放（服务器时钟校准，偏差 ≤1.5s）、滚动歌词、点歌队列、投票切歌、点赞、音量、聊天（图片链接自动展开） |
| 点歌 | 网易云搜索、歌单搜索、歌单 ID / 分享链接导入（单次最多 20 首批量加入）、东方电台预设 |
| 管理 | 房主 / 管理员口令；置顶、删除、清空队列、强切、禁点歌 / 禁切歌、点赞模式、房间音量 |
| 电台 | 服务端 DJ：队列不足自动从默认歌单补到 8 首，房间没人也一直放（不再需要常驻 bot） |
| 体验 | 桌面 / 移动端自适应，移动端四栏切换；键盘可访问；减少动态效果适配 |

---

## 快速开始（本地，无需 cookie）

依赖：**Go 1.25+**、**Node 20+**。

```bash
# 1. 构建前端并拷进 Go embed 目录
npm --prefix web install
npm --prefix web run build
node scripts/copy-dist.mjs

# 2. 编译单二进制
go build -o bin/musicd ./server/cmd/musicd

# 3. 跑起来
./bin/musicd            # Windows PowerShell： .\bin\musicd.exe
```

打开 <http://127.0.0.1:8080>。

没有网易云 cookie 时自动使用 **demo provider**：24 首演示曲目 + 程序合成的音频，
界面、播放、同步、点歌、聊天全部可以完整体验。Windows 也可以直接 `.\build.ps1`，
Linux/macOS 用 `make build`。

### 开发模式

```bash
# 后端
go run ./server/cmd/musicd            # :8080

# 前端（Vite HMR，/api 自动反代到 :8080）
npm --prefix web run dev              # :5173

# 只想看 UI：不进后端，用内置 mock 数据
VITE_MOCK=1 npm --prefix web run dev
```

---

## 接真实的网易云

在仓库根目录创建 `server/config.local.json`（**已在 .gitignore 中，不会入库**）：

```json
{
  "neteaseCookie": "MUSIC_U=...; __csrf=...",
  "provider": "auto"
}
```

Cookie 获取：浏览器登录 <https://music.163.com> → DevTools → Application → Cookies →
`https://music.163.com` → 复制 `MUSIC_U`（以及 `__csrf` 如果有）。
`provider: "auto"` 时有 cookie 就用网易云（`wy`），没有就用 demo。
VIP 账号才能拿到 320k 地址；普通账号/无版权曲目会在播放时被跳过并提示。

也可以全部用环境变量覆盖：`MUSIC_HTTP_ADDR`、`MUSIC_NETEASE_COOKIE`、`MUSIC_PROVIDER`、
`MUSIC_DATA_DIR`、`MUSIC_ROOT_PASSWORD`、`MUSIC_ADMIN_PASSWORD`、`MUSIC_LOG_LEVEL`、`MUSIC_CONFIG`。

> 示例配置里的默认口令（`654321` / `123456`）部署前务必改掉。

---

## 部署（无 Docker）

```bash
# 1. 构建（本机交叉编译到 Linux）
make build                       # 产物 bin/musicd
#   或：GOOS=linux GOARCH=amd64 make build

# 2. 服务器上
useradd -r -s /usr/sbin/nologin -d /var/lib/musicd musicd
install -m 0755 bin/musicd /usr/local/bin/musicd
install -d -o musicd -g musicd /var/lib/musicd /etc/musicd
install -m 0600 server/config.local.json /etc/musicd/config.json
install -m 0644 deploy/musicd.service /etc/systemd/system/musicd.service
systemctl daemon-reload && systemctl enable --now musicd

# 3. nginx（关键：WebSocket 的 Upgrade 头 + 长 read_timeout）
cp deploy/nginx.conf.example /etc/nginx/conf.d/music.conf
# 改 server_name / 证书路径后：
nginx -t && systemctl reload nginx
```

更新版本：替换 `/usr/local/bin/musicd` 后 `systemctl restart musicd`。
**前端已经 embed 进二进制**，不需要单独发静态文件。

---

## 架构

```
浏览器（Vue 3 SPA）
   │  HTTP /api/*            房间列表、开房、配置
   │  WebSocket /api/ws      进入房间后的全部实时状态
   ▼
musicd（Go 单二进制，内嵌 web/dist）
   ├─ hub        房间表、队列状态机、服务器时钟推进、投票/点赞、权限、电台 DJ
   ├─ httpapi    REST + WS 升级 + 静态资源（SPA fallback）
   └─ provider   wy = 网易云 weapi（纯标准库加密）/ demo = 内置演示曲目与合成音频
```

完整协议（HTTP 接口、WS 消息、数据模型、配置字段、验收清单）见
[`docs/architecture.md`](docs/architecture.md)。

---

## 目录

```
web/                    Vue 3 + TS + Vite 前端（唯一前端真源）
  src/styles/tokens.css 设计 token（与参考站同一套墨绿/米白/浅茶绿）
  src/views/            大厅与房间
  src/components/room/  播放器、歌词、队列、聊天、点歌面板、设置
server/                 Go 后端
  cmd/musicd/           入口
  internal/hub/         房间与 WS 协议
  internal/provider/    wy（网易云）与 demo
  internal/webui/       前端 embed 产物（构建时拷入）
docs/architecture.md    架构契约
deploy/                 systemd unit / nginx 示例
scripts/copy-dist.mjs   构建步骤：web/dist → server/internal/webui/dist
Makefile / build.ps1    构建入口
ui/ plus/ bot/          旧版实现（已停用，仅作参考）
```

### 测试

```bash
go test ./server/...           # 后端单测（含网易云在线测试，无网络会 skip）
npm --prefix web run typecheck # 前端类型检查
npm --prefix web run build     # 前端生产构建
```

---

## 旧版说明

仓库里的 `ui/`、`plus/`、`bot/` 是上一代实现：官方 Vue2 界面 + 运行时注入脚本 + Java jar + Docker。
新版已经在功能上完全覆盖（并补上了原来需要常驻 bot 的东方电台），这些目录保留仅作参考，**不再部署**。

---

## 致谢与声明

- 首屏 / 夜色 / 暮色插画来自 Pixiv 作者 **うさほり**、**みるるん**、**みふる**，
  版权归原作者所有，本站仅作视觉呈现；如需商用请另行取得授权。
- 东方 Project 二次创作，非官方粉丝站点。
- 歌曲、封面与歌词来自网易云音乐，版权归各自权利人；本项目**不含任何音乐文件**，
  也不破解付费内容，播放地址由部署方自己的账号凭据获取。
