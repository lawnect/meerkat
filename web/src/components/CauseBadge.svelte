<script lang="ts">
  import { Badge, type BadgeVariant } from '$lib/components/ui/badge/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { causeText } from '../failureCauses.svelte';
  import type { FailureCause } from '../types';

  // 실패 요청과 요청 기록의 결과 열이 함께 쓰는 원인 배지. 배지에 원인 이름을, 호버·포커스 툴팁에 그 뜻을 보여 줍니다.
  let { cause, variant = 'secondary' }: { cause: FailureCause; variant?: BadgeVariant } = $props();
</script>

<Tooltip.Root>
  <Tooltip.Trigger class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
    <Badge {variant} class="cursor-default select-none">{causeText().causes[cause] ?? cause}</Badge>
  </Tooltip.Trigger>
  <Tooltip.Content class="max-w-xs">{causeText().meanings[cause] ?? cause}</Tooltip.Content>
</Tooltip.Root>
