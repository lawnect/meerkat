<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import { api } from '../api';
  import { formatLatency } from '../format';
  import { translations } from '../i18n.svelte';
  import { Timer20, Warning } from '../icons';
  import { axisLabel, bucketTitle, createPeriod, trackPeriod } from '../period.svelte';
  import type { LatencySeries, LatencyStats } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';
  import PeriodControls from './PeriodControls.svelte';
  import StatCard from './StatCard.svelte';
  import TrendChart from './TrendChart.svelte';

  // 관찰의 지표 탭에서 접근 판정 카드 뒤에 오는 지연 시간 카드. Outlet 처리(Outlet이 요청을 받아 서비스로 보낼 연결을 확보하기까지)와
  // 업스트림 응답(요청을 본문까지 보낸 뒤 최종 응답 헤더까지, 실패한 서비스에 쓴 시간 포함)을 기간 전체의 평균·p95 카드와, 고른 한 쪽의 구간별 평균·p95 선 그래프로 보여 줍니다.
  // 두 측정은 크기가 수백 배 달라 한 그래프에 겹치지 않고 ToggleGroup으로 하나만 그립니다.
  // 기간은 접근 판정 카드와 같은 주소(?unit=&date=&live=)이고, 두 카드가 정책 표를 사이에 두고 떨어져 있어 이 카드에도 PeriodControls를 둡니다.
  type Measure = 'outlet' | 'upstream';
  type Line = 'p95' | 'avg';

  const t = translations({
    title: 'Latency',
    tip: 'Outlet: from the request arriving until the Outlet has a connection to the service (its own checks, routing, and opening a connection through the tunnel). Upstream: from the request fully sent until the answer\'s headers, including the tunnel round trip, the app, and time lost on services that failed before a retry. A client\'s slow upload counts as neither. Refused and unforwarded requests aren\'t timed. p95 is the time 95% of requests stayed under, estimated from hourly buckets kept for 400 days.',
    measures: { outlet: 'Outlet', upstream: 'Upstream' } as Record<Measure, string>,
    measuresAria: 'Measure shown in the chart',
    stats: { outletAvg: 'Outlet avg', outletP95: 'Outlet p95', upstreamAvg: 'Upstream avg', upstreamP95: 'Upstream p95' },
    lines: { p95: 'p95', avg: 'Average' } as Record<Line, string>,
    linesAria: 'Chart lines',
    emptyTitle: 'No requests timed',
    chartAria: (measure: string, avg: string, p95: string) => `${measure} latency in the selected period: average ${avg}, p95 ${p95}`,
    loadFailed: (code: string) => `Couldn't load latency: ${code}`,
  }, {
    title: '지연 시간',
    tip: 'Outlet 처리는 요청을 받은 뒤 서비스로 보낼 연결을 확보하기까지의 시간으로, Outlet의 자체 검사와 라우팅, 터널을 통한 연결 수립을 포함합니다. 업스트림 응답은 요청을 본문까지 보낸 뒤 응답 헤더가 올 때까지의 시간으로, 터널 왕복, 앱의 처리, 재시도 전에 실패한 서비스에 쓴 시간을 포함합니다. 클라이언트의 느린 업로드는 어느 쪽에도 넣지 않습니다. Outlet이 거부했거나 전달하지 않은 요청은 측정하지 않습니다. p95는 요청의 95%가 이 시간 이내에 끝난 값이며, 400일간 보관되는 시간별 분포에서 추정합니다.',
    measures: { outlet: 'Outlet 처리', upstream: '업스트림 응답' },
    measuresAria: '그래프에 표시할 측정 항목',
    stats: { outletAvg: 'Outlet 처리 평균', outletP95: 'Outlet 처리 p95', upstreamAvg: '업스트림 응답 평균', upstreamP95: '업스트림 응답 p95' },
    lines: { p95: 'p95', avg: '평균' },
    linesAria: '그래프 범례',
    emptyTitle: '측정된 요청이 없습니다',
    chartAria: (measure: string, avg: string, p95: string) => `선택한 기간의 ${measure} 지연 시간: 평균 ${avg}, p95 ${p95}`,
    loadFailed: (code: string) => `지연 시간을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const measures: Measure[] = ['outlet', 'upstream'];
  const lines: Line[] = ['p95', 'avg'];
  const colors: Record<Line, string> = { p95: 'var(--chart-1)', avg: 'var(--chart-3)' };

  const period = createPeriod();
  let latency = $state<LatencySeries | null>(null);
  let loadError = $state('');
  let measure = $state<Measure>('outlet');

  trackPeriod(period, {
    load: (query) => api<LatencySeries>(`/api/v1/observe/latency?${query}`),
    onData: (next) => { latency = { ...next, buckets: next.buckets ?? [] }; },
    onError: (code) => { loadError = code ? t().loadFailed(code) : ''; },
  });

  const unit = $derived(period.unit);
  // 단위를 바꾼 직후에는 이전 단위의 구간이 남아 있으므로 새 응답이 올 때까지 그리지 않습니다.
  const ready = $derived(latency?.unit === unit);
  const buckets = $derived(ready && latency ? latency.buckets : []);
  const range = $derived(ready && latency ? { outlet: latency.outlet, upstream: latency.upstream } : null);
  const empty = $derived(!!range && range.outlet.count === 0 && range.upstream.count === 0);

  // 잰 요청이 없으면 값을 "—"로 보입니다.
  const show = (stats: LatencyStats | undefined, value: (stats: LatencyStats) => number): string => (stats?.count ? formatLatency(value(stats)) : '—');

  const value = (bucket: LatencySeries['buckets'][number], line: Line): number | null => {
    const stats = bucket[measure];
    return stats.count ? (line === 'p95' ? stats.p95_ms : stats.avg_ms) : null;
  };

  // 두 선은 단위가 같아 둘 중 큰 값 하나로 나눠 크기를 견줄 수 있게 그립니다.
  // 아직 오지 않은 구간은 그리지 않고, 잰 요청이 없는 구간은 선이 끊깁니다(0ms로 떨어진 것처럼 보이지 않게).
  const started = $derived(buckets.filter((bucket) => Date.parse(bucket.start) <= Date.now()));
  const peak = $derived(Math.max(0, ...started.flatMap((bucket) => lines.map((line) => value(bucket, line) ?? 0))));
  const data = $derived(started.map((bucket) => ({
    date: new Date(bucket.start),
    ...Object.fromEntries(lines.map((line) => {
      const raw = value(bucket, line);
      return [line, raw === null ? null : peak ? raw / peak : 0];
    })),
  })));
  const series = $derived(lines.map((line) => ({
    key: line, label: t().lines[line], color: colors[line], scale: peak, format: formatLatency, fractional: true,
  })));
  const domain = $derived<[Date, Date]>(buckets.length ? [new Date(buckets[0].start), new Date(buckets[buckets.length - 1].start)] : [new Date(), new Date()]);
  const ticks = $derived.by(() => {
    const every = Math.ceil(buckets.length / 8) || 1;
    return buckets.filter((_, index) => index % every === 0).map((bucket) => new Date(bucket.start));
  });
</script>

<Card.Root size="sm" role="region" aria-labelledby="latency-title">
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id="latency-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="latency-tip" label={t().title} description={t().tip} /></Card.Title>
    <PeriodControls {period} />
  </Card.Header>
  {#if loadError}
    <Card.Content><Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root></Card.Content>
  {:else if !range}
    <Card.Content class="grid min-w-0 gap-4" aria-busy="true">
      <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        {#each [0, 1, 2, 3] as key (key)}<Skeleton class="h-24" />{/each}
      </div>
      <Skeleton class="h-56 w-full" />
    </Card.Content>
  {:else if empty}
    <Card.Content>
      <Empty.Root class="border border-dashed">
        <Empty.Header>
          <Empty.Media variant="icon"><Timer20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().emptyTitle}</Empty.Title>
        </Empty.Header>
      </Empty.Root>
    </Card.Content>
  {:else}
    <Card.Content class="grid min-w-0 gap-4">
      <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard label={t().stats.outletAvg} value={show(range.outlet, (stats) => stats.avg_ms)} />
        <StatCard label={t().stats.outletP95} value={show(range.outlet, (stats) => stats.p95_ms)} />
        <StatCard label={t().stats.upstreamAvg} value={show(range.upstream, (stats) => stats.avg_ms)} />
        <StatCard label={t().stats.upstreamP95} value={show(range.upstream, (stats) => stats.p95_ms)} />
      </div>
      <div class="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <ToggleGroup.Root
          type="single"
          variant="outline"
          size="sm"
          aria-label={t().measuresAria}
          bind:value={() => measure, (next) => { if (next) measure = next as Measure; }}
        >
          {#each measures as item (item)}
            <ToggleGroup.Item value={item}>{t().measures[item]}</ToggleGroup.Item>
          {/each}
        </ToggleGroup.Root>
        <ul class="flex items-center gap-4 text-xs text-muted-foreground" aria-label={t().linesAria}>
          {#each lines as line (line)}
            <li class="flex items-center gap-1.5">
              <span class="size-2.5 shrink-0" style:background-color={colors[line]} aria-hidden="true"></span>{t().lines[line]}
            </li>
          {/each}
        </ul>
      </div>
      <TrendChart
        {data}
        {series}
        {domain}
        {ticks}
        tickLabel={(date) => axisLabel(unit, date.toISOString())}
        pointLabel={(date) => bucketTitle(unit, date.toISOString())}
        ariaLabel={t().chartAria(t().measures[measure], show(range[measure], (stats) => stats.avg_ms), show(range[measure], (stats) => stats.p95_ms))}
      />
    </Card.Content>
  {/if}
</Card.Root>
