<script lang="ts">
  import { statusLabel } from '../api';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import Status from '../components/Status.svelte';
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
    <button class="btn btn-primary" onclick={() => navigate(data.connectors.length ? 'routes' : 'connectors')}>{data.connectors.length ? '공개 서비스 보기' : 'Connector 시작'}</button>
  {/snippet}
</PageHead>

<div class="dashboard-grid summary-grid">
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">등록된 Connector</div><div class="stat-value">{data.connectors.length}</div><div class="stat-note">앱 연결을 위해 만든 Connector</div></div>
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">현재 연결된 Connector</div><div class="stat-value">{connected}</div><div class="stat-note">전체 {data.connectors.length}개 중 최근 응답과 터널 연결 확인</div></div>
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">등록된 내부 앱</div><div class="stat-value">{data.services.length}</div><div class="stat-note">내부 주소와 포트를 등록한 앱</div></div>
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">설정된 공개 주소</div><div class="stat-value">{data.routes.length}</div><div class="stat-note">접속 상태와 별개로 등록된 도메인</div></div>
</div>

<style>
  .summary-grid { grid-template-columns: repeat(4, minmax(0, 1fr)); margin-bottom: 30px; }
  .summary-card { border: 1px solid var(--line); padding: 17px 18px; background: var(--panel); }
  .stat-label { color: var(--muted); font-size: 12px; }
  .stat-value { margin-top: 3px; font-size: 25px; font-weight: 750; letter-spacing: -.04em; }
  .stat-note { margin-top: 2px; color: var(--muted); font-size: 11px; }
  .two-col { grid-template-columns: minmax(0, 1.35fr) minmax(280px, .65fr); }
  .onboarding-list { display: grid; gap: 0; margin: 4px 0 0; }
  .onboarding-item { display: grid; width: 100%; grid-template-columns: 30px minmax(0, 1fr) auto; align-items: center; gap: 12px; border: 0; border-bottom: 1px solid var(--line); padding: 13px 0; background: transparent; color: inherit; text-align: left; }
  .onboarding-item:last-child { border-bottom: 0; }
  .onboarding-number { display: grid; width: 25px; height: 25px; place-items: center; border: 1px solid #aaa99f; border-radius: 50%; color: var(--muted); font-size: 12px; }
  .onboarding-item.done .onboarding-number { border-color: var(--green); background: var(--green); color: white; }
  .onboarding-item strong { display: block; font-size: 13px; }
  .onboarding-item small { color: var(--muted); }
  .onboarding-action { color: var(--accent-dark); font-size: 12px; font-weight: 700; }
  .callout { border-left: 3px solid var(--accent); padding: 13px 15px; background: #fbf3ed; color: #5f4b42; font-size: 13px; }
  .callout strong { display: block; margin-bottom: 2px; color: var(--ink); }
  .help-panel { margin-top: 16px; }
  .monitor-list { display: grid; gap: 12px; }
  .monitor-item { border: 1px solid var(--line); padding: 16px; background: var(--panel); }
  .monitor-item.attention { border-left: 3px solid #bb4d3d; }
  .monitor-head, .monitor-app { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .monitor-head strong { display: block; margin-bottom: 4px; font-size: 15px; }
  .monitor-head p, .monitor-empty { color: var(--muted); font-size: 12px; }
  .monitor-app { margin-top: 14px; border-top: 1px solid var(--line); padding-top: 14px; }
  .monitor-app strong { margin-right: 8px; }
  .monitor-app code { color: var(--muted); font-size: 12px; }
  .monitor-routes { display: flex; flex-wrap: wrap; gap: 8px 16px; margin-top: 10px; }
  .monitor-routes a { overflow-wrap: anywhere; color: var(--accent-dark); font-size: 13px; }
  .monitor-routes span { color: var(--muted); font-size: 12px; }
  @media (max-width: 900px) { .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .two-col { grid-template-columns: 1fr; } }
  @media (max-width: 600px) { .monitor-head, .monitor-app { align-items: flex-start; flex-direction: column; } }
</style>

{#if showOnboarding}
  <div class="dashboard-grid two-col">
    <Panel title="시작하기" description="Meerkat에서 서비스를 공개하는 순서입니다.">
      <div class="onboarding-list">
        {#each items as [label, done, action, view], i}
          <button class:done class="onboarding-item" onclick={() => navigate(view)}>
            <span class="onboarding-number">{done ? '✓' : i + 1}</span>
            <span><strong>{label}</strong><small>{done ? '완료됨' : action}</small></span>
            <span class="onboarding-action">{done ? '완료' : action}</span>
          </button>
        {/each}
      </div>
    </Panel>
    <div>
      <div class="callout"><strong>현재 할 일</strong><span>{next}</span></div>
      <div class="panel card bg-base-100 border-base-300 help-panel"><h2>용어 안내</h2><p>Connector는 앱이 실행되는 서버에서 Outlet으로 연결하는 프로그램입니다. 현재 Connector 하나에 내부 앱 하나를 등록하고, 앱에는 공개 주소를 추가할 수 있습니다.</p></div>
    </div>
  </div>
{:else}
  <Panel title="앱별 운영 현황" description="Connector의 마지막 응답과 내부 앱 상태, 공개 주소를 확인합니다.">
    <div class="monitor-list">
      {#each data.connectors as connector}
        {@const records = data.records.filter((record) => record.connector.id === connector.id)}
        <article class:attention={connector.status !== 'connected' || records.some((record) => record.service.observed_status !== 'connected')} class="monitor-item">
          <div class="monitor-head">
            <div><strong>{connector.name}</strong><p>마지막 응답: {lastResponse(connector.last_seen)}</p></div>
            <Status label={statusLabel(connector.status)} value={connector.status} />
          </div>
          {#each records as record}
            <div class="monitor-app">
              <div><strong>{record.service.name}</strong><code>{record.service.target.host}:{record.service.target.port}</code></div>
              <Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} />
            </div>
            <div class="monitor-routes">
              {#each record.routes as route}
                <a href="https://{route.hostname}" target="_blank" rel="noopener noreferrer">https://{route.hostname}</a>
              {:else}
                <span>공개 주소 없음</span>
              {/each}
            </div>
          {:else}
            <p class="monitor-empty">등록된 내부 앱이 없습니다.</p>
          {/each}
        </article>
      {/each}
    </div>
  </Panel>
{/if}
