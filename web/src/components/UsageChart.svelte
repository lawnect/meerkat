<script lang="ts">
  import { intlLocale, translations } from '../i18n.svelte';
  import type { UsageBucket, UsageHour } from '../types';
  import TrafficChart from './TrafficChart.svelte';

  // Card.Header 다음에 넣습니다. 시간별 집계(hours)를 기간에 맞는 구간으로 묶어 TrafficChart에 넘깁니다.
  // 24시간은 1시간, 7일은 6시간, 30일은 하루 단위입니다.
  type Step = 'hour' | 'sixHours' | 'day';

  const t = translations({
    average: { hour: 'Avg per hour', sixHours: 'Avg per 6 hours', day: 'Avg per day' } as Record<Step, string>,
  }, {
    average: { hour: '시간당 평균', sixHours: '6시간당 평균', day: '일 평균' },
  });

  let { hours, periodHours }: { hours: UsageHour[]; periodHours: number } = $props();

  const sixHours = 6 * 3600000;
  const step = $derived<Step>(periodHours >= 720 ? 'day' : periodHours >= 168 ? 'sixHours' : 'hour');

  const buckets = $derived.by(() => {
    const groups = new Map<string, UsageBucket>();
    for (const hour of hours) {
      const date = new Date(hour.hour);
      const key = step === 'day'
        ? `${date.getFullYear()}-${date.getMonth()}-${date.getDate()}`
        : step === 'sixHours'
          ? String(Math.floor(date.getTime() / sixHours))
          : hour.hour;
      const current = groups.get(key);
      if (current) {
        current.requests += hour.requests;
        current.request_bytes += hour.request_bytes;
        current.response_bytes += hour.response_bytes;
        current.error_responses += hour.error_responses;
      } else {
        groups.set(key, { start: hour.hour, requests: hour.requests, request_bytes: hour.request_bytes, response_bytes: hour.response_bytes, error_responses: hour.error_responses });
      }
    }
    return [...groups.values()];
  });

  function axisLabel(start: string): string {
    const date = new Date(start);
    const locale = intlLocale();
    const time = date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
    if (step === 'hour') return time;
    const day = date.toLocaleDateString(locale, { month: 'numeric', day: 'numeric' });
    return step === 'day' ? day : `${day} ${time}`;
  }

  function bucketTitle(start: string): string {
    const date = new Date(start);
    const locale = intlLocale();
    if (step === 'day') return date.toLocaleDateString(locale, { dateStyle: 'medium' });
    if (step === 'hour') return date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
    const from = new Date(Math.floor(date.getTime() / sixHours) * sixHours);
    const to = new Date(from.getTime() + sixHours);
    const time = (value: Date) => value.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
    return `${from.toLocaleDateString(locale, { dateStyle: 'medium' })} ${time(from)}–${time(to)}`;
  }
</script>

<TrafficChart {buckets} {axisLabel} {bucketTitle} averageLabel={t().average[step]} />
