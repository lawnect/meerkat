<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { ArrowSync, BranchRequestClosed20, Warning } from '../icons';
  import { onMount } from 'svelte';
  import { api, errorMessage } from '../api';
  import InfoTooltip from '../components/InfoTooltip.svelte';
  import PageHead from '../components/PageHead.svelte';
  import StatCard from '../components/StatCard.svelte';
  import TruncateText from '../components/TruncateText.svelte';
  import { intlLocale, translations } from '../i18n.svelte';
  import type { TraceSnapshot } from '../types';

  const t = translations({
    title: 'Path diagnostics',
    refresh: 'Refresh',
    refreshing: 'Refreshing…',
    loading: 'Loading diagnostics…',
    loadFailed: (message: string) => `Couldn't load request details: ${message}`,
    region: 'Region',
    regionTip: 'The region is the value you set manually in outlet.json.',
    notSet: 'Not set',
    outletHost: 'Outlet host',
    unavailable: 'Unavailable',
    observedAt: 'Observed at',
    pathTitle: 'How this request reached the Outlet',
    pathTip: 'When a CF-Ray header is present, its suffix is shown as the Cloudflare colo. Request headers can also be set on direct connections, so don\'t treat it as proof that the request came through a proxy. Forwarded addresses and location details are for diagnostics only and are never used for authentication.',
    client: 'Client',
    cloudflare: 'Cloudflare',
    outlet: 'Outlet',
    detected: 'Detected',
    notDetected: 'Not detected',
    none: 'None',
    colo: 'Colo',
    ray: 'Ray',
    country: 'Country',
    forwardedFor: 'Forwarded address',
    remoteAddress: 'Remote address',
    host: 'Host',
    protocol: 'Protocol',
    waiting: 'Waiting for diagnostics',
  }, {
    title: '경로 진단',
    refresh: '새로 고침',
    refreshing: '새로 고치는 중…',
    loading: '진단 정보를 불러오는 중…',
    loadFailed: (message: string) => `요청 정보를 불러오지 못했습니다: ${message}`,
    region: '지역',
    regionTip: '지역은 outlet.json에 직접 설정한 값입니다.',
    notSet: '설정되지 않음',
    outletHost: 'Outlet 호스트',
    unavailable: '확인할 수 없음',
    observedAt: '측정 시각',
    pathTitle: '이 요청이 Outlet에 도착한 경로',
    pathTip: 'CF-Ray 헤더가 있으면 그 값의 접미사로 Cloudflare colo를 표시합니다. 직접 연결에서도 요청 헤더를 넣을 수 있으므로 프록시 경유의 단독 증거로 쓰지 마세요. 전달 주소와 위치 정보는 진단용이며 인증 판단에 사용하지 않습니다.',
    client: '클라이언트',
    cloudflare: 'Cloudflare',
    outlet: 'Outlet',
    detected: '감지됨',
    notDetected: '감지되지 않음',
    none: '없음',
    colo: 'Colo',
    ray: 'Ray',
    country: '국가',
    forwardedFor: '전달된 주소',
    remoteAddress: '직접 연결한 주소',
    host: '호스트',
    protocol: '프로토콜',
    waiting: '진단 정보를 기다리는 중입니다',
  });

  let snapshot = $state<TraceSnapshot | null>(null);
  let error = $state('');
  let loading = $state(false);

  const observed = $derived(snapshot ? new Date(snapshot.observed_at) : null);
  const cloudflareDetected = $derived(Boolean(snapshot?.request.cloudflare_ray || snapshot?.request.cloudflare_colo || snapshot?.request.cloudflare_country));

  async function refresh() {
    loading = true;
    error = '';
    try {
      snapshot = await api<TraceSnapshot>('/api/v1/diagnostics/trace');
    } catch (cause) {
      error = t().loadFailed(errorMessage(cause));
    } finally {
      loading = false;
    }
  }

  onMount(refresh);
</script>

{#snippet kv(label: string, value?: string)}
  <div class="flex gap-4">
    <dt class="w-36 shrink-0 text-muted-foreground">{label}</dt>
    <dd class="min-w-0 font-mono wrap-anywhere">{#if value}{value}{:else}<span class="font-sans text-muted-foreground">{t().none}</span>{/if}</dd>
  </div>
{/snippet}

{#snippet refreshAction()}
  <Button variant="outline" onclick={refresh} disabled={loading}><ArrowSync class={loading ? 'animate-spin' : ''} aria-hidden="true" />{loading ? t().refreshing : t().refresh}</Button>
{/snippet}

<PageHead title={t().title} action={refreshAction} />

<div class="grid min-w-0 gap-4">
  {#if error}
    <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="wrap-anywhere">{error}</Alert.Description></Alert.Root>
  {/if}

  {#if snapshot && observed}
    <div class="grid gap-4 lg:grid-cols-3">
      <StatCard label={t().region} hint={t().regionTip} hintId="diag-region-tip">
        <TruncateText text={snapshot.outlet.region || t().notSet} class={snapshot.outlet.region ? 'text-2xl font-semibold' : 'text-2xl font-semibold text-muted-foreground'} />
      </StatCard>
      <StatCard label={t().outletHost}>
        <TruncateText text={snapshot.outlet.hostname || t().unavailable} class={snapshot.outlet.hostname ? 'text-2xl font-semibold' : 'text-2xl font-semibold text-muted-foreground'} />
      </StatCard>
      <StatCard label={t().observedAt} value={observed.toLocaleTimeString(intlLocale())} sub={observed.toLocaleDateString(intlLocale())} />
    </div>

    <Card.Root size="sm" role="region" aria-labelledby="diag-path-title">
      <Card.Header>
        <Card.Title class="flex items-center gap-1">
          <h2 id="diag-path-title">{t().pathTitle}</h2>
          <InfoTooltip id="diag-path-tip" label={t().pathTitle} description={t().pathTip} />
        </Card.Title>
      </Card.Header>
      <Card.Content>
        <Item.Group>
          <Item.Root variant="outline" size="sm" role="listitem">
            <Item.Content class="gap-2">
              <Item.Title class="text-sm">{t().client}</Item.Title>
              <dl class="grid gap-1">{@render kv(t().forwardedFor, snapshot.request.forwarded_for)}</dl>
            </Item.Content>
          </Item.Root>
          <Item.Root variant="outline" size="sm" role="listitem">
            <Item.Content class="gap-2">
              <Item.Title class="text-sm">{t().cloudflare}</Item.Title>
              {#if cloudflareDetected}
                <dl class="grid gap-1">
                  {@render kv(t().colo, snapshot.request.cloudflare_colo)}
                  {@render kv(t().ray, snapshot.request.cloudflare_ray)}
                  {@render kv(t().country, snapshot.request.cloudflare_country)}
                </dl>
              {/if}
            </Item.Content>
            <Item.Actions><Badge variant={cloudflareDetected ? 'secondary' : 'outline'} class="cursor-default select-none">{cloudflareDetected ? t().detected : t().notDetected}</Badge></Item.Actions>
          </Item.Root>
          <Item.Root variant="outline" size="sm" role="listitem">
            <Item.Content class="gap-2">
              <Item.Title class="text-sm">{t().outlet}</Item.Title>
              <dl class="grid gap-1">
                {@render kv(t().remoteAddress, snapshot.request.remote_address)}
                {@render kv(t().host, snapshot.request.forwarded_host || snapshot.request.host)}
                {@render kv(t().protocol, snapshot.request.forwarded_proto || snapshot.request.protocol)}
              </dl>
            </Item.Content>
          </Item.Root>
        </Item.Group>
      </Card.Content>
    </Card.Root>
  {:else if loading && !error}
    <div class="grid gap-4" role="status" aria-busy="true">
      <span class="sr-only">{t().loading}</span>
      <div class="grid gap-4 lg:grid-cols-3">
        <Skeleton class="h-24 w-full" />
        <Skeleton class="h-24 w-full" />
        <Skeleton class="h-24 w-full" />
      </div>
      <Skeleton class="h-72 w-full" />
    </div>
  {:else if !error}
    <Empty.Root class="border border-dashed">
      <Empty.Header>
        <Empty.Media variant="icon"><BranchRequestClosed20 class="size-5" aria-hidden="true" /></Empty.Media>
        <Empty.Title>{t().waiting}</Empty.Title>
      </Empty.Header>
      <Empty.Content>
        <Button onclick={refresh}><ArrowSync aria-hidden="true" />{t().refresh}</Button>
      </Empty.Content>
    </Empty.Root>
  {/if}
</div>
