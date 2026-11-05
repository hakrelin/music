#!/usr/bin/env bash
set -e

# 1) 回滚 compose（去掉静态挂载与环境变量）
cd /root/Jusic-Serve-Houses-cleaned
if [ -f /root/jusic-backup/docker-compose.yml.bak ]; then
  cp -a /root/jusic-backup/docker-compose.yml.bak docker-compose.yml
  echo "[1] compose 已还原"
fi
grep -c "jusic-static\|STATIC_LOCATIONS" docker-compose.yml || echo "    确认：已无挂载/环境变量"

docker compose up -d service_jusicServeHouses 2>&1 | tail -5

# 2) 把 jar 里的官方静态资源解出来（后面在它上面加功能）
mkdir -p /opt/jusic-live
python3 - <<'PY'
import zipfile, os
z = zipfile.ZipFile('/tmp/live.jar')
prefix = 'BOOT-INF/classes/static/'
out = '/opt/jusic-live'
n = 0
for name in z.namelist():
    if not name.startswith(prefix) or name.endswith('/'):
        continue
    rel = name[len(prefix):]
    dst = os.path.join(out, rel)
    os.makedirs(os.path.dirname(dst), exist_ok=True)
    with open(dst, 'wb') as f:
        f.write(z.read(name))
    n += 1
print("[2] 解出静态文件:", n)
PY
du -sh /opt/jusic-live
ls -1 /opt/jusic-live
