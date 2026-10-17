import type { SvelteConfig } from '@sveltejs/vite-plugin-svelte';

const config: SvelteConfig = {
  compilerOptions: {
    warningFilter: (warning) => warning.code !== 'a11y_no_noninteractive_tabindex',
  },
};

export default config;
