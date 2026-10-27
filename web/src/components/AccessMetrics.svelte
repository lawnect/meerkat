<script lang="ts">
  import { onMount } from 'svelte';
  import { createColumnHelper, createTable, renderComponent, renderSnippet } from '@tanstack/svelte-table';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Shield20, Warning } from '../icons';
  import { axisLabel, bucketTitle, createPeriod, trackPeriod } from '../period.svelte';
  import type { AccessAction, AccessCounts, AccessMetrics, AccessPoliciesView, AccessPolicy } from '../types';
  import DataTable from './data-table/DataTable.svelte';
  import { features, type DataTableFeatures } from './data-table/features';
  import SortHeader from './data-table/SortHeader.svelte';
  import { urlTableState } from './data-table/urlTable.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import PeriodControls from './PeriodControls.svelte';
  import TrendChart from './TrendChart.svelte';

  // 관찰의 지표 탭. 접근 정책이 판정한 요청을 통과·로그인 요구·거부로 나눠 기간의 추이와 정책별 합계로 보여 줍니다.
  // 정책별 합계는 shadcn Data Table 구성(클라이언트 표)이며 판정 열로 정렬합니다(?sort=denied.desc, 기본은 정책 순서).
  // 기간(단위, 기준 날짜, 자동 새로고침)은 대시보드의 트래픽 카드와 같은 PeriodControls·period.svelte.ts를 쓰고 주소(?unit=&date=&live=)가 단일 출처입니다.
  type Outcome = keyof AccessCounts;

  const t = translations({
    title: 'Access decisions',
    tip: 'What access policies decided. Passed: let in by a bypass, or by a session or token the policy takes. Sign-in required: sent to sign in (401 for non-browsers). Denied: refused by a block, a session or token the policy doesn\'t let in, or access SSO being off. Requests no policy covers aren\'t counted. Hourly counts are kept for 400 days.',
    outcomes: { passed: 'Passed', challenged: 'Sign-in required', denied: 'Denied' } as Record<Outcome, string>,
    outcomesAria: 'Decisions shown in the chart',
    byPolicy: 'By policy',
    name: 'Name',
    action: 'Action',
    actions: { allow: 'Allow', bypass: 'Bypass', block: 'Block' } as Record<AccessAction, string>,
    removed: 'Deleted policies',
    noPolicies: 'No access policies',
    count: (value: number) => `${value.toLocaleString(intlLocale())} ${value === 1 ? 'request' : 'requests'}`,
    chartAria: (totals: string) => `Access decisions in the selected period: ${totals}`,
    loadFailed: (code: string) => `Couldn't load access decisions: ${code}`,
  }, {
    title: '접근 판정',
    tip: '접근 정책이 요청을 판정한 결과입니다. 통과는 우회, 또는 정책이 받는 세션·토큰으로 들어온 요청, 로그인 요구는 로그인으로 보낸 요청(브라우저가 아니면 401), 거부는 차단, 허용 대상이 아닌 세션·토큰, 접근 보호 SSO 비활성화로 거부한 요청입니다. 정책이 적용되지 않은 요청은 집계되지 않습니다. 시간별 집계는 400일간 보관됩니다.',
    outcomes: { passed: '통과', challenged: '로그인 요구', denied: '거부' },
    outcomesAria: '그래프에 표시할 판정',
    byPolicy: '정책별',
    name: '이름',
    action: '동작',
    actions: { allow: '허용', bypass: '우회', block: '차단' },
    removed: '삭제된 정책',
    noPolicies: '접근 정책이 없습니다',
    count: (value: number) => `${value.toLocaleString(intlLocale())}건`,
    chartAria: (totals: string) => `선택한 기간의 접근 판정: ${totals}`,
    loadFailed: (code: string) => `접근 판정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const outcomes: Outcome[] = ['passed', 'challenged', 'denied'];
  // 거부는 붉은색, 나머지는 주 색 계열의 명도 차이로 구분합니다.
  const colors: Record<Outcome, string> = { passed: 'var(--chart-1)', challenged: 'var(--chart-3)', denied: 'var(--destructive)' };

  const period = createPeriod();
  let metrics = $state<AccessMetrics | null>(null);
  let policies = $state<AccessPolicy[] | null>(null);
  let loadError = $state('');
  let policiesError = $state('');
  let shown = $state<Outcome[]>([...outcomes]);

  trackPeriod(period, {
    load: (query) => api<AccessMetrics>(`/api/v1/observe/access-metrics?${query}`),
    onData: (next) => { metrics = { ...next, buckets: next.buckets ?? [], policies: next.policies ?? {} }; },
    onError: (code) => { loadError = code ? t().loadFailed(code) : ''; },
  });

  // 정책 이름과 동작을 보여 주는 데만 쓰므로 한 번만 불러옵니다.
  onMount(() => {
    api<AccessPoliciesView>('/api/v1/security/access-policies')
      .then((view) => { policies = view.policies; })
      .catch((cause) => { policiesError = t().loadFailed(errorMessage(cause)); });
  });

  const unit = $derived(period.unit);
  // 단위를 바꾼 직후에는 이전 단위의 구간이 남아 있으므로 새 응답이 올 때까지 그리지 않습니다.
  const ready = $derived(metrics?.unit === unit);
  const buckets = $derived(ready && metrics ? metrics.buckets : []);
  const totals = $derived(Object.fromEntries(outcomes.map((outcome) => [outcome, buckets.reduce((sum, bucket) => sum + bucket[outcome], 0)])) as Record<Outcome, number>);

  // 세 판정은 단위가 같으므로 보이는 판정 중 가장 큰 값 하나로 나눠 크기를 견줄 수 있게 그립니다.
  // 아직 오지 않은 구간은 0으로 그리면 급감한 것처럼 보이므로 이미 시작한 구간까지만 선을 그립니다.
  const started = $derived(buckets.filter((bucket) => Date.parse(bucket.start) <= Date.now()));
  const peak = $derived(Math.max(0, ...started.flatMap((bucket) => shown.map((outcome) => bucket[outcome]))));
  const data = $derived(started.map((bucket) => ({ date: new Date(bucket.start), ...Object.fromEntries(outcomes.map((outcome) => [outcome, peak ? bucket[outcome] / peak : 0])) })));
  const series = $derived(outcomes.filter((outcome) => shown.includes(outcome)).map((outcome) => ({
    key: outcome, label: t().outcomes[outcome], color: colors[outcome], scale: peak, format: (value: number) => t().count(value),
  })));
  const domain = $derived<[Date, Date]>(buckets.length ? [new Date(buckets[0].start), new Date(buckets[buckets.length - 1].start)] : [new Date(), new Date()]);
  const ticks = $derived.by(() => {
    const every = Math.ceil(buckets.length / 8) || 1;
    return buckets.filter((_, index) => index % every === 0).map((bucket) => new Date(bucket.start));
  });

  // 정책별 합계는 지금 정책 순서대로 보여 주고, 지워진 정책의 판정은 한 줄로 모읍니다(정렬해도 맨 아래).
  const zero: AccessCounts = { passed: 0, challenged: 0, denied: 0 };
  const removed = $derived.by(() => {
    const current = new Set(policies?.map((policy) => policy.id));
    const sum = { ...zero };
    for (const [id, counts] of Object.entries(metrics?.policies ?? {})) {
      if (!current.has(id)) for (const outcome of outcomes) sum[outcome] += counts[outcome];
    }
    return outcomes.some((outcome) => sum[outcome]) ? sum : null;
  });
  const number = (value: number) => value.toLocaleString(intlLocale());

  type PolicyRow = { id: string; order: number; policy: AccessPolicy | null; counts: AccessCounts };
  const rows = $derived<PolicyRow[]>([
    ...(policies ?? []).map((policy, index) => ({ id: policy.id, order: index + 1, policy, counts: metrics?.policies[policy.id] ?? zero })),
    ...(removed ? [{ id: 'removed', order: 0, policy: null, counts: removed }] : []),
  ]);
  const columnHelper = createColumnHelper<DataTableFeatures, PolicyRow>();
  type SortColumn = { getIsSorted: () => false | 'asc' | 'desc'; getToggleSortingHandler: () => ((event: unknown) => void) | undefined };
  const sortHeader = (column: SortColumn, label: string) => renderComponent(SortHeader, { label, sorted: column.getIsSorted(), onclick: column.getToggleSortingHandler(), align: 'end' });
  const columns = columnHelper.columns([
    columnHelper.accessor('order', { header: () => renderSnippet(orderHead), cell: ({ row }) => renderSnippet(orderCell, row.original), enableSorting: false, meta: { class: 'w-12' } }),
    columnHelper.accessor((row) => row.policy?.name ?? '', { id: 'name', header: () => t().name, cell: ({ row }) => renderSnippet(nameCell, row.original), enableSorting: false }),
    columnHelper.accessor((row) => row.policy?.action ?? '', { id: 'action', header: () => t().action, cell: ({ row }) => renderSnippet(actionCell, row.original), enableSorting: false }),
    // 지워진 정책 줄은 값이 없는 것으로 보고 정렬 방향과 관계없이 맨 아래에 둡니다.
    ...outcomes.map((outcome) => columnHelper.accessor((row) => (row.policy ? row.counts[outcome] : undefined), {
      id: outcome,
      header: ({ column }) => sortHeader(column, t().outcomes[outcome]),
      cell: ({ row }) => renderSnippet(countCell, { row: row.original, outcome }),
      sortFn: 'basic',
      sortUndefined: 'last',
      meta: { class: 'text-right' },
    })),
  ]);
  // 정책 수만큼 한 번에 보여 주므로 페이지로 나누지 않습니다.
  const urlTable = urlTableState({ filters: [], sortable: outcomes, defaultSort: [], pageSize: Number.MAX_SAFE_INTEGER });
  const table = createTable({
    features,
    get data() { return rows; },
    columns,
    getRowId: (row) => row.id,
    sortDescFirst: false,
    ...urlTable.handlers,
    get state() { return urlTable.state; },
  });
</script>

{#snippet orderHead()}<span class="sr-only">#</span>{/snippet}

{#snippet orderCell(row: PolicyRow)}
  <span class="text-muted-foreground tabular-nums">{row.order ? number(row.order) : ''}</span>
{/snippet}

{#snippet nameCell(row: PolicyRow)}
  {#if row.policy}
    <span class={['block max-w-64 truncate font-medium', !row.policy.enabled && 'text-muted-foreground']}>{row.policy.name}</span>
  {:else}
    <span class="text-muted-foreground">{t().removed}</span>
  {/if}
{/snippet}

{#snippet actionCell(row: PolicyRow)}
  {#if row.policy}<Badge variant={row.policy.action === 'block' ? 'destructive' : row.policy.action === 'bypass' ? 'secondary' : 'outline'} class="select-none">{t().actions[row.policy.action]}</Badge>{/if}
{/snippet}

{#snippet countCell({ row, outcome }: { row: PolicyRow; outcome: Outcome })}
  <span class={['tabular-nums', (!row.counts[outcome] || !(row.policy?.enabled ?? false)) && 'text-muted-foreground']}>{number(row.counts[outcome])}</span>
{/snippet}

<Card.Root size="sm" role="region" aria-labelledby="access-metrics-title">
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id="access-metrics-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="access-metrics-tip" label={t().title} description={t().tip} /></Card.Title>
    <PeriodControls {period} />
  </Card.Header>
  {#if loadError}
    <Card.Content><Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root></Card.Content>
  {:else if !ready}
    <Card.Content class="flex justify-center py-8"><Spinner /></Card.Content>
  {:else}
    <Card.Content class="grid min-w-0 gap-4">
      <ToggleGroup.Root
        type="multiple"
        variant="outline"
        spacing={2}
        class="grid w-full grid-cols-3"
        aria-label={t().outcomesAria}
        bind:value={() => shown, (next) => { if (next.length) shown = next as Outcome[]; }}
      >
        {#each outcomes as outcome (outcome)}
          <ToggleGroup.Item value={outcome} class="h-auto min-w-0 flex-col items-start gap-0.5 px-3 py-2 text-start">
            <span class="flex w-full min-w-0 items-center gap-1.5 text-xs font-normal text-muted-foreground">
              <span class={['size-2.5 shrink-0', !shown.includes(outcome) && 'opacity-30']} style:background-color={colors[outcome]} aria-hidden="true"></span>
              <span class="truncate">{t().outcomes[outcome]}</span>
            </span>
            <span class="truncate text-xl font-semibold tabular-nums">{number(totals[outcome])}</span>
          </ToggleGroup.Item>
        {/each}
      </ToggleGroup.Root>
      <TrendChart
        {data}
        {series}
        {domain}
        {ticks}
        tickLabel={(date) => axisLabel(unit, date.toISOString())}
        pointLabel={(date) => bucketTitle(unit, date.toISOString())}
        ariaLabel={t().chartAria(outcomes.map((outcome) => `${t().outcomes[outcome]} ${t().count(totals[outcome])}`).join(', '))}
      />
    </Card.Content>
  {/if}
</Card.Root>

<Card.Root size="sm" role="region" aria-labelledby="access-metrics-policies-title">
  <Card.Header>
    <Card.Title id="access-metrics-policies-title">{t().byPolicy}</Card.Title>
  </Card.Header>
  <Card.Content>
    {#if policiesError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{policiesError}</Alert.Description></Alert.Root>
    {:else if !ready || !policies}
      {#if !loadError}<div class="flex justify-center py-8"><Spinner /></div>{/if}
    {:else if policies.length || removed}
      <DataTable {table} ariaLabel={t().byPolicy} />
    {:else}
      <Empty.Root class="border border-dashed">
        <Empty.Header>
          <Empty.Media variant="icon"><Shield20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().noPolicies}</Empty.Title>
        </Empty.Header>
      </Empty.Root>
    {/if}
  </Card.Content>
</Card.Root>
