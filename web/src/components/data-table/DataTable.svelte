<script lang="ts" generics="TData extends RowData">
  import { FlexRender, type RowData, type Table as TableInstance } from '@tanstack/svelte-table';
  import type { DataTableFeatures } from './features';
  import * as Table from '$lib/components/ui/table/index.js';
  import { cn } from '$lib/utils.js';

  // shadcn Data Table 문서의 표 부분. 정렬·필터·페이지는 호출하는 쪽의 TanStack 테이블이 맡습니다.
  // 행 전체를 링크처럼 쓰려면 첫 열의 <a>에 after:absolute after:inset-0을 주면 됩니다(행이 relative입니다).
  let { table, ariaLabel }: { table: TableInstance<DataTableFeatures, TData>; ariaLabel: string } = $props();
</script>

<div class="overflow-hidden border bg-card">
  <Table.Root aria-label={ariaLabel}>
    <Table.Header class="bg-muted">
      {#each table.getHeaderGroups() as headerGroup (headerGroup.id)}
        <Table.Row class="hover:bg-transparent">
          {#each headerGroup.headers as header (header.id)}
            {@const direction = header.column.getIsSorted()}
            <Table.Head class={cn('first:pl-4 last:pr-4', (header.column.columnDef.meta as { class?: string } | undefined)?.class)} aria-sort={header.column.getCanSort() ? (direction === 'asc' ? 'ascending' : direction === 'desc' ? 'descending' : 'none') : undefined}>
              {#if !header.isPlaceholder}<FlexRender {header} />{/if}
            </Table.Head>
          {/each}
        </Table.Row>
      {/each}
    </Table.Header>
    <Table.Body>
      {#each table.getRowModel().rows as row (row.id)}
        <Table.Row class="relative">
          {#each row.getVisibleCells() as cell (cell.id)}
            <Table.Cell class={cn('first:pl-4 last:pr-4', (cell.column.columnDef.meta as { class?: string } | undefined)?.class)}><FlexRender {cell} /></Table.Cell>
          {/each}
        </Table.Row>
      {/each}
    </Table.Body>
  </Table.Root>
</div>
