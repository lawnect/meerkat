// 화면 상태를 주소의 query(?q=…&page=2)에 담습니다. 새로고침·뒤로 가기·주소 공유로 같은 화면이 복원됩니다.
// urlState.search가 단일 출처이며, 읽는 함수는 이 값을 따라 다시 계산되고, 쓰는 함수는 history.replaceState로 주소를 바꿉니다.
// 탭·보기 전환처럼 화면을 옮기는 변경은 { push: true }로 방문 기록에 남겨, 뒤로 가기가 바로 이전 탭·보기로 돌아가게 합니다.
// 검색어·필터·정렬·페이지는 기록에 남기지 않습니다(입력할 때마다 뒤로 가기가 쌓이지 않게).
// 경로(언어 접두사 포함)와 hash는 그대로 두고, 기본값과 같은 값은 주소에서 뺍니다.
import type { SortingState } from '@tanstack/svelte-table';

export const urlState = $state({ search: window.location.search });

// popstate나 pushState 뒤에 주소의 query를 다시 읽습니다.
export function syncUrlState(): void {
  urlState.search = window.location.search;
}

export type ParamValue = string | number | boolean | null | undefined;

const search = (): URLSearchParams => new URLSearchParams(urlState.search);

export function param(name: string): string | null {
  return search().get(name);
}

// 허용한 값이 아니면 조용히 기본값을 씁니다.
export function oneOf<T extends string>(name: string, allowed: readonly T[], fallback: T): T {
  const value = param(name);
  return value !== null && (allowed as readonly string[]).includes(value) ? (value as T) : fallback;
}

export function intParam(name: string, fallback: number, min = 1, max = Number.MAX_SAFE_INTEGER): number {
  const value = param(name);
  if (value === null || !/^\d+$/.test(value)) return fallback;
  const number = Number(value);
  return number >= min && number <= max ? number : fallback;
}

// 0으로 끈 값만 false이고, 그 밖에는 기본값을 따릅니다.
export function boolParam(name: string, fallback: boolean): boolean {
  const value = param(name);
  return value === '1' || value === 'true' ? true : value === '0' || value === 'false' ? false : fallback;
}

export function dateParam(name: string): string | null {
  const value = param(name);
  if (!value || !/^\d{4}-\d{2}-\d{2}$/.test(value)) return null;
  return Number.isNaN(Date.parse(`${value}T00:00:00`)) ? null : value;
}

const text = (value: ParamValue): string => (typeof value === 'boolean' ? (value ? '1' : '0') : String(value ?? ''));

// changes의 값을 합쳐 주소에 씁니다. 값이 비었거나 defaults와 같으면 그 항목을 지웁니다.
export function setParams(changes: Record<string, ParamValue>, defaults: Record<string, ParamValue> = {}, options: { push?: boolean } = {}): void {
  const next = new URLSearchParams(window.location.search);
  for (const [name, value] of Object.entries(changes)) {
    const asText = text(value);
    if (asText === '' || (name in defaults && asText === text(defaults[name]))) next.delete(name);
    else next.set(name, asText);
  }
  const query = next.toString();
  const nextSearch = query ? `?${query}` : '';
  if (nextSearch !== window.location.search) {
    const url = `${window.location.pathname}${nextSearch}${window.location.hash}`;
    if (options.push) window.history.pushState(null, '', url);
    else window.history.replaceState(window.history.state, '', url);
  }
  urlState.search = window.location.search;
}

// 정렬은 `status.desc,name`처럼 적습니다(.desc가 없으면 오름차순, `none`은 정렬 없음).
export function parseSort(value: string | null, sortable: readonly string[], fallback: SortingState): SortingState {
  if (value === null) return fallback;
  if (value === 'none') return [];
  const sorting: SortingState = [];
  for (const part of value.split(',')) {
    const [id, direction] = part.split('.');
    if (!sortable.includes(id) || (direction !== undefined && direction !== 'asc' && direction !== 'desc') || sorting.some((entry) => entry.id === id)) return fallback;
    sorting.push({ id, desc: direction === 'desc' });
  }
  return sorting.length ? sorting : fallback;
}

export function formatSort(sorting: SortingState): string {
  return sorting.length ? sorting.map((entry) => (entry.desc ? `${entry.id}.desc` : entry.id)).join(',') : 'none';
}
