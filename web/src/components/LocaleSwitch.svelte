<script lang="ts">
  import { chooseLocale, localizePath, stripLocale, translations, type Locale } from '../i18n.svelte';
  import { LocalLanguage } from '../icons';
  import IconButton from './IconButton.svelte';

  let { side = 'bottom', align = 'center' }: { side?: 'top' | 'bottom' | 'left' | 'right'; align?: 'start' | 'center' | 'end' } = $props();

  // 툴팁은 바꿀 언어로 적어 그 언어 사용자가 알아볼 수 있게 합니다.
  const t = translations({ target: 'ko' as Locale, label: '한국어로 보기' }, { target: 'en', label: 'View in English' });

  function localizedCurrentUrl(locale: Locale): string {
    return `${localizePath(stripLocale(window.location.pathname), locale)}${window.location.search}${window.location.hash}`;
  }

  // 같은 화면을 유지한 채 주소의 언어 접두사만 바꾸고, 고른 언어를 기억합니다.
  function switchLocale(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    const locale = t().target;
    window.history.pushState(null, '', localizedCurrentUrl(locale));
    chooseLocale(locale);
  }
</script>

<IconButton {side} {align} label={t().label} icon={LocalLanguage} href={localizedCurrentUrl(t().target)} hreflang={t().target} onclick={switchLocale} />
