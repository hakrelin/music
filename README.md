# music —— 一起听歌吧（Jusic）二次开发：旧版 UI 增强 + 歌单导入

在官方 **jusic_serve_houses:5.1**（2024-11 构建）之上做的一层增强，**不改后端、不改它的打包产物**，
用「静态资源挂载 + 运行时注入脚本」的方式加功能。线上已经在跑这套。

```
浏览器（官方旧版 UI）
  ├─ js/app.e615c8b3.js   官方打包产物，原样不动
  ├─ js/plus.js           ← 本仓库 plus/jusic-plus.js，运行时注入的增强脚本
  └─ /plus-api/tracks     ← nginx 反代到本仓库 plus/tracks_api.py（只读取歌单曲目）
                                └→ jusicMusicApi:3000 /playlist/detail
```

---

## 功能

| 功能 | 位置 | 说明 |
|---|---|---|
| **昵称（全局自动）** | 顶栏右上角胶囊 | 首次进站自动生成 `游客XXXX`，存 localStorage，连上房间自动 `/setting/name`；点击可改，改完立刻同步 |
| **导入歌单 → 点歌队列** | 顶栏「导入歌单」弹窗 | 关键词搜歌单 / 按用户昵称或用户ID查他的歌单 / 直接填歌单 ID → 取该歌单曲目，**逐首 `/music/pick` 进点歌队列**（不是默认列表） |
| **批量点歌** | 同上 | 一次最多 20 首（后端队列上限 `PlaylistSize=23`），多歌单平均分配名额 |
| **东方电台预设** | 弹窗底部 | 内置 6 个东方 Project 歌单，一键把曲目加进队列 |
| **权限认证** | 弹窗顶部 | 房主 / 管理员口令 → `/auth/root`、`/auth/admin`；点歌不需要权限，清空队列需要 |
| **快捷键** | 全局 | `空格` / `→` 投票切歌，`R` 重载当前 |
| **播放自愈** | 后台无界面 | `http://` 音源自动升 `https://`、`error/stalled` 重试两次后自动切歌、9 秒无进度判卡死、提前预热下一首 |

---

## 目录

```
plus/jusic-plus.js     注入脚本（前端全部功能，17 KB 单文件，无构建）
plus/tracks_api.py     歌单曲目接口（Python 3 标准库，无依赖）
deploy/                部署脚本
  deploy-inject.sh     把 plus.js 注入到挂载目录的 index.html
  deploy-api.sh        安装 plus-api 服务（systemd）
  patch-nginx.py       给所有 listen 443 的 server 块加 /plus-api/ 反代
  rollback.sh          还原成纯官方静态
bot/touhou-radio.js    东方电台常驻 bot（需改成"补队列"版本见 README 末尾）
ui/                    官方前端源码（Vue2 + muse-ui，供参考/大改时用）
```

---

## 部署（目标机器上有 jusic 容器 + 宝塔 + nginx）

### 1. 把官方静态资源解出来做基底

```bash
docker cp jusicServeHouses:/app/jusic-serve.jar /tmp/live.jar
mkdir -p /opt/jusic-live
python3 - <<'PY'
import zipfile, os
z = zipfile.ZipFile('/tmp/live.jar'); p = 'BOOT-INF/classes/static/'
for n in z.namelist():
    if n.startswith(p) and not n.endswith('/'):
        dst = os.path.join('/opt/jusic-live', n[len(p):])
        os.makedirs(os.path.dirname(dst), exist_ok=True)
        open(dst, 'wb').write(z.read(n))
PY
```

### 2. 准备挂载目录 + 注入脚本

```bash
rm -rf /opt/jusic-static/*
cp -a /opt/jusic-live/. /opt/jusic-static/
cp plus/jusic-plus.js /opt/jusic-static/js/plus.js
python3 - <<'PY'
p='/opt/jusic-static/index.html'
h=open(p,encoding='utf-8').read()
if 'js/plus.js' not in h:
    h=h.replace('</body>','<script src="js/plus.js" defer></script>\n</body>',1)
    open(p,'w',encoding='utf-8').write(h)
PY
```

### 3. 给容器加挂载 + 静态目录指向

`docker-compose.yml` 的 `service_jusicServeHouses`：

```yaml
    volumes:
      - /opt/jusic-static:/app/static:ro
    environment:
      - SPRING_RESOURCES_STATIC_LOCATIONS=file:/app/static/
```

```bash
docker compose up -d --force-recreate service_jusicServeHouses
```

### 4. 装歌单曲目接口

```bash
mkdir -p /opt/plus-api && cp plus/tracks_api.py /opt/plus-api/
# systemd 单元见 deploy/deploy-api.sh，默认监听 127.0.0.1:18099
bash deploy/deploy-api.sh
```

### 5. nginx 反代（两个域名 + IP 的 443 块都要加）

```bash
python3 deploy/patch-nginx.py && nginx -t && systemctl reload nginx
```

```nginx
location /plus-api/ {
    proxy_pass http://127.0.0.1:18099/;
    proxy_set_header Host $host;
    proxy_read_timeout 30s;
}
```

---

## 后端接口备忘（照这个写就不会踩坑）

| 操作 | 指令 | 报文 |
|---|---|---|
| 点歌 | `/music/pick` | `{name, id, source:"wy", quality:"320k", sendTime}` |
| 搜歌单 | `/music/searchsonglist` | `{name:<关键词>, source:"wy", pageIndex:1, pageSize:10}` |
| 按用户查他的歌单 | `/music/searchsonglist` | `{name:<用户ID>, source:"wy_user", pageIndex:1, pageSize:10}` |
| 搜用户 | `/music/searchuser` | `{nickname:<昵称>, source:"wy", pageIndex:1, pageSize:10}` |
| 房主认证 | `/auth/root` | `{password}` |
| 清空点歌队列 | `/music/clear` | 空 |
| 默认列表（本仓库不用） | `/music/setDefaultPlaylist` | `{id:"歌单ID,逗号分隔", source:"wy"}` |

**四个必须知道的坑**

1. `source` 只能是 **`wy`**（不是 `netease`），写成 `netease` 会让后端 `setDefaultPlaylist` 抛 NPE。
2. 分页字段是 **`pageIndex`**，不是 `page`。
3. 队列上限 `PlaylistSize=23`（compose 里配的），超了后端直接拒。
4. `GET /house/search` 返回 **401**，旧 jar 只映射了 **POST**。

结果通过 SockJS 的裸消息下发，类型：`SEARCH_USER` / `SEARCH_SONGLIST` / `MUSIC` / `PICK` / `NOTICE`。
旧版 store 里已经存好了（`getSearchDataUser` / `getSearchDataGd`），直接读 store 比钩 `onmessage` 稳。

---

## 已知坑（部署侧）

- 旧 jar 的 Spring Security 白名单只放行 `/js/** /css/** /img/** /index.html /favicon.ico`，
  **放根目录的 `plus.js` 会 401**，必须放 `js/` 下。
- SockJS **不能带 `?houseId=`**，query 会被拼进路径导致握手失败；进房间走连上默认房后再发 STOMP `/house/enter`。
- 容器 bind mount 的目录**不能 `rm -rf` 重建**（inode 换掉后容器看不到新文件），只能清空内容再拷。
- 8899 端口常被宝塔占，plus-api 用 18099。

---

## 协议声明

本项目只做**界面增强与播放入口整理**，不含任何音乐文件，也不破解任何付费内容。
播放地址由部署方自己的 jusic 后端提供，登录态（网易云 cookie）属于部署方本人。

---

## 待办：东方电台 24h 常驻

`bot/touhou-radio.js` 现在还是按「默认列表」写的旧版。正确的做法是：

```
每 30 秒检查一次队列长度（store.getPlayerPick / 或后端 playlistSize）
低于 5 首时，从 /plus-api/tracks 取东方歌单曲目，逐首 /music/pick 补到 ~20 首
同时保持一个 WS 连接（房间没人 20 秒会被回收）
```

欢迎 PR。
