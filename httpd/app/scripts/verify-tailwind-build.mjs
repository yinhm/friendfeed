import { readFile } from 'node:fs/promises';

const css = await readFile('build/static/css/bundle.min.css', 'utf8');
const requiredSelectors = [
  'bg-background',
  'text-foreground',
  'border-border',
  'bg-popover',
  'px-\\[96px\\]',
  'max-lg\\:hidden',
  'data-\\[state\\=open\\]\\:animate-in',
  '\\[\\&_\\>_iframe\\]\\:absolute',
];

const missing = requiredSelectors.filter((selector) => !css.includes(selector));
if (missing.length > 0) {
  throw new Error(`Tailwind build is missing selectors: ${missing.join(', ')}`);
}

if (css.includes('@tailwind')) {
  throw new Error('Tailwind directives were not compiled');
}
