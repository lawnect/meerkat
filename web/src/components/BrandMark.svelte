<script lang="ts">
  import logo from '../assets/meerkat-inverted.svg';
  import logoOutline from '../assets/meerkat.svg';
  import { theme } from '../theme.svelte';

  let { class: className = 'size-8' }: { class?: string } = $props();
</script>

<!-- 라이트 테마는 반전 로고, 다크 테마는 원본 선 로고를 밝게 뒤집어 씁니다. -->
<img class={[className, theme.current === 'dark' && 'invert']} src={theme.current === 'dark' ? logoOutline : logo} alt="" aria-hidden="true" />
