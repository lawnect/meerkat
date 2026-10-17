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

export function statusLabel(value: string | undefined): string {
  const labels: Record<string, string> = {
    draft: '등록 대기',
    connected: '연결됨',
    connecting: '연결 중',
    stale: '응답 없음',
    disconnected: '연결 끊김',
    unknown: '확인 대기',
    error: '오류',
  };
  return value ? labels[value] || value : '확인 대기';
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
