import { defineConfig, loadEnv } from 'vite';
import { svelte } from '@sveltejs/vite-plugin-svelte';
import tailwindcss from '@tailwindcss/vite';
import Icons from 'unplugin-icons/vite';
import { fileURLToPath } from 'node:url';
import path from 'node:path';

const root = fileURLToPath(new URL('.', import.meta.url));
const envDir = fileURLToPath(new URL('..', import.meta.url));
const output = fileURLToPath(new URL('./dist/', import.meta.url));

export default defineConfig(({ mode }) => {
  const apiTarget = loadEnv(mode, envDir, '').MEERKAT_DEV_API || 'https://localhost:18443';

  return {
    root,
    envDir,
    plugins: [tailwindcss(), svelte(), Icons({ compiler: 'svelte' })],
    resolve: {
      alias: { $lib: path.resolve(root, 'src/lib') },
    },
    server: {
      proxy: {
        '/api': {
          target: apiTarget,
          changeOrigin: true,
          secure: false,
        },
      },
    },
    build: {
      outDir: output,
      emptyOutDir: true,
      assetsDir: 'assets',
      // CSP(img-src 'self')가 data: URI를 막으므로 작은 파일도 인라인하지 않습니다.
      assetsInlineLimit: 0,
    },
  };
});
