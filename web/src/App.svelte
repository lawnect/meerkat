<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, mockApiEnabled } from './api';
  import { pageFromPath, pagePath } from './navigation';
  import type { AuthMode, Connector, DashboardData, Outlet, PageKey, Route, Service } from './types';
  import AuthPage from './components/AuthPage.svelte';
  import Status from './components/Status.svelte';
  import OverviewPage from './pages/OverviewPage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import ConnectorsPage from './pages/ConnectorsPage.svelte';
  import RoutesPage from './pages/RoutesPage.svelte';
  import DiagnosticsPage from './pages/DiagnosticsPage.svelte';
  import Button from './components/ui/Button.svelte';
  import Card from './components/ui/Card.svelte';
  import LinkButton from './components/ui/LinkButton.svelte';

  let authMode = $state<AuthMode>('loading');
  let view = $state<PageKey>(pageFromPath(window.location.pathname));
  let loading = $state(false);
  let loadError = $state('');
  let data = $state<DashboardData>({ connectors: [], services: [], routes: [], records: [], outlet: {} });

  async function refresh(background = false) {
    if (!background) loading = true;
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
      if (!background) loading = false;
    }
  }

  async function navigate(nextView: PageKey): Promise<void> {
    if (window.location.pathname !== pagePath[nextView]) {
      window.history.pushState(null, '', pagePath[nextView]);
    }
    view = nextView;
    if (nextView === 'connectors') await refresh();
  }

  function navigateFromLink(event: MouseEvent, nextView: PageKey): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    void navigate(nextView);
  }

  function handlePopState(): void {
    view = pageFromPath(window.location.pathname);
    if (view === 'connectors' && authMode === 'dashboard') void refresh();
  }

  async function logout() {
    await api('/api/v1/session', { method: 'DELETE' });
    authMode = 'login';
  }

  onMount(() => {
    window.addEventListener('popstate', handlePopState);
    void (async () => {
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
    })();
    const timer = window.setInterval(() => {
      if (authMode === 'dashboard' && !loading) void refresh(true);
    }, 10000);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('popstate', handlePopState);
    };
  });
</script>

{#if authMode === 'setup'}
  <AuthPage mode="setup" onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
{:else if authMode === 'login'}
  <AuthPage mode="login" onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
{:else if authMode === 'loading'}
  <main class="auth-wrap"><p>Outlet에 연결하는 중입니다…</p></main>
{:else if authMode === 'unavailable'}
  <main class="auth-wrap"><Card as="section" class="auth"><div class="auth-brand">MEERKAT / OUTLET</div><h1>연결할 수 없습니다</h1><p class="auth-intro">Outlet API에 연결할 수 없습니다. 서버 상태와 인증서를 확인하고 다시 시도하세요.</p><Button onclick={() => location.reload()}>다시 시도</Button></Card></main>
{:else}
  <header class="topbar">
    <div class="brand">MEERKAT <small>OUTLET</small></div>
    <div class="topbar-meta">{#if mockApiEnabled}<span class="mock-badge" title="변경 내용은 현재 브라우저 세션에만 유지됩니다">MOCK API</span>{/if}<span>{data.outlet.domain || '도메인 미설정'}</span><Status label="Outlet 정상" value="connected" /><Button variant="plain" class="logout-button" onclick={logout}>로그아웃</Button></div>
  </header>
  <div class="layout">
    <aside class="rail">
      <p class="rail-label">운영</p>
      <nav class="nav" aria-label="운영 메뉴">
        <LinkButton href={pagePath.overview} variant="text" class={view === 'overview' ? 'nav-item active' : 'nav-item'} aria-current={view === 'overview' ? 'page' : undefined} onclick={(event) => navigateFromLink(event, 'overview')}>개요</LinkButton>
        <LinkButton href={pagePath.connectors} variant="text" class={view === 'connectors' ? 'nav-item active' : 'nav-item'} aria-current={view === 'connectors' ? 'page' : undefined} onclick={(event) => navigateFromLink(event, 'connectors')}>앱 연결</LinkButton>
        <LinkButton href={pagePath.routes} variant="text" class={view === 'routes' ? 'nav-item active' : 'nav-item'} aria-current={view === 'routes' ? 'page' : undefined} onclick={(event) => navigateFromLink(event, 'routes')}>공개 서비스</LinkButton>
        <LinkButton href={pagePath.diagnostics} variant="text" class={view === 'diagnostics' ? 'nav-item active' : 'nav-item'} aria-current={view === 'diagnostics' ? 'page' : undefined} onclick={(event) => navigateFromLink(event, 'diagnostics')}>요청 진단</LinkButton>
        <LinkButton href={pagePath.settings} variant="text" class={view === 'settings' ? 'nav-item active' : 'nav-item'} aria-current={view === 'settings' ? 'page' : undefined} onclick={(event) => navigateFromLink(event, 'settings')}>설정</LinkButton>
      </nav>
      <div class="rail-note"><strong>도움이 필요하신가요?</strong>앱과 Connector를 연결하면 등록한 공개 주소로 접속할 수 있습니다.</div>
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
    height: 68px;
    display: flex;
    align-items: center;
    gap: 26px;
    padding: 0 34px;
    border-bottom: 1px solid var(--line);
    background: #fffdfa;
    color: var(--ink);
  }
  .brand { font-size: 16px; font-weight: 670; letter-spacing: .24em; }
  .brand small { margin-left: 6px; color: #8d8780; font-size: 9px; font-weight: 600; letter-spacing: .1em; }
  .topbar-meta { display: flex; align-items: center; gap: 18px; margin-left: auto; color: #69655f; font-size: 12px; }
  .topbar-meta :global(.connection-status) { color: inherit; }
  .topbar :global(.logout-button) { padding: 6px 0; color: #57534e; }
  .mock-badge { border: 1px solid #f2c8ae; border-radius: 3px; padding: 3px 6px; color: var(--accent-dark); font-size: 10px; font-weight: 700; letter-spacing: .08em; }
  .layout { display: grid; grid-template-columns: 220px minmax(0, 1fr); min-height: calc(100vh - 68px); }
  .rail { display: flex; flex-direction: column; border-right: 1px solid var(--line); padding: 32px 18px; background: #f8f6f2; }
  .rail-label { margin: 0 12px 11px; color: #938d85; font-size: 10px; font-weight: 700; letter-spacing: .12em; text-transform: uppercase; }
  .nav { display: grid; gap: 3px; }
  .nav :global(.nav-item) { display: block; width: 100%; border-left: 2px solid transparent; border-radius: 0 5px 5px 0; padding: 11px 12px; color: #635f59; text-align: left; }
  .nav :global(.nav-item:hover) { background: #f1ebe4; text-decoration: none; }
  .nav :global(.nav-item.active) { border-left-color: var(--accent); background: #f6eee8; color: var(--accent-dark); font-weight: 700; }
  .rail-note { margin-top: auto; border-top: 1px solid var(--line); padding: 18px 10px 0; color: var(--muted); font-size: 12px; }
  .rail-note strong { display: block; margin-bottom: 4px; color: var(--ink); font-size: 13px; }
  .content { width: 100%; max-width: 1440px; padding: 38px 44px 72px; }
  .loading-note { margin: 0 0 16px; color: var(--muted); }
  @media (max-width: 900px) {
    .layout { grid-template-columns: minmax(0, 1fr); grid-template-rows: max-content minmax(0, 1fr); }
    .rail { display: block; min-width: 0; border-right: 0; border-bottom: 1px solid var(--line); padding: 10px 16px; }
    .rail-label, .rail-note { display: none; }
    .nav { display: flex; overflow: auto; }
    .nav :global(.nav-item) { width: auto; border-bottom: 3px solid transparent; border-left: 0; white-space: nowrap; }
    .nav :global(.nav-item.active) { border-bottom-color: var(--accent); }
    .content { min-width: 0; padding: 28px 22px 56px; }
  }
  @media (max-width: 600px) {
    .topbar { height: auto; min-height: 64px; display: grid; gap: 8px; padding: 12px 16px; }
    .topbar-meta { margin-left: 0; flex-wrap: wrap; gap: 8px 14px; }
  }
</style>
