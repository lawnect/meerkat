import type { EnrollmentToken } from './types';

export const connectorState = $state<{ selectedId: string; enrollment: EnrollmentToken | null; enrollmentWasReset: boolean }>({
  selectedId: '',
  enrollment: null,
  enrollmentWasReset: false,
});

const enrollmentStoragePrefix = 'meerkat:enrollment:';
// seen는 토큰을 발급할 때 터널의 마지막 응답 시각입니다. 이 값이 바뀐 연결 상태가 되면 토큰을 더는 보관하지 않습니다.
type StoredEnrollment = { enrollment: EnrollmentToken; reset: boolean; seen?: string };

export function storedEnrollment(id: string): StoredEnrollment | null {
  try {
    const raw = sessionStorage.getItem(`${enrollmentStoragePrefix}${id}`);
    if (!raw) return null;
    const value = JSON.parse(raw) as StoredEnrollment;
    if (typeof value.enrollment?.token === 'string' && value.enrollment.token && !('expires_at' in value.enrollment)) return value;
    sessionStorage.removeItem(`${enrollmentStoragePrefix}${id}`);
  } catch { /* Private browsing or disabled storage. */ }
  return null;
}

export function rememberEnrollment(id: string, enrollment: EnrollmentToken, reset: boolean, seen?: string): void {
  try { sessionStorage.setItem(`${enrollmentStoragePrefix}${id}`, JSON.stringify({ enrollment, reset, seen } satisfies StoredEnrollment)); } catch { /* Keep the current screen usable. */ }
}

export function forgetEnrollment(id: string): void {
  try { sessionStorage.removeItem(`${enrollmentStoragePrefix}${id}`); } catch { /* Storage may be unavailable. */ }
}

// 연결된 터널의 토큰은 sessionStorage에서 지웁니다. 토큰을 교체한 직후에는 이전 인스턴스가 아직 연결 상태일 수 있어,
// 발급한 뒤에 응답이 있었을 때(마지막 응답 시각이 바뀐 때)만 지웁니다. 화면에 열어 둔 토큰은 이동하기 전까지 그대로 둡니다.
export function forgetConnectedEnrollments(connectors: { id: string; status: string; last_seen?: string }[]): void {
  for (const connector of connectors) {
    if (connector.status !== 'connected' || !connector.last_seen) continue;
    const saved = storedEnrollment(connector.id);
    if (saved && saved.seen !== connector.last_seen) forgetEnrollment(connector.id);
  }
}

export function clearStoredEnrollments(): void {
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index--) {
      const key = sessionStorage.key(index);
      if (key?.startsWith(enrollmentStoragePrefix)) sessionStorage.removeItem(key);
    }
  } catch { /* Storage may be unavailable. */ }
}
