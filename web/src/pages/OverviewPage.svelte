<script lang="ts">
  import { statusLabel } from '../api';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import ConnectionMap from '../components/ConnectionMap.svelte';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import type { DashboardData, PageKey } from '../types';

  type ConnectionIssue = { id: string; name: string; details: string[] };

  let { data, navigate }: { data: DashboardData; navigate: (view: PageKey) => Promise<void> } = $props();
  const connected = $derived(data.connectors.filter((connector) => connector.status === 'connected').length);
  const next = $derived(!data.outlet.domain
    ? '도메인 관리에서 Outlet 도메인을 입력하세요.'
    : data.connectors.length === 0
      ? 'Connector를 하나 만드세요.'
      : data.services.length === 0
        ? 'Connector에 연결할 서비스를 등록하세요.'
        : data.routes.length === 0
          ? '연결 서비스에 공개 주소를 추가하세요.'
          : connected === 0
            ? 'Connector를 실행하고 등록 토큰으로 연결하세요.'
            : null);
  const items = $derived([
    ['도메인 확인', Boolean(data.outlet.domain), '도메인 관리에서 변경', 'domains'],
    ['Connector 만들기', data.connectors.length > 0, 'Connector 등록', 'connectors'],
    ['연결 서비스 등록', data.services.length > 0, '서비스 이름과 내부 주소 입력', 'connectors'],
    ['공개 주소 추가', data.routes.length > 0, '도메인 등록', 'connectors'],
    ['Connector 연결', connected > 0, '등록 토큰 발급', 'connectors'],
  ] as [string, boolean, string, PageKey][]);
  const showOnboarding = $derived(Boolean(next) && !(data.outlet.domain && data.routes.length > 0 && data.connectors.some((connector) => connector.status !== 'draft')));
  const connectionIssues = $derived.by((): ConnectionIssue[] => {
    const issues: ConnectionIssue[] = [];
    for (const connector of data.connectors) {
      const record = data.records.find((item) => item.connector.id === connector.id);
      const details: string[] = [];

      if (connector.status !== 'connected') details.push(`터널 ${statusLabel(connector.status)}`);
      if (!record) {
        details.push('연결 서비스 미등록');
      } else {
        if (!record.service.enabled) details.push('연결 서비스 사용 안 함');
        else if (record.service.observed_status !== 'connected') details.push(`서비스 포트 ${statusLabel(record.service.observed_status)}`);

        if (!record.routes.length) details.push('공개 주소 없음');
        else {
          const needsReview = record.routes.filter((route) => !route.enabled || Boolean(route.last_error) || route.applied_revision < route.desired_revision).length;
          if (needsReview) details.push(`공개 주소 ${needsReview}개 확인 필요`);
        }
      }

      if (details.length) issues.push({ id: connector.id, name: connector.name, details });
    }
    return issues;
  });
</script>

<PageHead kicker="MEERKAT OUTLET" title="오늘의 운영 상태" description={next ?? 'Connector와 공개 주소의 현황을 확인할 수 있습니다.'}>
  {#snippet action()}
    <Button onclick={() => navigate('connectors')}>{data.connectors.length ? 'Connector 관리' : 'Connector 시작'}</Button>
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
  <Card class="summary-card"><div class="stat-label">등록된 연결 서비스</div><div class="stat-value">{data.services.length}</div><div class="stat-note">내부 주소와 포트를 등록한 서비스</div></Card>
  <Card class="summary-card"><div class="stat-label">설정된 공개 주소</div><div class="stat-value">{data.routes.length}</div><div class="stat-note">접속 상태와 별개로 등록된 도메인</div></Card>
</div>

<ConnectionMap {data} {navigate} />

<style>
  .summary-grid { grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10px; margin-bottom: 20px; }
  .summary-grid :global(.summary-card) { padding: 13px 16px; }
  .stat-label { color: #6f6a65; font-size: 12px; }
  .stat-value { margin-top: 3px; font-size: 24px; font-weight: 650; letter-spacing: -.05em; line-height: 1.2; }
  .stat-note { margin-top: 4px; color: var(--muted); font-size: 11px; }
  .connector-stats { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); margin-top: 3px; }
  .connector-stats > div + div { border-left: 1px solid var(--line); padding-left: 14px; }
  .connector-stats .stat-value { margin-top: 0; }
  .two-col { grid-template-columns: minmax(0, 1.35fr) minmax(280px, .65fr); }
  .onboarding-list { display: grid; gap: 0; margin: 4px 0 0; }
  .onboarding-list :global(.onboarding-item) { display: grid; width: 100%; grid-template-columns: 26px minmax(0, 1fr) auto; align-items: center; gap: 10px; border-bottom: 1px solid var(--line); padding: 9px 0; color: inherit; text-align: left; }
  .onboarding-list :global(.onboarding-item:last-child) { border-bottom: 0; }
  .onboarding-number { display: grid; width: 22px; height: 22px; place-items: center; border: 1px solid #aaa99f; border-radius: 50%; color: var(--muted); font-size: 12px; }
  :global(.onboarding-item.done) .onboarding-number { border-color: var(--green); background: var(--green); color: white; }
  .onboarding-list strong { display: block; font-size: 13px; }
  .onboarding-list small { color: var(--muted); }
  .onboarding-action { color: var(--accent-dark); font-size: 12px; font-weight: 700; }
  :global(.callout) { border: 0; border-left: 2px solid var(--accent); border-radius: 0 6px 6px 0; padding: 10px 13px; background: #fbf3ed; color: #5f4b42; font-size: 13px; }
  :global(.callout strong) { display: block; margin-bottom: 2px; color: var(--ink); }
  :global(.help-panel) { margin-top: 10px; }
  :global(.help-panel h2) { margin: 0 0 5px; font-size: 17px; }
  :global(.help-panel p) { margin: 0; color: var(--muted); }
  .issue-heading { margin: 0 0 9px; font-size: 17px; font-weight: 700; letter-spacing: -.035em; }
  .issue-list { display: grid; gap: 6px; }
  .issue-list :global(.issue-card) { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .issue-copy { display: flex; align-items: start; gap: 10px; min-width: 0; }
  .issue-dot { flex: none; width: 7px; height: 7px; margin-top: 7px; border-radius: 50%; background: var(--accent); }
  .issue-copy h3 { margin: 0 0 2px; font-size: 13px; font-weight: 700; }
  .issue-copy p { margin: 0; color: var(--muted); font-size: 12px; }
  .issue-list :global(.issue-action) { flex: none; }
  @media (max-width: 900px) { .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .two-col { grid-template-columns: 1fr; } }
  @media (max-width: 600px) { .summary-grid { grid-template-columns: 1fr; } .issue-list :global(.issue-card) { align-items: start; flex-direction: column; gap: 8px; } .issue-list :global(.issue-action) { margin-left: 17px; } }
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
      <Card class="help-panel"><h2>용어 안내</h2><p>Connector는 서비스가 실행되는 서버에서 Outlet으로 연결하는 프로그램입니다. 현재 Connector 하나에 연결 서비스 하나를 등록하고, 서비스에는 공개 주소를 추가할 수 있습니다.</p></Card>
    </div>
  </div>
{:else if connectionIssues.length}
  <section class="connection-alerts" aria-label="연결 상태">
    <h2 class="issue-heading">확인이 필요한 연결</h2>
    <div class="issue-list">
      {#each connectionIssues as issue (issue.id)}
        <Card as="article" padding="sm" class="issue-card">
          <div class="issue-copy">
            <span class="issue-dot" aria-hidden="true"></span>
            <div><h3>{issue.name}</h3><p>{issue.details.join(' · ')}</p></div>
          </div>
          <Button variant="text" size="sm" class="issue-action" onclick={() => navigate('connectors')}>Connector 관리 <span aria-hidden="true">↗</span></Button>
        </Card>
      {/each}
    </div>
  </section>
{/if}
