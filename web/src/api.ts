import { translations } from './i18n.svelte';
import { mockApi } from './mockApi';

export const mockApiEnabled = import.meta.env.VITE_MOCK_API === 'true';

export async function api<T = Record<string, unknown>>(url: string, options: RequestInit = {}): Promise<T> {
  if (mockApiEnabled) return await mockApi(url, options) as T;

  const headers = new Headers(options.headers ?? {});
  if (options.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const response = await fetch(url, {
    credentials: 'same-origin',
    ...options,
    headers,
  });
  const body: unknown = response.status === 204 ? {} : await response.json().catch(() => ({}));
  if (!response.ok) {
    const code = typeof body === 'object' && body !== null && 'code' in body && typeof body.code === 'string'
      ? body.code
      : 'request_failed';
    throw new Error(code);
  }
  return body as T;
}

// 화면에 보이는 터널 이름: 표시 이름이 있으면 그것을, 없으면 실행 이름을 씁니다. 터널 이름을 보여주는 곳은 모두 이 함수를 씁니다.
export const connectorLabel = (connector: { name: string; display_name?: string }): string => connector.display_name || connector.name;

const statusLabels = translations({
  draft: 'Awaiting setup',
  connected: 'Connected',
  connecting: 'Connecting',
  stale: 'Not responding',
  disconnected: 'Disconnected',
  unknown: 'Pending check',
  error: 'Error',
  disabled: 'Disabled',
} as Record<string, string>, {
  draft: '등록 대기',
  connected: '연결됨',
  connecting: '연결 중',
  stale: '응답 없음',
  disconnected: '연결 끊김',
  unknown: '확인 대기',
  error: '오류',
  disabled: '사용 안 함',
});

export function statusLabel(value: string | undefined): string {
  const labels = statusLabels();
  return value ? labels[value] || value : labels.unknown;
}

export function statusKind(value: string | undefined): 'ok' | 'bad' | 'warn' {
  if (value === 'connected') return 'ok';
  if (value && ['error', 'stale', 'disconnected'].includes(value)) return 'bad';
  return 'warn';
}

export function errorMessage(error: unknown): string {
  return error instanceof Error ? error.message : String(error);
}

export function messageFor(error: unknown, fallback: string): string {
  return `${fallback}: ${errorMessage(error)}`;
}
