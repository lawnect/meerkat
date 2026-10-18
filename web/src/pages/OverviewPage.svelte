<script lang="ts">
  import { statusLabel } from '../api';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import Status from '../components/Status.svelte';
  import ConnectionMap from '../components/ConnectionMap.svelte';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import LinkButton from '../components/ui/LinkButton.svelte';
  import type { DashboardData, PageKey } from '../types';

  let { data, navigate }: { data: DashboardData; navigate: (view: PageKey) => Promise<void> } = $props();
  const connected = $derived(data.connectors.filter((connector) => connector.status === 'connected').length);
  const next = $derived(!data.outlet.domain
    ? '설정에서 Outlet 도메인을 입력하세요.'
    : data.connectors.length === 0
      ? 'Connector를 하나 만드세요.'
      : data.services.length === 0
        ? 'Connector에 연결할 내부 앱을 등록하세요.'
        : data.routes.length === 0
          ? '내부 앱에 공개 주소를 추가하세요.'
          : connected === 0
            ? 'Connector를 실행하고 등록 토큰으로 연결하세요.'
            : null);
  const items = $derived([
    ['도메인 확인', Boolean(data.outlet.domain), '설정에서 변경', 'settings'],
    ['Connector 만들기', data.connectors.length > 0, 'Connector 등록', 'connectors'],
    ['내부 앱 등록', data.services.length > 0, '앱 이름과 내부 주소 입력', 'connectors'],
    ['공개 주소 추가', data.routes.length > 0, '도메인 등록', 'connectors'],
    ['Connector 연결', connected > 0, '등록 토큰 발급', 'connectors'],
  ] as [string, boolean, string, PageKey][]);
  const showOnboarding = $derived(Boolean(next) && !(data.outlet.domain && data.routes.length > 0 && data.connectors.some((connector) => connector.status !== 'draft')));

  function lastResponse(value?: string): string {
    if (!value) return '아직 없음';
    const date = new Date(value);
    return Number.isNaN(date.getTime()) ? '확인할 수 없음' : date.toLocaleString('ko-KR');
  }
</script>

<PageHead kicker="MEERKAT OUTLET" title="오늘의 운영 상태" description={next ?? 'Connector와 공개 주소의 현황을 확인할 수 있습니다.'}>
  {#snippet action()}
    <Button onclick={() => navigate(data.connectors.length ? 'routes' : 'connectors')}>{data.connectors.length ? '공개 서비스 보기' : 'Connector 시작'}</Button>
  {/snippet}
</PageHead>

<div class="dashboard-grid summary-grid">
  <Card class="summary-card">
    <div class="stat-label">Connector</div>
    <div class="connector-stats">
      <div><div class="stat-value">{data.connectors.length}</div><div class="stat-note">등록됨</div></div>
      <div><div class="stat-value">{connected}</div><div class="stat-note">현재 연결됨</div></div>
    </div>
  </Card>
  <Card class="summary-card"><div class="stat-label">등록된 내부 앱</div><div class="stat-value">{data.services.length}</div><div class="stat-note">내부 주소와 포트를 등록한 앱</div></Card>
  <Card class="summary-card"><div class="stat-label">설정된 공개 주소</div><div class="stat-value">{data.routes.length}</div><div class="stat-note">접속 상태와 별개로 등록된 도메인</div></Card>
</div>

<ConnectionMap {data} {navigate} />

<style>
  .summary-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 12px; margin-bottom: 30px; }
  .summary-grid :global(.summary-card) { padding: 18px 20px; }
  .stat-label { color: #6f6a65; font-size: 12px; }
  .stat-value { margin-top: 5px; font-size: 27px; font-weight: 650; letter-spacing: -.05em; line-height: 1.2; }
  .stat-note { margin-top: 7px; color: var(--muted); font-size: 11px; }
  .connector-stats { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 5px; }
  .connector-stats > div + div { border-left: 1px solid var(--line); padding-left: 20px; }
  .connector-stats .stat-value { margin-top: 0; }
  .two-col { grid-template-columns: minmax(0, 1.35fr) minmax(280px, .65fr); }
  .onboarding-list { display: grid; gap: 0; margin: 4px 0 0; }
  .onboarding-list :global(.onboarding-item) { display: grid; width: 100%; grid-template-columns: 30px minmax(0, 1fr) auto; align-items: center; gap: 12px; border-bottom: 1px solid var(--line); padding: 13px 0; color: inherit; text-align: left; }
  .onboarding-list :global(.onboarding-item:last-child) { border-bottom: 0; }
  .onboarding-number { display: grid; width: 25px; height: 25px; place-items: center; border: 1px solid #aaa99f; border-radius: 50%; color: var(--muted); font-size: 12px; }
  :global(.onboarding-item.done) .onboarding-number { border-color: var(--green); background: var(--green); color: white; }
  .onboarding-list strong { display: block; font-size: 13px; }
  .onboarding-list small { color: var(--muted); }
  .onboarding-action { color: var(--accent-dark); font-size: 12px; font-weight: 700; }
  :global(.callout) { border: 0; border-left: 2px solid var(--accent); border-radius: 0 6px 6px 0; padding: 13px 15px; background: #fbf3ed; color: #5f4b42; font-size: 13px; }
  :global(.callout strong) { display: block; margin-bottom: 2px; color: var(--ink); }
  :global(.help-panel) { margin-top: 16px; }
  :global(.help-panel h2) { margin: 0 0 5px; font-size: 17px; }
  :global(.help-panel p) { margin: 0; color: var(--muted); }
  .monitor-list { display: grid; gap: 8px; }
  :global(.monitor-item) { padding: 13px 15px; }
  :global(.monitor-item.attention) { border-left: 3px solid #bb4d3d; }
  .monitor-head { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .monitor-type { display: block; margin-bottom: 2px; color: var(--muted); font-size: 11px; font-weight: 700; }
  .monitor-head .monitor-type { display: inline; margin-right: 8px; }
  .monitor-head h3 { display: inline; margin: 0; font-size: 16px; }
  .monitor-head p, .monitor-empty { color: var(--muted); font-size: 12px; }
  :global(.monitor-app-card) { display: grid; grid-template-columns: minmax(150px, .8fr) minmax(0, 1.4fr) auto; align-items: center; gap: 12px; margin-top: 10px; border-radius: 5px; padding: 10px 12px; }
  :global(.monitor-app-card.attention) { border-left: 2px solid #bb4d3d; }
  :global(.monitor-app-card.empty-app) { display: block; }
  .monitor-app-head h4 { display: inline; margin: 0 8px 0 0; font-size: 14px; }
  .monitor-app-head code { color: var(--muted); font-size: 12px; }
  .monitor-route-group { min-width: 0; }
  .monitor-routes { display: flex; flex-wrap: wrap; gap: 4px 12px; }
  .monitor-routes :global(.ui-button) { justify-content: flex-start; overflow-wrap: anywhere; white-space: normal; font-size: 13px; text-align: left; }
  .monitor-routes span { color: var(--muted); font-size: 12px; }
  @media (max-width: 900px) { .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .two-col { grid-template-columns: 1fr; } }
  @media (max-width: 700px) { :global(.monitor-app-card) { grid-template-columns: 1fr; gap: 8px; } }
  @media (max-width: 600px) { .summary-grid { grid-template-columns: 1fr; } .monitor-head { align-items: flex-start; flex-direction: column; } }
</style>

{#if showOnboarding}
  <div class="dashboard-grid two-col">
    <Panel title="시작하기" description="Meerkat에서 서비스를 공개하는 순서입니다.">
      <div class="onboarding-list">
        {#each items as [label, done, action, view], i}
          <Button variant="plain" class={done ? 'onboarding-item done' : 'onboarding-item'} onclick={() => navigate(view)}>
            <span class="onboarding-number">{done ? '✓' : i + 1}</span>
            <span><strong>{label}</strong><small>{done ? '완료됨' : action}</small></span>
            <span class="onboarding-action">{done ? '완료' : action}</span>
          </Button>
        {/each}
      </div>
    </Panel>
    <div>
      <Card variant="solid" padding="none" class="callout"><strong>현재 할 일</strong><span>{next}</span></Card>
      <Card class="help-panel"><h2>용어 안내</h2><p>Connector는 앱이 실행되는 서버에서 Outlet으로 연결하는 프로그램입니다. 현재 Connector 하나에 내부 앱 하나를 등록하고, 앱에는 공개 주소를 추가할 수 있습니다.</p></Card>
    </div>
  </div>
{:else}
  <Panel title="앱별 운영 현황" description="Connector의 마지막 응답과 내부 앱 상태, 공개 주소를 확인합니다.">
    <div class="monitor-list">
      {#each data.connectors as connector}
        {@const records = data.records.filter((record) => record.connector.id === connector.id)}
        <Card as="article" padding="none" class={connector.status !== 'connected' || records.some((record) => record.service.observed_status !== 'connected') ? 'monitor-item attention' : 'monitor-item'}>
          <div class="monitor-head">
            <div><span class="monitor-type">CONNECTOR</span><h3>{connector.name}</h3><p>마지막 응답: {lastResponse(connector.last_seen)}</p></div>
            <Status label={statusLabel(connector.status)} value={connector.status} />
          </div>
          {#each records as record}
            <Card variant="solid" padding="none" class={record.service.observed_status !== 'connected' ? 'monitor-app-card attention' : 'monitor-app-card'}>
              <div class="monitor-app-head">
                <span class="monitor-type">내부 앱</span><h4>{record.service.name}</h4><code>내부 주소 {record.service.target.host}:{record.service.target.port}</code>
              </div>
              <div class="monitor-route-group">
                <span class="monitor-type">공개 주소</span>
                <div class="monitor-routes">
                  {#each record.routes as route}
                    <LinkButton href="https://{route.hostname}" variant="text" size="sm" target="_blank" rel="noopener noreferrer">https://{route.hostname}</LinkButton>
                  {:else}
                    <span>공개 주소 없음</span>
                  {/each}
                </div>
              </div>
              <Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} />
            </Card>
          {:else}
            <Card variant="solid" padding="none" class="monitor-app-card attention empty-app"><span class="monitor-type">내부 앱</span><p class="monitor-empty">등록된 내부 앱이 없습니다.</p></Card>
          {/each}
        </Card>
      {/each}
    </div>
  </Panel>
{/if}
