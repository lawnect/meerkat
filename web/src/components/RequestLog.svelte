<script lang="ts">
  import { createColumnHelper, renderSnippet } from '@tanstack/svelte-table';
  import { Badge, type BadgeVariant } from '$lib/components/ui/badge/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { formatBytes, formatDuration, formatLatency } from '../format';
  import { translations } from '../i18n.svelte';
  import { TextBulletListLtr20 } from '../icons';
  import { refusedCauses, requestClasses, type RequestClass, type RequestLogEntry } from '../types';
  import { oneOf, param, setParams } from '../urlState.svelte';
  import CauseBadge from './CauseBadge.svelte';
  import type { DataTableFeatures } from './data-table/features';
  import LogCard from './LogCard.svelte';
  import type { LogFilter } from './logFilter';
  import RequestTop from './RequestTop.svelte';
  import TruncateText from './TruncateText.svelte';

  // 관찰의 요청 기록 탭. 위의 선택(?view=list|endpoints|clients)으로 요청 기록(목록)과, 같은 기록을 엔드포인트별·클라이언트별로 묶은 통계(RequestTop)를 오갑니다.
  // 목록은 공개 주소로 온 모든 요청을 검색어와 상태 종류, 주소로 거르며(?q=&class=&host=), 통계의 "기록 보기"에서 넘어온 경로·클라이언트 출처(?path=&source=, IPv4 주소 또는 IPv6 /56)는 칩으로 보여 줍니다.
  // addresses는 콘솔이 이미 불러온 공개 주소이고, 주소에서 온 값이 목록에 없으면(지워진 주소 등) 그 값도 선택지에 남깁니다.
  let { addresses }: { addresses: string[] } = $props();

  const views = ['list', 'endpoints', 'clients'] as const;
  type View = (typeof views)[number];

  const t = translations({
    viewsAria: 'Request log views',
    views: { list: 'Log', endpoints: 'Endpoints', clients: 'Clients' } as Record<View, string>,
    title: 'Request log',
    tip: 'Every public request to a routed address, including those the Outlet refused. The management address and the connector API aren\'t included. Use it to find attack points and for statistics. Kept in a file of its own within the size limit and retention period, the oldest going first. Requests are never held up by logging; when it can\'t keep up, entries are given up.',
    emptyTitle: 'No requests logged',
    offTitle: 'Logging is disabled',
    offHint: 'Enable it to keep every public request.',
    suppressedTip: 'Each client\'s 4xx responses are logged up to 600 per minute and other requests up to 6,000 per minute. Anything over that is only counted, and the count is since the Outlet started.',
    search: 'Search by address, path, client or agent',
    statusClass: 'Status',
    allClasses: 'All statuses',
    classes: { '1xx': '1xx Informational', '2xx': '2xx Success', '3xx': '3xx Redirect', '4xx': '4xx Client error', '5xx': '5xx Server error' } as Record<RequestClass, string>,
    host: 'Address',
    allHosts: 'All addresses',
    path: 'Path',
    client: 'Client',
    request: 'Request',
    result: 'Result',
    outlet: 'Outlet',
    upstream: 'Upstream',
    meanings: {
      101: 'The connection was upgraded, such as to a WebSocket.',
      499: 'The client closed the connection before the response.',
    } as Record<number, string>,
    loadFailed: (code: string) => `Couldn't load the request log: ${code}`,
  }, {
    viewsAria: '요청 기록 보기',
    views: { list: '기록', endpoints: '엔드포인트', clients: '클라이언트' },
    title: '요청 기록',
    tip: '라우팅된 공개 주소로 온 모든 요청을, Outlet이 거부한 요청까지 포함하여 기록합니다. 관리 주소와 커넥터 API는 포함하지 않습니다. 공격 지점 파악과 통계에 활용합니다. 별도 파일에 용량 상한과 보관 기간 범위 내에서 보관하며, 오래된 기록부터 삭제됩니다. 기록 때문에 요청이 지연되지 않으며, 처리량을 초과하면 기록을 포기합니다.',
    emptyTitle: '기록된 요청이 없습니다',
    offTitle: '기록이 비활성화되어 있습니다',
    offHint: '활성화하면 공개 주소로 온 모든 요청을 기록합니다.',
    suppressedTip: '클라이언트별로 4xx 응답은 분당 600건, 그 밖의 요청은 분당 6,000건까지만 기록합니다. 초과분은 건수만 집계하며, Outlet 시작 이후의 누계입니다.',
    search: '주소, 경로, 클라이언트, User-Agent 검색',
    statusClass: '상태',
    allClasses: '모든 상태',
    classes: { '1xx': '1xx 정보', '2xx': '2xx 성공', '3xx': '3xx 리디렉션', '4xx': '4xx 클라이언트 오류', '5xx': '5xx 서버 오류' },
    host: '주소',
    allHosts: '모든 주소',
    path: '경로',
    client: '클라이언트',
    request: '요청',
    result: '결과',
    outlet: 'Outlet',
    upstream: '업스트림',
    meanings: {
      101: '다른 프로토콜(웹소켓 등)로 연결이 전환되었습니다.',
      499: '응답 전에 클라이언트가 연결을 끊었습니다.',
    },
    loadFailed: (code: string) => `요청 기록을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const view = $derived(oneOf('view', views, 'list'));

  // 보기마다 쓰는 주소 query가 달라, 보기를 바꾸면 검색어·필터·페이지를 지웁니다. 통계끼리(엔드포인트 ↔ 클라이언트)는 기간과 정렬을 이어 갑니다.
  function setView(next: View): void {
    const keepsTop = next !== 'list' && view !== 'list';
    setParams({ q: null, page: null, class: null, host: null, path: null, source: null, ...(keepsTop ? {} : { period: null, sort: null }), view: next }, { view: 'list' }, { push: true });
  }

  const host = $derived(param('host') ?? '');
  const hosts = $derived([...new Set([...addresses, ...(host ? [host] : [])])].sort());

  const filters = $derived<LogFilter[]>([
    { param: 'class', label: t().statusClass, class: 'sm:w-44', options: [{ value: '', label: t().allClasses }, ...requestClasses.map((value) => ({ value, label: t().classes[value] }))] },
    { param: 'host', label: t().host, class: 'sm:w-56', free: true, options: [{ value: '', label: t().allHosts }, ...hosts.map((item) => ({ value: item, label: item }))] },
    { param: 'path', label: t().path, chip: true },
    { param: 'source', label: t().client, chip: true },
  ]);

  const columnHelper = createColumnHelper<DataTableFeatures, RequestLogEntry>();
  const columns = columnHelper.columns([
    columnHelper.display({ id: 'request', header: () => t().request, cell: ({ row }) => renderSnippet(requestCell, row.original), enableSorting: false }),
    columnHelper.display({ id: 'result', header: () => t().result, cell: ({ row }) => renderSnippet(resultCell, row.original), enableSorting: false, meta: { class: 'w-44' } }),
    columnHelper.display({ id: 'client', header: () => t().client, cell: ({ row }) => renderSnippet(clientCell, row.original), enableSorting: false, meta: { class: 'w-40' } }),
  ]);

  // 원인이 있는 요청: Outlet이 거부했거나 4xx이면 보조 색, 서비스 쪽 5xx는 붉은색. 원인이 없으면 상태 코드를 배지로 보여 줍니다(4xx 보조 색, 5xx 붉은색).
  const refused = (cause: string): boolean => (refusedCauses as readonly string[]).includes(cause);
  const causeVariant = (entry: RequestLogEntry): BadgeVariant => (entry.status >= 500 && !refused(entry.cause) ? 'destructive' : 'secondary');
  // 전달한 요청만 Outlet 처리와 업스트림 응답 시간을 가집니다(마이크로초, 전달하지 않았으면 0). 0인 쪽은 빼고 "Outlet 0.42 ms · 업스트림 118 ms"처럼 잇습니다.
  const latencySplit = (entry: RequestLogEntry): string => [
    entry.outlet_us ? `${t().outlet} ${formatLatency(entry.outlet_us / 1000)}` : '',
    entry.upstream_us ? `${t().upstream} ${formatLatency(entry.upstream_us / 1000)}` : '',
  ].filter(Boolean).join(' · ');
  const statusVariant = (status: number): BadgeVariant => (status >= 500 ? 'destructive' : status >= 400 ? 'secondary' : 'outline');
</script>

{#snippet requestCell(entry: RequestLogEntry)}
  <div class="grid gap-0.5">
    <TruncateText text={entry.host} class="font-mono" />
    <span class="flex min-w-0 items-center gap-1.5 font-mono text-muted-foreground"><span>{entry.method}</span><TruncateText text={entry.path} /></span>
    {#if entry.agent}<TruncateText text={entry.agent} class="text-muted-foreground" />{/if}
  </div>
{/snippet}

{#snippet resultCell(entry: RequestLogEntry)}
  <div class="grid gap-0.5">
    <span class="flex min-w-0 items-center gap-1.5">
      {#if entry.cause}
        <CauseBadge cause={entry.cause} variant={causeVariant(entry)} />
        <span class="font-mono text-muted-foreground tabular-nums">{entry.status}</span>
      {:else if t().meanings[entry.status]}
        <Tooltip.Root>
          <Tooltip.Trigger class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
            <Badge variant={statusVariant(entry.status)} class="cursor-default font-mono tabular-nums select-none">{entry.status}</Badge>
          </Tooltip.Trigger>
          <Tooltip.Content class="max-w-xs">{t().meanings[entry.status]}</Tooltip.Content>
        </Tooltip.Root>
      {:else}
        <Badge variant={statusVariant(entry.status)} class="font-mono tabular-nums select-none">{entry.status}</Badge>
      {/if}
    </span>
    <TruncateText text={entry.service || '—'} class="text-muted-foreground" />
  </div>
{/snippet}

{#snippet clientCell(entry: RequestLogEntry)}
  {@const split = latencySplit(entry)}
  <div class="grid gap-0.5">
    <span class="font-mono">{entry.client}</span>
    <span class="text-muted-foreground tabular-nums">
      {#if split}
        <Tooltip.Root>
          <Tooltip.Trigger class="cursor-default underline decoration-dotted underline-offset-2 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">{formatDuration(entry.duration_ms)}</Tooltip.Trigger>
          <Tooltip.Content>{split}</Tooltip.Content>
        </Tooltip.Root>
      {:else}
        {formatDuration(entry.duration_ms)}
      {/if}
      · {formatBytes(entry.bytes)}
    </span>
  </div>
{/snippet}

<ToggleGroup.Root type="single" variant="outline" aria-label={t().viewsAria} bind:value={() => view, (next) => { if (next) setView(next as View); }}>
  {#each views as item (item)}
    <ToggleGroup.Item value={item}>{t().views[item]}</ToggleGroup.Item>
  {/each}
</ToggleGroup.Root>

{#if view === 'list'}
  <LogCard
    kind="request"
    title={t().title}
    tip={t().tip}
    suppressedTip={t().suppressedTip}
    searchLabel={t().search}
    emptyTitle={t().emptyTitle}
    offTitle={t().offTitle}
    offHint={t().offHint}
    emptyIcon={TextBulletListLtr20}
    loadFailed={t().loadFailed}
    {filters}
    {columns}
  />
{:else if view === 'endpoints'}
  <RequestTop by="endpoint" {addresses} />
{:else}
  <RequestTop by="client" {addresses} />
{/if}
