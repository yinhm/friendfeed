import { readFile } from 'node:fs/promises';
import { gzipSync } from 'node:zlib';

const manifest = JSON.parse(await readFile('build/static/manifest.json', 'utf8'));

// Budgets include modest headroom over the 2026-07-22 production baseline.
// Raising one requires reviewing both the raw and compressed size change.
// 2026-07-25: entry CSS raw 70,000 -> 72,000 for the React account pages
// (profile/import); reviewed: +1,035 raw / +247 gzip over baseline.
// 2026-07-30: entry CSS raw 72,000 -> 73,000, gzip 14,000 -> 14,200 for the
// Plate 49 toolbar registry migration (focus ring, toolbar-group separators,
// size-4 icon convention); reviewed: +1,315 raw / +197 gzip over the 71,135
// baseline, after dropping the dead aria-invalid rules from the official base.
// 2026-08-29: authenticated account, Group, notification, and profile relation pages moved to
// route-level lazy chunks. Reset entry JS to the 217,648-byte baseline plus
// roughly 10% headroom and add explicit budgets for each route group.
const budgets = [
  {
    name: 'entry JS',
    manifestKey: 'src/index.jsx',
    maxBytes: 240_000,
    maxGzipBytes: 74_000,
  },
  {
    name: 'account pages JS',
    manifestKey: 'src/account-pages.jsx',
    maxBytes: 16_000,
    maxGzipBytes: 5_000,
  },
  {
    name: 'Group pages JS',
    manifestKey: 'src/group-pages.jsx',
    maxBytes: 12_000,
    maxGzipBytes: 3_000,
  },
  {
    name: 'notification pages JS',
    manifestKey: 'src/notification-pages.jsx',
    maxBytes: 4_000,
    maxGzipBytes: 1_500,
  },
  {
    name: 'profile relations JS',
    manifestKey: 'src/profile-relations.jsx',
    maxBytes: 4_000,
    maxGzipBytes: 1_500,
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
    maxBytes: 73_000,
    maxGzipBytes: 14_200,
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
