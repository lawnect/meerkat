<script lang="ts">
  import * as Pagination from '$lib/components/ui/pagination/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';

  // 목록 위·아래에 두는 줄: 왼쪽은 표시 범위, 오른쪽은 shadcn Pagination(문서의 Usage 구성 그대로)입니다.
  // 위쪽(placement="top")은 페이지가 둘 이상일 때만 보이고, 아래쪽은 한 페이지여도 범위를 보여줍니다.
  let { total, perPage, page = $bindable(1), placement = 'bottom', class: className }: {
    total: number;
    perPage: number;
    page?: number;
    placement?: 'top' | 'bottom';
    class?: string;
  } = $props();

  const t = translations({
    range: (from: number, to: number, total: number) => `${from}–${to} of ${total}`,
    top: 'Pagination, above the list',
    bottom: 'Pagination, below the list',
  }, {
    range: (from: number, to: number, total: number) => `${total}개 중 ${from}–${to}`,
    top: '목록 위 페이지 이동',
    bottom: '목록 아래 페이지 이동',
  });

  const from = $derived(total ? (page - 1) * perPage + 1 : 0);
  const to = $derived(Math.min(page * perPage, total));
  const paged = $derived(total > perPage);

  // 목록이 줄어 현재 페이지가 범위를 벗어나면(주소에서 온 값 포함) 첫 페이지로 돌아갑니다.
  $effect(() => {
    if (total > 0 && (page - 1) * perPage >= total) page = 1;
  });
</script>

{#if placement === 'bottom' || paged}
  <div class={cn('flex flex-wrap items-center justify-between gap-2 text-xs text-muted-foreground tabular-nums', className)} data-placement={placement}>
    <span aria-live={placement === 'bottom' ? 'polite' : undefined}>{t().range(from, to, total)}</span>
    {#if paged}
      <Pagination.Root class="mx-0 w-auto" count={total} {perPage} aria-label={placement === 'top' ? t().top : t().bottom} bind:page>
        {#snippet children({ pages, currentPage })}
          <Pagination.Content>
            <Pagination.Item>
              <Pagination.Previous />
            </Pagination.Item>
            {#each pages as item (item.key)}
              {#if item.type === 'ellipsis'}
                <Pagination.Item>
                  <Pagination.Ellipsis />
                </Pagination.Item>
              {:else}
                <Pagination.Item>
                  <Pagination.Link page={item} isActive={currentPage === item.value}>{item.value}</Pagination.Link>
                </Pagination.Item>
              {/if}
            {/each}
            <Pagination.Item>
              <Pagination.Next />
            </Pagination.Item>
          </Pagination.Content>
        {/snippet}
      </Pagination.Root>
    {/if}
  </div>
{/if}
