<script lang="ts">
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';
  import { copyToClipboard } from '../toast.svelte';

  // 값 자체를 눌러 복사합니다. 결과는 토스트로 알립니다.
  let { value, class: className = '' }: { value: string; class?: string } = $props();

  const t = translations({ copy: 'Click to copy' }, { copy: '눌러서 복사' });
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <button {...props} type="button" class={cn('max-w-full cursor-pointer truncate text-left font-mono text-link focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none', className)} onclick={() => copyToClipboard(value)}>{value}</button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content>{t().copy}</Tooltip.Content>
</Tooltip.Root>
