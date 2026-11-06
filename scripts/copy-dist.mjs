// 把 web/dist 拷贝到 server/internal/webui/dist，供 //go:embed 打包。
// 用法：node scripts/copy-dist.mjs
import { cpSync, existsSync, rmSync, mkdirSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const root = dirname(dirname(fileURLToPath(import.meta.url)));
const src = join(root, 'web', 'dist');
const dst = join(root, 'server', 'internal', 'webui', 'dist');

if (!existsSync(src)) {
  console.error(`[copy-dist] 找不到 ${src}，先运行 npm --prefix web run build`);
  process.exit(1);
}

rmSync(dst, { recursive: true, force: true });
mkdirSync(dst, { recursive: true });
cpSync(src, dst, { recursive: true });

console.log(`[copy-dist] ${src} -> ${dst}`);
