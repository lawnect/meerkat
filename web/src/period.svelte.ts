// 기간 탐색 상태: 단위(시간별·일별·월별·연별), 기준 날짜, 자동 새로고침을 주소의 query(?unit=day&date=2026-03-04&live=0)에 담습니다.
// 대시보드의 트래픽 카드(RequestTrend)와 관찰의 지표 탭(AccessMetrics)이 같은 값을 같은 방식으로 쓰도록 모았습니다.
// 화면에는 PeriodControls를 두고, 값을 불러오는 쪽은 trackPeriod로 기간이 바뀔 때와 5초마다 다시 불러옵니다.
import { CalendarDate } from '@internationalized/date';
import { untrack } from 'svelte';
import { errorMessage } from './api';
import { intlLocale } from './i18n.svelte';
import type { UsageUnit } from './types';
import { boolParam, dateParam, oneOf, setParams } from './urlState.svelte';

export const periodUnits: UsageUnit[] = ['hour', 'day', 'month', 'year'];
export const defaultUnit: UsageUnit = 'hour';
export const refreshMs = 5000;
const yearSpan = 5;
const timeZone = Intl.DateTimeFormat().resolvedOptions().timeZone;

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

// 컴포넌트 초기화 중에 부릅니다. 읽는 값은 모두 주소(urlState)를 따라 다시 계산됩니다.
export function createPeriod() {
  const unit = $derived(oneOf('unit', periodUnits, defaultUnit));
  const cursor = $derived(cursorFromUrl());
  const live = $derived(boolParam('live', true));
  const from = $derived(periodStart(unit, cursor));
  const to = $derived(shift(unit, from, 1));
  const isCurrent = $derived(to.getTime() > Date.now());

  const label = $derived.by(() => {
    const locale = intlLocale();
    if (unit === 'hour') return from.toLocaleDateString(locale, { dateStyle: 'long' });
    if (unit === 'day') return from.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
    if (unit === 'month') return from.toLocaleDateString(locale, { year: 'numeric' });
    return `${from.toLocaleDateString(locale, { year: 'numeric' })} – ${new Date(to.getFullYear() - 1, 0, 1).toLocaleDateString(locale, { year: 'numeric' })}`;
  });

  // GET …?unit&from&to&tz에 붙이는 query입니다.
  const query = $derived(new URLSearchParams({ unit, from: from.toISOString(), to: to.toISOString(), tz: timeZone }).toString());

  // 달력은 기준 날짜를 선택된 날짜로 보여줍니다.
  const selectedDate = $derived(new CalendarDate(cursor.getFullYear(), cursor.getMonth() + 1, cursor.getDate()));

  function setUnit(next: UsageUnit): void {
    setParams({ unit: next }, { unit: defaultUnit });
  }

  // 오늘이면 date를 주소에서 뺍니다.
  function setCursor(date: Date): void {
    const key = dayKey(date);
    setParams({ date: key === dayKey(new Date()) ? null : key });
  }

  function setLive(next: boolean): void {
    setParams({ live: next }, { live: true });
  }

  // 고른 날짜가 들어 있는 기간으로 이동합니다.
  function pickDate(value: { year: number; month: number; day: number }): void {
    setCursor(new Date(value.year, value.month - 1, value.day));
  }

  // 지금이 들어 있는 기간으로 돌아오면 오늘로 보아 date를 비웁니다.
  function move(steps: number): void {
    const target = shift(unit, from, steps);
    setCursor(periodStart(unit, target).getTime() === periodStart(unit, new Date()).getTime() ? new Date() : target);
  }

  return {
    get unit() { return unit; },
    get live() { return live; },
    get from() { return from; },
    get to() { return to; },
    get isCurrent() { return isCurrent; },
    get label() { return label; },
    get query() { return query; },
    get selectedDate() { return selectedDate; },
    setUnit,
    setLive,
    pickDate,
    move,
  };
}

export type Period = ReturnType<typeof createPeriod>;

interface Tracked<T> {
  // query는 period.query입니다.
  load: (query: string) => Promise<T>;
  onData: (data: T) => void;
  // 불러오기를 시작하거나 성공하면 ''로, 화면에서 시작한 불러오기가 실패하면 오류 코드로 부릅니다.
  onError: (code: string) => void;
}

// 기간이나 단위가 바뀌면 다시 불러오고 이전 기간의 늦은 응답은 버립니다. 새로고침(background)은 이전 데이터를 그대로 두고,
// 실패해도 알리지 않고 다음 주기에 다시 시도합니다. 컴포넌트 초기화 중에 부릅니다.
export function trackPeriod<T>(period: Period, { load, onData, onError }: Tracked<T>): void {
  let latest = 0;
  async function run(background: boolean): Promise<void> {
    const id = ++latest;
    if (!background) onError('');
    try {
      const data = await load(period.query);
      if (id !== latest) return;
      onData(data);
      onError('');
    } catch (cause) {
      if (id === latest && !background) onError(errorMessage(cause));
    }
  }

  $effect(() => {
    void period.query;
    untrack(() => { void run(false); });
    return () => { latest += 1; };
  });

  // 자동 새로고침: 지금이 들어 있는 기간을 보고 있고 탭이 보일 때만 5초마다 불러오며, 탭이 다시 보이면 바로 한 번 불러옵니다.
  $effect(() => {
    if (!period.live) return;
    const refresh = () => {
      if (document.visibilityState === 'visible' && period.to.getTime() > Date.now()) void run(true);
    };
    const timer = setInterval(refresh, refreshMs);
    document.addEventListener('visibilitychange', refresh);
    return () => {
      clearInterval(timer);
      document.removeEventListener('visibilitychange', refresh);
    };
  });
}

// 구간(start, RFC 3339)의 가로축 눈금과 툴팁 제목입니다. 단위에 맞춰 시·일·월·연으로 줄여 씁니다.
export function axisLabel(unit: UsageUnit, start: string): string {
  const date = new Date(start);
  const locale = intlLocale();
  if (unit === 'hour') return date.toLocaleTimeString(locale, { hour: '2-digit', hourCycle: 'h23' });
  if (unit === 'day') return date.toLocaleDateString(locale, { day: 'numeric' });
  if (unit === 'month') return date.toLocaleDateString(locale, { month: 'short' });
  return date.toLocaleDateString(locale, { year: 'numeric' });
}

export function bucketTitle(unit: UsageUnit, start: string): string {
  const date = new Date(start);
  const locale = intlLocale();
  if (unit === 'hour') return date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
  if (unit === 'day') return date.toLocaleDateString(locale, { dateStyle: 'medium' });
  if (unit === 'month') return date.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
  return date.toLocaleDateString(locale, { year: 'numeric' });
}
