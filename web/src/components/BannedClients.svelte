<script lang="ts">
  import { untrack } from 'svelte';
  import { type ColumnDef, type FilterFn, createColumnHelper, createTable, renderSnippet } from '@tanstack/svelte-table';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { displayName, knownRules } from '../blockRules.svelte';
  import { formatClock } from '../format';
  import { intlLocale, translations } from '../i18n.svelte';
  import { ArrowReset20, ArrowSync, Settings, Shield20, Warning } from '../icons';
  import type { ClientBan, ClientBansView } from '../types';
  import ClientBanSettingsDialog from './ClientBanSettingsDialog.svelte';
  import DataTable from './data-table/DataTable.svelte';
  import DataTablePagination from './data-table/DataTablePagination.svelte';
  import DataTableSearch from './data-table/DataTableSearch.svelte';
  import { features, type DataTableFeatures } from './data-table/features';
  import { urlTableState } from './data-table/urlTable.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TruncateText from './TruncateText.svelte';

  // 설정 > 보안의 차단된 클라이언트 카드. 클라이언트 차단 규칙(허니팟 등)에 해당해 차단된 클라이언트를 보여 주고 해제합니다.
  // 표는 shadcn Data Table 구성(TanStack + DataTable + DataTablePagination)이며 거르기와 페이지 나누기는 서버가 합니다(LogCard와 같은 방식):
  // 검색어와 페이지는 주소 query가 단일 출처이고(bans_q, bans_page), 같은 화면의 다른 표와 겹치지 않도록 이름 앞에 bans_를 붙입니다.
  // 차단 시간과 제외 대상은 머리글의 설정 대화상자에서 저장하고, 해제는 바로 서버에 반영합니다.
  const t = translations({
    title: 'Banned clients',
    tip: 'Clients banned for sending a request that matched a ban rule. Every request from them is refused until the ban ends. Bans are kept in memory and cleared when the Outlet restarts. IPv4 clients are banned per address and IPv6 clients per /64.',
    search: 'Search by client, address or path',
    refresh: 'Refresh',
    settings: 'Ban settings',
    reset: 'Reset search',
    client: 'Client',
    rule: 'Rule',
    request: 'Request',
    time: 'Banned / releases',
    hits: 'Blocked',
    actions: 'Actions',
    removed: 'Deleted',
    release: 'Release',
    releaseClient: (client: string) => `Release ${client}`,
    released: 'Released',
    releaseAll: 'Release all',
    releaseAllTitle: 'Release all banned clients?',
    releaseAllHint: 'Every banned client can send requests again at once.',
    releasedAll: 'All bans released',
    cancel: 'Cancel',
    emptyTitle: 'No banned clients',
    filteredEmpty: 'No matching clients',
    errors: { ban_not_found: 'Already released' } as Record<string, string>,
    failed: (code: string) => `Couldn't release: ${code}`,
    loadFailed: (code: string) => `Couldn't load banned clients: ${code}`,
  }, {
    title: '차단된 클라이언트',
    tip: '클라이언트 차단 규칙에 해당하여 차단된 클라이언트입니다. 차단 기간에는 해당 클라이언트의 모든 요청이 거부됩니다. 차단 목록은 Outlet 메모리에만 저장되어 재시작하면 초기화되며, IPv4는 주소별, IPv6는 /64 대역별로 차단됩니다.',
    search: '클라이언트, 주소, 경로 검색',
    refresh: '새로 고침',
    settings: '차단 설정',
    reset: '검색 초기화',
    client: '클라이언트',
    rule: '규칙',
    request: '요청',
    time: '차단 시각 / 해제 예정',
    hits: '차단 횟수',
    actions: '작업',
    removed: '삭제됨',
    release: '해제',
    releaseClient: (client: string) => `${client} 해제`,
    released: '해제되었습니다',
    releaseAll: '모두 해제',
    releaseAllTitle: '모든 차단을 해제하시겠습니까?',
    releaseAllHint: '차단된 모든 클라이언트의 요청이 즉시 다시 허용됩니다.',
    releasedAll: '모든 차단이 해제되었습니다',
    cancel: '취소',
    emptyTitle: '차단된 클라이언트가 없습니다',
    filteredEmpty: '조건에 해당하는 클라이언트가 없습니다',
    errors: { ban_not_found: '이미 해제되었습니다' },
    failed: (code: string) => `해제 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `차단된 클라이언트를 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const pageSize = 10;
  const endpoint = '/api/v1/security/client-bans';

  // 검색어는 표의 숨은 열입니다(주소의 이름은 bans_q, 서버 query의 이름은 q).
  const searchColumn = 'filter_bans_q';
  const urlTable = urlTableState({ filters: [{ param: 'bans_q', column: searchColumn }], sortable: [], defaultSort: [], pageSize, prefix: 'bans_' });
  const tableState = $derived(urlTable.state);

  let view = $state<ClientBansView | null>(null);

  const columnHelper = createColumnHelper<DataTableFeatures, ClientBan>();
  // 거르는 일은 서버가 하므로 필터 함수는 모든 행을 통과시킵니다.
  const serverFiltered: FilterFn<DataTableFeatures, ClientBan> = () => true;
  const columns = [
    ...columnHelper.columns([
      columnHelper.display({ id: 'client', header: () => t().client, cell: ({ row }) => renderSnippet(clientCell, row.original), enableSorting: false, meta: { class: 'w-32' } }),
      columnHelper.display({ id: 'rule', header: () => t().rule, cell: ({ row }) => renderSnippet(ruleCell, row.original), enableSorting: false, meta: { class: 'w-24' } }),
      columnHelper.display({ id: 'request', header: () => t().request, cell: ({ row }) => renderSnippet(requestCell, row.original), enableSorting: false }),
      columnHelper.display({ id: 'time', header: () => t().time, cell: ({ row }) => renderSnippet(timeCell, row.original), enableSorting: false, meta: { class: 'w-36' } }),
      columnHelper.display({ id: 'hits', header: () => t().hits, cell: ({ row }) => renderSnippet(hitsCell, row.original), enableSorting: false, meta: { class: 'w-16 text-right' } }),
      columnHelper.display({ id: 'actions', header: () => renderSnippet(actionsHeader, undefined), cell: ({ row }) => renderSnippet(actionCell, row.original), enableSorting: false, meta: { class: 'w-20' } }),
    ]),
    columnHelper.accessor(() => '', { id: searchColumn, header: () => '', cell: () => '', enableSorting: false, filterFn: serverFiltered }),
  ] as ColumnDef<DataTableFeatures, ClientBan, any>[];
  const table = createTable({
    features,
    get data() { return view?.bans ?? []; },
    columns,
    getRowId: (row) => row.client,
    manualPagination: true,
    manualFiltering: true,
    get rowCount() { return view?.total; },
    initialState: { columnVisibility: untrack(() => ({ [searchColumn]: false })) },
    ...urlTable.handlers,
    get state() { return urlTable.state; },
  });

  const search = $derived((tableState.columnFilters.find((filter) => filter.id === searchColumn)?.value as string | undefined) ?? '');
  const filtered = $derived(search !== '');
  const query = $derived.by(() => {
    const params = new URLSearchParams({ page: String(tableState.pagination.pageIndex + 1), per_page: String(tableState.pagination.pageSize) });
    if (search) params.set('q', search);
    return params.toString();
  });

  let loadError = $state('');
  let reloads = $state(0);
  let busy = $state(false);
  let releasing = $state('');
  let settingsOpen = $state(false);
  let confirmOpen = $state(false);

  $effect(() => {
    void reloads;
    const current = query;
    let active = true;
    api<ClientBansView>(`${endpoint}?${current}`)
      .then((next) => {
        if (!active) return;
        // Go는 빈 목록을 null로 보낼 수 있습니다.
        view = { ...next, settings: { ...next.settings, exempt: next.settings.exempt ?? [] }, bans: next.bans ?? [] };
        loadError = '';
        // 범위를 벗어난 페이지 번호는 첫 페이지로 되돌립니다.
        if (next.total > 0 && !next.bans?.length && tableState.pagination.pageIndex > 0) table.setPageIndex(0);
      })
      .catch((cause) => { if (active) loadError = t().loadFailed(errorMessage(cause)); });
    return () => { active = false; };
  });

  // 설정이나 해제를 저장한 응답은 검색 없는 첫 페이지이므로, 설정만 반영하고 목록은 지금 조건으로 다시 받습니다.
  function adopt(next: ClientBansView): void {
    if (view) view = { ...view, settings: { ...next.settings, exempt: next.settings.exempt ?? [] } };
    reloads += 1;
  }

  async function release(body: { client: string } | { all: true }): Promise<void> {
    if (busy) return;
    busy = true;
    releasing = 'client' in body ? body.client : '';
    try {
      adopt(await api<ClientBansView>(`${endpoint}/release`, { method: 'POST', body: JSON.stringify(body) }));
      toast.success('client' in body ? t().released : t().releasedAll);
    } catch (cause) {
      const code = errorMessage(cause);
      toast.error(t().errors[code] ?? t().failed(code));
      reloads += 1;
    } finally {
      busy = false;
      releasing = '';
      confirmOpen = false;
    }
  }

  const resetSearch = (): void => { table.setColumnFilters([]); };

  // 규칙 id는 차단 규칙 카드가 불러온 이름으로 바꿔 보여 주고, 지워진 규칙은 "삭제됨"으로 둡니다.
  const ruleName = (id: string): string | null => {
    const rules = knownRules.rules;
    if (!rules) return id;
    const rule = rules.find((entry) => entry.id === id);
    return rule ? displayName(rule) : null;
  };
  const number = (value: number): string => value.toLocaleString(intlLocale());
  // 차단은 대부분 오늘 시작해 오늘 끝나므로 시각만 보이고, 다른 날이면 월·일을 덧붙입니다.
  const today = (at: string): boolean => new Date(at).toDateString() === new Date().toDateString();
  const shortDay = (at: string): string => new Date(at).toLocaleDateString(intlLocale(), { month: 'numeric', day: 'numeric' });
</script>

{#snippet clientCell(ban: ClientBan)}
  <TruncateText text={ban.client} class="font-mono" />
{/snippet}

{#snippet ruleCell(ban: ClientBan)}
  {@const name = ruleName(ban.rule)}
  {#if name === null}
    <span class="block truncate text-muted-foreground">{t().removed}</span>
  {:else}
    <TruncateText text={name} />
  {/if}
{/snippet}

{#snippet requestCell(ban: ClientBan)}
  <div class="grid gap-0.5">
    <TruncateText text={ban.host} class="font-mono" />
    <TruncateText text={ban.path} class="font-mono text-muted-foreground" />
  </div>
{/snippet}

{#snippet timeCell(ban: ClientBan)}
  <div class="grid gap-0.5 whitespace-nowrap tabular-nums">
    <span>{formatClock(ban.since)}{#if !today(ban.since)} <span class="text-muted-foreground">{shortDay(ban.since)}</span>{/if}</span>
    <span class="text-muted-foreground">{formatClock(ban.until)}{#if !today(ban.until)} {shortDay(ban.until)}{/if}</span>
  </div>
{/snippet}

{#snippet hitsCell(ban: ClientBan)}
  <span class="tabular-nums">{number(ban.hits)}</span>
{/snippet}

{#snippet actionsHeader()}
  <span class="sr-only">{t().actions}</span>
{/snippet}

{#snippet actionCell(ban: ClientBan)}
  <div class="flex justify-end">
    <Button variant="outline" size="sm" disabled={busy} aria-label={t().releaseClient(ban.client)} onclick={() => release({ client: ban.client })}>{#if releasing === ban.client}<Spinner aria-hidden="true" />{/if}{t().release}</Button>
  </div>
{/snippet}

<Card.Root size="sm" role="region" aria-labelledby="banned-clients-title">
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id="banned-clients-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="banned-clients-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if view}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <div class="flex items-center gap-2">
          <IconButton label={t().refresh} icon={ArrowSync} onclick={() => { reloads += 1; }} />
          <IconButton label={t().settings} icon={Settings} onclick={() => { settingsOpen = true; }} />
          <Button variant="outline" size="sm" disabled={busy || (view.total === 0 && !filtered)} onclick={() => { confirmOpen = true; }}>{t().releaseAll}</Button>
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
      <Skeleton class="h-48 w-full" />
    {:else}
      {#if view.total > 0 || filtered}
        <div class="flex flex-wrap items-center gap-2" role="search" aria-label={t().search}>
          <DataTableSearch {table} column={searchColumn} label={t().search} class="sm:max-w-72" />
          {#if filtered}<IconButton variant="outline" size="icon" label={t().reset} icon={ArrowReset20} onclick={resetSearch} />{/if}
        </div>
      {/if}
      {#if view.bans.length}
        <DataTablePagination {table} placement="top" />
        <DataTable {table} ariaLabel={t().title} class="min-w-160 lg:table-fixed" />
        <DataTablePagination {table} />
      {:else if view.total > 0}
        <Skeleton class="h-48 w-full" />
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><Shield20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{filtered ? t().filteredEmpty : t().emptyTitle}</Empty.Title>
          </Empty.Header>
        </Empty.Root>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>

{#if view}
  <ClientBanSettingsDialog bind:open={settingsOpen} settings={view.settings} onSaved={adopt} />
{/if}

<AlertDialog.Root bind:open={() => confirmOpen, (open) => { if (!busy) confirmOpen = open; }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{t().releaseAllTitle}</AlertDialog.Title>
      <AlertDialog.Description>{t().releaseAllHint}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>{t().cancel}</AlertDialog.Cancel>
      <Button disabled={busy} onclick={() => release({ all: true })}>{#if busy}<Spinner aria-hidden="true" />{/if}{t().releaseAll}</Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
