import { readFile } from 'node:fs/promises';
import { gzipSync } from 'node:zlib';

const manifest = JSON.parse(await readFile('build/static/manifest.json', 'utf8'));

// Budgets include modest headroom over the 2026-07-22 production baseline.
// Raising one requires reviewing both the raw and compressed size change.
// 2026-07-25: entry CSS raw 70,000 -> 72,000 for the React account pages
// (profile/import); reviewed: +1,035 raw / +247 gzip over baseline.
const budgets = [
  {
    name: 'entry JS',
    manifestKey: 'src/index.jsx',
    maxBytes: 245_000,
    maxGzipBytes: 75_000,
  },
  {
    name: 'editor JS',
    manifestKey: 'src/editor.jsx',
    maxBytes: 1_500_000,
    maxGzipBytes: 500_000,
  },
  {
    name: 'static renderer JS',
    manifestName: 'server.browser',
    maxBytes: 200_000,
    maxGzipBytes: 62_000,
  },
  {
    name: 'entry CSS',
    manifestKey: 'style.css',
    maxBytes: 72_000,
    maxGzipBytes: 14_000,
  },
];

let failed = false;
for (const budget of budgets) {
  const asset = budget.manifestKey
    ? manifest[budget.manifestKey]
    : Object.values(manifest).find((entry) => entry.name === budget.manifestName);
  const file = asset?.file;
  if (!file) {
    throw new Error(`Vite manifest is missing ${budget.name}`);
  }

  const content = await readFile(`build/${file}`);
  const bytes = content.byteLength;
  const gzipBytes = gzipSync(content, { level: 9 }).byteLength;
  console.log(
    `${budget.name}: ${bytes}/${budget.maxBytes} bytes, ` +
      `${gzipBytes}/${budget.maxGzipBytes} gzip bytes`
  );

  if (bytes > budget.maxBytes || gzipBytes > budget.maxGzipBytes) {
    failed = true;
  }
}

if (failed) {
  throw new Error('production bundle exceeds its size budget');
}
