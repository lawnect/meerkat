export type Theme = 'light' | 'dark';

const storageKey = 'meerkat:theme';
const systemDark = window.matchMedia('(prefers-color-scheme: dark)');

function storedTheme(): Theme | null {
  try {
    const value = localStorage.getItem(storageKey);
    return value === 'light' || value === 'dark' ? value : null;
  } catch { return null; }
}

// 사용자가 고르기 전에는 운영체제 설정을 따릅니다.
export const theme = $state<{ current: Theme }>({ current: storedTheme() ?? (systemDark.matches ? 'dark' : 'light') });

// shadcn-svelte 테마 변수는 html의 .dark 클래스로 다크 테마를 고릅니다.
export function applyTheme(): void {
  document.documentElement.classList.toggle('dark', theme.current === 'dark');
  document.documentElement.style.colorScheme = theme.current;
  // 모바일 브라우저 주소창 색도 본문 배경과 같게 맞춥니다.
  const meta = document.querySelector<HTMLMetaElement>('meta[name="theme-color"]');
  if (meta) meta.content = getComputedStyle(document.documentElement).backgroundColor;
}

export function setTheme(next: Theme): void {
  theme.current = next;
  try { localStorage.setItem(storageKey, next); } catch { /* Keep the choice for this page only. */ }
  applyTheme();
}

systemDark.addEventListener('change', (event) => {
  if (storedTheme()) return;
  theme.current = event.matches ? 'dark' : 'light';
  applyTheme();
});
