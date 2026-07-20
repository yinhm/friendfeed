import { readFile } from 'node:fs/promises';

const css = await readFile('build/static/css/bundle.min.css', 'utf8');
const pageCss = await readFile('../static/css/style.css', 'utf8');
const requiredSelectors = [
  'bg-background',
  'text-foreground',
  'border-border',
  'bg-popover',
  'px-\\[96px\\]',
  'max-lg\\:hidden',
  'data-\\[state\\=open\\]\\:animate-in',
  'data-\\[state\\=checked\\]\\:bg-primary',
  'aria-checked\\:bg-accent',
  'focus-visible\\:ring-2',
  'dark\\:bg-primary\\/40',
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

const unsafeLegacySelectors = [
  /^button\s*,/m,
  /^input\s*,/m,
  /^textarea\s*,/m,
  /^select\s*\{/m,
  /^a\s*\{/m,
];
if (unsafeLegacySelectors.some((selector) => selector.test(pageCss))) {
  throw new Error('Page CSS contains a global selector that overrides component utilities');
}
