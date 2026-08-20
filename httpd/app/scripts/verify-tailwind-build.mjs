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

// Bare element selectors in the site layer can affect any React component.
// Catch both a standalone selector (`button {`) and the first selector in a
// comma-separated group (`button,`) while still allowing scoped selectors
// such as `button.inline-action` or `.entry img`.
const unsafeLegacySelector =
  /^\s*(?:button|input|textarea|select|a|img|table)\s*(?:,|\{)/m;
if (unsafeLegacySelector.test(pageCss)) {
  throw new Error('Page CSS contains an unscoped global selector that can override components');
}
