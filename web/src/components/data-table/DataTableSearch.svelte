<script lang="ts" generics="TData extends RowData">
  import { untrack } from 'svelte';
  import type { RowData, Table as TableInstance } from '@tanstack/svelte-table';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import { cn } from '$lib/utils.js';
  import { Search } from '../../icons';
  import type { DataTableFeatures } from './features';

  // 검색 입력. 입력칸은 `delay`ms 멈춘 뒤에 열(column)의 필터 값에 반영하고(서버가 거르는 표에서 글자마다 다시 불러오지 않게 합니다),
  // 필터 값이 밖에서 바뀌면(주소, 초기화) 입력칸도 따라갑니다.
  let { table, column, label, delay = 300, class: className }: { table: TableInstance<DataTableFeatures, TData>; column: string; label: string; delay?: number; class?: string } = $props();

  const applied = $derived((table.getColumn(column)?.getFilterValue() as string | undefined) ?? '');
  let text = $state(untrack(() => applied));

  $effect(() => {
    const fromTable = applied;
    if (untrack(() => text.trim()) !== fromTable) text = fromTable;
  });

  $effect(() => {
    const value = text.trim();
    const timer = window.setTimeout(() => {
      if (value !== untrack(() => applied)) table.getColumn(column)?.setFilterValue(value || undefined);
    }, delay);
    return () => window.clearTimeout(timer);
  });
</script>

<InputGroup.Root class={cn('min-w-48 grow sm:max-w-sm sm:grow-0', className)}>
  <InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
  <InputGroup.Input type="search" bind:value={text} placeholder={label} aria-label={label} autocomplete="off" />
</InputGroup.Root>
