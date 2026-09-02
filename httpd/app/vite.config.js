import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import { configDefaults } from 'vitest/config';
import react from '@vitejs/plugin-react';
import tailwindcss from '@tailwindcss/vite';

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), 'src');

export default defineConfig(({ mode }) => ({
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      '@': srcDir,
      components: path.join(srcDir, 'components'),
      styles: path.join(srcDir, 'styles'),
    },
  },
  build: {
    manifest: 'static/manifest.json',
    outDir: 'build',
    emptyOutDir: true,
    sourcemap: mode === 'development',
    // Keep all CSS in the single entry stylesheet: chunk-split CSS files
    // would not be picked up by the Go templates / publish script.
    cssCodeSplit: false,
    rollupOptions: {
      // No index.html: the Go templates are the HTML shell.
      input: path.join(srcDir, 'index.jsx'),
      output: {
        // Mirror the CRA layout (build/static/js, build/static/css) that the
        // Go server and scripts/publish-build.mjs rely on.
        // Emit the exact URL loaded by the templates. Lazy chunks import this
        // same module, avoiding a second evaluation of the React runtime.
        entryFileNames: 'static/js/bundle-[hash].min.js',
        chunkFileNames: 'static/js/[name]-[hash].js',
        assetFileNames: (assetInfo) =>
          // The merged CSS must keep the deterministic name templates expect —
          // and must NOT be Vite's default "style.css", which would clobber
          // our hand-written static/css/style.css during publish.
          assetInfo.name?.endsWith('.css')
            ? 'static/css/bundle-[hash].min.css'
            : 'static/[ext]/[name][extname]',
      },
    },
  },
  test: {
    coverage: {
      provider: 'v8',
      reportsDirectory: 'coverage',
      reporter: ['text', 'json-summary', 'html'],
      include: ['src/**/*.{js,jsx,ts,tsx}'],
      exclude: [
        'src/**/*.test.{js,jsx,ts,tsx}',
        'src/setupTests.js',
        'src/vite-env.d.ts',
      ],
    },
    exclude: [...configDefaults.exclude, 'e2e/**'],
    environment: 'jsdom',
    globals: true,
    setupFiles: './src/setupTests.js',
    server: {
      deps: {
        // plate-emoji imports @emoji-mart JSON without import attributes,
        // react-tweet/react-lite-youtube-embed import CSS; run them through
        // Vite's pipeline instead of externalizing them.
        inline: ['@udecode/plate-emoji', /@emoji-mart/, 'react-tweet', 'react-lite-youtube-embed'],
      },
    },
  },
}));
