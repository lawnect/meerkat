<script>
  import { onMount } from 'svelte';
  import { api, mockApiEnabled } from './api.js';
  import AuthPage from './components/AuthPage.svelte';
  import Status from './components/Status.svelte';
  import OverviewPage from './pages/OverviewPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import ConnectorsPage from './pages/ConnectorsPage.svelte';
  import RoutesPage from './pages/RoutesPage.svelte';
  import DiagnosticsPage from './pages/DiagnosticsPage.svelte';

  let authMode = $state('loading');
  let view = $state('overview');
  let loading = $state(false);
  let loadError = $state('');
  let data = $state({ connectors: [], services: [], routes: [], outlet: {} });

  async function refresh() {
    loading = true;
    loadError = '';
    try {
      const [connectorResponse, outlet] = await Promise.all([
        api('/api/v1/connectors'),
        api('/api/v1/outlet'),
      ]);
      const groups = await Promise.all(connectorResponse.connectors.map(async (connector) => {
        const response = await api(`/api/v1/connectors/${encodeURIComponent(connector.id)}/services`);
        return Promise.all(response.services.map(async (service) => {
          const routeResponse = await api(`/api/v1/services/${encodeURIComponent(service.id)}/routes`);
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
      if (cause.message === 'invalid_admin_session' || cause.message === 'invalid_admin_credential') {
        authMode = 'login';
      } else {
        loadError = `대시보드를 불러오지 못했습니다: ${cause.message}`;
      }
    } finally {
      loading = false;
    }
  }

  async function navigate(nextView) {
    view = nextView;
    if (nextView === 'connectors') await refresh();
  }

  async function logout() {
    await api('/api/v1/session', { method: 'DELETE' });
    authMode = 'login';
  }

  onMount(async () => {
    try {
      const status = await api('/api/v1/setup/status');
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

{#if authMode === 'setup' || authMode === 'login'}
  <AuthPage mode={authMode} onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
{:else if authMode === 'loading'}
  <main class="auth-wrap"><p>Outlet에 연결하는 중입니다…</p></main>
{:else if authMode === 'unavailable'}
  <main class="auth-wrap"><section class="auth"><div class="auth-brand">MEERKAT / OUTLET</div><h1>연결할 수 없습니다</h1><p class="auth-intro">Outlet API에 연결할 수 없습니다. 서버 상태와 인증서를 확인하고 다시 시도하세요.</p><button class="button" onclick={() => location.reload()}>다시 시도</button></section></main>
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
