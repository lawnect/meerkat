<script lang="ts">
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { translations } from '../i18n.svelte';
  import { Info12 } from '../icons';

  // 제목·입력 항목 옆의 도움말. 설명 요소 id를 입력칸 aria-describedby에도 쓸 수 있습니다.
  let { id, label, description }: { id: string; label: string; description: string } = $props();

  const t = translations({ about: (name: string) => `About ${name}` }, { about: (name: string) => `${name} 설명` });
</script>

<Tooltip.Root>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <button {...props} type="button" class="inline-flex size-5 shrink-0 items-center justify-center text-muted-foreground hover:text-foreground focus-visible:ring-3 focus-visible:ring-ring/50 focus-visible:outline-none" aria-label={t().about(label)} aria-describedby={id}><Info12 class="size-3" aria-hidden="true" /></button>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content class="max-w-xs"><span {id}>{description}</span></Tooltip.Content>
</Tooltip.Root>
