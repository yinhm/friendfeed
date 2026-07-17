import { cp, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const buildDir = path.join(appDir, 'build');
const staticDir = path.resolve(appDir, '..', 'static');

await mkdir(staticDir, { recursive: true });
await cp(path.join(buildDir, 'static'), staticDir, { recursive: true });
