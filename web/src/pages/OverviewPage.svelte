<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { Add, ArrowRight, ArrowRight12, Checkmark, Warning } from '../icons';
  import { connectorLabel, statusKind, statusLabel } from '../api';
  import { translations } from '../i18n.svelte';
  import { connectorDetailPath, pageHref } from '../navigation';
  import PageHead from '../components/PageHead.svelte';
  import ConnectionMap from '../components/ConnectionMap.svelte';
  import InfoTooltip from '../components/InfoTooltip.svelte';
  import RequestTrend from '../components/RequestTrend.svelte';
  import StatCard from '../components/StatCard.svelte';
  import TruncateText from '../components/TruncateText.svelte';
  import TunnelShare from '../components/TunnelShare.svelte';
  import type { DashboardData, PageKey, UsageSeries } from '../types';

  type ConnectionIssue = { id: string; name: string; details: string[]; severe: boolean };
  type Step = { label: string; done: boolean; action: string; view: PageKey; openDetail?: boolean };

  const t = translations({
    title: 'Overview',
    description: 'See the status of your tunnels and public addresses.',
    manageTunnels: 'Tunnels',
    createTunnel: 'Create a tunnel',
    nextDomain: 'Enter the Outlet domain in Domains.',
    nextCreate: 'Create a tunnel.',
    nextInstall: 'Check the tunnel install command and run it on your server.',
    nextConnection: 'Check the tunnel connection status.',
    nextService: 'Register an app address in the tunnel details.',
    nextRoute: 'Add a public address in the tunnel details.',
    stepDomain: 'Check the domain',
    stepDomainAction: 'Change it in Domains',
    stepTunnel: 'Create a tunnel',
    stepTunnelAction: 'Create a new tunnel',
    stepConnect: 'Install and connect the tunnel',
    stepConnectInstall: 'Check the install command',
    stepConnectStatus: 'Check the connection status',
    stepService: 'Register a service',
    stepServiceAction: 'Enter the service name and internal address',
    stepRoute: 'Add a public address',
    stepRouteAction: 'Register a domain',
    completed: 'Completed',
    tunnels: 'Tunnels',
    tunnelsHint: 'Connected tunnels out of all registered tunnels.',
    tunnelsConnected: (count: number) => `${count} connected now`,
    services: 'Services',
    servicesHint: 'Services with an internal address and port.',
    servicesAcross: (count: number) => `Across ${count} ${count === 1 ? 'tunnel' : 'tunnels'}`,
    noTunnelsYet: 'No tunnels yet',
    publicAddresses: 'Public addresses',
    publicAddressesHint: 'Registered public addresses, whether or not they are reachable.',
    domainCount: (count: number) => `Under ${count} ${count === 1 ? 'domain' : 'domains'}`,
    noDomain: 'No domain set',
    attention: 'Needs attention',
    attentionHint: 'Tunnels, services, or public addresses that need review.',
    attentionCount: (count: number) => `${count} ${count === 1 ? 'connection' : 'connections'} to review`,
    allHealthy: 'All connections healthy',
    gettingStarted: 'Getting started',
    glossaryBody: 'A tunnel is the connection from your app server to Outlet. In the tunnel details you can register several app addresses and add public addresses to each service.',
    issuesTitle: 'Connections that need attention',
    showAll: (count: number) => `Show all (${count})`,
    tunnelStatus: (status: string) => `Tunnel ${status.toLowerCase()}`,
    noService: 'No service registered',
    serviceDisabled: 'Service disabled',
    servicePort: (status: string) => `Service port ${status.toLowerCase()}`,
    noRoute: 'No public address',
    routesNeedReview: (count: number) => `${count} public ${count === 1 ? 'address needs' : 'addresses need'} review`,
  }, {
    title: '개요',
    description: '터널과 공개 주소의 현황을 확인할 수 있습니다.',
    manageTunnels: '터널 관리',
    createTunnel: '터널 만들기',
    nextDomain: '도메인 관리에서 Outlet 도메인을 입력하세요.',
    nextCreate: '터널을 만드세요.',
    nextInstall: '터널 설치 명령을 확인하고 서버에서 실행하세요.',
    nextConnection: '터널 연결 상태를 확인하세요.',
    nextService: '터널 상세에서 앱 주소를 등록하세요.',
    nextRoute: '터널 상세에서 공개 주소를 추가하세요.',
    stepDomain: '도메인 확인',
    stepDomainAction: '도메인 관리에서 변경',
    stepTunnel: '터널 만들기',
    stepTunnelAction: '새 터널 만들기',
    stepConnect: '터널 설치 및 연결',
    stepConnectInstall: '설치 명령 확인',
    stepConnectStatus: '연결 상태 확인',
    stepService: '서비스 등록',
    stepServiceAction: '서비스 이름과 내부 주소 입력',
    stepRoute: '공개 주소 추가',
    stepRouteAction: '도메인 등록',
    completed: '완료됨',
    tunnels: '터널',
    tunnelsHint: '등록된 터널 중 연결된 터널의 수입니다.',
    tunnelsConnected: (count: number) => `현재 ${count}개 연결됨`,
    services: '서비스',
    servicesHint: '내부 주소와 포트를 등록한 서비스입니다.',
    servicesAcross: (count: number) => `터널 ${count}개에 등록됨`,
    noTunnelsYet: '아직 터널이 없습니다',
    publicAddresses: '공개 주소',
    publicAddressesHint: '접속 가능 여부와 관계없이 등록된 공개 주소입니다.',
    domainCount: (count: number) => `도메인 ${count}개에 설정됨`,
    noDomain: '도메인 미설정',
    attention: '확인 필요',
    attentionHint: '확인이 필요한 터널, 서비스, 공개 주소입니다.',
    attentionCount: (count: number) => `확인할 연결 ${count}개`,
    allHealthy: '모든 연결이 정상입니다',
    gettingStarted: '시작하기',
    glossaryBody: '터널은 앱 서버에서 Outlet으로 이어지는 연결입니다. 터널 상세에서 여러 앱 주소를 등록하고 각 서비스에 공개 주소를 추가할 수 있습니다.',
    issuesTitle: '확인이 필요한 연결',
    showAll: (count: number) => `더보기 (${count})`,
    tunnelStatus: (status: string) => `터널 ${status}`,
    noService: '서비스 미등록',
    serviceDisabled: '서비스 사용 안 함',
    servicePort: (status: string) => `서비스 포트 ${status}`,
    noRoute: '공개 주소 없음',
    routesNeedReview: (count: number) => `공개 주소 ${count}개 확인 필요`,
  });

  let { data, navigate, onOpenDetail }: { data: DashboardData; navigate: (view: PageKey) => Promise<void>; onOpenDetail: (id: string, search?: string) => void } = $props();
  // 트래픽 카드가 불러온 사용량을 터널별 카드도 함께 씁니다(요청은 한 번).
  let usage = $state<UsageSeries | null>(null);
  let usageError = $state('');
  const connected = $derived(data.connectors.filter((connector) => connector.status === 'connected').length);
  const serviceTunnels = $derived(new Set(data.records.map((record) => record.connector.id)).size);
  const domainCount = $derived(data.outlet.domains?.length ?? (data.outlet.domain ? 1 : 0));
  const next = $derived(!data.outlet.domain
    ? t().nextDomain
    : data.connectors.length === 0
      ? t().nextCreate
      : connected === 0
        ? data.services.length === 0 ? t().nextInstall : t().nextConnection
        : data.services.length === 0
          ? t().nextService
          : data.routes.length === 0
            ? t().nextRoute
            : null);
  const items = $derived<Step[]>([
    { label: t().stepDomain, done: Boolean(data.outlet.domain), action: t().stepDomainAction, view: 'domains' },
    { label: t().stepTunnel, done: data.connectors.length > 0, action: t().stepTunnelAction, view: 'connectors' },
    { label: t().stepConnect, done: connected > 0, action: data.connectors.some((connector) => connector.status === 'draft') ? t().stepConnectInstall : t().stepConnectStatus, view: 'connectors' },
    { label: t().stepService, done: data.services.length > 0, action: t().stepServiceAction, view: 'connectors', openDetail: true },
    { label: t().stepRoute, done: data.routes.length > 0, action: t().stepRouteAction, view: 'connectors', openDetail: true },
  ]);
  const currentStep = $derived(items.find((item) => !item.done));

  function openOnboardingTarget(view: PageKey, openDetail?: boolean): void {
    if (openDetail) {
      const connector = data.connectors.find((item) => item.status === 'connected') ?? data.connectors[0];
      if (connector) {
        onOpenDetail(connector.id);
        return;
      }
    }
    void navigate(view);
  }
  // 문제가 많아도 개요가 길어지지 않도록 일부만 보여주고, 나머지는 터널 관리에서 봅니다.
  const issueLimit = 6;
  const showOnboarding = $derived(Boolean(next) && !(data.outlet.domain && data.routes.length > 0 && data.connectors.some((connector) => connector.status !== 'draft')));
  const connectionIssues = $derived.by((): ConnectionIssue[] => {
    const issues: ConnectionIssue[] = [];
    for (const connector of data.connectors) {
      const record = data.records.find((item) => item.connector.id === connector.id);
      const details: string[] = [];
      let severe = statusKind(connector.status) === 'bad';

      if (connector.status !== 'connected') details.push(t().tunnelStatus(statusLabel(connector.status)));
      if (!record) {
        details.push(t().noService);
      } else {
        if (!record.service.enabled) details.push(t().serviceDisabled);
        else if (record.service.observed_status !== 'connected') {
          details.push(t().servicePort(statusLabel(record.service.observed_status)));
          if (statusKind(record.service.observed_status) === 'bad') severe = true;
        }

        if (!record.routes.length) details.push(t().noRoute);
        else {
          const needsReview = record.routes.filter((route) => !route.enabled || Boolean(route.last_error) || route.applied_revision < route.desired_revision).length;
          if (needsReview) details.push(t().routesNeedReview(needsReview));
        }
      }

      if (details.length) issues.push({ id: connector.id, name: connectorLabel(connector), details, severe });
    }
    // 끊김·오류처럼 심각한 연결을 먼저 보여줍니다.
    return issues.sort((a, b) => Number(b.severe) - Number(a.severe));
  });
</script>

<PageHead title={t().title} />

<div class="grid gap-4">
  <div class="grid grid-cols-2 gap-4 lg:grid-cols-4">
    <StatCard label={t().tunnels} hint={t().tunnelsHint} hintId="stat-tunnels-hint" value={String(data.connectors.length)} sub={data.connectors.length ? t().tunnelsConnected(connected) : t().noTunnelsYet} />
    <StatCard label={t().services} hint={t().servicesHint} hintId="stat-services-hint" value={String(data.services.length)} sub={t().servicesAcross(serviceTunnels)} />
    <StatCard label={t().publicAddresses} hint={t().publicAddressesHint} hintId="stat-addresses-hint" value={String(new Set(data.routes.map((route) => route.hostname)).size)} sub={domainCount ? t().domainCount(domainCount) : t().noDomain} />
    <StatCard label={t().attention} hint={t().attentionHint} hintId="stat-attention-hint" value={String(connectionIssues.length)} sub={connectionIssues.length ? t().attentionCount(connectionIssues.length) : t().allHealthy} />
  </div>

  {#if showOnboarding}
    <Card.Root size="sm" role="region" aria-labelledby="getting-started-heading">
      <Card.Header>
        <Card.Title id="getting-started-heading" class="flex items-center gap-0.5">{t().gettingStarted}<InfoTooltip id="getting-started-hint" label={t().gettingStarted} description={t().glossaryBody} /></Card.Title>
      </Card.Header>
      <Card.Content>
        <ol class="grid gap-1 lg:grid-cols-5">
          {#each items as item, index (index)}
            <li class="flex min-w-0 items-center gap-2" aria-current={item === currentStep ? 'step' : undefined}>
              <span class={['flex size-6 shrink-0 items-center justify-center text-xs font-medium', item === currentStep ? 'bg-foreground text-background' : 'bg-muted text-muted-foreground']} aria-hidden="true">
                {#if item.done}<Checkmark class="size-4" />{:else}{index + 1}{/if}
              </span>
              <span class={['min-w-0', item === currentStep ? 'font-medium' : item.done && 'text-muted-foreground']}>{item.label}{#if item.done}<span class="sr-only"> ({t().completed})</span>{/if}</span>
            </li>
          {/each}
        </ol>
        {#if currentStep}
          <div class="mt-4 flex justify-end">
            <Tooltip.Root>
              <Tooltip.Trigger>
                {#snippet child({ props })}
                  <Button {...props} class="max-sm:w-full" onclick={() => openOnboardingTarget(currentStep.view, currentStep.openDetail)}>{currentStep.label}<ArrowRight aria-hidden="true" /></Button>
                {/snippet}
              </Tooltip.Trigger>
              <Tooltip.Content>{currentStep.action}</Tooltip.Content>
            </Tooltip.Root>
          </div>
        {/if}
      </Card.Content>
    </Card.Root>
  {:else if connectionIssues.length}
    <Card.Root size="sm" aria-labelledby="issues-title">
      <Card.Header>
        <Card.Title id="issues-title" class="flex items-center gap-2">{t().issuesTitle}<Badge variant="secondary">{connectionIssues.length}</Badge></Card.Title>
        {#if connectionIssues.length > issueLimit}
          <Card.Action>
            <Button variant="outline" size="sm" href={pageHref('connectors')} onclick={(event) => { event.preventDefault(); void navigate('connectors'); }}>{t().showAll(connectionIssues.length)}<ArrowRight12 aria-hidden="true" /></Button>
          </Card.Action>
        {/if}
      </Card.Header>
      <Card.Content>
        <Item.Group class="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          {#each connectionIssues.slice(0, issueLimit) as issue (issue.id)}
            <Item.Root variant="outline" size="sm">
              {#snippet child({ props })}
                <a {...props} href={connectorDetailPath(issue.id)} onclick={(event) => { event.preventDefault(); onOpenDetail(issue.id); }}>
                  <Item.Media><Warning class={issue.severe ? 'text-destructive' : 'text-amber-600 dark:text-amber-400'} aria-hidden="true" /></Item.Media>
                  <Item.Content class="min-w-0">
                    <Item.Title class="w-full text-sm"><TruncateText text={issue.name} /></Item.Title>
                    <Item.Description class="w-full"><TruncateText text={issue.details.join(' · ')} /></Item.Description>
                  </Item.Content>
                </a>
              {/snippet}
            </Item.Root>
          {/each}
        </Item.Group>
      </Card.Content>
    </Card.Root>
  {/if}

  <div class="grid grid-cols-1 gap-4 lg:grid-cols-3">
    <RequestTrend bind:series={usage} bind:error={usageError} class="lg:col-span-2" />
    <TunnelShare series={usage} error={usageError} labels={Object.fromEntries(data.connectors.map((connector) => [connector.id, connectorLabel(connector)]))} {onOpenDetail} />
  </div>

  <ConnectionMap {data} {navigate} {onOpenDetail} />
</div>
