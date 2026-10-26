<script lang="ts" module>
  // href가 있는 항목은 범례에서 링크가 됩니다. 앱 안에서 옮겨 가려면 onclick에서 preventDefault 후 직접 이동합니다.
  export type ShareItem = { label: string; value: number; href?: string; onclick?: (event: MouseEvent) => void };
</script>

<script lang="ts">
  import { PieChart, Text } from 'layerchart';
  import * as Chart from '$lib/components/ui/chart/index.js';
  import { translations } from '../i18n.svelte';
  import TruncateText from './TruncateText.svelte';

  // shadcn chart-pie-donut-text 구성의 비율 그래프와 범례. 항목이 많으면 상위 4개 다음을 "기타"로 묶습니다(묶음은 링크가 아닙니다).
  // 색은 config 대신 데이터에 직접 지정합니다(config 색은 인라인 <style>을 만들어 Outlet CSP에 막힙니다).
  // layout="stacked"는 좁은 카드용으로, 도넛 아래에 범례를 쌓습니다. 기본값은 넓은 화면에서 좌우로 나란히 놓습니다.
  let { items, valueLabel, centerValue, centerLabel, ariaLabel, layout = 'auto' }: {
    items: ShareItem[];
    valueLabel: (value: number) => string;
    centerValue: string;
    centerLabel: string;
    ariaLabel: string;
    layout?: 'auto' | 'stacked';
  } = $props();

  const t = translations({ others: 'Others' }, { others: '기타' });
  const colors = ['var(--chart-1)', 'var(--chart-2)', 'var(--chart-3)', 'var(--chart-4)', 'var(--chart-5)'];
  const total = $derived(items.reduce((sum, item) => sum + item.value, 0));
  const slices = $derived.by(() => {
    const sorted = [...items].filter((item) => item.value > 0).sort((a, b) => b.value - a.value);
    const shown = sorted.length > colors.length ? sorted.slice(0, colors.length - 1) : sorted;
    const rest = sorted.slice(shown.length).reduce((sum, item) => sum + item.value, 0);
    const all: ShareItem[] = rest ? [...shown, { label: t().others, value: rest }] : shown;
    return all.map((item, index) => ({ key: `slice-${index}`, ...item, color: colors[index] }));
  });
  const config = $derived(Object.fromEntries(slices.map((slice) => [slice.key, { label: slice.label }])) satisfies Chart.ChartConfig);
  // 링크 행에 hover 배경을 깔 수 있도록 모든 행이 같은 가로·세로 여백을 가집니다(바깥으로 같은 만큼 빼서 글자 위치는 그대로).
  const rowClass = '-mx-1.5 flex min-w-0 items-center gap-2 px-1.5 py-1';
  const percent = (value: number) => (total ? `${Math.round((value / total) * 100)}%` : '0%');
</script>

<div class={['grid min-w-0 items-center gap-4', layout === 'auto' && 'sm:grid-cols-[auto_minmax(0,1fr)]']}>
  <div role="img" aria-label={ariaLabel} class="mx-auto">
    <Chart.Container {config} class="aspect-square h-44">
      <PieChart data={slices} key="key" value="value" c="color" innerRadius={52} padding={8}>
        {#snippet aboveMarks()}
          <Text value={centerValue} textAnchor="middle" verticalAnchor="middle" class="fill-foreground text-xl! font-semibold" dy={-6} />
          <Text value={centerLabel} textAnchor="middle" verticalAnchor="middle" class="fill-muted-foreground! text-xs!" dy={14} />
        {/snippet}
        {#snippet tooltip()}
          <Chart.Tooltip hideLabel nameKey="key">
            {#snippet formatter({ value, name })}
              <span class="text-muted-foreground">{slices.find((slice) => slice.key === name)?.label ?? name}</span>
              <span class="ms-auto font-medium text-foreground tabular-nums">{valueLabel(Number(value) || 0)}</span>
            {/snippet}
          </Chart.Tooltip>
        {/snippet}
      </PieChart>
    </Chart.Container>
  </div>
  <ul class="grid min-w-0 text-xs">
    {#each slices as slice (slice.key)}
      <li class="min-w-0">
        {#if slice.href}
          <a href={slice.href} onclick={slice.onclick} class={[rowClass, 'outline-none hover:bg-muted focus-visible:ring-3 focus-visible:ring-ring/50']}>{@render row(slice)}</a>
        {:else}
          <div class={rowClass}>{@render row(slice)}</div>
        {/if}
      </li>
    {/each}
  </ul>
</div>

{#snippet row(slice: (typeof slices)[number])}
  <span class="size-2.5 shrink-0" style:background-color={slice.color} aria-hidden="true"></span>
  <TruncateText text={slice.label} class="flex-1" />
  <span class="text-muted-foreground tabular-nums">{valueLabel(slice.value)}</span>
  <span class="w-10 text-end font-medium tabular-nums">{percent(slice.value)}</span>
{/snippet}
