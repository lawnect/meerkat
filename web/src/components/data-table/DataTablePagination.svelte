<script lang="ts" generics="TData extends RowData">
  import type { RowData, Table as TableInstance } from '@tanstack/svelte-table';
  import type { DataTableFeatures } from './features';
  import PaginationBar from '../PaginationBar.svelte';

  // TanStack 테이블의 페이지 상태를 PaginationBar에 이어 줍니다. 목록 위(top)와 아래(bottom)에 한 번씩 둡니다.
  // 전체 수는 table.getRowCount()입니다: 서버가 페이지를 나누는 표(manualPagination)는 rowCount 옵션, 그 밖의 표는 페이지로 나누기 전의 행 수입니다.
  let { table, placement = 'bottom' }: { table: TableInstance<DataTableFeatures, TData>; placement?: 'top' | 'bottom' } = $props();

  const pagination = $derived(table.atoms.pagination.get());
  const total = $derived(table.getRowCount());
</script>

<PaginationBar {total} perPage={pagination.pageSize} {placement} bind:page={() => pagination.pageIndex + 1, (next) => table.setPageIndex(next - 1)} />
