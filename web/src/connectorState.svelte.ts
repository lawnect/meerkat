import type { EnrollmentToken } from './types';

export const connectorState = $state<{ selectedId: string; enrollment: EnrollmentToken | null; enrollmentWasReset: boolean }>({
  selectedId: '',
  enrollment: null,
  enrollmentWasReset: false,
});

const enrollmentStoragePrefix = 'meerkat:enrollment:';
type StoredEnrollment = { enrollment: EnrollmentToken; reset: boolean };

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

export function rememberEnrollment(id: string, enrollment: EnrollmentToken, reset: boolean): void {
  try { sessionStorage.setItem(`${enrollmentStoragePrefix}${id}`, JSON.stringify({ enrollment, reset } satisfies StoredEnrollment)); } catch { /* Keep the current screen usable. */ }
}

export function forgetEnrollment(id: string): void {
  try { sessionStorage.removeItem(`${enrollmentStoragePrefix}${id}`); } catch { /* Storage may be unavailable. */ }
}

export function clearStoredEnrollments(): void {
  try {
    for (let index = sessionStorage.length - 1; index >= 0; index--) {
      const key = sessionStorage.key(index);
      if (key?.startsWith(enrollmentStoragePrefix)) sessionStorage.removeItem(key);
    }
  } catch { /* Storage may be unavailable. */ }
}
