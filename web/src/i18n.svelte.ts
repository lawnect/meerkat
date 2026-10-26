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
