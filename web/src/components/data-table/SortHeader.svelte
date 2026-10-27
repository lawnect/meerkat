<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { cn } from '$lib/utils.js';
  import { ArrowSort, ArrowSortDown, ArrowSortUp } from '../../icons';

  // Data Table 문서의 정렬 머리글 버튼. column.getToggleSortingHandler()를 onclick으로 넘깁니다.
  // 숫자 열처럼 오른쪽으로 맞춘 열(열의 meta.class에 text-right)은 align="end"로 버튼의 안쪽 여백만큼 오른쪽 가장자리를 맞춥니다.
  let { label, sorted, onclick, align = 'start' }: { label: string; sorted: false | 'asc' | 'desc'; onclick?: (event: unknown) => void; align?: 'start' | 'end' } = $props();
</script>

<Button variant="ghost" size="sm" class={cn('text-muted-foreground hover:text-foreground data-[active=true]:text-foreground', align === 'end' ? '-mr-2.5' : '-ml-2.5')} data-active={Boolean(sorted)} {onclick}>
  {label}
  {#if sorted === 'asc'}<ArrowSortUp class="size-4" aria-hidden="true" />{:else if sorted === 'desc'}<ArrowSortDown class="size-4" aria-hidden="true" />{:else}<ArrowSort class="size-4 opacity-50" aria-hidden="true" />{/if}
</Button>
