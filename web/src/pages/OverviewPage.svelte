<script lang="ts">
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import type { DashboardData, PageKey } from '../types';

  let { data, navigate }: { data: DashboardData; navigate: (view: PageKey) => Promise<void> } = $props();
  const connected = $derived(data.connectors.filter((connector) => connector.status === 'connected').length);
  const next = $derived(data.connectors.length === 0
    ? 'Connector를 하나 만들어 내부 앱을 연결하세요.'
    : connected === 0
      ? 'Connector를 실행하고 등록 토큰으로 연결하세요.'
      : '공개 서비스의 상태를 확인하세요.');
  const items = $derived([
    ['도메인 확인', Boolean(data.outlet.domain), '설정에서 변경', 'settings'],
    ['Connector 만들기', data.connectors.length > 0, 'Connector 등록', 'connectors'],
    ['Connector 연결', connected > 0, '등록 토큰 발급', 'connectors'],
    ['공개 서비스 만들기', data.routes.length > 0, '공개 주소 추가', 'connectors'],
  ] as [string, boolean, string, PageKey][]);
</script>

<PageHead kicker="MEERKAT OUTLET" title="오늘의 운영 상태" description={next}>
  {#snippet action()}
    <button class="btn btn-primary" onclick={() => navigate(data.connectors.length ? 'routes' : 'connectors')}>{data.connectors.length ? '공개 서비스 보기' : 'Connector 시작'}</button>
  {/snippet}
</PageHead>

<div class="dashboard-grid summary-grid">
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">Connector</div><div class="stat-value">{data.connectors.length}</div><div class="stat-note">등록된 연결</div></div>
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">연결됨</div><div class="stat-value">{connected}</div><div class="stat-note">heartbeat 기준</div></div>
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">내부 앱</div><div class="stat-value">{data.services.length}</div><div class="stat-note">등록된 앱</div></div>
  <div class="summary-card card bg-base-100 border-base-300"><div class="stat-label">공개 주소</div><div class="stat-value">{data.routes.length}</div><div class="stat-note">등록된 주소</div></div>
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
  @media (max-width: 900px) { .summary-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .two-col { grid-template-columns: 1fr; } }
</style>

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
