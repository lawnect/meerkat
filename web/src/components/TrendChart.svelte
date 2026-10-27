<script lang="ts">
  import { scaleUtc } from 'd3-scale';
  import { LineChart } from 'layerchart';
  import * as Chart from '$lib/components/ui/chart/index.js';
  import { cn } from '$lib/utils.js';

  // shadcn chart-line-multiple 구성의 선 그래프. 값 축은 숨기고 툴팁으로 실제 값을 보여줍니다.
  // 단위가 다른 지표(건수, 바이트)를 겹쳐 그리도록 각 시리즈는 자기 최댓값(scale)으로 나눈 0–1 값을 받습니다.
  // x축은 기간 전체(domain)를 잡아 두고, 아직 오지 않은 구간은 점을 넘기지 않아 선이 현재에서 끝납니다.
  // 지연 시간처럼 소수 값이면 시리즈에 fractional을 주어 툴팁 값을 반올림하지 않고 format에 넘깁니다.
  // 값이 null인 점은 선이 끊기고(그 구간에 잰 값이 없음) 툴팁에는 —로 보입니다.
  // 색은 config 대신 시리즈에 직접 지정합니다. config에 색을 넣으면 ChartStyle이 인라인 <style>을 만들고, Outlet CSP가 이를 막기 때문입니다.
  type Series = { key: string; label: string; color: string; scale: number; format: (value: number) => string; fractional?: boolean };

  let { data, series, domain, ticks, tickLabel, pointLabel, ariaLabel, class: className = 'h-44' }: {
    data: Array<{ date: Date } & Record<string, number | Date | null>>;
    series: Series[];
    domain: [Date, Date];
    ticks: Date[];
    tickLabel: (date: Date) => string;
    pointLabel: (date: Date) => string;
    ariaLabel: string;
    class?: string;
  } = $props();

  const config = $derived(Object.fromEntries(series.map((item) => [item.key, { label: item.label }])) satisfies Chart.ChartConfig);
  const byKey = $derived(new Map(series.map((item) => [item.key, item])));
</script>

<div role="img" aria-label={ariaLabel} class="w-full">
  <Chart.Container {config} class={cn('aspect-auto w-full', className)}>
    <LineChart
      {data}
      x="date"
      xScale={scaleUtc()}
      xDomain={domain}
      yDomain={[0, 1]}
      axis="x"
      padding={{ top: 8, right: 20, bottom: 24, left: 20 }}
      series={series.map((item) => ({ key: item.key, label: item.label, color: item.color }))}
      props={{
        spline: { strokeWidth: 2 },
        xAxis: { ticks, format: (date: Date) => tickLabel(date) },
        highlight: { points: { r: 4 } },
      }}
    >
      {#snippet tooltip()}
        <Chart.Tooltip labelFormatter={(date: Date) => pointLabel(date)}>
          {#snippet formatter({ value, item })}
            {@const current = byKey.get(String(item.key))}
            <span class="size-2.5 shrink-0" style:background-color={current?.color} aria-hidden="true"></span>
            <span class="text-muted-foreground">{current?.label ?? item.label}</span>
            <span class="ms-auto ps-3 font-medium text-foreground tabular-nums">{value === null ? '—' : current ? current.format(current.fractional ? (Number(value) || 0) * current.scale : Math.round((Number(value) || 0) * current.scale)) : value}</span>
          {/snippet}
        </Chart.Tooltip>
      {/snippet}
    </LineChart>
  </Chart.Container>
</div>
