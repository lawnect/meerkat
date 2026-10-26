<script lang="ts" module>
  // 트래픽 그래프에서 겹쳐 그릴 수 있는 지표. 주소의 metrics 값도 이 이름을 씁니다.
  export type Metric = 'requests' | 'error_responses' | 'request_bytes' | 'response_bytes';
  export const metrics: readonly Metric[] = ['requests', 'error_responses', 'request_bytes', 'response_bytes'];
</script>

<script lang="ts">
  import * as Card from '$lib/components/ui/card/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import { formatBytes } from '../format';
  import { intlLocale, translations } from '../i18n.svelte';
  import type { UsageBucket } from '../types';
  import TrendChart from './TrendChart.svelte';

  // Card.Header 다음에 넣는 트래픽 본문입니다. Card.Content에 지표 범례(여러 개를 켜고 끄는 ToggleGroup)와
  // 지표를 겹친 선 그래프를, Card.Footer에 처리 건수의 평균과 최대를 보여줍니다.
  // 표시할 지표는 부모가 주소에서 읽어 shown으로 넘기고, 바꾸면 onShownChange로 알립니다(트래픽 카드).
  // 넘기지 않으면 이 컴포넌트가 직접 들고 있습니다(터널 상세의 사용량).
  let { buckets, shown: shownProp, onShownChange, axisLabel, bucketTitle, averageLabel }: {
    buckets: UsageBucket[];
    shown?: Metric[];
    onShownChange?: (next: Metric[]) => void;
    axisLabel: (start: string) => string;
    bucketTitle: (start: string) => string;
    averageLabel: string;
  } = $props();

  const t = translations({
    metricsAria: 'Metrics shown in the chart',
    metrics: { requests: 'Requests', error_responses: '5xx responses', request_bytes: 'Received', response_bytes: 'Sent' } as Record<Metric, string>,
    count: (value: number) => `${value.toLocaleString(intlLocale())} ${value === 1 ? 'request' : 'requests'}`,
    peak: 'Peak',
    chartAria: (totals: string) => `Traffic in the selected period: ${totals}`,
  }, {
    metricsAria: '그래프에 표시할 지표',
    metrics: { requests: '처리 건수', error_responses: '5xx 응답', request_bytes: '받은 데이터', response_bytes: '보낸 데이터' },
    count: (value: number) => `${value.toLocaleString(intlLocale())}건`,
    peak: '최대',
    chartAria: (totals: string) => `선택한 기간의 트래픽: ${totals}`,
  });

  // 오류는 붉은색, 나머지는 주 색 계열의 명도 차이로 구분합니다.
  const colors: Record<Metric, string> = { requests: 'var(--chart-1)', error_responses: 'var(--destructive)', request_bytes: 'var(--chart-3)', response_bytes: 'var(--chart-4)' };

  let ownShown = $state<Metric[]>([...metrics]);
  const shown = $derived(shownProp ?? ownShown);
  function setShown(next: Metric[]): void {
    if (!next.length) return;
    if (onShownChange) onShownChange(next);
    else ownShown = next;
  }

  const isBytes = (metric: Metric) => metric === 'request_bytes' || metric === 'response_bytes';
  const format = (metric: Metric, value: number) => (isBytes(metric) ? formatBytes(value) : t().count(value));
  const total = (metric: Metric) => buckets.reduce((sum, bucket) => sum + bucket[metric], 0);

  const totals = $derived(Object.fromEntries(metrics.map((metric) => [metric, total(metric)])) as Record<Metric, number>);
  const errorRate = $derived(totals.requests ? `${((totals.error_responses / totals.requests) * 100).toFixed(totals.error_responses ? 2 : 0)}%` : '0%');

  // 그래프는 이미 시작한 구간까지만 선을 그립니다. 아직 오지 않은 시간을 0으로 그리면 급감한 것처럼 보이기 때문입니다.
  const started = $derived(buckets.filter((bucket) => Date.parse(bucket.start) <= Date.now()));
  const domain = $derived<[Date, Date]>(buckets.length ? [new Date(buckets[0].start), new Date(buckets[buckets.length - 1].start)] : [new Date(), new Date()]);
  const ticks = $derived.by(() => {
    const step = Math.ceil(buckets.length / 8) || 1;
    return buckets.filter((_, index) => index % step === 0).map((bucket) => new Date(bucket.start));
  });

  // 단위가 다른 지표를 한 그래프에 겹치므로 각 지표를 자기 최댓값으로 나눠 0–1로 그립니다. 툴팁은 실제 값을 보여줍니다.
  const peaks = $derived(Object.fromEntries(metrics.map((metric) => [metric, Math.max(0, ...started.map((bucket) => bucket[metric]))])) as Record<Metric, number>);
  const data = $derived(started.map((bucket) => ({
    date: new Date(bucket.start),
    ...Object.fromEntries(metrics.map((metric) => [metric, peaks[metric] ? bucket[metric] / peaks[metric] : 0])),
  })));
  const series = $derived(metrics.filter((metric) => shown.includes(metric)).map((metric) => ({
    key: metric, label: t().metrics[metric], color: colors[metric], scale: peaks[metric], format: (value: number) => format(metric, value),
  })));

  // 평균과 최대는 처리 건수 기준입니다. 평균은 이미 시작한 구간으로만 나눕니다.
  const summary = $derived.by(() => {
    const peak = started.reduce<UsageBucket | null>((best, bucket) => (!best || bucket.requests > best.requests ? bucket : best), null);
    return { average: Math.round(totals.requests / (started.length || 1)), peak: peak && peak.requests > 0 ? peak : null };
  });
</script>

<Card.Content class="grid min-w-0 gap-4">
  <ToggleGroup.Root
    type="multiple"
    variant="outline"
    spacing={2}
    class="grid w-full grid-cols-2 sm:grid-cols-4"
    aria-label={t().metricsAria}
    bind:value={() => shown, (next) => setShown(next as Metric[])}
  >
    {#each metrics as metric (metric)}
      <ToggleGroup.Item value={metric} class="h-auto min-w-0 flex-col items-start gap-0.5 px-3 py-2 text-start">
        <span class="flex w-full min-w-0 items-center gap-1.5 text-xs font-normal text-muted-foreground">
          <span class={['size-2.5 shrink-0', !shown.includes(metric) && 'opacity-30']} style:background-color={colors[metric]} aria-hidden="true"></span>
          <span class="truncate">{t().metrics[metric]}</span>
        </span>
        <span class="flex w-full min-w-0 items-baseline gap-1.5">
          <span class="truncate text-xl font-semibold tabular-nums">{isBytes(metric) ? formatBytes(totals[metric]) : totals[metric].toLocaleString(intlLocale())}</span>
          {#if metric === 'error_responses'}<span class="text-xs font-normal text-muted-foreground tabular-nums">{errorRate}</span>{/if}
        </span>
      </ToggleGroup.Item>
    {/each}
  </ToggleGroup.Root>
  <TrendChart
    {data}
    {series}
    {domain}
    {ticks}
    tickLabel={(date) => axisLabel(date.toISOString())}
    pointLabel={(date) => bucketTitle(date.toISOString())}
    ariaLabel={t().chartAria(series.map((item) => `${item.label} ${format(item.key, totals[item.key])}`).join(', '))}
  />
</Card.Content>
<Card.Footer class="flex-wrap gap-x-4 gap-y-1 text-muted-foreground tabular-nums">
  <span>{averageLabel} <span class="font-medium text-foreground">{format('requests', summary.average)}</span></span>
  {#if summary.peak}
    <span>{t().peak} <span class="font-medium text-foreground">{format('requests', summary.peak.requests)}</span> · {bucketTitle(summary.peak.start)}</span>
  {/if}
</Card.Footer>
