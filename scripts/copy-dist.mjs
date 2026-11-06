// Copy the Vite build into the Go embed directory, including on Windows.
import { access, mkdir, readdir, readFile, writeFile, rm } from 'node:fs/promises';
import { dirname, join, resolve, relative, isAbsolute } from 'node:path';
import { fileURLToPath } from 'node:url';
const root = dirname(dirname(fileURLToPath(import.meta.url)));
const source = join(root, 'web', 'dist');
const target = resolve(root, 'server', 'internal', 'webui', 'dist');
const within = relative(root, target);
if (within.startsWith('..') || isAbsolute(within)) throw new Error('Embed output must stay inside the repository');
await access(join(source, 'index.html')).catch(() => { throw new Error('Run npm --prefix web run build first'); });
await mkdir(target, { recursive: true });
for (const entry of await readdir(target)) {
  const output = resolve(target, entry);
  if (!relative(target, output).startsWith('..') && output !== target) await rm(output, { recursive: true, force: true });
}
async function copyTree(from, to) {
  await mkdir(to, { recursive: true });
  for (const entry of await readdir(from, { withFileTypes: true })) {
    if (entry.isDirectory()) await copyTree(join(from, entry.name), join(to, entry.name));
    else if (entry.isFile()) await writeFile(join(to, entry.name), await readFile(join(from, entry.name)));
  }
}
await copyTree(source, target);
console.log('[copy-dist] Frontend copied into Go embed directory');
