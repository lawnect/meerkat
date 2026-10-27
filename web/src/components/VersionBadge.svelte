<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { translations } from '../i18n.svelte';

  // 터널(Connector)이 마지막 하트비트로 알린 버전을 이름 옆에 붙이는 작은 배지(예: v0.5.1). 버전이 없으면(첫 연결 전) 아무것도 그리지 않습니다.
  // 목록은 이름 링크가 행 전체를 덮으므로 툴팁 없이 보조 기술용 이름만 두고, 상세 머리글은 tip으로 "Connector 버전" 툴팁을 답니다.
  let { version, tip = false }: { version?: string; tip?: boolean } = $props();

  const t = translations({ label: 'Connector version' }, { label: 'Connector 버전' });
  const text = $derived(`v${(version ?? '').replace(/^v/i, '')}`);
</script>

{#if version}
  {#if tip}
    <Tooltip.Root>
      <Tooltip.Trigger class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50" aria-label={`${t().label} ${text}`}>
        <Badge variant="outline" class="cursor-default font-mono tabular-nums select-none">{text}</Badge>
      </Tooltip.Trigger>
      <Tooltip.Content>{t().label}</Tooltip.Content>
    </Tooltip.Root>
  {:else}
    <Badge variant="outline" class="font-mono tabular-nums select-none"><span class="sr-only">{t().label} </span>{text}</Badge>
  {/if}
{/if}
