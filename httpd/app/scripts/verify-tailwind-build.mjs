import { readFile } from 'node:fs/promises';

// Entry asset names are content-hashed; resolve them from the Vite manifest.
const manifest = JSON.parse(await readFile('build/static/manifest.json', 'utf8'));
const cssAsset = manifest['style.css']?.file;
const jsAsset = manifest['src/index.jsx']?.file;
if (!cssAsset || !jsAsset) {
  throw new Error('build/static/manifest.json is missing entry js/css assets');
}

const css = await readFile(`build/${cssAsset}`, 'utf8');
const pageCss = await readFile('../static/css/style.css', 'utf8');
const requiredSelectors = [
  'bg-background',
  'text-foreground',
  'border-border',
  'bg-popover',
  'min-h-\\[60px\\]',
  'max-lg\\:hidden',
  'data-\\[state\\=open\\]\\:animate-in',
  'data-\\[state\\=checked\\]\\:bg-primary',
  'aria-checked\\:bg-accent',
  'focus-visible\\:ring-2',
  'print\\:hidden',
  '\\[\\&_\\>_iframe\\]\\:absolute',
];

const missing = requiredSelectors.filter((selector) => !css.includes(selector));
if (missing.length > 0) {
  throw new Error(`Tailwind build is missing selectors: ${missing.join(', ')}`);
}

if (css.includes('@tailwind')) {
  throw new Error('Tailwind directives were not compiled');
}

// Site CSS must remain in its named cascade layer. This is the contract that
// lets semantic FriendFeed rules restore Preflight without unexpectedly
// defeating explicit component utilities.
if (!/^@layer site\s*\{/m.test(pageCss)) {
  throw new Error('Page CSS must be wrapped in @layer site');
}

const unsafeLegacySelectors = [
  /^button\s*,/m,
  /^input\s*,/m,
  /^textarea\s*,/m,
  /^select\s*\{/m,
  /^a\s*\{/m,
  /^img\s*\{/m,
  /^table\s*\{/m,
];
if (unsafeLegacySelectors.some((selector) => selector.test(pageCss))) {
  throw new Error('Page CSS contains an unscoped global selector that can override components');
}
