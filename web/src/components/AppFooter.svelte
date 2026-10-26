<script lang="ts">
  import { translations } from '../i18n.svelte';

  // 화면 맨 아래 정보 줄: 연도, 만든 곳, 소스, 버전.
  let { version }: { version?: string } = $props();

  const t = translations({
    poweredBy: 'Powered by',
    source: 'GitHub',
    newTab: '(opens in a new tab)',
  }, {
    poweredBy: 'Powered by',
    source: 'GitHub',
    newTab: '(새 탭에서 열림)',
  });
</script>

{#snippet external(href: string, label: string)}
  <a {href} target="_blank" rel="noopener noreferrer" class="text-link outline-none focus-visible:ring-3 focus-visible:ring-ring/50">{label}<span class="sr-only"> {t().newTab}</span></a>
{/snippet}

<footer class="flex flex-wrap items-center justify-between gap-x-4 gap-y-1 border-t border-dashed pt-4 text-xs text-muted-foreground">
  <span class="flex flex-wrap items-center gap-x-1.5">
    <span class="tabular-nums">© {new Date().getFullYear()}</span>
    <span aria-hidden="true">·</span>
    {t().poweredBy} {@render external('https://inc.lawnect.com', 'Lawnect Inc.')}
  </span>
  <span class="flex flex-wrap items-center gap-x-1.5">
    {@render external('https://github.com/lawnect/meerkat', t().source)}
    {#if version}<span aria-hidden="true">·</span><span class="font-mono tabular-nums">v{version}</span>{/if}
  </span>
</footer>
