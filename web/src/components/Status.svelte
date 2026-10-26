<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { cn } from '$lib/utils.js';
  import { statusKind } from '../api';

  // 연결 상태 배지. 정상은 초록, 대기는 노랑, 문제는 빨강으로 표시합니다.
  let { label, value, class: className = '' }: { label: string; value?: string; class?: string } = $props();

  const tone = {
    ok: 'bg-emerald-500/10 text-emerald-700 dark:bg-emerald-400/15 dark:text-emerald-300',
    warn: 'bg-amber-500/15 text-amber-800 dark:bg-amber-400/15 dark:text-amber-300',
    bad: 'bg-destructive/10 text-destructive dark:bg-destructive/20',
  } as const;
</script>

<Badge variant="secondary" class={cn('cursor-default select-none', tone[statusKind(value)], className)}>{label}</Badge>
