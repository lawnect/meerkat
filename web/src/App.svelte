<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, mockApiEnabled } from './api';
  import type { AuthMode, Connector, DashboardData, Outlet, PageKey, Route, Service } from './types';
  import AuthPage from './components/AuthPage.svelte';
  import Status from './components/Status.svelte';
  import OverviewPage from './pages/OverviewPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import ConnectorsPage from './pages/ConnectorsPage.svelte';
  import RoutesPage from './pages/RoutesPage.svelte';
  import DiagnosticsPage from './pages/DiagnosticsPage.svelte';

  let authMode = $state<AuthMode>('loading');
  let view = $state<PageKey>('overview');
  let loading = $state(false);
  let loadError = $state('');
  let data = $state<DashboardData>({ connectors: [], services: [], routes: [], records: [], outlet: {} });

  async function refresh() {
    loading = true;
    loadError = '';
    try {
      const [connectorResponse, outlet] = await Promise.all([
        api<{ connectors: Connector[] }>('/api/v1/connectors'),
        api<Outlet>('/api/v1/outlet'),
      ]);
      const groups = await Promise.all(connectorResponse.connectors.map(async (connector) => {
        const response = await api<{ services: Service[] }>(`/api/v1/connectors/${encodeURIComponent(connector.id)}/services`);
        return Promise.all(response.services.map(async (service) => {
          const routeResponse = await api<{ routes: Route[] }>(`/api/v1/services/${encodeURIComponent(service.id)}/routes`);
          return { connector, service, routes: routeResponse.routes };
        }));
      }));
      const records = groups.flat();
      data = {
        connectors: connectorResponse.connectors,
        services: records.map((record) => record.service),
        routes: records.flatMap((record) => record.routes),
        records,
        outlet,
      };
      authMode = 'dashboard';
    } catch (cause) {
      const code = errorMessage(cause);
      if (code === 'invalid_admin_session' || code === 'invalid_admin_credential') {
        authMode = 'login';
      } else {
        loadError = `대시보드를 불러오지 못했습니다: ${code}`;
      }
    } finally {
      loading = false;
    }
  }

  async function navigate(nextView: PageKey): Promise<void> {
    view = nextView;
    if (nextView === 'connectors') await refresh();
  }

  async function logout() {
    await api('/api/v1/session', { method: 'DELETE' });
    authMode = 'login';
  }

  onMount(async () => {
    try {
      const status = await api<{ setup_complete: boolean }>('/api/v1/setup/status');
      if (!status.setup_complete) {
        authMode = 'setup';
        return;
      }
      await refresh();
      if (loadError) authMode = 'login';
    } catch {
      authMode = 'unavailable';
    }
  });
</script>

{#if authMode === 'setup'}
  <AuthPage mode="setup" onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
{:else if authMode === 'login'}
  <AuthPage mode="login" onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
{:else if authMode === 'loading'}
  <main class="auth-wrap"><p>Outlet에 연결하는 중입니다…</p></main>
{:else if authMode === 'unavailable'}
  <main class="auth-wrap"><section class="auth"><div class="auth-brand">MEERKAT / OUTLET</div><h1>연결할 수 없습니다</h1><p class="auth-intro">Outlet API에 연결할 수 없습니다. 서버 상태와 인증서를 확인하고 다시 시도하세요.</p><button class="btn btn-primary" onclick={() => location.reload()}>다시 시도</button></section></main>
{:else}
  <header class="topbar">
    <div class="brand">MEERKAT <small>OUTLET</small></div>
    <div class="topbar-meta">{#if mockApiEnabled}<span class="mock-badge" title="변경 내용은 현재 브라우저 세션에만 유지됩니다">MOCK API</span>{/if}<span>{data.outlet.domain || '도메인 미설정'}</span><Status label="Outlet 정상" value="connected" /><button onclick={logout}>로그아웃</button></div>
  </header>
  <div class="layout">
    <aside class="rail">
      <p class="rail-label">운영</p>
      <nav class="nav" aria-label="운영 메뉴">
        <button class:active={view === 'overview'} onclick={() => navigate('overview')}>개요</button>
        <button class:active={view === 'connectors'} onclick={() => navigate('connectors')}>연결 서버</button>
        <button class:active={view === 'routes'} onclick={() => navigate('routes')}>공개 서비스</button>
        <button class:active={view === 'diagnostics'} onclick={() => navigate('diagnostics')}>요청 진단</button>
        <button class:active={view === 'settings'} onclick={() => navigate('settings')}>설정</button>
      </nav>
      <div class="rail-note"><strong>도움이 필요하신가요?</strong>공개 서비스는 연결 서버가 연결된 뒤 외부에서 접근할 수 있습니다.</div>
    </aside>
    <main class="content">
      {#if loadError}<p class="error" role="alert">{loadError}</p>{/if}
      {#if loading}<p class="loading-note">상태를 불러오는 중입니다…</p>{/if}
      {#if view === 'overview'}
        <OverviewPage {data} {navigate} />
      {:else if view === 'settings'}
        <SettingsPage outlet={data.outlet} onSaved={refresh} />
      {:else if view === 'connectors'}
        <ConnectorsPage {data} onChanged={refresh} />
      {:else if view === 'routes'}
        <RoutesPage {data} onChanged={refresh} {navigate} />
      {:else if view === 'diagnostics'}
        <DiagnosticsPage />
      {/if}
    </main>
  </div>
{/if}

<style>
  .topbar {
    height: 64px;
    display: flex;
    align-items: center;
    gap: 26px;
    padding: 0 32px;
    border-bottom: 1px solid #15191c;
    background: #242a2f;
    color: #f8f6ef;
  }
  .brand { font-size: 16px; font-weight: 760; letter-spacing: .08em; }
  .brand small { margin-left: 7px; color: #b8c0c2; font-size: 10px; letter-spacing: .14em; }
  .topbar-meta { display: flex; align-items: center; gap: 18px; margin-left: auto; color: #c3c9c9; font-size: 12px; }
  .topbar button { border: 0; padding: 6px 0; background: transparent; color: #e8ece8; }
  .mock-badge { border: 1px solid #e5aa47; border-radius: 2px; padding: 3px 6px; color: #ffd27c; font-size: 10px; font-weight: 800; letter-spacing: .08em; }
  .layout { display: grid; grid-template-columns: 232px minmax(0, 1fr); min-height: calc(100vh - 64px); }
  .rail { display: flex; flex-direction: column; border-right: 1px solid var(--line); padding: 24px 16px; background: #ebe9e2; }
  .rail-label { margin: 0 10px 9px; color: #85857f; font-size: 11px; font-weight: 700; letter-spacing: .1em; text-transform: uppercase; }
  .nav { display: grid; gap: 3px; }
  .nav button { border: 0; border-left: 3px solid transparent; border-radius: 2px; padding: 10px 11px; background: transparent; color: #4f5659; text-align: left; }
  .nav button:hover { background: #e2dfd7; }
  .nav button.active { border-left-color: var(--accent); background: var(--panel); color: var(--ink); font-weight: 700; }
  .rail-note { margin-top: auto; border-top: 1px solid var(--line); padding: 18px 10px 0; color: var(--muted); font-size: 12px; }
  .rail-note strong { display: block; margin-bottom: 4px; color: var(--ink); font-size: 13px; }
  .content { width: 100%; max-width: 1180px; padding: 38px 48px 72px; }
  .loading-note { margin: 0 0 16px; color: var(--muted); }
  @media (max-width: 900px) {
    .layout { grid-template-columns: 1fr; }
    .rail { display: block; border-right: 0; border-bottom: 1px solid var(--line); padding: 10px 16px; }
    .rail-label, .rail-note { display: none; }
    .nav { display: flex; overflow: auto; }
    .nav button { border-bottom: 3px solid transparent; border-left: 0; white-space: nowrap; }
    .nav button.active { border-bottom-color: var(--accent); }
    .content { padding: 28px 22px 56px; }
  }
</style>
