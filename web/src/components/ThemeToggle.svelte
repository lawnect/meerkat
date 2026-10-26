<script lang="ts">
  import { translations } from '../i18n.svelte';
  import { WeatherMoon, WeatherSunny } from '../icons';
  import { setTheme, theme } from '../theme.svelte';
  import IconButton from './IconButton.svelte';

  let { side = 'bottom', align = 'center' }: { side?: 'top' | 'bottom' | 'left' | 'right'; align?: 'start' | 'center' | 'end' } = $props();

  const t = translations({ toDark: 'Dark theme', toLight: 'Light theme' }, { toDark: '다크 테마', toLight: '라이트 테마' });
  const isDark = $derived(theme.current === 'dark');
</script>

<IconButton {side} {align} label={isDark ? t().toLight : t().toDark} icon={isDark ? WeatherSunny : WeatherMoon} onclick={() => setTheme(isDark ? 'light' : 'dark')} />
