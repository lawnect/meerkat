<script lang="ts" generics="Entry extends { id: number; at: string }">
  import { untrack } from 'svelte';
  import { type ColumnDef, type FilterFn, createColumnHelper, createTable, renderSnippet } from '@tanstack/svelte-table';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import { cn } from '$lib/utils.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { formatClock, formatDay } from '../format';
  import { intlLocale, translations } from '../i18n.svelte';
  import { ArrowReset20, ArrowSync, Settings, Warning, type IconComponent } from '../icons';
  import type { LogView } from '../types';
  import type { LogFilter } from './logFilter';
  import DataTable from './data-table/DataTable.svelte';
  import DataTablePagination from './data-table/DataTablePagination.svelte';
  import DataTableSearch from './data-table/DataTableSearch.svelte';
  import { features, type DataTableFeatures } from './data-table/features';
  import { urlTableState, type UrlFilter } from './data-table/urlTable.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import LogSettingsDialog from './LogSettingsDialog.svelte';

  // 관찰의 요청 기록 카드(접근 기록, 실패 요청). 머리글에 사용 중인 크기, 새로 고침, 기록 설정, 기록 스위치를 두고,
  // 본문은 필터 줄(검색, 선택, 초기화, 생략한 반복 요청 수), 표, 위·아래 페이지 이동입니다.
  // 표는 shadcn Data Table 구성(TanStack + DataTable + DataTablePagination)이지만 거르기와 페이지 나누기는 서버가 합니다:
  // manualFiltering·manualPagination, rowCount는 서버의 total. 검색어(q)·필터·페이지는 주소 query가 단일 출처이고(urlTableState),
  // 필터는 보이지 않는 열로 두며 기록은 GET /api/v1/observe/{kind}-log에서 받습니다.
  // 부모는 시각 다음의 열(columns: columnHelper + renderSnippet 셀)과 검색 외의 필터 선택(filters)을 채웁니다.
  let { kind, title, tip, suppressedTip, searchLabel, emptyTitle, offTitle, offHint, emptyIcon: EmptyIcon, loadFailed, filters, columns }: {
    kind: 'access' | 'failure';
    title: string;
    tip: string;
    suppressedTip: string; // 한도를 넘어 생략한 요청이 무엇인지 설명하는 툴팁
    searchLabel: string;
    emptyTitle: string;
    offTitle: string;
    offHint: string;
    emptyIcon: IconComponent;
    loadFailed: (code: string) => string;
    filters: LogFilter[];
    columns: ColumnDef<DataTableFeatures, Entry, any>[];
  } = $props();

  const t = translations({
    enabled: 'Logging',
    refresh: 'Refresh',
    settings: 'Log settings',
    time: 'Time',
    reset: 'Reset filters',
    filteredEmpty: 'No matching entries',
    skipped: (count: number, text: string) => `${text} repeated ${count === 1 ? 'request' : 'requests'} skipped`,
    unavailable: 'The log file couldn\'t be opened, so nothing is logged.',
    dropped: (count: string) => `${count} entries weren't logged because the queue was full (since the Outlet started).`,
    on: 'Logging enabled',
    off: 'Logging disabled',
    errors: { log_unavailable: 'The log file couldn\'t be opened.' } as Record<string, string>,
    failed: (code: string) => `Couldn't change logging: ${code}`,
  }, {
    enabled: '기록',
    refresh: '새로 고침',
    settings: '기록 설정',
    time: '시각',
    reset: '필터 초기화',
    filteredEmpty: '조건에 해당하는 기록이 없습니다',
    skipped: (_count: number, text: string) => `반복 요청 ${text}건 생략`,
    unavailable: '기록 파일을 열 수 없어 기록하지 않습니다.',
    dropped: (count: string) => `대기열 초과로 ${count}건을 기록하지 못했습니다(Outlet 시작 이후).`,
    on: '기록이 활성화되었습니다',
    off: '기록이 비활성화되었습니다',
    errors: { log_unavailable: '기록 파일을 열 수 없습니다.' },
    failed: (code: string) => `기록 설정 변경 중 오류가 발생했습니다: ${code}`,
  });

  const id = $derived(`${kind}-log`);
  const endpoint = $derived(`/api/v1/observe/${kind}-log`);
  const pageSize = 20;

  // 검색어(q)와 선택 필터는 표의 숨은 열입니다(열 id는 부모가 정한 열과 겹치지 않게 filter_ 접두사). 주소·서버 query의 이름은 param입니다.
  const columnOf = (param: string): string => `filter_${param}`;
  const optionValues = (filter: LogFilter): string[] => filter.options.flatMap((option) => ('group' in option ? option.options : [option])).map((option) => option.value).filter(Boolean);
  const urlFilters = $derived<UrlFilter[]>([{ param: 'q', column: columnOf('q') }, ...filters.map((filter) => ({ param: filter.param, column: columnOf(filter.param), allowed: filter.free ? undefined : optionValues(filter) }))]);
  const urlTable = urlTableState({ get filters() { return urlFilters; }, sortable: [], defaultSort: [], pageSize });
  const tableState = $derived(urlTable.state);

  let view = $state<LogView<Entry> | null>(null);

  const columnHelper = createColumnHelper<DataTableFeatures, Entry>();
  // 거르는 일은 서버가 하므로 필터 함수는 모든 행을 통과시킵니다.
  const serverFiltered: FilterFn<DataTableFeatures, Entry> = () => true;
  const filterColumns = $derived(urlFilters.map((filter) => columnHelper.accessor(() => '', { id: filter.column, header: () => '', cell: () => '', enableSorting: false, filterFn: serverFiltered })));
  const timeColumn = columnHelper.accessor((entry) => entry.at, {
    id: 'time',
    header: () => t().time,
    cell: ({ row }) => renderSnippet(timeCell, row.original),
    enableSorting: false,
    meta: { class: 'w-28' },
  });
  const table = createTable({
    features,
    get data() { return view?.entries ?? []; },
    get columns() { return [timeColumn, ...columns, ...filterColumns] as ColumnDef<DataTableFeatures, Entry, any>[]; },
    getRowId: (row) => String(row.id),
    manualPagination: true,
    manualFiltering: true,
    get rowCount() { return view?.total; },
    initialState: { columnVisibility: untrack(() => Object.fromEntries(urlFilters.map((filter) => [filter.column, false]))) },
    ...urlTable.handlers,
    get state() { return urlTable.state; },
  });

  const filtered = $derived(tableState.columnFilters.length > 0);
  const filterValue = (param: string): string => (tableState.columnFilters.find((filter) => filter.id === columnOf(param))?.value as string | undefined) ?? '';
  const query = $derived.by(() => {
    const params = new URLSearchParams({ page: String(tableState.pagination.pageIndex + 1), per_page: String(tableState.pagination.pageSize) });
    for (const filter of urlFilters) if (filterValue(filter.param)) params.set(filter.param, filterValue(filter.param));
    return params.toString();
  });

  let loadError = $state('');
  let reloads = $state(0);
  let busy = $state(false);
  let settingsOpen = $state(false);

  $effect(() => {
    void reloads;
    const current = query;
    let active = true;
    api<LogView<Entry>>(`${endpoint}?${current}`)
      .then((next) => {
        if (!active) return;
        // Go는 빈 목록을 null로 보낼 수 있습니다.
        view = { ...next, entries: next.entries ?? [] };
        loadError = '';
        // 범위를 벗어난 페이지 번호는 첫 페이지로 되돌립니다.
        if (next.total > 0 && !next.entries?.length && tableState.pagination.pageIndex > 0) table.setPageIndex(0);
      })
      .catch((cause) => { if (active) loadError = loadFailed(errorMessage(cause)); });
    return () => { active = false; };
  });

  // 설정을 저장한 응답은 필터 없는 첫 페이지이므로, 설정과 크기만 반영하고 목록은 지금 조건으로 다시 받습니다.
  function adopt(next: LogView<unknown>): void {
    if (view) view = { ...view, settings: next.settings, available: next.available, size_bytes: next.size_bytes, dropped: next.dropped, suppressed: next.suppressed };
    reloads += 1;
  }

  async function toggle(enabled: boolean): Promise<void> {
    if (!view || busy) return;
    busy = true;
    try {
      adopt(await api<LogView<unknown>>(endpoint, { method: 'PUT', body: JSON.stringify({ ...view.settings, enabled }) }));
      toast.success(enabled ? t().on : t().off);
    } catch (cause) {
      const code = errorMessage(cause);
      toast.error(t().errors[code] ?? t().failed(code));
    } finally {
      busy = false;
    }
  }

  const resetFilters = (): void => { table.setColumnFilters([]); };

  const number = (value: number): string => value.toLocaleString(intlLocale());
  // 크기는 MiB 한 자리까지만 보여 줍니다.
  const mib = (bytes: number): string => (bytes / 1048576).toLocaleString(intlLocale(), { maximumFractionDigits: 1 });
</script>

{#snippet timeCell(entry: Entry)}
  <div class="grid gap-0.5 tabular-nums">
    <span>{formatClock(entry.at)}</span>
    <span class="text-muted-foreground">{formatDay(entry.at)}</span>
  </div>
{/snippet}

<Card.Root size="sm" role="region" aria-labelledby={`${id}-title`}>
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id={`${id}-title`} class="flex items-center gap-1">{title}<InfoTooltip id={`${id}-tip`} label={title} description={tip} /></Card.Title>
    {#if view}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <div class="flex items-center gap-2">
          <span class="me-1 text-xs text-muted-foreground tabular-nums">{mib(view.size_bytes)} MiB / {number(view.settings.max_mib)} MiB</span>
          <IconButton label={t().refresh} icon={ArrowSync} onclick={() => { reloads += 1; }} />
          <IconButton label={t().settings} icon={Settings} onclick={() => { settingsOpen = true; }} />
          <Field.Label for={`${id}-enabled`} class="text-muted-foreground">{t().enabled}</Field.Label>
          <Switch id={`${id}-enabled`} size="sm" bind:checked={() => view?.settings.enabled ?? false, toggle} disabled={busy || !view.available} />
        </div>
      </Card.Action>
    {:else if loadError}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <IconButton label={t().refresh} icon={ArrowSync} onclick={() => { reloads += 1; }} />
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid min-w-0 gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !view}
      <Skeleton class="h-96 w-full" />
    {:else}
      {#if !view.available}
        <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{t().unavailable}</Alert.Description></Alert.Root>
      {:else if view.dropped > 0}
        <Alert.Root><Warning aria-hidden="true" /><Alert.Description>{t().dropped(number(view.dropped))}</Alert.Description></Alert.Root>
      {/if}
      <div class="flex flex-wrap items-center gap-2" role="search" aria-label={searchLabel}>
        <DataTableSearch {table} column={columnOf('q')} label={searchLabel} class="sm:max-w-72" />
        {#each filters as filter (filter.param)}
          <NativeSelect.Root class={cn('w-full', filter.class)} aria-label={filter.label} bind:value={() => filterValue(filter.param), (next) => { table.getColumn(columnOf(filter.param))?.setFilterValue(next || undefined); }}>
            {#each filter.options as option, index (index)}
              {#if 'group' in option}
                <NativeSelect.OptGroup label={option.group}>
                  {#each option.options as item (item.value)}<NativeSelect.Option value={item.value}>{item.label}</NativeSelect.Option>{/each}
                </NativeSelect.OptGroup>
              {:else}
                <NativeSelect.Option value={option.value}>{option.label}</NativeSelect.Option>
              {/if}
            {/each}
          </NativeSelect.Root>
        {/each}
        {#if filtered}<IconButton variant="outline" size="icon" label={t().reset} icon={ArrowReset20} onclick={resetFilters} />{/if}
        {#if view.suppressed > 0}
          {@const skipped = t().skipped(view.suppressed, number(view.suppressed))}
          <span class="ms-auto flex items-center gap-1 text-xs text-muted-foreground tabular-nums">{skipped}<InfoTooltip id={`${id}-suppressed-tip`} label={skipped} description={suppressedTip} /></span>
        {/if}
      </div>
      {#if view.entries.length}
        <DataTablePagination {table} placement="top" />
        <DataTable {table} ariaLabel={title} class="min-w-160 lg:table-fixed" />
        <DataTablePagination {table} />
      {:else if view.total > 0}
        <Skeleton class="h-96 w-full" />
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><EmptyIcon class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{filtered ? t().filteredEmpty : view.settings.enabled ? emptyTitle : offTitle}</Empty.Title>
            {#if !filtered && !view.settings.enabled}<Empty.Description>{offHint}</Empty.Description>{/if}
          </Empty.Header>
          {#if filtered}<Empty.Content><Button variant="outline" onclick={resetFilters}>{t().reset}</Button></Empty.Content>{/if}
        </Empty.Root>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>

{#if view}
  <LogSettingsDialog bind:open={settingsOpen} {kind} settings={view.settings} onSaved={adopt} />
{/if}
