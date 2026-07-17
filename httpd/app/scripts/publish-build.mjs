import { cp, mkdir } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const buildDir = path.join(appDir, 'build');
const staticDir = path.resolve(appDir, '..', 'static');

await mkdir(staticDir, { recursive: true });
await cp(path.join(buildDir, 'static'), staticDir, { recursive: true });

// Vite emits deterministic names (static/js/index.js, static/css/index.css);
// the Go templates reference fixed bundle paths.
const mainJs = path.join(buildDir, 'static', 'js', 'index.js');
await cp(mainJs, path.join(staticDir, 'js', 'bundle.min.js'));

const mainCss = path.join(buildDir, 'static', 'css', 'index.css');
await cp(mainCss, path.join(staticDir, 'css', 'bundle.min.css'));
