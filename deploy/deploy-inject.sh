#!/usr/bin/env bash
set -e

# 1) 用官方旧版静态资源做基底
rm -rf /opt/jusic-static
cp -a /opt/jusic-live /opt/jusic-static
cp -f /tmp/plus.js /opt/jusic-static/plus.js
chmod -R a+r /opt/jusic-static

# 2) 往 index.html 末尾挂上增强脚本
python3 - <<'PY'
p = '/opt/jusic-static/index.html'
html = open(p, encoding='utf-8').read()
tag = '<script src="plus.js" defer></script>\n</body>'
if 'plus.js' not in html:
    html = html.replace('</body>', tag, 1)
    open(p, 'w', encoding='utf-8').write(html)
    print('[2] index.html 已注入 plus.js')
else:
    print('[2] index.html 已有 plus.js')
PY

ls -l /opt/jusic-static/index.html /opt/jusic-static/plus.js
grep -o 'plus.js' /opt/jusic-static/index.html | head -2

# 3) 重新打开挂载（compose 由备份恢复过，重新加两行）
cd /root/Jusic-Serve-Houses-cleaned
if ! grep -q "jusic-static" docker-compose.yml; then
  sed -i 's#^    image: jumpalang/jusic_serve_houses:5.1$#    image: jumpalang/jusic_serve_houses:5.1\n    volumes:\n      - /opt/jusic-static:/app/static:ro#' docker-compose.yml
fi
if ! grep -q "STATIC_LOCATIONS" docker-compose.yml; then
  sed -i 's#^      - QqRefreshInterval=86000000$#      - QqRefreshInterval=86000000\n      - SPRING_RESOURCES_STATIC_LOCATIONS=file:/app/static/#' docker-compose.yml
fi
grep -n "jusic-static\|STATIC_LOCATIONS" docker-compose.yml

docker compose up -d service_jusicServeHouses 2>&1 | tail -4
