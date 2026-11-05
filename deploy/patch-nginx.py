#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""在每个 listen 443 的 server 块里加 /plus-api/ 反代（幂等，跳过已有）"""
import glob
import re

LOC = ('    location /plus-api/ {\n'
       '        proxy_pass http://127.0.0.1:18099/;\n'
       '        proxy_set_header Host $host;\n'
       '        proxy_read_timeout 30s;\n'
       '    }\n')

patched = 0
for f in glob.glob('/etc/nginx/conf.d/*.conf'):
    src = open(f, encoding='utf-8').read()
    if 'location /plus-api/' in src and src.count('location /plus-api/') >= 2:
        print('[skip] already full:', f)
        continue
    lines = src.split('\n')
    out = []
    in_block = False
    block_has_443 = False
    block_has_api = False
    for line in lines:
        # 进入 server 块
        if re.match(r'^\s*server\s*\{', line):
            in_block = True
            block_has_443 = False
            block_has_api = False
            out.append(line)
            continue
        if in_block:
            if 'listen' in line and '443' in line:
                block_has_443 = True
            if 'location /plus-api/' in line:
                block_has_api = True
            # 在 include jusic-proxy 之前插入
            if block_has_443 and not block_has_api and 'include /etc/nginx/snippets/jusic-proxy.inc;' in line:
                out.append(LOC.rstrip('\n'))
                block_has_api = True
                patched += 1
                print('[patch]', f, '-> 插入到 443 块')
            if line.strip() == '}':
                in_block = False
        out.append(line)
    open(f, 'w', encoding='utf-8').write('\n'.join(out))

print('[done] inserted =', patched)
