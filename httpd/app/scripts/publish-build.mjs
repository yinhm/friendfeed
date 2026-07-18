import { cp, mkdir, readdir, rm } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const buildDir = path.join(appDir, 'build');
const staticDir = path.resolve(appDir, '..', 'static');

async function cleanPublishedDir(name, keep = new Set()) {
  const dir = path.join(staticDir, name);
  await mkdir(dir, { recursive: true });
  const entries = await readdir(dir);
  await Promise.all(entries
    .filter((entry) => !keep.has(entry))
    .map((entry) => rm(path.join(dir, entry), { recursive: true, force: true })));
}

await mkdir(staticDir, { recursive: true });
await cleanPublishedDir('js');
await cleanPublishedDir('css', new Set(['style.css']));
await cp(path.join(buildDir, 'static'), staticDir, { recursive: true });
