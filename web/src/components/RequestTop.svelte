<script lang="ts">
  import { untrack } from 'svelte';
  import { type ColumnDef, type FilterFn, createColumnHelper, createTable, renderComponent, renderSnippet } from '@tanstack/svelte-table';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import { api, errorMessage } from '../api';
  import { csvHref, formatClock, formatDay } from '../format';
  import { intlLocale, translations } from '../i18n.svelte';
  import { ArrowReset20, ArrowSync, TextBulletListLtr20, Warning } from '../icons';
  import { requestTopPeriods, requestTopSorts, type RequestTopPeriod, type RequestTopRow, type RequestTopView } from '../types';
  import { oneOf, setParams } from '../urlState.svelte';
  import CsvDownload from './CsvDownload.svelte';
  import DataTable from './data-table/DataTable.svelte';
  import DataTablePagination from './data-table/DataTablePagination.svelte';
  import DataTableSearch from './data-table/DataTableSearch.svelte';
  import { features, type DataTableFeatures } from './data-table/features';
  import SortHeader from './data-table/SortHeader.svelte';
  import { urlTableState, type UrlFilter } from './data-table/urlTable.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TruncateText from './TruncateText.svelte';

  // 요청 기록 탭의 통계 두 가지: 엔드포인트(주소+경로)별과 클라이언트별 요청 수(by). 요청 기록에 남아 있는 요청을 서버가 기간·주소·검색어로 거르고 묶어 정렬하고 나눕니다
  // (GET /api/v1/observe/request-log/top). 표는 manualSorting·manualPagination이고 rowCount는 서버의 total입니다.
  // 기간(?period=), 검색어(?q=), 주소(?host=, 엔드포인트만), 정렬(?sort=requests.desc), 페이지는 주소 query가 단일 출처입니다.
  // 각 줄의 "기록 보기"는 요청 기록 목록(?view=list)으로 돌아가며 그 엔드포인트의 주소·경로(?host=&path=) 또는 클라이언트 출처(?source=, IPv4 주소 또는 IPv6 /56)로 거릅니다.
  // addresses는 콘솔이 이미 불러온 공개 주소이고, 주소에서 온 값이 목록에 없으면 그 값도 선택지에 남깁니다.
  let { by, addresses }: { by: 'endpoint' | 'client'; addresses: string[] } = $props();

  const t = translations({
    titleEndpoint: 'Requests by endpoint',
    titleClient: 'Requests by client',
    tip: 'Counts the requests the request log still holds in the period, so the figures are limited by its size limit, its retention period and the per-client logging limits. 4xx and 5xx count client and server error responses.',
    search: 'Search by address, path, client or agent',
    host: 'Address',
    allHosts: 'All addresses',
    periodAria: 'Period',
    periods: { '1h': '1h', '24h': '24h', '7d': '7d', '30d': '30d', all: 'All' } as Record<RequestTopPeriod, string>,
    refresh: 'Refresh',
    reset: 'Reset filters',
    endpoint: 'Endpoint',
    client: 'Client',
    requests: 'Requests',
    clients: 'Clients',
    endpoints: 'Endpoints',
    lastRequest: 'Last request',
    actions: 'Actions',
    viewLog: 'View log',
    viewLogOf: (target: string) => `View log for ${target}`,
    offTitle: 'Request log is disabled',
    offHint: 'Turn on logging in the Log view to collect statistics.',
    openLog: 'Go to log',
    unavailable: 'The log file couldn\'t be opened, so nothing is logged.',
    emptyTitle: 'No requests in this period',
    filteredEmpty: 'No matching requests',
    loadFailed: (code: string) => `Couldn't load the request statistics: ${code}`,
  }, {
    titleEndpoint: '엔드포인트별 요청',
    titleClient: '클라이언트별 요청',
    tip: '요청 기록에 남아 있는 기간 내 요청을 집계합니다. 따라서 수치는 기록의 용량 상한, 보관 기간, 클라이언트별 기록 한도의 제한을 받습니다. 4xx와 5xx는 각각 클라이언트 오류와 서버 오류 응답 건수입니다.',
    search: '주소, 경로, 클라이언트, User-Agent 검색',
    host: '주소',
    allHosts: '모든 주소',
    periodAria: '기간',
    periods: { '1h': '1시간', '24h': '24시간', '7d': '7일', '30d': '30일', all: '전체' },
    refresh: '새로 고침',
    reset: '필터 초기화',
    endpoint: '엔드포인트',
    client: '클라이언트',
    requests: '요청',
    clients: '클라이언트',
    endpoints: '엔드포인트',
    lastRequest: '마지막 요청',
    actions: '작업',
    viewLog: '기록 보기',
    viewLogOf: (target: string) => `${target} 기록 보기`,
    offTitle: '요청 기록이 비활성화되어 있습니다',
    offHint: '기록 화면에서 기록을 활성화하면 통계를 집계합니다.',
    openLog: '기록으로 이동',
    unavailable: '기록 파일을 열 수 없어 기록하지 않습니다.',
    emptyTitle: '이 기간의 요청이 없습니다',
    filteredEmpty: '조건에 해당하는 요청이 없습니다',
    loadFailed: (code: string) => `요청 통계를 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const endpoint = '/api/v1/observe/request-log';
  const title = $derived(by === 'endpoint' ? t().titleEndpoint : t().titleClient);
  const pageSize = 20;
  const defaultPeriod: RequestTopPeriod = '24h';
  const period = $derived(oneOf('period', requestTopPeriods, defaultPeriod));

  // 검색어(q)와 주소(host)는 표의 숨은 열입니다(LogCard와 같은 방식). 클라이언트별에는 주소 필터가 없습니다.
  const columnOf = (name: string): string => `filter_${name}`;
  const urlFilters = $derived<UrlFilter[]>([{ param: 'q', column: columnOf('q') }, ...(by === 'endpoint' ? [{ param: 'host', column: columnOf('host') }] : [])]);
  const urlTable = urlTableState({ get filters() { return urlFilters; }, sortable: requestTopSorts, defaultSort: [{ id: 'requests', desc: true }], pageSize });
  const tableState = $derived(urlTable.state);

  let view = $state<RequestTopView | null>(null);

  const columnHelper = createColumnHelper<DataTableFeatures, RequestTopRow>();
  // 거르는 일은 서버가 하므로 필터 함수는 모든 행을 통과시킵니다.
  const serverFiltered: FilterFn<DataTableFeatures, RequestTopRow> = () => true;
  type SortColumn = { getIsSorted: () => false | 'asc' | 'desc'; getToggleSortingHandler: () => ((event: unknown) => void) | undefined };
  const sortHeader = (column: SortColumn, label: string) => renderComponent(SortHeader, { label, sorted: column.getIsSorted(), onclick: column.getToggleSortingHandler(), align: 'end' });
  const countColumn = (key: 'requests' | 'client_errors' | 'server_errors' | 'distinct', label: () => string, width: string, danger = false) => columnHelper.accessor(key, {
    id: key,
    header: ({ column }) => sortHeader(column, label()),
    cell: ({ row }) => renderSnippet(countCell, { value: row.original[key], danger }),
    meta: { class: `${width} text-right` },
  });
  const columns = $derived([
    columnHelper.display({ id: 'name', header: () => (by === 'endpoint' ? t().endpoint : t().client), cell: ({ row }) => renderSnippet(by === 'endpoint' ? endpointCell : clientCell, row.original), enableSorting: false }),
    countColumn('requests', () => t().requests, 'w-24'),
    countColumn('client_errors', () => '4xx', 'w-20'),
    countColumn('server_errors', () => '5xx', 'w-20', true),
    countColumn('distinct', () => (by === 'endpoint' ? t().clients : t().endpoints), 'w-28'),
    columnHelper.accessor('last_at', { id: 'last_at', header: ({ column }) => sortHeader(column, t().lastRequest), cell: ({ row }) => renderSnippet(lastCell, row.original), meta: { class: 'w-36 text-right' } }),
    columnHelper.display({ id: 'actions', header: () => renderSnippet(actionsHead, undefined), cell: ({ row }) => renderSnippet(actionCell, row.original), enableSorting: false, meta: { class: 'w-28 text-right' } }),
    ...urlFilters.map((filter) => columnHelper.accessor(() => '', { id: filter.column, header: () => '', cell: () => '', enableSorting: false, filterFn: serverFiltered })),
  ] as ColumnDef<DataTableFeatures, RequestTopRow, any>[]);
  const table = createTable({
    features,
    get data() { return view?.rows ?? []; },
    get columns() { return columns; },
    // 서버가 묶은 줄은 줄마다 다르지만, 화면의 키는 서버 값에 기대지 않고 쪽 안의 순서로 둡니다.
    getRowId: (_row, index) => String(index),
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    // 숫자와 시각은 큰 값(최근)부터 보고, 정렬은 풀지 않고 방향만 바꿉니다.
    sortDescFirst: true,
    enableSortingRemoval: false,
    get rowCount() { return view?.total; },
    initialState: { columnVisibility: untrack(() => Object.fromEntries(urlFilters.map((filter) => [filter.column, false]))) },
    ...urlTable.handlers,
    get state() { return urlTable.state; },
  });

  const filtered = $derived(tableState.columnFilters.length > 0);
  const filterValue = (name: string): string => (tableState.columnFilters.find((filter) => filter.id === columnOf(name))?.value as string | undefined) ?? '';
  const host = $derived(filterValue('host'));
  const hosts = $derived([...new Set([...addresses, ...(host ? [host] : [])])].sort());
  const sort = $derived(tableState.sorting[0] ?? { id: 'requests', desc: true });
  // 통계 조건(기간·정렬·검색어·주소)만 담은 query. 목록은 여기에 page·per_page를 붙이고, CSV 내려받기는 페이지 없이 모든 묶음을 받습니다.
  const statsQuery = $derived.by(() => {
    const params = new URLSearchParams({ by, period, sort: `${sort.id}.${sort.desc ? 'desc' : 'asc'}` });
    for (const filter of urlFilters) if (filterValue(filter.param)) params.set(filter.param, filterValue(filter.param));
    return params;
  });
  const query = $derived.by(() => {
    const params = new URLSearchParams(statsQuery);
    params.set('page', String(tableState.pagination.pageIndex + 1));
    params.set('per_page', String(tableState.pagination.pageSize));
    return params.toString();
  });
  const exportHref = $derived(csvHref(`${endpoint}/top`, new URLSearchParams({ format: 'csv', ...Object.fromEntries(statsQuery) })));

  let loadError = $state('');
  let reloads = $state(0);

  $effect(() => {
    void reloads;
    const current = query;
    let active = true;
    // 조건이 바뀌면 이전 요청은 취소해, 큰 기록의 통계가 서버에 쌓이지 않게 합니다.
    const controller = new AbortController();
    api<RequestTopView>(`${endpoint}/top?${current}`, { signal: controller.signal })
      .then((next) => {
        if (!active) return;
        // Go는 빈 목록을 null로 보낼 수 있습니다.
        view = { ...next, rows: next.rows ?? [] };
        loadError = '';
        // 범위를 벗어난 페이지 번호는 첫 페이지로 되돌립니다.
        if (next.total > 0 && !next.rows?.length && tableState.pagination.pageIndex > 0) table.setPageIndex(0);
      })
      .catch((cause) => { if (active) loadError = t().loadFailed(errorMessage(cause)); });
    return () => { active = false; controller.abort(); };
  });

  const setPeriod = (next: RequestTopPeriod): void => { setParams({ period: next, page: 1 }, { period: defaultPeriod, page: 1 }); };
  const resetFilters = (): void => { table.setColumnFilters([]); };

  // 요청 기록 목록으로 돌아갑니다. 통계에서만 쓰는 query(기간·정렬·검색어)는 지우고, 고른 줄의 조건만 목록의 필터로 넘깁니다.
  function showLog(filters: { host?: string; path?: string; source?: string } = {}): void {
    setParams({
      view: 'list', period: null, sort: null, page: null, q: null, class: null,
      host: filters.host ?? null, path: filters.path ?? null, source: filters.source ?? null,
    }, { view: 'list' }, { push: true });
  }
  // 클라이언트별 줄의 client는 출처(IPv4 주소, IPv6는 /56)이므로 목록의 source로 거릅니다.
  const viewLog = (row: RequestTopRow): void => { showLog(by === 'endpoint' ? { host: row.host, path: row.path } : { source: row.client }); };
  const target = (row: RequestTopRow): string => (by === 'endpoint' ? `${row.host ?? ''}${row.path ?? ''}` : row.client ?? '');

  const number = (value: number): string => value.toLocaleString(intlLocale());
</script>

{#snippet endpointCell(row: RequestTopRow)}
  <div class="grid gap-0.5">
    <TruncateText text={row.host ?? ''} class="font-mono" />
    <TruncateText text={row.path ?? ''} class="font-mono text-muted-foreground" />
  </div>
{/snippet}

{#snippet clientCell(row: RequestTopRow)}
  <TruncateText text={row.client ?? ''} class="font-mono" />
{/snippet}

{#snippet countCell({ value, danger }: { value: number; danger: boolean })}
  <span class={['tabular-nums', !value ? 'text-muted-foreground' : danger && 'text-destructive']}>{number(value)}</span>
{/snippet}

{#snippet lastCell(row: RequestTopRow)}
  <div class="grid gap-0.5 tabular-nums">
    <span>{formatClock(row.last_at)}</span>
    <span class="text-muted-foreground">{formatDay(row.last_at)}</span>
  </div>
{/snippet}

{#snippet actionsHead()}
  <span class="sr-only">{t().actions}</span>
{/snippet}

{#snippet actionCell(row: RequestTopRow)}
  <div class="flex justify-end">
    <Button variant="outline" size="sm" aria-label={t().viewLogOf(target(row))} onclick={() => viewLog(row)}>{t().viewLog}</Button>
  </div>
{/snippet}

<Card.Root size="sm" role="region" aria-labelledby={`request-top-${by}-title`}>
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id={`request-top-${by}-title`} class="flex items-center gap-1">{title}<InfoTooltip id={`request-top-${by}-tip`} label={title} description={t().tip} /></Card.Title>
    {#if view || loadError}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <div class="flex items-center gap-2">
          {#if view}<CsvDownload href={exportHref} disabled={!view.available} />{/if}
          <IconButton label={t().refresh} icon={ArrowSync} onclick={() => { reloads += 1; }} />
        </div>
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid min-w-0 gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !view}
      <Skeleton class="h-96 w-full" />
    {:else if !view.available}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{t().unavailable}</Alert.Description></Alert.Root>
    {:else}
      <div class="flex flex-wrap items-center gap-2" role="search" aria-label={t().search}>
        <DataTableSearch {table} column={columnOf('q')} label={t().search} class="sm:max-w-72" />
        {#if by === 'endpoint'}
          <NativeSelect.Root class="w-full sm:w-56" aria-label={t().host} bind:value={() => host, (next) => { table.getColumn(columnOf('host'))?.setFilterValue(next || undefined); }}>
            <NativeSelect.Option value="">{t().allHosts}</NativeSelect.Option>
            {#each hosts as item (item)}<NativeSelect.Option value={item}>{item}</NativeSelect.Option>{/each}
          </NativeSelect.Root>
        {/if}
        {#if filtered}<IconButton variant="outline" size="icon" label={t().reset} icon={ArrowReset20} onclick={resetFilters} />{/if}
        <ToggleGroup.Root class="sm:ms-auto" type="single" variant="outline" aria-label={t().periodAria} bind:value={() => period, (next) => { if (next) setPeriod(next as RequestTopPeriod); }}>
          {#each requestTopPeriods as item (item)}
            <ToggleGroup.Item value={item}>{t().periods[item]}</ToggleGroup.Item>
          {/each}
        </ToggleGroup.Root>
      </div>
      {#if view.rows.length}
        <DataTablePagination {table} placement="top" />
        <DataTable {table} ariaLabel={title} class="min-w-180 lg:table-fixed" />
        <DataTablePagination {table} />
      {:else if view.total > 0}
        <Skeleton class="h-96 w-full" />
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><TextBulletListLtr20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{filtered ? t().filteredEmpty : view.settings.enabled ? t().emptyTitle : t().offTitle}</Empty.Title>
            {#if !filtered && !view.settings.enabled}<Empty.Description>{t().offHint}</Empty.Description>{/if}
          </Empty.Header>
          {#if filtered}
            <Empty.Content><Button variant="outline" onclick={resetFilters}>{t().reset}</Button></Empty.Content>
          {:else if !view.settings.enabled}
            <Empty.Content><Button variant="outline" onclick={() => { showLog(); }}>{t().openLog}</Button></Empty.Content>
          {/if}
        </Empty.Root>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>
