import { defineConfig } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import { fileURLToPath } from 'node:url';

const root = fileURLToPath(new URL('.', import.meta.url));
const output = fileURLToPath(new URL('./dist/', import.meta.url));

export default defineConfig({
  root,
  plugins: [svelte()],
  server: {
    proxy: {
      '/api': {
        target: process.env.MEERKAT_DEV_API || 'https://localhost:18443',
        changeOrigin: true,
        secure: false,
      },
    },
  },
  build: {
    outDir: output,
    emptyOutDir: true,
    assetsDir: 'assets',
  },
});
