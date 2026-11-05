#!/usr/bin/env bash
set -e

# 1) 装 plus-api
mkdir -p /opt/plus-api
cp -f /tmp/tracks_api.py /opt/plus-api/tracks_api.py
chmod 755 /opt/plus-api/tracks_api.py

cat > /etc/systemd/system/plus-api.service <<'UNIT'
[Unit]
Description=jusic plus-api (playlist tracks for injected UI)
After=network.target docker.service

[Service]
Type=simple
ExecStart=/usr/bin/python3 /opt/plus-api/tracks_api.py
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
UNIT

systemctl daemon-reload
systemctl enable --now plus-api.service
sleep 2
systemctl is-active plus-api.service
curl -s http://127.0.0.1:8899/healthz; echo
curl -s "http://127.0.0.1:8899/tracks?id=92775631&limit=3" | head -c 300; echo

# 2) nginx 加 /plus-api/ 反代（只在 zjz.tea-sugar.xyz 的 443 块里加）
python3 - <<'PY'
import glob, re
patched = 0
for f in glob.glob('/etc/nginx/conf.d/*.conf'):
    s = open(f, encoding='utf-8').read()
    if 'zjz.tea-sugar.xyz' not in s or '/plus-api/' in s:
        continue
    block = ('    location /plus-api/ {\n'
             '        proxy_pass http://127.0.0.1:8899/;\n'
             '        proxy_set_header Host $host;\n'
             '        proxy_read_timeout 30s;\n'
             '    }\n')
    # 插在 server_name 之后第一个 location 之前
    idx = s.find('server_name zjz.tea-sugar.xyz;')
    loc = s.find('location', idx if idx >= 0 else 0)
    if loc < 0:
        continue
    s = s[:loc] + block + s[loc:]
    open(f, 'w', encoding='utf-8').write(s)
    patched += 1
    print('[nginx] patched', f)
print('[nginx] files patched =', patched)
PY

nginx -t && systemctl reload nginx && echo "[nginx] reloaded"
