<script lang="ts">
  import type { Snippet } from 'svelte';
  import * as Card from '$lib/components/ui/card/index.js';
  import { cn } from '$lib/utils.js';
  import InfoTooltip from './InfoTooltip.svelte';

  // shadcn dashboard-01 블록의 섹션 카드. 이름, 값, 보조 설명을 한 카드에 보여줍니다.
  // 값이 배지처럼 문자열이 아니면 children으로 넘깁니다.
  let { label, value, sub, hint, hintId, action, children, class: className }: {
    label: string;
    value?: string;
    sub?: string;
    hint?: string;
    hintId?: string;
    action?: Snippet;
    children?: Snippet;
    class?: string;
  } = $props();
</script>

<Card.Root size="sm" class={cn('min-w-0 gap-1', className)}>
  <Card.Header>
    <Card.Description class="flex min-w-0 items-center gap-0.5">
      <span class="truncate">{label}</span>
      {#if hint && hintId}<InfoTooltip id={hintId} {label} description={hint} />{/if}
    </Card.Description>
    {#if action}<Card.Action>{@render action()}</Card.Action>{/if}
  </Card.Header>
  <Card.Content class="grid min-w-0 gap-1">
    {#if children}
      <div class="flex min-h-8 items-center">{@render children()}</div>
    {:else}
      <div class="truncate text-2xl font-semibold tabular-nums">{value}</div>
    {/if}
    {#if sub}<div class="truncate text-muted-foreground">{sub}</div>{/if}
  </Card.Content>
</Card.Root>
