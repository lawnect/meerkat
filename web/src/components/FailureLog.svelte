<script lang="ts">
  import { createColumnHelper, renderSnippet } from '@tanstack/svelte-table';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Warning20 } from '../icons';
  import { refusedCauses, serviceCauses, type FailureCause, type FailureLogEntry } from '../types';
  import { param } from '../urlState.svelte';
  import type { DataTableFeatures } from './data-table/features';
  import LogCard from './LogCard.svelte';
  import type { LogFilter } from './logFilter';
  import TruncateText from './TruncateText.svelte';

  // 관찰의 실패 요청 탭. 공개 주소로 온 요청 중 실패했거나 앱에 닿기 전에 Outlet이 거부한 요청을 검색어와 원인, 주소로 거르며(?q=&cause=&host=) 보여 줍니다.
  // 원인은 서비스 오류(앱 오류·연결 실패·응답 시간 초과·서비스 없음)와 Outlet 거부(요청 제한·차단·허니팟·차단된 클라이언트·길이·크기·연결 제한)로 묶어 고를 수 있어,
  // 누구나 만들 수 있는 거부 기록 속에서 서비스 문제를 바로 찾습니다.
  // addresses는 콘솔이 이미 불러온 공개 주소이고, 주소에서 온 값이 목록에 없으면(지워진 주소 등) 그 값도 선택지에 남깁니다.
  let { addresses }: { addresses: string[] } = $props();

  const t = translations({
    title: 'Failed requests',
    tip: 'Public requests to routed addresses that failed, or that the Outlet refused before they reached an app. Kept in a file of its own within the size limit and retention period, the oldest going first. Requests are never held up by logging; when it can\'t keep up, entries are given up.',
    emptyTitle: 'No failed requests logged',
    offTitle: 'Logging is disabled',
    offHint: 'Enable it to keep failed requests.',
    suppressedTip: 'Requests the Outlet refused are logged up to 60 per minute per client. Anything over that is only counted, and the count is since the Outlet started.',
    search: 'Search by address, path, client or service',
    cause: 'Cause',
    host: 'Address',
    allCauses: 'All causes',
    allService: 'All service errors',
    allRefused: 'All Outlet refusals',
    serviceGroup: 'Service errors',
    refusedGroup: 'Outlet refusals',
    allHosts: 'All addresses',
    causes: {
      app_error: 'App error', unreachable: 'Unreachable', timeout: 'Timeout', no_service: 'No service', rate_limited: 'Rate limited',
      blocked: 'Blocked', trapped: 'Honeypot hit', banned: 'Banned client', too_long: 'Path too long', too_large: 'Body too large', busy: 'Connection limit',
    } as Record<FailureCause, string>,
    meanings: {
      app_error: 'The app answered with a 5xx.',
      unreachable: 'The Outlet couldn\'t reach the service through its tunnel.',
      timeout: 'The service didn\'t answer in time.',
      no_service: 'The address has no live service.',
      rate_limited: 'Refused by a rate limit.',
      blocked: 'Refused by a block rule.',
      trapped: 'Matched a honeypot rule, so the client was banned.',
      banned: 'Refused because the client is banned.',
      too_long: 'The path was over 8,192 bytes.',
      too_large: 'The request body was over the upload limit.',
      busy: 'Refused by a connection limit.',
    } as Record<FailureCause, string>,
    request: 'Request',
    result: 'Result',
    client: 'Client',
    loadFailed: (code: string) => `Couldn't load the failed requests: ${code}`,
  }, {
    title: '실패 요청',
    tip: '공개 주소로 온 요청 중 실패했거나, 앱에 도달하기 전에 Outlet이 거부한 요청을 기록합니다. 별도 파일에 용량 상한과 보관 기간 범위 내에서 보관하며, 오래된 기록부터 삭제됩니다. 기록 때문에 요청이 지연되지 않으며, 처리량을 초과하면 기록을 포기합니다.',
    emptyTitle: '기록된 실패 요청이 없습니다',
    offTitle: '기록이 비활성화되어 있습니다',
    offHint: '활성화하면 실패한 요청을 기록합니다.',
    suppressedTip: 'Outlet이 거부한 요청은 클라이언트별로 분당 60건까지만 기록합니다. 초과분은 건수만 집계하며, Outlet 시작 이후의 누계입니다.',
    search: '주소, 경로, 클라이언트, 서비스 검색',
    cause: '원인',
    host: '주소',
    allCauses: '모든 원인',
    allService: '서비스 오류 전체',
    allRefused: 'Outlet 거부 전체',
    serviceGroup: '서비스 오류',
    refusedGroup: 'Outlet 거부',
    allHosts: '모든 주소',
    causes: {
      app_error: '앱 오류', unreachable: '연결 실패', timeout: '응답 시간 초과', no_service: '서비스 없음', rate_limited: '요청 제한',
      blocked: '차단', trapped: '허니팟 감지', banned: '차단된 클라이언트', too_long: '경로 길이 초과', too_large: '본문 크기 초과', busy: '동시 연결 제한',
    },
    meanings: {
      app_error: '앱이 5xx로 응답했습니다.',
      unreachable: 'Outlet이 터널을 통해 서비스에 연결하지 못했습니다.',
      timeout: '서비스가 제한 시간 내에 응답하지 않았습니다.',
      no_service: '이 주소에 연결된 활성 서비스가 없습니다.',
      rate_limited: '요청 제한으로 거부되었습니다.',
      blocked: '차단 규칙으로 거부되었습니다.',
      trapped: '허니팟 규칙에 해당하여 클라이언트를 차단했습니다.',
      banned: '차단된 클라이언트의 요청을 거부했습니다.',
      too_long: '경로가 8,192바이트를 초과했습니다.',
      too_large: '요청 본문이 업로드 제한을 초과했습니다.',
      busy: '연결 제한으로 거부되었습니다.',
    },
    request: '요청',
    result: '결과',
    client: '클라이언트',
    loadFailed: (code: string) => `실패 요청을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const host = $derived(param('host') ?? '');
  const hosts = $derived([...new Set([...addresses, ...(host ? [host] : [])])].sort());
  const causeOption = (value: FailureCause) => ({ value, label: t().causes[value] });

  const filters = $derived<LogFilter[]>([
    {
      param: 'cause',
      label: t().cause,
      class: 'sm:w-44',
      options: [
        { value: '', label: t().allCauses },
        { value: 'service', label: t().allService },
        { value: 'refused', label: t().allRefused },
        { group: t().serviceGroup, options: serviceCauses.map(causeOption) },
        { group: t().refusedGroup, options: refusedCauses.map(causeOption) },
      ],
    },
    { param: 'host', label: t().host, class: 'sm:w-56', free: true, options: [{ value: '', label: t().allHosts }, ...hosts.map((item) => ({ value: item, label: item }))] },
  ]);

  const columnHelper = createColumnHelper<DataTableFeatures, FailureLogEntry>();
  const columns = columnHelper.columns([
    columnHelper.display({ id: 'request', header: () => t().request, cell: ({ row }) => renderSnippet(requestCell, row.original), enableSorting: false }),
    columnHelper.display({ id: 'result', header: () => t().result, cell: ({ row }) => renderSnippet(resultCell, row.original), enableSorting: false, meta: { class: 'w-52' } }),
    columnHelper.display({ id: 'client', header: () => t().client, cell: ({ row }) => renderSnippet(clientCell, row.original), enableSorting: false, meta: { class: 'w-40' } }),
  ]);

  // 서비스 쪽 문제(앱, 연결, 서비스)는 붉은색, Outlet이 규칙에 따라 거부한 요청(routine)은 보조 색입니다.
  const badge = (entry: FailureLogEntry) => (entry.routine ? 'secondary' : 'destructive');
  const duration = (ms: number): string => (ms < 1000 ? `${ms.toLocaleString(intlLocale())} ms` : `${(ms / 1000).toLocaleString(intlLocale(), { maximumFractionDigits: 1 })} s`);
</script>

{#snippet requestCell(entry: FailureLogEntry)}
  <div class="grid gap-0.5">
    <TruncateText text={entry.host} class="font-mono" />
    <span class="flex min-w-0 items-center gap-1.5 font-mono text-muted-foreground"><span>{entry.method}</span><TruncateText text={entry.path} /></span>
  </div>
{/snippet}

{#snippet resultCell(entry: FailureLogEntry)}
  <div class="grid gap-0.5">
    <span class="flex min-w-0 items-center gap-1.5">
      <Tooltip.Root>
        <Tooltip.Trigger class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
          <Badge variant={badge(entry)} class="cursor-default select-none">{t().causes[entry.cause] ?? entry.cause}</Badge>
        </Tooltip.Trigger>
        <Tooltip.Content class="max-w-xs">{t().meanings[entry.cause] ?? entry.cause}</Tooltip.Content>
      </Tooltip.Root>
      <span class="font-mono text-muted-foreground tabular-nums">{entry.status}</span>
    </span>
    <TruncateText text={entry.service || '—'} class="text-muted-foreground" />
  </div>
{/snippet}

{#snippet clientCell(entry: FailureLogEntry)}
  <div class="grid gap-0.5">
    <span class="font-mono">{entry.client}</span>
    <span class="text-muted-foreground tabular-nums">{duration(entry.duration_ms)}</span>
  </div>
{/snippet}

<LogCard
  kind="failure"
  title={t().title}
  tip={t().tip}
  suppressedTip={t().suppressedTip}
  searchLabel={t().search}
  emptyTitle={t().emptyTitle}
  offTitle={t().offTitle}
  offHint={t().offHint}
  emptyIcon={Warning20}
  loadFailed={t().loadFailed}
  {filters}
  {columns}
/>
