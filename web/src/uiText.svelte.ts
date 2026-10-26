import { translations } from './i18n.svelte';

// shadcn-svelte 컴포넌트 안에 들어가는 공통 문구입니다.
export const uiText = translations({
  close: 'Close',
  morePages: 'More pages',
  more: 'More',
  previous: 'Previous',
  next: 'Next',
  previousPage: 'Go to previous page',
  nextPage: 'Go to next page',
  toggleSidebar: 'Toggle sidebar',
  loading: 'Loading',
}, {
  close: '닫기',
  morePages: '페이지 더 보기',
  more: '더 보기',
  previous: '이전',
  next: '다음',
  previousPage: '이전 페이지로',
  nextPage: '다음 페이지로',
  toggleSidebar: '사이드바 열기·닫기',
  loading: '불러오는 중',
});
