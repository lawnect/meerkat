<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import { Cube, Globe, Open, Open12, PlugConnected } from '../icons';
  import type { Route, ServiceRecord } from '../types';
  import { statusLabel } from '../api';
  import { translations } from '../i18n.svelte';
  import Status from './Status.svelte';
  import TruncateText from './TruncateText.svelte';

  let { tunnelName, records }: { tunnelName: string; records: ServiceRecord[] } = $props();

  const t = translations({
    graphLabel: (name: string) => `Request path diagram for tunnel ${name}`,
    addresses: 'Public addresses',
    tunnel: 'Tunnel',
    services: 'Services',
    noAddresses: 'No public addresses',
    openAddress: (hostname: string, service: string) => `Open public address ${hostname}, service ${service}`,
    routeLive: 'Live',
    routeWaiting: 'Waiting',
    routeError: 'Error',
    routeOff: 'Disabled',
  }, {
    graphLabel: (name: string) => `${name} 터널의 요청 경로 연결도`,
    addresses: '공개 주소',
    tunnel: '터널',
    services: '서비스',
    noAddresses: '공개 주소 없음',
    openAddress: (hostname: string, service: string) => `${hostname} 공개 주소 열기, 서비스 ${service}`,
    routeLive: '공개 중',
    routeWaiting: '대기 중',
    routeError: '오류',
    routeOff: '사용 안 함',
  });

  type RouteKind = 'live' | 'waiting' | 'error' | 'off';
  const kindStatus = { live: 'connected', waiting: 'connecting', error: 'error', off: 'disabled' } as const;

  const paths = $derived(records.flatMap((record) => record.routes.map((route) => ({ record, route }))));
  const graphHeight = $derived(Math.max(190, Math.max(paths.length, records.length) * 76 + 64));
  const centerY = $derived(graphHeight / 2);
  const tunnelStatus = $derived(records[0]?.connector.status);

  function nodeY(index: number, count: number): number {
    if (count < 2) return graphHeight / 2;
    return 65 + (index * (graphHeight - 130)) / (count - 1);
  }

  // 왼쪽부터 연결 서비스(내부) → 터널 → 공개 주소(외부) 순서로 둡니다.
  function servicePath(index: number): string {
    const y = nodeY(index, records.length);
    return `M 270 ${y} C 340 ${y}, 340 ${centerY}, 410 ${centerY}`;
  }

  function addressPath(index: number): string {
    const y = nodeY(index, paths.length);
    return `M 610 ${centerY} C 650 ${centerY}, 650 ${y}, 690 ${y}`;
  }

  function routeKind(record: ServiceRecord, route: Route): RouteKind {
    if (!route.enabled || !record.service.enabled) return 'off';
    if (route.last_error) return 'error';
    if (route.applied_revision < route.desired_revision) return 'waiting';
    if (record.connector.status !== 'connected' || record.service.observed_status !== 'connected') return 'waiting';
    return 'live';
  }

  function routeLabel(kind: RouteKind): string {
    return kind === 'live' ? t().routeLive : kind === 'waiting' ? t().routeWaiting : kind === 'error' ? t().routeError : t().routeOff;
  }
</script>

<div class="overflow-x-auto border bg-muted/30 outline-none focus-visible:ring-3 focus-visible:ring-ring/50" role="region" aria-label={t().graphLabel(tunnelName)} tabindex="0">
  <div class="relative mx-auto w-full max-w-[1080px] min-w-[760px]" style:height={`${graphHeight}px`}>
    <svg class="pointer-events-none absolute inset-0 size-full overflow-visible fill-none stroke-muted-foreground/40" viewBox={`0 0 1000 ${graphHeight}`} preserveAspectRatio="none" aria-hidden="true">
      {#each records as record, index (record.service.id)}
        <path d={servicePath(index)} stroke-width="2" vector-effect="non-scaling-stroke" stroke-dasharray={record.service.enabled ? undefined : '5 5'} />
      {/each}
      {#each paths as path, index (path.route.id)}
        <path d={addressPath(index)} stroke-width="2" vector-effect="non-scaling-stroke" stroke-dasharray={path.route.enabled ? undefined : '5 5'} />
      {/each}
    </svg>

    <div class="absolute top-4 left-[3%] z-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><Cube class="size-4" aria-hidden="true" />{t().services}</div>
    <div class="absolute top-4 left-[41%] z-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><PlugConnected class="size-4" aria-hidden="true" />{t().tunnel}</div>
    <div class="absolute top-4 left-[69%] z-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><Globe class="size-4" aria-hidden="true" />{t().addresses}</div>

    {#if paths.length}
      {#each paths as path, index (path.route.id)}
        {@const kind = routeKind(path.record, path.route)}
        <div class={['absolute left-[69%] z-1 h-15 w-[28%] -translate-y-1/2', !path.route.enabled && 'opacity-55']} style:top={`${nodeY(index, paths.length)}px`}>
          <Item.Root variant="outline" size="xs" class="h-full flex-nowrap bg-card">
            {#snippet child({ props })}
              <a {...props} href={`https://${path.route.hostname}`} target="_blank" rel="noopener noreferrer" aria-label={t().openAddress(path.route.hostname, path.record.service.name)}>
                <Item.Media variant="icon"><Globe class="text-muted-foreground" aria-hidden="true" /></Item.Media>
                <Item.Content class="min-w-0">
                  <Item.Title class="max-w-full min-w-0 text-sm"><TruncateText text={path.route.hostname} /></Item.Title>
                  <Item.Description>
                    {#if kind === 'off'}
                      <Badge variant="outline" class="text-muted-foreground select-none">{routeLabel(kind)}</Badge>
                    {:else}
                      <Status label={routeLabel(kind)} value={kindStatus[kind]} />
                    {/if}
                  </Item.Description>
                </Item.Content>
                <Open12 class="size-3 shrink-0 text-muted-foreground" aria-hidden="true" />
              </a>
            {/snippet}
          </Item.Root>
        </div>
      {/each}
    {:else}
      <div class="absolute left-[69%] h-15 w-[28%] -translate-y-1/2" style:top={`${centerY}px`}>
        <Item.Root variant="outline" size="xs" class="h-full flex-nowrap border-dashed bg-card">
          <Item.Media variant="icon"><Globe class="text-muted-foreground" aria-hidden="true" /></Item.Media>
          <Item.Content><Item.Description>{t().noAddresses}</Item.Description></Item.Content>
        </Item.Root>
      </div>
    {/if}

    <div class="absolute left-[41%] z-1 h-15 w-[20%] -translate-y-1/2" style:top={`${centerY}px`}>
      <Item.Root variant="outline" size="xs" class="h-full flex-nowrap border-foreground/40 bg-card">
        <Item.Media variant="icon"><PlugConnected class="text-muted-foreground" aria-hidden="true" /></Item.Media>
        <Item.Content class="min-w-0">
          <Item.Title class="max-w-full min-w-0 text-sm"><TruncateText text={tunnelName} /></Item.Title>
          {#if tunnelStatus}<Item.Description><Status label={statusLabel(tunnelStatus)} value={tunnelStatus} /></Item.Description>{/if}
        </Item.Content>
      </Item.Root>
    </div>

    {#each records as record, index (record.service.id)}
      <!-- 터널이 연결되지 않았으면 서비스도 터널 상태로 보여줍니다(도메인 그래프와 같음). -->
      {@const state = record.connector.status === 'connected' ? record.service.observed_status : record.connector.status}
      <div class={['absolute left-[3%] z-1 h-15 w-[24%] -translate-y-1/2', !record.service.enabled && 'opacity-55']} style:top={`${nodeY(index, records.length)}px`}>
        <Item.Root variant="outline" size="xs" class="h-full flex-nowrap bg-card">
          <Item.Media variant="icon"><Cube class="text-muted-foreground" aria-hidden="true" /></Item.Media>
          <Item.Content class="min-w-0">
            <Item.Title class="max-w-full min-w-0 text-sm"><TruncateText text={record.service.name} /></Item.Title>
            <Item.Description class="flex min-w-0 items-center gap-1.5">
              {#if record.service.enabled}
                <Status label={statusLabel(state)} value={state} />
              {:else}
                <Badge variant="outline" class="text-muted-foreground select-none">{t().routeOff}</Badge>
              {/if}
              <TruncateText text={`${record.service.target.host}:${record.service.target.port}`} class="font-mono" />
            </Item.Description>
          </Item.Content>
        </Item.Root>
      </div>
    {/each}
  </div>
</div>
