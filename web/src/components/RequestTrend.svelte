<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import { cn } from '$lib/utils.js';
  import { api } from '../api';
  import { translations } from '../i18n.svelte';
  import { Warning } from '../icons';
  import { axisLabel, bucketTitle, createPeriod, trackPeriod, trafficUnits } from '../period.svelte';
  import type { UsageSeries, UsageUnit } from '../types';
  import { param, setParams } from '../urlState.svelte';
  import PeriodControls from './PeriodControls.svelte';
  import TrafficChart, { metrics, type Metric } from './TrafficChart.svelte';

  // 트래픽 카드. 화면 상태(unit, date, metrics, live)는 주소의 query가 단일 출처이고, 불러온 series는 bind로 부모에 넘겨
  // 같은 기간을 쓰는 다른 카드(터널별 처리 건수)가 요청 없이 함께 씁니다. 기간 탐색은 PeriodControls·period.svelte.ts와 같이 씁니다.
  let { series = $bindable(null), error = $bindable(''), class: className }: {
    series?: UsageSeries | null;
    error?: string;
    class?: string;
  } = $props();

  const t = translations({
    title: 'Traffic',
    average: { minute: 'Avg per minute', hour: 'Avg per hour', day: 'Avg per day', month: 'Avg per month', year: 'Avg per year' } as Record<UsageUnit, string>,
    loadFailed: (code: string) => `Couldn't load request counts: ${code}`,
  }, {
    title: '트래픽',
    average: { minute: '분당 평균', hour: '시간당 평균', day: '일 평균', month: '월 평균', year: '연 평균' },
    loadFailed: (code: string) => `요청 처리 건수를 불러오지 못했습니다: ${code}`,
  });

  // 트래픽만 분별을 받습니다(Outlet 전체 합계를 분 단위로 7일 보관).
  const period = createPeriod({ units: trafficUnits });

  // 표시할 지표는 metrics=requests,error_responses처럼 적고, 알 수 없는 값만 있거나 비어 있으면 모두 보여줍니다.
  function metricsFromUrl(): Metric[] {
    const wanted = (param('metrics') ?? '').split(',');
    const valid = metrics.filter((metric) => wanted.includes(metric));
    return valid.length ? valid : [...metrics];
  }

  const shown = $derived(metricsFromUrl());

  function setShown(next: Metric[]): void {
    const ordered = metrics.filter((metric) => next.includes(metric));
    setParams({ metrics: ordered.join(',') }, { metrics: metrics.join(',') });
  }

  trackPeriod(period, {
    load: (query) => api<UsageSeries>(`/api/v1/usage?${query}`),
    onData: (response) => { series = response; },
    onError: (code) => { error = code ? t().loadFailed(code) : ''; },
  });

  const unit = $derived(period.unit);
  const buckets = $derived(series?.unit === unit ? series.buckets : []);
</script>

<Card.Root size="sm" aria-labelledby="request-trend-title" class={cn('lg:h-full', className)}>
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id="request-trend-title">{t().title}</Card.Title>
    <PeriodControls {period} />
  </Card.Header>
  {#if error}
    <Card.Content>
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>
    </Card.Content>
  {:else}
    <TrafficChart {buckets} {shown} onShownChange={setShown} axisLabel={(start) => axisLabel(unit, start)} bucketTitle={(start) => bucketTitle(unit, start)} averageLabel={t().average[unit]} />
  {/if}
</Card.Root>
