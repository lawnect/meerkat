<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { cn } from '$lib/utils.js';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Pulse20, Warning } from '../icons';
  import { connectorDetailPath } from '../navigation';
  import type { UsageSeries } from '../types';
  import ShareChart, { type ShareItem } from './ShareChart.svelte';

  // 트래픽 카드가 불러온 series를 그대로 받아 같은 기간의 터널별 처리 건수를 도넛으로 보여줍니다(따로 요청하지 않습니다).
  // 범례의 터널은 그 터널 상세의 사용량 탭으로 가는 링크입니다.
  let { series, error = '', labels = {}, onOpenDetail, class: className }: {
    series: UsageSeries | null;
    error?: string;
    // 터널 id별로 화면에 보여줄 이름(표시 이름). 없으면 응답의 name을 씁니다.
    labels?: Record<string, string>;
    onOpenDetail: (id: string, search?: string) => void;
    class?: string;
  } = $props();

  const t = translations({
    title: 'Requests by tunnel',
    requests: 'Requests',
    empty: 'No requests in this period',
    chartAria: (count: string) => `Requests by tunnel: ${count} in total`,
  }, {
    title: '터널별 처리 건수',
    requests: '처리 건수',
    empty: '이 기간에 처리한 요청이 없습니다',
    chartAria: (count: string) => `터널별 처리 건수: 총 ${count}건`,
  });

  const usageTab = '?tab=usage';
  const count = (value: number): string => value.toLocaleString(intlLocale());

  function openTunnel(event: MouseEvent, id: string): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpenDetail(id, usageTab);
  }

  const items = $derived<ShareItem[]>((series?.connectors ?? []).map((connector) => ({
    label: labels[connector.connector_id] ?? connector.name,
    value: connector.requests,
    href: `${connectorDetailPath(connector.connector_id)}${usageTab}`,
    onclick: (event: MouseEvent) => openTunnel(event, connector.connector_id),
  })));
  const requests = $derived(items.reduce((sum, item) => sum + item.value, 0));
</script>

<Card.Root size="sm" aria-labelledby="tunnel-share-title" class={cn('lg:h-full', className)}>
  <Card.Header>
    <Card.Title id="tunnel-share-title">{t().title}</Card.Title>
  </Card.Header>
  <Card.Content class="flex min-w-0 flex-1 flex-col justify-center">
    {#if error && !series}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>
    {:else if !series}
      <div class="grid justify-items-center gap-4" aria-busy="true">
        <Skeleton class="size-44" />
        <div class="grid w-full gap-2"><Skeleton class="h-4" /><Skeleton class="h-4" /><Skeleton class="h-4" /></div>
      </div>
    {:else if requests === 0}
      <Empty.Root class="border border-dashed">
        <Empty.Header>
          <Empty.Media variant="icon"><Pulse20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().empty}</Empty.Title>
        </Empty.Header>
      </Empty.Root>
    {:else}
      <ShareChart layout="stacked" {items} valueLabel={count} centerValue={count(requests)} centerLabel={t().requests} ariaLabel={t().chartAria(count(requests))} />
    {/if}
  </Card.Content>
</Card.Root>
