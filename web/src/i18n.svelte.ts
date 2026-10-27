export type Locale = 'en' | 'ko';

const intlLocales: Record<Locale, string> = { en: 'en-US', ko: 'ko-KR' };

// 영어는 접두사 없이, 한국어는 /ko 아래에서 같은 화면 경로를 사용합니다.
export function localeFromPath(pathname: string): Locale {
  return /^\/ko(?:\/|$)/.test(pathname) ? 'ko' : 'en';
}

export function stripLocale(pathname: string): string {
  return pathname.replace(/^\/ko(?=\/|$)/, '') || '/';
}

export const i18n = $state<{ locale: Locale }>({ locale: localeFromPath(window.location.pathname) });

export function localizePath(path: string, locale: Locale = i18n.locale): string {
  if (locale === 'en') return path;
  return path === '/' ? '/ko' : `/ko${path}`;
}

export function setLocale(locale: Locale): void {
  i18n.locale = locale;
  document.documentElement.lang = locale;
}

const storageKey = 'meerkat:locale';

function storedLocale(): Locale | null {
  try {
    const value = localStorage.getItem(storageKey);
    return value === 'en' || value === 'ko' ? value : null;
  } catch { return null; }
}

// 사용자가 고른 언어입니다. 고르기 전에는 브라우저 언어 설정에서 먼저 나오는 영어나 한국어를 따릅니다.
export function preferredLocale(): Locale {
  const stored = storedLocale();
  if (stored) return stored;
  for (const language of navigator.languages?.length ? navigator.languages : [navigator.language]) {
    const primary = language.toLowerCase().split(/[-_;]/)[0].trim();
    if (primary === 'ko' || primary === 'en') return primary;
  }
  return 'en';
}

// 언어 전환으로 고른 언어를 이 브라우저에 기억합니다.
export function chooseLocale(locale: Locale): void {
  try { localStorage.setItem(storageKey, locale); } catch { /* Keep the choice for this page only. */ }
  setLocale(locale);
}

export function intlLocale(): string {
  return intlLocales[i18n.locale];
}

/**
 * 컴포넌트별 문구를 영어·한국어 한 쌍으로 정의합니다. 반환한 함수를 템플릿에서 호출하면
 * 현재 언어가 바뀔 때 다시 그려집니다. 한국어 문구는 영어와 같은 키와 타입을 가져야 합니다.
 */
export function translations<T extends object>(en: T, ko: NoInfer<T>): () => T {
  return () => (i18n.locale === 'ko' ? ko : en);
}
