<script lang="ts">
  import { Button } from '$lib/components/ui/button/index.js';
  import { ArrowSort, ArrowSortDown, ArrowSortUp } from '../../icons';

  // Data Table 문서의 정렬 머리글 버튼. column.getToggleSortingHandler()를 onclick으로 넘깁니다.
  let { label, sorted, onclick }: { label: string; sorted: false | 'asc' | 'desc'; onclick?: (event: unknown) => void } = $props();
</script>

<Button variant="ghost" size="sm" class="-ml-2.5 text-muted-foreground hover:text-foreground data-[active=true]:text-foreground" data-active={Boolean(sorted)} {onclick}>
  {label}
  {#if sorted === 'asc'}<ArrowSortUp class="size-4" aria-hidden="true" />{:else if sorted === 'desc'}<ArrowSortDown class="size-4" aria-hidden="true" />{:else}<ArrowSort class="size-4 opacity-50" aria-hidden="true" />{/if}
</Button>
