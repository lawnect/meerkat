import type { ColumnFiltersState, OnChangeFn, PaginationState, SortingState, Updater } from '@tanstack/svelte-table';
import { formatSort, intParam, oneOf, param, parseSort, setParams } from '../../urlState.svelte';

// 목록 표의 필터·정렬·페이지를 주소 query에 담는 TanStack 제어 상태입니다(shadcn Data Table 문서의 "controlled state" 구성).
// const url = urlTableState(config); createTable({ ...url.handlers, get state() { return url.state; }, features, columns, … })로 쓰면 상태의 단일 출처는 주소이고,
// 필터나 정렬이 바뀌면 page는 1로 돌아갑니다. 허용하지 않는 값은 조용히 기본값이 됩니다.
export type UrlFilter = {
  param: string; // 주소의 이름(q, status, domain, mode …)
  column: string; // 표 열 id
  allowed?: readonly string[] | (() => readonly string[]); // 정해진 값만 받을 때(데이터에 따라 달라지면 함수)
};

export type UrlTableConfig = {
  filters: readonly UrlFilter[];
  sortable: readonly string[];
  defaultSort: SortingState;
  pageSize: number;
};

const apply = <T>(updater: Updater<T>, current: T): T => (updater instanceof Function ? updater(current) : updater);

export function urlTableState(config: UrlTableConfig) {
  const read = {
    columnFilters: (): ColumnFiltersState => config.filters.flatMap((filter) => {
      const allowed = filter.allowed instanceof Function ? filter.allowed() : filter.allowed;
      const value = allowed ? oneOf(filter.param, allowed, '') : (param(filter.param) ?? '');
      return value ? [{ id: filter.column, value }] : [];
    }),
    sorting: (): SortingState => parseSort(param('sort'), config.sortable, config.defaultSort),
    pagination: (): PaginationState => ({ pageIndex: intParam('page', 1) - 1, pageSize: config.pageSize }),
  };
  const defaultSort = formatSort(config.defaultSort);

  const onColumnFiltersChange: OnChangeFn<ColumnFiltersState> = (updater) => {
    const next = apply(updater, read.columnFilters());
    const changes: Record<string, string | number> = { page: 1 };
    for (const filter of config.filters) {
      const value = next.find((entry) => entry.id === filter.column)?.value;
      changes[filter.param] = typeof value === 'string' ? value : '';
    }
    setParams(changes, { page: 1 });
  };
  const onSortingChange: OnChangeFn<SortingState> = (updater) => {
    setParams({ sort: formatSort(apply(updater, read.sorting())), page: 1 }, { sort: defaultSort, page: 1 });
  };
  const onPaginationChange: OnChangeFn<PaginationState> = (updater) => {
    setParams({ page: apply(updater, read.pagination()).pageIndex + 1 }, { page: 1 });
  };

  return {
    // createTable 옵션의 state는 getter로 넘겨야 주소가 바뀔 때마다 다시 읽힙니다(스프레드하면 값이 굳습니다).
    get state() {
      return { columnFilters: read.columnFilters(), sorting: read.sorting(), pagination: read.pagination() };
    },
    // 페이지 번호가 바뀔 때마다 알아서 1쪽으로 돌아가는 동작은 끄고, 필터·정렬을 바꿀 때만 위에서 직접 1쪽으로 돌립니다.
    handlers: { autoResetPageIndex: false, onColumnFiltersChange, onSortingChange, onPaginationChange },
  };
}
