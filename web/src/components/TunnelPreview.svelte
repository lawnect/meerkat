<script lang="ts">
  import type { Snippet } from 'svelte';
  import { mergeProps } from 'bits-ui';
  import * as HoverCard from '$lib/components/ui/hover-card/index.js';
  import { translations } from '../i18n.svelte';
  import { Open12 } from '../icons';
  import { connectorDetailPath } from '../navigation';

  // 터널 이름에 올리면 공개 주소를 미리 보여주는 카드(shadcn Hover Card). 최대 3개를 한 줄씩 보여주고,
  // 더 있으면 "+N개 더보기"로 터널 상세의 경로 탭을 엽니다. 이름이 잘렸으면 전체 이름이 첫 줄에 나옵니다.
  let { id, name, hostnames, onOpenDetail, trigger, nested = false }: {
    id: string;
    name: string;
    hostnames: string[];
    onOpenDetail: (id: string, search?: string) => void;
    // 트리거 요소에 펼쳐 쓸 props를 받습니다. 이름을 한 줄로 자르는(truncate) 요소여야 잘림을 잴 수 있습니다.
    trigger: Snippet<[Record<string, unknown>]>;
    // 이미 링크 안에 있는 트리거(카드 제목)는 role=button 같은 상호작용 속성을 빼서 링크 안에 버튼이 중첩되지 않게 합니다.
    nested?: boolean;
  } = $props();

  const limit = 3;
  const t = translations({
    none: 'No public addresses',
    more: (n: number) => `+${n} more`,
    openAddress: (host: string) => `Open ${host} in a new tab`,
    moreAria: (n: number) => `${n} more public addresses, open the routes tab`,
  }, {
    none: '공개 주소 없음',
    more: (n: number) => `+${n}개 더보기`,
    openAddress: (host: string) => `${host} 새 탭에서 열기`,
    moreAria: (n: number) => `공개 주소 ${n}개 더 있음, 경로 탭 열기`,
  });

  let truncated = $state(false);
  const measure = (event: Event) => {
    const element = event.currentTarget as HTMLElement | null;
    if (element) truncated = element.scrollWidth > element.clientWidth;
  };

  function triggerProps(props: Record<string, unknown>): Record<string, unknown> {
    if (!nested) return props;
    const { role: _role, 'aria-haspopup': _popup, 'aria-expanded': _expanded, ...rest } = props;
    return rest;
  }

  const routesHref = $derived(`${connectorDetailPath(id)}?tab=routes`);
  function openRoutes(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpenDetail(id, '?tab=routes');
  }
</script>

<HoverCard.Root openDelay={300} closeDelay={100}>
  <HoverCard.Trigger>
    {#snippet child({ props })}
      {@render trigger(triggerProps(mergeProps(props, { onpointerenter: measure, onfocusin: measure })))}
    {/snippet}
  </HoverCard.Trigger>
  <HoverCard.Content align="start" class="grid w-auto max-w-xs min-w-48 gap-1">
    {#if truncated}<div class="font-medium wrap-anywhere">{name}</div>{/if}
    {#if hostnames.length}
      {#each hostnames.slice(0, limit) as hostname (hostname)}
        <a href={`https://${hostname}`} target="_blank" rel="noopener noreferrer" aria-label={t().openAddress(hostname)} class="flex min-w-0 items-center gap-1.5 font-mono text-xs outline-none hover:underline focus-visible:ring-3 focus-visible:ring-ring/50">
          <span class="truncate">{hostname}</span><Open12 class="size-3 shrink-0 text-muted-foreground" aria-hidden="true" />
        </a>
      {/each}
      {#if hostnames.length > limit}
        <a href={routesHref} onclick={openRoutes} aria-label={t().moreAria(hostnames.length - limit)} class="w-fit text-xs text-muted-foreground outline-none hover:text-foreground hover:underline focus-visible:ring-3 focus-visible:ring-ring/50">{t().more(hostnames.length - limit)}</a>
      {/if}
    {:else}
      <span class="text-muted-foreground">{t().none}</span>
    {/if}
  </HoverCard.Content>
</HoverCard.Root>
