<script lang="ts">
  import { CalendarDate, getLocalTimeZone, today } from '@internationalized/date';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
  import { Calendar } from '$lib/components/ui/calendar/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Popover from '$lib/components/ui/popover/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { cn } from '$lib/utils.js';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { CalendarLtr12, ChevronLeft, ChevronRight, Warning } from '../icons';
  import type { UsageSeries, UsageUnit } from '../types';
  import { boolParam, dateParam, oneOf, param, setParams } from '../urlState.svelte';
  import IconButton from './IconButton.svelte';
  import TrafficChart, { metrics, type Metric } from './TrafficChart.svelte';

  // 트래픽 카드. 화면 상태(unit, date, metrics, live)는 주소의 query가 단일 출처이고, 불러온 series는 bind로 부모에 넘겨
  // 같은 기간을 쓰는 다른 카드(터널별 처리 건수)가 요청 없이 함께 씁니다.
  let { series = $bindable(null), error = $bindable(''), class: className }: {
    series?: UsageSeries | null;
    error?: string;
    class?: string;
  } = $props();

  const t = translations({
    title: 'Traffic',
    units: { hour: 'Hourly', day: 'Daily', month: 'Monthly', year: 'Yearly' } as Record<UsageUnit, string>,
    unitsAria: 'Chart interval',
    previous: 'Previous period',
    next: 'Next period',
    pickDate: 'Pick a date',
    autoRefresh: 'Auto-refresh every 5 seconds',
    average: { hour: 'Avg per hour', day: 'Avg per day', month: 'Avg per month', year: 'Avg per year' } as Record<UsageUnit, string>,
    loadFailed: (code: string) => `Couldn't load request counts: ${code}`,
  }, {
    title: '트래픽',
    units: { hour: '시간별', day: '일별', month: '월별', year: '연별' },
    unitsAria: '그래프 단위',
    previous: '이전 기간',
    next: '다음 기간',
    pickDate: '날짜 선택',
    autoRefresh: '5초마다 자동 새로 고침',
    average: { hour: '시간당 평균', day: '일 평균', month: '월 평균', year: '연 평균' },
    loadFailed: (code: string) => `요청 처리 건수를 불러오지 못했습니다: ${code}`,
  });

  const units: UsageUnit[] = ['hour', 'day', 'month', 'year'];
  const yearSpan = 5;

  const refreshMs = 5000;
  const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;
  const defaultUnit: UsageUnit = 'hour';

  let calendarOpen = $state(false);

  const pad = (value: number): string => String(value).padStart(2, '0');
  const dayKey = (date: Date): string => `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;

  // 주소의 date는 기준 날짜(YYYY-MM-DD)이고, 없거나 올바르지 않거나 미래이면 오늘입니다.
  function cursorFromUrl(): Date {
    const now = new Date();
    const key = dateParam('date');
    if (!key) return now;
    const [year, month, day] = key.split('-').map(Number);
    const date = new Date(year, month - 1, day);
    return dayKey(date) === key && date.getTime() <= now.getTime() ? date : now;
  }

  // 표시할 지표는 metrics=requests,error_responses처럼 적고, 알 수 없는 값만 있거나 비어 있으면 모두 보여줍니다.
  function metricsFromUrl(): Metric[] {
    const wanted = (param('metrics') ?? '').split(',');
    const valid = metrics.filter((metric) => wanted.includes(metric));
    return valid.length ? valid : [...metrics];
  }

  const unit = $derived(oneOf('unit', units, defaultUnit));
  const cursor = $derived(cursorFromUrl());
  const shown = $derived(metricsFromUrl());
  const live = $derived(boolParam('live', true));

  function setUnit(next: UsageUnit): void {
    setParams({ unit: next }, { unit: defaultUnit });
  }

  // 오늘이면 date를 주소에서 뺍니다.
  function setCursor(date: Date): void {
    const key = dayKey(date);
    setParams({ date: key === dayKey(new Date()) ? null : key });
  }

  function setShown(next: Metric[]): void {
    const ordered = metrics.filter((metric) => next.includes(metric));
    setParams({ metrics: ordered.join(',') }, { metrics: metrics.join(',') });
  }

  function setLive(next: boolean): void {
    setParams({ live: next }, { live: true });
  }

  // 달력은 기준 날짜를 선택된 날짜로 보여주고, 고른 날짜가 들어 있는 기간으로 이동합니다.
  const selectedDate = $derived(new CalendarDate(cursor.getFullYear(), cursor.getMonth() + 1, cursor.getDate()));

  function pickDate(value: { year: number; month: number; day: number } | undefined): void {
    if (!value) return;
    setCursor(new Date(value.year, value.month - 1, value.day));
    calendarOpen = false;
  }

  // 시간별은 하루, 일별은 한 달, 월별은 한 해, 연별은 5년을 한 화면에 보여줍니다.
  function periodStart(kind: UsageUnit, date: Date): Date {
    if (kind === 'hour') return new Date(date.getFullYear(), date.getMonth(), date.getDate());
    if (kind === 'day') return new Date(date.getFullYear(), date.getMonth(), 1);
    if (kind === 'month') return new Date(date.getFullYear(), 0, 1);
    return new Date(date.getFullYear() - yearSpan + 1, 0, 1);
  }

  function shift(kind: UsageUnit, date: Date, steps: number): Date {
    if (kind === 'hour') return new Date(date.getFullYear(), date.getMonth(), date.getDate() + steps);
    if (kind === 'day') return new Date(date.getFullYear(), date.getMonth() + steps, 1);
    if (kind === 'month') return new Date(date.getFullYear() + steps, 0, 1);
    return new Date(date.getFullYear() + steps * yearSpan, 0, 1);
  }

  // 지금이 들어 있는 기간으로 돌아오면 오늘로 보아 date를 비웁니다.
  function move(steps: number): void {
    const target = shift(unit, from, steps);
    setCursor(periodStart(unit, target).getTime() === periodStart(unit, new Date()).getTime() ? new Date() : target);
  }

  const from = $derived(periodStart(unit, cursor));
  const to = $derived(shift(unit, from, 1));
  const isCurrent = $derived(to.getTime() > Date.now());

  const periodLabel = $derived.by(() => {
    const locale = intlLocale();
    if (unit === 'hour') return from.toLocaleDateString(locale, { dateStyle: 'long' });
    if (unit === 'day') return from.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
    if (unit === 'month') return from.toLocaleDateString(locale, { year: 'numeric' });
    return `${from.toLocaleDateString(locale, { year: 'numeric' })} – ${new Date(to.getFullYear() - 1, 0, 1).toLocaleDateString(locale, { year: 'numeric' })}`;
  });

  const query = $derived(new URLSearchParams({ unit, from: from.toISOString(), to: to.toISOString(), tz: timeZone }).toString());

  // 가장 최근에 시작한 요청의 응답만 반영합니다. 새로고침(background)은 이전 데이터를 그대로 두고, 실패해도 알리지 않고 다음 주기에 다시 시도합니다.
  let latest = 0;
  async function load(background: boolean): Promise<void> {
    const id = ++latest;
    if (!background) error = '';
    try {
      const response = await api<UsageSeries>(`/api/v1/usage?${query}`);
      if (id !== latest) return;
      series = response;
      error = '';
    } catch (cause) {
      if (id === latest && !background) error = t().loadFailed(errorMessage(cause));
    }
  }

  // 기간이나 단위가 바뀌면 다시 불러오고, 이전 기간의 늦은 응답은 버립니다.
  $effect(() => {
    void query;
    void load(false);
    return () => { latest += 1; };
  });

  // 자동 새로고침: 지금이 들어 있는 기간을 보고 있고 탭이 보일 때만 5초마다 불러오며, 탭이 다시 보이면 바로 한 번 불러옵니다.
  $effect(() => {
    if (!live) return;
    const refresh = () => {
      if (document.visibilityState === 'visible' && to.getTime() > Date.now()) void load(true);
    };
    const timer = setInterval(refresh, refreshMs);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', refresh);
    };
  });

  const buckets = $derived(series?.unit === unit ? series.buckets : []);
  function axisLabel(start: string): string {
    const date = new Date(start);
    const locale = intlLocale();
    if (unit === 'hour') return date.toLocaleTimeString(locale, { hour: '2-digit', hourCycle: 'h23' });
    if (unit === 'day') return date.toLocaleDateString(locale, { day: 'numeric' });
    if (unit === 'month') return date.toLocaleDateString(locale, { month: 'short' });
    return date.toLocaleDateString(locale, { year: 'numeric' });
  }

  function bucketTitle(start: string): string {
    const date = new Date(start);
    const locale = intlLocale();
    if (unit === 'hour') return date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
    if (unit === 'day') return date.toLocaleDateString(locale, { dateStyle: 'medium' });
    if (unit === 'month') return date.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
    return date.toLocaleDateString(locale, { year: 'numeric' });
  }

</script>

<Card.Root size="sm" aria-labelledby="request-trend-title" class={cn('lg:h-full', className)}>
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id="request-trend-title">{t().title}</Card.Title>
    <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
      <div class="flex flex-wrap items-center gap-x-3 gap-y-2">
        <Tooltip.Root>
          <Tooltip.Trigger>
            {#snippet child({ props })}
              <Switch {...props} size="sm" aria-label={t().autoRefresh} bind:checked={() => live, setLive} />
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content>{t().autoRefresh}</Tooltip.Content>
        </Tooltip.Root>
        <ButtonGroup.Root>
          <ButtonGroup.Root>
            <NativeSelect.Root size="sm" aria-label={t().unitsAria} bind:value={() => unit, (next) => { if (next) setUnit(next as UsageUnit); }}>
              {#each units as option (option)}
                <NativeSelect.Option value={option}>{t().units[option]}</NativeSelect.Option>
              {/each}
            </NativeSelect.Root>
          </ButtonGroup.Root>
          <ButtonGroup.Root>
            <IconButton variant="outline" label={t().previous} icon={ChevronLeft} onclick={() => move(-1)} />
            <Popover.Root bind:open={calendarOpen}>
              <Popover.Trigger>
                {#snippet child({ props })}
                  <Button {...props} variant="outline" size="sm" class="tabular-nums" aria-label={`${t().pickDate}: ${periodLabel}`}>
                    <CalendarLtr12 aria-hidden="true" /><span aria-live="polite">{periodLabel}</span>
                  </Button>
                {/snippet}
              </Popover.Trigger>
              <Popover.Content class="w-auto p-0" align="end">
                <Calendar type="single" preventDeselect value={selectedDate} onValueChange={pickDate} maxValue={today(getLocalTimeZone())} locale={intlLocale()} captionLayout="dropdown" />
              </Popover.Content>
            </Popover.Root>
            <IconButton variant="outline" label={t().next} icon={ChevronRight} side="left" disabled={isCurrent} onclick={() => move(1)} />
          </ButtonGroup.Root>
        </ButtonGroup.Root>
      </div>
    </Card.Action>
  </Card.Header>
  {#if error}
    <Card.Content>
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>
    </Card.Content>
  {:else}
    <TrafficChart {buckets} {shown} onShownChange={setShown} {axisLabel} {bucketTitle} averageLabel={t().average[unit]} />
  {/if}
</Card.Root>
