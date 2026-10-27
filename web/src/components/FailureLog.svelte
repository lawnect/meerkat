<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Warning20 } from '../icons';
  import { failureCauses, type FailureCause, type FailureLogEntry } from '../types';
  import { oneOf, param, setParams } from '../urlState.svelte';
  import LogCard from './LogCard.svelte';
  import TruncateText from './TruncateText.svelte';

  // 관찰의 실패 요청 탭. 공개 주소로 온 요청 중 실패했거나 앱에 닿기 전에 Outlet이 거부한 요청을 원인과 주소로 거르며(?cause=&host=) 보여 줍니다.
  // addresses는 콘솔이 이미 불러온 공개 주소이고, 주소에서 온 값이 목록에 없으면(지워진 주소 등) 그 값도 선택지에 남깁니다.
  let { addresses }: { addresses: string[] } = $props();

  const t = translations({
    title: 'Failed requests',
    tip: 'Public requests to routed addresses that failed, or that the Outlet refused before they reached an app. Kept in a file of its own within the size limit and retention period, the oldest going first. Requests are never held up by logging; when it can\'t keep up, entries are given up.',
    emptyTitle: 'No failed requests logged',
    offTitle: 'Logging is disabled',
    offHint: 'Enable it to keep failed requests.',
    cause: 'Cause',
    host: 'Address',
    allCauses: 'All causes',
    allHosts: 'All addresses',
    causes: {
      app_error: 'App error', unreachable: 'Unreachable', timeout: 'Timeout', no_service: 'No service', rate_limited: 'Rate limited',
      blocked: 'Blocked', too_long: 'Path too long', too_large: 'Body too large', busy: 'Connection limit',
    } as Record<FailureCause, string>,
    meanings: {
      app_error: 'The app answered with a 5xx.',
      unreachable: 'The Outlet couldn\'t reach the service through its tunnel.',
      timeout: 'The service didn\'t answer in time.',
      no_service: 'The address has no live service.',
      rate_limited: 'Refused by a rate limit.',
      blocked: 'Refused by a block rule.',
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
    cause: '원인',
    host: '주소',
    allCauses: '모든 원인',
    allHosts: '모든 주소',
    causes: {
      app_error: '앱 오류', unreachable: '연결 실패', timeout: '응답 시간 초과', no_service: '서비스 없음', rate_limited: '요청 제한',
      blocked: '차단', too_long: '경로 길이 초과', too_large: '본문 크기 초과', busy: '동시 연결 제한',
    },
    meanings: {
      app_error: '앱이 5xx로 응답했습니다.',
      unreachable: 'Outlet이 터널을 통해 서비스에 연결하지 못했습니다.',
      timeout: '서비스가 제한 시간 내에 응답하지 않았습니다.',
      no_service: '이 주소에 연결된 활성 서비스가 없습니다.',
      rate_limited: '요청 제한으로 거부되었습니다.',
      blocked: '차단 규칙으로 거부되었습니다.',
      too_long: '경로가 8,192바이트를 초과했습니다.',
      too_large: '요청 본문이 업로드 제한을 초과했습니다.',
      busy: '연결 제한으로 거부되었습니다.',
    },
    request: '요청',
    result: '결과',
    client: '클라이언트',
    loadFailed: (code: string) => `실패 요청을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const causeOptions = ['', ...failureCauses] as const;
  const cause = $derived(oneOf('cause', causeOptions, ''));
  const host = $derived(param('host') ?? '');
  const hosts = $derived([...new Set([...addresses, ...(host ? [host] : [])])].sort());

  // 서버 쪽 문제(앱, 연결, 서비스)는 붉은색, Outlet이 규칙에 따라 거부한 요청은 보조 색입니다.
  const badge = (value: FailureCause) => (['app_error', 'unreachable', 'timeout', 'no_service'].includes(value) ? 'destructive' : 'secondary');
  const duration = (ms: number): string => (ms < 1000 ? `${ms.toLocaleString(intlLocale())} ms` : `${(ms / 1000).toLocaleString(intlLocale(), { maximumFractionDigits: 1 })} s`);
</script>

<LogCard
  kind="failure"
  title={t().title}
  tip={t().tip}
  emptyTitle={t().emptyTitle}
  offTitle={t().offTitle}
  offHint={t().offHint}
  emptyIcon={Warning20}
  loadFailed={t().loadFailed}
  filters={{ cause, host }}
  columns={[{ label: t().request }, { label: t().result, class: 'w-52' }, { label: t().client, class: 'w-40' }]}
>
  {#snippet filterControls()}
    <NativeSelect.Root size="sm" class="w-full sm:w-44" aria-label={t().cause} bind:value={() => cause, (next) => { setParams({ cause: next, page: null }); }}>
      <NativeSelect.Option value="">{t().allCauses}</NativeSelect.Option>
      {#each failureCauses as item (item)}<NativeSelect.Option value={item}>{t().causes[item]}</NativeSelect.Option>{/each}
    </NativeSelect.Root>
    <NativeSelect.Root size="sm" class="w-full sm:w-64" aria-label={t().host} bind:value={() => host, (next) => { setParams({ host: next, page: null }); }}>
      <NativeSelect.Option value="">{t().allHosts}</NativeSelect.Option>
      {#each hosts as item (item)}<NativeSelect.Option value={item}>{item}</NativeSelect.Option>{/each}
    </NativeSelect.Root>
  {/snippet}
  {#snippet cells(entry: FailureLogEntry)}
    <Table.Cell class="align-top">
      <div class="grid gap-0.5">
        <TruncateText text={entry.host} class="font-mono" />
        <span class="flex min-w-0 items-center gap-1.5 font-mono text-muted-foreground"><span>{entry.method}</span><TruncateText text={entry.path} /></span>
      </div>
    </Table.Cell>
    <Table.Cell class="align-top">
      <div class="grid gap-0.5">
        <span class="flex min-w-0 items-center gap-1.5">
          <Tooltip.Root>
            <Tooltip.Trigger class="inline-flex shrink-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
              <Badge variant={badge(entry.cause)} class="cursor-default select-none">{t().causes[entry.cause] ?? entry.cause}</Badge>
            </Tooltip.Trigger>
            <Tooltip.Content class="max-w-xs">{t().meanings[entry.cause] ?? entry.cause}</Tooltip.Content>
          </Tooltip.Root>
          <span class="font-mono text-muted-foreground tabular-nums">{entry.status}</span>
        </span>
        <TruncateText text={entry.service || '—'} class="text-muted-foreground" />
      </div>
    </Table.Cell>
    <Table.Cell class="pr-4 align-top">
      <div class="grid gap-0.5">
        <span class="font-mono">{entry.client}</span>
        <span class="text-muted-foreground tabular-nums">{duration(entry.duration_ms)}</span>
      </div>
    </Table.Cell>
  {/snippet}
</LogCard>
