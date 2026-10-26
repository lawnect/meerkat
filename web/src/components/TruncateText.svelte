<script lang="ts">
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { cn } from '$lib/utils.js';

  // 한 줄로 자르고, 실제로 잘렸을 때만 전체 문구를 툴팁으로 보여줍니다.
  let { text, class: className = '' }: { text: string; class?: string } = $props();

  let element = $state<HTMLElement | null>(null);
  let truncated = $state(false);
  const measure = () => { if (element) truncated = element.scrollWidth > element.clientWidth; };
</script>

<Tooltip.Root disabled={!truncated}>
  <Tooltip.Trigger>
    {#snippet child({ props })}
      <span {...props} bind:this={element} class={cn('block min-w-0 max-w-full truncate', className)} role="presentation" onpointerenter={measure} onfocusin={measure}>{text}</span>
    {/snippet}
  </Tooltip.Trigger>
  <Tooltip.Content>{text}</Tooltip.Content>
</Tooltip.Root>
