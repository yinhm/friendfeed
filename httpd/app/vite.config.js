/// <reference types="vitest/config" />
import path from 'node:path';
import { fileURLToPath } from 'node:url';
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

const srcDir = path.resolve(path.dirname(fileURLToPath(import.meta.url)), 'src');

export default defineConfig(({ mode }) => ({
  plugins: [react()],
  resolve: {
    alias: {
      '@': srcDir,
      components: path.join(srcDir, 'components'),
      styles: path.join(srcDir, 'styles'),
    },
  },
  build: {
    outDir: 'build',
    emptyOutDir: true,
    sourcemap: mode === 'development',
    rollupOptions: {
      // No index.html: the Go templates are the HTML shell.
      input: path.join(srcDir, 'index.jsx'),
      output: {
        // Mirror the CRA layout (build/static/js, build/static/css) that the
        // Go server and scripts/publish-build.mjs rely on.
        entryFileNames: 'static/js/[name].js',
        chunkFileNames: 'static/js/[name]-[hash].js',
        assetFileNames: 'static/[ext]/[name][extname]',
      },
    },
  },
  test: {
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
