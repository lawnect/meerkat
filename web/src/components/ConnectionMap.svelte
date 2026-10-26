<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import { Kbd } from '$lib/components/ui/kbd/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { connectorLabel, statusLabel } from '../api';
  import { translations } from '../i18n.svelte';
  import { ArrowRight, ArrowRight12, ChevronRight, Cube, Globe, PlugConnected, PlugConnected20, type IconComponent } from '../icons';
  import { connectorDetailPath, pageHref } from '../navigation';
  import type { DashboardData, PageKey, Route as RouteRecord } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';
  import Status from './Status.svelte';
  import TruncateText from './TruncateText.svelte';

  const t = translations({
    title: 'Service connection path',
    description: 'The path from a public address through a tunnel to a service.',
    manageTunnels: 'Tunnels',
    publicAddress: 'Public address',
    tunnel: 'Tunnel',
    service: 'Service',
    noRoute: 'No public address set',
    routeUnset: 'Not set',
    viewTunnel: 'View tunnel',
    disabled: 'Disabled',
    error: 'Error',
    applying: 'Applying',
    applied: 'Applied',
    showAll: (count: number) => `Show all (${count})`,
    empty: 'No connections to show yet.',
    firstService: 'Register your first service',
    firstTunnel: 'Create your first tunnel',
  }, {
    title: '서비스 연결 경로',
    description: '공개 주소에서 터널을 거쳐 서비스로 이어지는 경로입니다.',
    manageTunnels: '터널 관리',
    publicAddress: '공개 주소',
    tunnel: '터널',
    service: '서비스',
    noRoute: '공개 주소 미설정',
    routeUnset: '미설정',
    viewTunnel: '터널 보기',
    disabled: '사용 안 함',
    error: '오류',
    applying: '반영 중',
    applied: '반영됨',
    showAll: (count: number) => `더보기 (${count})`,
    empty: '아직 표시할 연결이 없습니다.',
    firstService: '첫 서비스 등록',
    firstTunnel: '첫 터널 만들기',
  });

  let { data, navigate, onOpenDetail }: { data: DashboardData; navigate: (view: PageKey) => Promise<void>; onOpenDetail: (id: string) => void } = $props();

  const visible = $derived(data.records.slice(0, 4));
  const hasConnectedTunnel = $derived(data.connectors.some((connector) => connector.status === 'connected'));

  function openFirstTunnel(): void {
    const connector = data.connectors.find((item) => item.status === 'connected') ?? data.connectors[0];
    if (connector) onOpenDetail(connector.id);
    else void navigate('connectors');
  }

  // 같은 탭에서 터널 상세로 이동하고, 새 탭 열기 같은 기본 동작은 그대로 둡니다.
  function openTunnel(event: MouseEvent, id: string): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpenDetail(id);
  }

  // 같은 탭에서 전체 목록으로 이동하고, 새 탭 열기 같은 기본 동작은 그대로 둡니다.
  function openAll(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    void navigate('connectors');
  }

  function routeState(route?: RouteRecord): { label: string; value: string } {
    if (!route) return { label: t().routeUnset, value: 'unknown' };
    if (!route.enabled) return { label: t().disabled, value: 'disabled' };
    if (route.last_error) return { label: t().error, value: 'error' };
    if (route.applied_revision < route.desired_revision) return { label: t().applying, value: 'pending' };
    return { label: t().applied, value: 'connected' };
  }
</script>

{#snippet nodeIcon(Icon: IconComponent, label: string)}
  <Tooltip.Root>
    <Tooltip.Trigger>
      {#snippet child({ props })}
        <span {...props} class="shrink-0 text-muted-foreground"><Icon class="size-4" aria-hidden="true" /></span>
      {/snippet}
    </Tooltip.Trigger>
    <Tooltip.Content>{label}</Tooltip.Content>
  </Tooltip.Root>
{/snippet}

{#snippet flowArrow()}
  <Item.Media variant="icon" class="hidden text-muted-foreground md:flex"><ArrowRight aria-hidden="true" /></Item.Media>
{/snippet}

{#if visible.length}
  <Card.Root size="sm" role="region" aria-labelledby="connection-heading">
    <Card.Header>
      <Card.Title id="connection-heading" class="flex items-center gap-1">{t().title}<InfoTooltip id="connection-hint" label={t().title} description={t().description} /></Card.Title>
    </Card.Header>
    <Card.Content>
      <Item.Group>
        {#each visible as record (record.service.id)}
          {@const route = record.routes[0]}
          {@const routeInfo = routeState(route)}
          <Item.Root variant="outline" size="sm" role="listitem" class="relative flex-col items-stretch pe-9 md:flex-row md:flex-nowrap md:items-center md:pe-3 [&_[data-slot=badge]]:cursor-pointer">
            {#snippet child({ props })}
              <a {...props} href={connectorDetailPath(record.connector.id)} onclick={(event) => openTunnel(event, record.connector.id)}>
                <Item.Content class="min-w-0">
                  <Item.Title class="w-full min-w-0 text-sm">
                    {@render nodeIcon(Cube, t().service)}
                    <span class="sr-only">{t().service}: </span>
                    <TruncateText text={record.service.name} />
                  </Item.Title>
                  <Item.Description class="line-clamp-none flex flex-wrap items-center gap-2">
                    <Status label={record.service.enabled ? statusLabel(record.service.observed_status) : t().disabled} value={record.service.enabled ? record.service.observed_status : 'disabled'} />
                    <Kbd class="max-w-full truncate font-mono">{record.service.target.host}:{record.service.target.port}</Kbd>
                  </Item.Description>
                </Item.Content>
                {@render flowArrow()}

                <Item.Content class="min-w-0">
                  <Item.Title class="w-full min-w-0 text-sm">
                    {@render nodeIcon(PlugConnected, t().tunnel)}
                    <span class="sr-only">{t().tunnel}: </span>
                    <TruncateText text={connectorLabel(record.connector)} />
                  </Item.Title>
                  <Item.Description class="line-clamp-none flex flex-wrap items-center gap-2"><Status label={statusLabel(record.connector.status)} value={record.connector.status} /></Item.Description>
                </Item.Content>

                {@render flowArrow()}

                <Item.Content class="min-w-0">
                  <Item.Title class="w-full min-w-0 text-sm">
                    {@render nodeIcon(Globe, t().publicAddress)}
                    <span class="sr-only">{t().publicAddress}: </span>
                    {#if route}
                      <TruncateText text={route.hostname} />
                    {:else}
                      <span class="truncate text-muted-foreground">{t().noRoute}</span>
                    {/if}
                    {#if record.routes.length > 1}
                      <Tooltip.Root>
                        <Tooltip.Trigger>
                          {#snippet child({ props })}
                            <Badge {...props} variant="secondary" class="cursor-default select-none">+{record.routes.length - 1}</Badge>
                          {/snippet}
                        </Tooltip.Trigger>
                        <Tooltip.Content>{record.routes.slice(1).map((item) => item.hostname).join(', ')}</Tooltip.Content>
                      </Tooltip.Root>
                    {/if}
                  </Item.Title>
                  <Item.Description class="line-clamp-none flex flex-wrap items-center gap-2"><Status label={routeInfo.label} value={routeInfo.value} /></Item.Description>
                </Item.Content>


                <Item.Actions class="absolute end-3 top-1/2 -translate-y-1/2 text-muted-foreground md:static md:translate-y-0"><ChevronRight aria-hidden="true" /></Item.Actions>
                <span class="sr-only">{t().viewTunnel}</span>
              </a>
            {/snippet}
          </Item.Root>
        {/each}
      </Item.Group>
    </Card.Content>
    {#if data.records.length > visible.length}
      <Card.Footer class="justify-end">
        <Button variant="outline" size="sm" href={pageHref('connectors')} onclick={openAll}>{t().showAll(data.records.length - visible.length)}<ArrowRight12 aria-hidden="true" /></Button>
      </Card.Footer>
    {/if}
  </Card.Root>
{:else}
  <Empty.Root class="border border-dashed">
    <Empty.Header>
      <Empty.Media variant="icon"><PlugConnected20 class="size-5" aria-hidden="true" /></Empty.Media>
      <Empty.Title>{t().empty}</Empty.Title>
    </Empty.Header>
    <Empty.Content>
      <Button variant="outline" onclick={openFirstTunnel}>{hasConnectedTunnel ? t().firstService : t().firstTunnel}<ArrowRight aria-hidden="true" /></Button>
    </Empty.Content>
  </Empty.Root>
{/if}
