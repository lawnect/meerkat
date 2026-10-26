<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import { Cube, Globe, Open12 } from '../icons';
  import type { Route, ServiceRecord } from '../types';
  import { connectorLabel, statusLabel } from '../api';
  import { translations } from '../i18n.svelte';
  import Status from './Status.svelte';
  import TruncateText from './TruncateText.svelte';

  // 한 도메인의 공개 주소가 어느 터널의 어느 서비스로 전달되는지 모아 보여 줍니다.
  // 장애 조치 주소는 지금 요청을 받는 대상만 실선, 대기 중인 대상은 점선으로 그립니다.
  let { domain, records }: { domain: string; records: ServiceRecord[] } = $props();

  const t = translations({
    graphLabel: (domain: string) => `Routing diagram for ${domain}`,
    addresses: 'Public addresses',
    targets: 'Targets',
    shared: (mode: string, n: number) => `${mode === 'balance' ? 'Load balancing' : 'Failover'} · ${n}`,
    openAddress: (hostname: string) => `Open ${hostname} in a new tab`,
    live: 'Live',
    waiting: 'Waiting',
    off: 'Disabled',
  }, {
    graphLabel: (domain: string) => `${domain} 라우팅 연결도`,
    addresses: '공개 주소',
    targets: '대상',
    shared: (mode: string, n: number) => `${mode === 'balance' ? '부하 분산' : '장애 조치'} · ${n}`,
    openAddress: (hostname: string) => `${hostname} 새 탭에서 열기`,
    live: '공개 중',
    waiting: '대기 중',
    off: '비활성화됨',
  });

  type Target = { record: ServiceRecord; route: Route };
  type Address = { hostname: string; targets: Target[]; route: Route };

  const usable = (target: Target): boolean => target.route.enabled && target.record.service.enabled && !target.route.last_error
    && target.record.connector.status === 'connected' && target.record.service.observed_status === 'connected';

  const addresses = $derived.by<Address[]>(() => {
    const byHost = new Map<string, Target[]>();
    for (const record of records) {
      for (const route of record.routes) {
        if (!route.hostname.endsWith(`.${domain}`)) continue;
        byHost.set(route.hostname, [...(byHost.get(route.hostname) ?? []), { record, route }]);
      }
    }
    return [...byHost.entries()]
      .sort(([a], [b]) => a.localeCompare(b))
      .map(([hostname, targets]) => {
        const ordered = targets.sort((a, b) => a.route.priority - b.route.priority);
        return { hostname, targets: ordered, route: ordered[0].route };
      });
  });

  // 대상은 처음 나타난 주소 순서대로 두어 선이 덜 엇갈리게 합니다.
  const targets = $derived.by<ServiceRecord[]>(() => {
    const seen = new Map<string, ServiceRecord>();
    for (const address of addresses) for (const target of address.targets) if (!seen.has(target.record.service.id)) seen.set(target.record.service.id, target.record);
    return [...seen.values()];
  });

  // 지금 요청을 받는 대상: 장애 조치는 순서상 첫 정상 대상, 부하 분산은 정상 대상 전부.
  function active(address: Address, target: Target): boolean {
    if (!usable(target)) return false;
    if (address.route.address_mode === 'balance') return true;
    return address.targets.find(usable) === target;
  }

  function addressState(address: Address): 'live' | 'waiting' | 'off' {
    if (address.targets.every((target) => !target.route.enabled || !target.record.service.enabled)) return 'off';
    return address.targets.some(usable) ? 'live' : 'waiting';
  }

  const graphHeight = $derived(Math.max(190, Math.max(addresses.length, targets.length) * 76 + 64));

  function nodeY(index: number, count: number): number {
    if (count < 2) return graphHeight / 2;
    return 65 + (index * (graphHeight - 130)) / (count - 1);
  }

  function edge(from: number, to: number): string {
    const y1 = nodeY(from, addresses.length);
    const y2 = nodeY(to, targets.length);
    return `M 370 ${y1} C 485 ${y1}, 485 ${y2}, 600 ${y2}`;
  }

  const edges = $derived(addresses.flatMap((address, from) => address.targets.map((target) => ({
    key: `${address.hostname}:${target.record.service.id}`,
    from,
    to: targets.findIndex((record) => record.service.id === target.record.service.id),
    active: active(address, target),
  }))));

  function targetState(record: ServiceRecord): string {
    if (!record.service.enabled) return 'disabled';
    return record.connector.status === 'connected' ? record.service.observed_status : record.connector.status;
  }
</script>

<div class="overflow-x-auto border bg-muted/30 outline-none focus-visible:ring-3 focus-visible:ring-ring/50" role="region" aria-label={t().graphLabel(domain)} tabindex="0">
  <div class="relative mx-auto w-full max-w-[1080px] min-w-[760px]" style:height={`${graphHeight}px`}>
    <svg class="pointer-events-none absolute inset-0 size-full overflow-visible fill-none stroke-muted-foreground/40" viewBox={`0 0 1000 ${graphHeight}`} preserveAspectRatio="none" aria-hidden="true">
      {#each edges as line (line.key)}
        <path d={edge(line.from, line.to)} stroke-width="2" vector-effect="non-scaling-stroke" stroke-dasharray={line.active ? undefined : '5 5'} class={line.active ? 'stroke-primary/60' : undefined} />
      {/each}
    </svg>

    <div class="absolute top-4 left-[3%] z-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><Globe class="size-4" aria-hidden="true" />{t().addresses}</div>
    <div class="absolute top-4 left-[60%] z-1 flex items-center gap-1.5 text-xs font-medium text-muted-foreground"><Cube class="size-4" aria-hidden="true" />{t().targets}</div>

    {#each addresses as address, index (address.hostname)}
      {@const state = addressState(address)}
      <div class={['absolute left-[3%] z-1 h-15 w-[34%] -translate-y-1/2', state === 'off' && 'opacity-55']} style:top={`${nodeY(index, addresses.length)}px`}>
        <Item.Root variant="outline" size="xs" class="h-full flex-nowrap bg-card">
          {#snippet child({ props })}
            <a {...props} href={`https://${address.hostname}`} target="_blank" rel="noopener noreferrer" aria-label={t().openAddress(address.hostname)}>
              <Item.Media variant="icon"><Globe class="text-muted-foreground" aria-hidden="true" /></Item.Media>
              <Item.Content class="min-w-0">
                <Item.Title class="max-w-full min-w-0 text-sm"><TruncateText text={address.hostname} /></Item.Title>
                <Item.Description class="flex items-center gap-1.5">
                  {#if state === 'off'}
                    <Badge variant="outline" class="text-muted-foreground select-none">{t().off}</Badge>
                  {:else}
                    <Status label={state === 'live' ? t().live : t().waiting} value={state === 'live' ? 'connected' : 'connecting'} />
                  {/if}
                  {#if address.targets.length > 1}<Badge variant="outline" class="select-none">{t().shared(address.route.address_mode, address.targets.length)}</Badge>{/if}
                </Item.Description>
              </Item.Content>
              <Open12 class="size-3 shrink-0 text-muted-foreground" aria-hidden="true" />
            </a>
          {/snippet}
        </Item.Root>
      </div>
    {/each}

    {#each targets as record, index (record.service.id)}
      {@const state = targetState(record)}
      <div class={['absolute left-[60%] z-1 h-15 w-[37%] -translate-y-1/2', !record.service.enabled && 'opacity-55']} style:top={`${nodeY(index, targets.length)}px`}>
        <Item.Root variant="outline" size="xs" class="h-full flex-nowrap bg-card">
          <Item.Media variant="icon"><Cube class="text-muted-foreground" aria-hidden="true" /></Item.Media>
          <Item.Content class="min-w-0">
            <Item.Title class="max-w-full min-w-0 text-sm"><TruncateText text={`${connectorLabel(record.connector)} / ${record.service.name}`} /></Item.Title>
            <Item.Description class="flex min-w-0 items-center gap-1.5">
              <Status label={statusLabel(state)} value={state} />
              <TruncateText text={`${record.service.target.host}:${record.service.target.port}`} class="font-mono" />
            </Item.Description>
          </Item.Content>
        </Item.Root>
      </div>
    {/each}
  </div>
</div>
