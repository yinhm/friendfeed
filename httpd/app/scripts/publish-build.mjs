import { cp, mkdir, readFile } from 'node:fs/promises';
import path from 'node:path';
import { fileURLToPath } from 'node:url';

const appDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), '..');
const buildDir = path.join(appDir, 'build');
const staticDir = path.resolve(appDir, '..', 'static');
const manifest = JSON.parse(
  await readFile(path.join(buildDir, 'asset-manifest.json'), 'utf8'),
);

await mkdir(staticDir, { recursive: true });
await cp(path.join(buildDir, 'static'), staticDir, { recursive: true });

const mainJs = manifest.files['main.js'];
if (!mainJs) {
  throw new Error('The frontend build did not produce a main JavaScript bundle');
}
await cp(
  path.join(buildDir, mainJs.replace(/^\//, '')),
  path.join(staticDir, 'js', 'bundle.min.js'),
);

const mainCss = manifest.files['main.css'];
if (mainCss) {
  await cp(
    path.join(buildDir, mainCss.replace(/^\//, '')),
    path.join(staticDir, 'css', 'bundle.min.css'),
  );
}
