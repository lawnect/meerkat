<script lang="ts">
  import { Chart, BarController, BarElement, CategoryScale, LinearScale, Tooltip } from 'chart.js';
  import type { UsageHour } from '../types';

  Chart.register(BarController, BarElement, CategoryScale, LinearScale, Tooltip);

  let { hours, periodHours }: { hours: UsageHour[]; periodHours: number } = $props();
  let canvas = $state<HTMLCanvasElement>();

  const grouped = $derived.by(() => {
    const groups = new Map<string, { label: string; fullLabel: string; requests: number }>();
    for (const hour of hours) {
      const date = new Date(hour.hour);
      const dateLabel = date.toLocaleDateString('ko-KR', { month: 'numeric', day: 'numeric' });
      const timeLabel = date.toLocaleTimeString('ko-KR', { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
      const key = periodHours === 720
        ? date.toLocaleDateString('en-CA')
        : periodHours === 168
          ? String(Math.floor(date.getTime() / 21600000))
          : hour.hour;
      const label = periodHours === 720
        ? dateLabel
        : periodHours === 24
          ? timeLabel
          : `${dateLabel} ${timeLabel}`;
      const current = groups.get(key);
      if (current) current.requests += hour.requests;
      else groups.set(key, { label, fullLabel: `${dateLabel} ${timeLabel}`, requests: hour.requests });
    }
    return [...groups.values()];
  });

  $effect(() => {
    if (!canvas) return;
    const points = grouped;
    const chart = new Chart(canvas, {
      type: 'bar',
      data: {
        labels: points.map((point) => point.label),
        datasets: [{ label: '요청', data: points.map((point) => point.requests), backgroundColor: '#e88959', hoverBackgroundColor: '#bd592c', borderRadius: 3, maxBarThickness: 32 }],
      },
      options: {
        responsive: true,
        maintainAspectRatio: false,
        animation: false,
        plugins: {
          legend: { display: false },
          tooltip: { callbacks: {
            title: (items) => points[items[0]?.dataIndex]?.fullLabel || '',
            label: (context) => `요청 ${Number(context.parsed.y || 0).toLocaleString('ko-KR')}건`,
          } },
        },
        scales: {
          x: { grid: { display: false }, ticks: { maxTicksLimit: 7, color: '#89837e' }, border: { display: false } },
          y: { beginAtZero: true, ticks: { precision: 0, color: '#89837e' }, border: { display: false }, grid: { color: '#eae4de' } },
        },
      },
    });
    return () => chart.destroy();
  });
</script>

<div class="chart-frame" role="img" aria-label={`선택한 기간의 요청 추이, 총 ${hours.reduce((sum, hour) => sum + hour.requests, 0)}건`}>
  <canvas bind:this={canvas} aria-hidden="true"></canvas>
</div>

<style>
  .chart-frame { position: relative; height: 180px; width: 100%; margin-top: 18px; }
</style>
