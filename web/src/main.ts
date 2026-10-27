import { mount } from 'svelte';
import App from './App.svelte';
import { i18n, localizePath, preferredLocale, setLocale } from './i18n.svelte';
import { applyTheme } from './theme.svelte';
import './app.css';

const target = document.getElementById('app');
if (!target) throw new Error('App mount element #app was not found');

// 접두사 없는(영어) 주소로 들어왔는데 고른 언어나 브라우저 언어가 한국어이면, 그리기 전에 같은 화면의 /ko 주소로 바꿉니다.
// /ko 주소는 언어를 명시한 것이므로 그대로 둡니다.
if (i18n.locale === 'en' && preferredLocale() === 'ko') {
  window.history.replaceState(window.history.state, '', `${localizePath(window.location.pathname, 'ko')}${window.location.search}${window.location.hash}`);
  setLocale('ko');
}
document.documentElement.lang = i18n.locale;
applyTheme();
mount(App, { target });
