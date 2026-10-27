// 기간 탐색 상태: 단위(분별·시간별·일별·월별·연별), 기준 날짜, 자동 새로고침을 주소의 query(?unit=day&date=2026-03-04&live=0)에 담습니다.
// 분별은 대시보드 트래픽만 씁니다(createPeriod({ units: trafficUnits })). 한 화면은 60분이며, 시작(?start=Unix 초)이 없으면 최근 60분을
// 1분마다 따라가고, 있으면 그 정시부터 60분입니다. 분별 합계의 보관 기간(7일) 안에서만 거슬러 갑니다.
// 대시보드의 트래픽 카드(RequestTrend)와 관찰의 지표 탭(AccessMetrics)이 같은 값을 같은 방식으로 쓰도록 모았습니다.
// 화면에는 PeriodControls를 두고, 값을 불러오는 쪽은 trackPeriod로 기간이 바뀔 때와 5초마다 다시 불러옵니다.
import { CalendarDate } from '@internationalized/date';
import { untrack } from 'svelte';
import { errorMessage } from './api';
import { intlLocale } from './i18n.svelte';
import type { UsageUnit } from './types';
import { boolParam, dateParam, intParam, oneOf, setParams } from './urlState.svelte';

export const periodUnits: UsageUnit[] = ['hour', 'day', 'month', 'year'];
export const trafficUnits: UsageUnit[] = ['minute', ...periodUnits];
const MINUTE_MS = 60000;
const HOUR_MS = 60 * MINUTE_MS;
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

// 분별은 한 시간, 시간별은 하루, 일별은 한 달, 월별은 한 해, 연별은 5년을 한 화면에 보여줍니다.
function periodStart(kind: UsageUnit, date: Date): Date {
  if (kind === 'minute') return new Date(date.getFullYear(), date.getMonth(), date.getDate(), date.getHours());
  if (kind === 'hour') return new Date(date.getFullYear(), date.getMonth(), date.getDate());
  if (kind === 'day') return new Date(date.getFullYear(), date.getMonth(), 1);
  if (kind === 'month') return new Date(date.getFullYear(), 0, 1);
  return new Date(date.getFullYear() - yearSpan + 1, 0, 1);
}

function shift(kind: UsageUnit, date: Date, steps: number): Date {
  if (kind === 'minute') return new Date(date.getTime() + steps * HOUR_MS);
  if (kind === 'hour') return new Date(date.getFullYear(), date.getMonth(), date.getDate() + steps);
  if (kind === 'day') return new Date(date.getFullYear(), date.getMonth() + steps, 1);
  if (kind === 'month') return new Date(date.getFullYear() + steps, 0, 1);
  return new Date(date.getFullYear() + steps * yearSpan, 0, 1);
}

// 분별 한 시간의 시작(Unix 초, 그 지역의 정시)입니다. 시각이 아니라 순간이라 서머타임이 끝나 같은 시가 두 번 와도 구분됩니다.
// 없으면 최근 60분입니다.
function startFromUrl(): Date | null {
  const seconds = intParam('start', 0, 1);
  return seconds > 0 && seconds % 60 === 0 ? new Date(seconds * 1000) : null;
}

// 분별 합계는 7일 보관합니다.
const minuteRetentionMs = 7 * 24 * HOUR_MS;

// 컴포넌트 초기화 중에 부릅니다. 읽는 값은 모두 주소(urlState)를 따라 다시 계산됩니다.
export function createPeriod({ units = periodUnits }: { units?: UsageUnit[] } = {}) {
  const unit = $derived(oneOf('unit', units, defaultUnit));
  const cursor = $derived(cursorFromUrl());
  const live = $derived(boolParam('live', true));
  const start = $derived(unit === 'minute' ? startFromUrl() : null);
  // 최근 60분은 지금을 따라 1분마다 옮겨 갑니다(자동 새로고침이 켜져 있을 때). 다시 켜면 그때부터 따라갑니다.
  let now = $state(Date.now());
  const rolling = $derived(unit === 'minute' && start === null);
  $effect(() => {
    if (!rolling || !live) return;
    let timer = 0;
    const tick = (): void => {
      now = Date.now();
      timer = window.setTimeout(tick, MINUTE_MS - (Date.now() % MINUTE_MS) + 50);
    };
    tick();
    return () => window.clearTimeout(timer);
  });
  const from = $derived.by(() => {
    if (rolling) return new Date(Math.floor(now / MINUTE_MS) * MINUTE_MS - 59 * MINUTE_MS);
    if (start) return start;
    return periodStart(unit, cursor);
  });
  const to = $derived(unit === 'minute' ? new Date(from.getTime() + HOUR_MS) : shift(unit, from, 1));
  const isCurrent = $derived(rolling || to.getTime() > Date.now());
  // 분별은 보관 기간(7일) 안에서만 거슬러 갑니다.
  const earliest = $derived(unit === 'minute' ? new Date(Date.now() - minuteRetentionMs) : null);
  const atEarliest = $derived(earliest !== null && from.getTime() - HOUR_MS < earliest.getTime());

  const label = $derived.by(() => {
    const locale = intlLocale();
    if (unit === 'minute') {
      if (rolling && live) return locale.startsWith('ko') ? '최근 60분' : 'Last 60 minutes';
      const date = from.toLocaleDateString(locale, { dateStyle: 'long' });
      const time = (at: Date): string => at.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
      return `${date} ${time(from)}–${time(to)}`;
    }
    if (unit === 'hour') return from.toLocaleDateString(locale, { dateStyle: 'long' });
    if (unit === 'day') return from.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
    if (unit === 'month') return from.toLocaleDateString(locale, { year: 'numeric' });
    return `${from.toLocaleDateString(locale, { year: 'numeric' })} – ${new Date(to.getFullYear() - 1, 0, 1).toLocaleDateString(locale, { year: 'numeric' })}`;
  });

  // GET …?unit&from&to&tz에 붙이는 query입니다.
  const query = $derived(new URLSearchParams({ unit, from: from.toISOString(), to: to.toISOString(), tz: timeZone }).toString());

  // 달력은 기준 날짜를, 분별은 보고 있는 시간의 날짜를 선택된 날짜로 보여줍니다.
  const selectedDate = $derived.by(() => {
    const day = unit === 'minute' ? from : cursor;
    return new CalendarDate(day.getFullYear(), day.getMonth() + 1, day.getDate());
  });

  // 단위를 바꾸면 분별의 시간과, 분별로 갈 때는 기준 날짜도 비웁니다(최근 60분부터).
  function setUnit(next: UsageUnit): void {
    setParams({ unit: next, start: null, ...(next === 'minute' ? { date: null } : {}) }, { unit: defaultUnit });
  }

  // 오늘이면 date를 주소에서 뺍니다.
  function setCursor(date: Date): void {
    const key = dayKey(date);
    setParams({ date: key === dayKey(new Date()) ? null : key });
  }

  function setLive(next: boolean): void {
    setParams({ live: next }, { live: true });
  }

  // 분별의 한 시간(at은 그 지역의 정시)으로 갑니다. 지금이 들어 있는 시간이나 그 뒤면 최근 60분으로,
  // 보관 기간 밖이면 그 안의 가장 오래된 시간으로 갑니다. 한 시간씩 옮기므로 정시는 그대로입니다.
  function setStart(at: Date): void {
    if (at.getTime() + HOUR_MS > Date.now()) {
      setParams({ date: null, start: null });
      return;
    }
    const short = Date.now() - minuteRetentionMs - at.getTime();
    const time = short > 0 ? at.getTime() + Math.ceil(short / HOUR_MS) * HOUR_MS : at.getTime();
    setParams({ date: null, start: time / 1000 });
  }

  // 고른 날짜가 들어 있는 기간으로 이동합니다. 분별은 지금 보는 시각(최근 60분이면 지금의 시)을 그 날짜에서 엽니다.
  function pickDate(value: { year: number; month: number; day: number }): void {
    if (unit === 'minute') {
      setStart(new Date(value.year, value.month - 1, value.day, (start ?? new Date()).getHours()));
      return;
    }
    setCursor(new Date(value.year, value.month - 1, value.day));
  }

  // 지금이 들어 있는 기간으로 돌아오면 오늘로 보아 date를 비웁니다. 분별은 한 시간씩 옮기며, 최근 60분에서는 지금의 시 정각이 기준입니다.
  function move(steps: number): void {
    if (unit === 'minute') {
      const anchor = rolling ? periodStart('minute', new Date()).getTime() : from.getTime();
      setStart(new Date(anchor + steps * HOUR_MS));
      return;
    }
    const target = shift(unit, from, steps);
    setCursor(periodStart(unit, target).getTime() === periodStart(unit, new Date()).getTime() ? new Date() : target);
  }

  return {
    get units() { return units; },
    get earliest() { return earliest; },
    get atEarliest() { return atEarliest; },
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
  if (unit === 'minute') return date.toLocaleTimeString(locale, { hour: '2-digit', minute: '2-digit', hourCycle: 'h23' });
  if (unit === 'hour') return date.toLocaleTimeString(locale, { hour: '2-digit', hourCycle: 'h23' });
  if (unit === 'day') return date.toLocaleDateString(locale, { day: 'numeric' });
  if (unit === 'month') return date.toLocaleDateString(locale, { month: 'short' });
  return date.toLocaleDateString(locale, { year: 'numeric' });
}

export function bucketTitle(unit: UsageUnit, start: string): string {
  const date = new Date(start);
  const locale = intlLocale();
  if (unit === 'minute' || unit === 'hour') return date.toLocaleString(locale, { dateStyle: 'medium', timeStyle: 'short' });
  if (unit === 'day') return date.toLocaleDateString(locale, { dateStyle: 'medium' });
  if (unit === 'month') return date.toLocaleDateString(locale, { year: 'numeric', month: 'long' });
  return date.toLocaleDateString(locale, { year: 'numeric' });
}
