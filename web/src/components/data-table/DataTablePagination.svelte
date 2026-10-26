<script lang="ts" generics="TData extends RowData">
  import type { RowData, Table as TableInstance } from '@tanstack/svelte-table';
  import type { DataTableFeatures } from './features';
  import PaginationBar from '../PaginationBar.svelte';

  // TanStack 테이블의 페이지 상태를 PaginationBar에 이어 줍니다. 목록 위(top)와 아래(bottom)에 한 번씩 둡니다.
  let { table, placement = 'bottom' }: { table: TableInstance<DataTableFeatures, TData>; placement?: 'top' | 'bottom' } = $props();

  const pagination = $derived(table.atoms.pagination.get());
  const total = $derived(table.getFilteredRowModel().rows.length);
</script>

<PaginationBar {total} perPage={pagination.pageSize} {placement} bind:page={() => pagination.pageIndex + 1, (next) => table.setPageIndex(next - 1)} />
