import { mockApi } from './mockApi.js';

export const mockApiEnabled = import.meta.env.VITE_MOCK_API === 'true';

export async function api(url, options = {}) {
  if (mockApiEnabled) return mockApi(url, options);

  const headers = new Headers(options.headers ?? {});
  if (options.body && !headers.has('Content-Type')) {
    headers.set('Content-Type', 'application/json');
  }
  const response = await fetch(url, {
    credentials: 'same-origin',
    ...options,
    headers,
  });
  const body = response.status === 204 ? {} : await response.json().catch(() => ({}));
  if (!response.ok) throw new Error(body.code || 'request_failed');
  return body;
}

export function statusLabel(value) {
  return ({
    draft: '등록 대기',
    connected: '연결됨',
    connecting: '연결 중',
    stale: '응답 없음',
    disconnected: '연결 끊김',
    unknown: '확인 대기',
    error: '오류',
  })[value] || value || '확인 대기';
}

export function statusKind(value) {
  if (value === 'connected') return 'ok';
  if (['error', 'stale', 'disconnected'].includes(value)) return 'bad';
  return 'warn';
}

export function messageFor(error, fallback) {
  return `${fallback}: ${error.message}`;
}
