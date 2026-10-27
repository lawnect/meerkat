import { intlLocale } from './i18n.svelte';

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes.toLocaleString(intlLocale())} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

// 요청 하나가 걸린 시간: 1초 미만은 ms, 그 이상은 초(소수 한 자리).
export function formatDuration(ms: number): string {
  return ms < 1000 ? `${ms.toLocaleString(intlLocale())} ms` : `${(ms / 1000).toLocaleString(intlLocale(), { maximumFractionDigits: 1 })} s`;
}

// 지연 시간(밀리초, 실수): 1ms 미만은 소수 둘째 자리, 100ms 미만은 첫째 자리, 1초 미만은 정수, 그 이상은 초(소수 한 자리).
export function formatLatency(ms: number): string {
  const locale = intlLocale();
  const fixed = (value: number, digits: number) => value.toLocaleString(locale, { minimumFractionDigits: digits, maximumFractionDigits: digits });
  if (ms < 1) return `${fixed(ms, 2)} ms`;
  if (ms < 99.95) return `${fixed(ms, 1)} ms`;
  if (ms < 999.5) return `${fixed(ms, 0)} ms`;
  return `${(ms / 1000).toLocaleString(locale, { maximumFractionDigits: 1 })} s`;
}

// 기록 표의 시각 열은 시각(시:분:초)과 날짜를 두 줄로 나눠 좁게 씁니다.
export function formatClock(at: string): string {
  return new Date(at).toLocaleTimeString(intlLocale(), { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' });
}

export function formatDay(at: string): string {
  return new Date(at).toLocaleDateString(intlLocale(), { dateStyle: 'medium' });
}

// CSV 내보내기 주소. 서버가 시각 열을 브라우저의 시간대로 적도록 UTC에서 동쪽으로 얼마나 떨어졌는지(분)를 utc_offset으로 붙입니다.
export function csvHref(path: string, params: URLSearchParams): string {
  const query = new URLSearchParams(params);
  query.set('utc_offset', String(-new Date().getTimezoneOffset()));
  return `${path}?${query}`;
}
