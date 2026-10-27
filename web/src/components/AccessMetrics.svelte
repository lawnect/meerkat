<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Shield20, Warning } from '../icons';
  import type { AccessAction, AccessCounts, AccessMetrics, AccessPoliciesView, AccessPolicy } from '../types';
  import { oneOf, setParams } from '../urlState.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TrendChart from './TrendChart.svelte';

  // 접근 보호 화면의 지표 탭. 접근 정책이 판정한 요청을 통과·로그인 요구·거부로 나눠 기간의 추이와 정책별 합계로 보여 줍니다.
  // 기간은 주소(?period=7d)가 단일 출처이고, 24시간은 1시간, 7일은 6시간, 30일은 하루 단위로 묶어 그립니다.
  type Outcome = keyof AccessCounts;
  type Step = 'hour' | 'sixHours' | 'day';

  const t = translations({
    title: 'Access decisions',
    tip: 'What access policies decided. Passed: let in by a bypass, or by a session or token the policy takes. Sign-in required: sent to sign in (401 for non-browsers). Denied: refused by a block, a session or token the policy doesn\'t let in, or access SSO being off. Requests no policy covers aren\'t counted.',
    outcomes: { passed: 'Passed', challenged: 'Sign-in required', denied: 'Denied' } as Record<Outcome, string>,
    outcomesAria: 'Decisions shown in the chart',
    period: 'Period',
    periods: { 24: '24 hours', 168: '7 days', 720: '30 days' } as Record<number, string>,
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
    tip: '접근 정책이 요청을 판정한 결과입니다. 통과는 우회, 또는 정책이 받는 세션·토큰으로 들어온 요청, 로그인 요구는 로그인으로 보낸 요청(브라우저가 아니면 401), 거부는 차단, 허용 대상이 아닌 세션·토큰, 접근 보호 SSO 비활성화로 거부한 요청입니다. 정책이 적용되지 않은 요청은 세지 않습니다.',
    outcomes: { passed: '통과', challenged: '로그인 요구', denied: '거부' },
    outcomesAria: '그래프에 표시할 판정',
    period: '기간',
    periods: { 24: '24시간', 168: '7일', 720: '30일' },
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
  const periods = [24, 168, 720] as const;
  const periodParams = { 24: '24h', 168: '7d', 720: '30d' } as const;
  const period = $derived(({ '24h': 24, '7d': 168, '30d': 720 } as const)[oneOf('period', ['24h', '7d', '30d'], '24h')]);

  let metrics = $state<AccessMetrics | null>(null);
  let policies = $state<AccessPolicy[] | null>(null);
  let loadError = $state('');
  let shown = $state<Outcome[]>([...outcomes]);

  $effect(() => {
    const hours = period;
    loadError = '';
    Promise.all([api<AccessMetrics>(`/api/v1/security/access-metrics?hours=${hours}`), api<AccessPoliciesView>('/api/v1/security/access-policies')])
      .then(([next, view]) => { if (hours === period) [metrics, policies] = [next, view.policies]; })
      .catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  const sixHours = 6 * 3600000;
  const step = $derived<Step>(period >= 720 ? 'day' : period >= 168 ? 'sixHours' : 'hour');
  // 시간별 판정을 기간에 맞는 구간으로 묶습니다.
  const buckets = $derived.by(() => {
    const groups = new Map<string, AccessCounts & { start: string }>();
    for (const hour of metrics?.hours ?? []) {
      const date = new Date(hour.hour);
      const key = step === 'day' ? `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}` : step === 'sixHours' ? String(Math.floor(date.getTime() / sixHours)) : hour.hour;
      const current = groups.get(key);
      if (current) for (const outcome of outcomes) current[outcome] += hour[outcome];
      else groups.set(key, { start: hour.hour, passed: hour.passed, challenged: hour.challenged, denied: hour.denied });
    }
    return [...groups.values()];
  });
  const totals = $derived(Object.fromEntries(outcomes.map((outcome) => [outcome, buckets.reduce((sum, bucket) => sum + bucket[outcome], 0)])) as Record<Outcome, number>);

  // 세 판정은 단위가 같으므로 보이는 판정 중 가장 큰 값 하나로 나눠 크기를 견줄 수 있게 그립니다.
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

  function axisLabel(date: Date): string {
    const locale = intlLocale();
    const time = date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
    if (step === 'hour') return time;
    const day = date.toLocaleDateString(locale, { month: 'numeric', day: 'numeric' });
    return step === 'day' ? day : `${day} ${time}`;
  }

  function bucketTitle(date: Date): string {
    const locale = intlLocale();
    if (step === 'day') return date.toLocaleDateString(locale, { dateStyle: 'medium' });
    if (step === 'hour') return date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
    const from = new Date(Math.floor(date.getTime() / sixHours) * sixHours);
    const to = new Date(from.getTime() + sixHours);
    const time = (value: Date) => value.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
    return `${from.toLocaleDateString(locale, { dateStyle: 'medium' })} ${time(from)}–${time(to)}`;
  }

  // 정책별 합계는 지금 정책 순서대로 보여 주고, 지워진 정책의 판정은 한 줄로 모읍니다.
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
</script>

<Card.Root size="sm" role="region" aria-labelledby="access-metrics-title">
  <Card.Header>
    <Card.Title id="access-metrics-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="access-metrics-tip" label={t().title} description={t().tip} /></Card.Title>
    <Card.Action>
      <NativeSelect.Root size="sm" aria-label={t().period} bind:value={() => String(period), (next) => { if (next) setParams({ period: periodParams[Number(next) as 24 | 168 | 720] }, { period: '24h' }); }}>
        {#each periods as hours (hours)}
          <NativeSelect.Option value={String(hours)}>{t().periods[hours]}</NativeSelect.Option>
        {/each}
      </NativeSelect.Root>
    </Card.Action>
  </Card.Header>
  {#if loadError}
    <Card.Content><Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root></Card.Content>
  {:else if !metrics}
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
        tickLabel={axisLabel}
        pointLabel={bucketTitle}
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
    {#if !metrics || !policies}
      {#if !loadError}<div class="flex justify-center py-8"><Spinner /></div>{/if}
    {:else if policies.length || removed}
      <div class="overflow-x-auto border">
        <Table.Root>
          <Table.Header class="bg-muted">
            <Table.Row class="hover:bg-transparent">
              <Table.Head class="w-12 pl-4"><span class="sr-only">#</span></Table.Head>
              <Table.Head>{t().name}</Table.Head>
              <Table.Head>{t().action}</Table.Head>
              {#each outcomes as outcome (outcome)}<Table.Head class="text-right last:pr-4">{t().outcomes[outcome]}</Table.Head>{/each}
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {#each policies as policy, index (policy.id)}
              {@const counts = metrics.policies[policy.id] ?? zero}
              <Table.Row class={[!policy.enabled && 'text-muted-foreground']}>
                <Table.Cell class="pl-4 text-muted-foreground tabular-nums">{number(index + 1)}</Table.Cell>
                <Table.Cell><span class="block max-w-64 truncate font-medium">{policy.name}</span></Table.Cell>
                <Table.Cell><Badge variant={policy.action === 'block' ? 'destructive' : policy.action === 'bypass' ? 'secondary' : 'outline'} class="select-none">{t().actions[policy.action]}</Badge></Table.Cell>
                {#each outcomes as outcome (outcome)}
                  <Table.Cell class={['text-right tabular-nums last:pr-4', !counts[outcome] && 'text-muted-foreground']}>{number(counts[outcome])}</Table.Cell>
                {/each}
              </Table.Row>
            {/each}
            {#if removed}
              <Table.Row class="text-muted-foreground">
                <Table.Cell class="pl-4"></Table.Cell>
                <Table.Cell colspan={2}>{t().removed}</Table.Cell>
                {#each outcomes as outcome (outcome)}
                  <Table.Cell class="text-right tabular-nums last:pr-4">{number(removed[outcome])}</Table.Cell>
                {/each}
              </Table.Row>
            {/if}
          </Table.Body>
        </Table.Root>
      </div>
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
