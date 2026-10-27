<script lang="ts">
  import type { ButtonSize, ButtonVariant } from '$lib/components/ui/button/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';
  import { ArrowDownload } from '../icons';
  import IconButton from './IconButton.svelte';

  // 현재 필터에 해당하는 항목 전체를 CSV로 내려받는 아이콘 링크. href는 서버의 내보내기 주소(필터 + utc_offset, format.ts의 csvHref)이고,
  // 페이지 이동이 아닌 내려받기라서 fetch가 아닌 <a download>입니다. 파일 이름은 서버가 정합니다. disabled이면 링크가 아니라 꺼진 버튼처럼 보입니다.
  let { href, disabled = false, variant, size, class: className }: { href: string; disabled?: boolean; variant?: ButtonVariant; size?: ButtonSize; class?: string } = $props();

  const t = translations({
    label: 'Download CSV',
    tip: 'Download every entry matching the current filters',
  }, {
    label: 'CSV 다운로드',
    tip: '현재 필터에 해당하는 모든 항목을 내려받습니다',
  });
</script>

<IconButton {href} download {disabled} {variant} {size} class={cn('aria-disabled:pointer-events-none aria-disabled:opacity-50', className)} label={t().label} tip={t().tip} icon={ArrowDownload} />
