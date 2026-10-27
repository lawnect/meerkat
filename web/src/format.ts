import { intlLocale } from './i18n.svelte';

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes.toLocaleString(intlLocale())} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}

// 기록 표의 시각 열은 시각(시:분:초)과 날짜를 두 줄로 나눠 좁게 씁니다.
export function formatClock(at: string): string {
  return new Date(at).toLocaleTimeString(intlLocale(), { hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' });
}

export function formatDay(at: string): string {
  return new Date(at).toLocaleDateString(intlLocale(), { dateStyle: 'medium' });
}
