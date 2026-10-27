<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage } from './api';
  import { clearStoredEnrollments, connectorState } from './connectorState.svelte';
  import { connectorDetailPath, connectorIdFromPath, domainDetailPath, domainFromPath, pageFromPath, pageHref } from './navigation';
  import { localeFromPath, setLocale, stripLocale, translations } from './i18n.svelte';
  import { connectionPollMs } from './polling';
  import { syncUrlState } from './urlState.svelte';
  import type { AuthMode, Connector, DashboardData, Outlet, PageKey, Route, Service, SignInOptions } from './types';
  import AuthPage from './components/AuthPage.svelte';
  import BrandMark from './components/BrandMark.svelte';
  import LocaleSwitch from './components/LocaleSwitch.svelte';
  import ThemeToggle from './components/ThemeToggle.svelte';
  import OverviewPage from './pages/OverviewPage.svelte';
  import DomainsPage from './pages/DomainsPage.svelte';
  import ConnectorsPage from './pages/ConnectorsPage.svelte';
  import DiagnosticsPage from './pages/DiagnosticsPage.svelte';
  import AccessPage from './pages/AccessPage.svelte';
  import ObservePage from './pages/ObservePage.svelte';
  import SettingsPage from './pages/SettingsPage.svelte';
  import { ArrowExit, Warning } from './icons';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Sidebar from '$lib/components/ui/sidebar/index.js';
  import { Toaster } from '$lib/components/ui/sonner/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import AppFooter from './components/AppFooter.svelte';
  import AppSidebar from './components/AppSidebar.svelte';
  import IconButton from './components/IconButton.svelte';

  const t = translations({
    unavailableTitle: 'Unable to connect',
    unavailableBody: 'The Outlet API is unreachable. Check the server status and certificate, then try again.',
    retry: 'Try again',
    logout: 'Log out',
    loadFailed: (code: string) => `Couldn't load the dashboard: ${code}`,
  }, {
    unavailableTitle: '연결할 수 없습니다',
    unavailableBody: 'Outlet API에 연결할 수 없습니다. 서버 상태와 인증서를 확인하고 다시 시도하세요.',
    retry: '다시 시도',
    logout: '로그아웃',
    loadFailed: (code: string) => `대시보드를 불러오지 못했습니다: ${code}`,
  });

  let authMode = $state<AuthMode>('loading');
  let signIn = $state<SignInOptions>({ password: true, sso: false, sso_label: '' });
  let view = $state<PageKey>(currentPage());
  let connectorDetailId = $state<string | null>(connectorIdFromPath(window.location.pathname));
  let domainDetail = $state<string | null>(domainFromPath(window.location.pathname));
  let loading = $state(false);
  let loadError = $state('');
  let data = $state<DashboardData>({ connectors: [], services: [], routes: [], records: [], outlet: {} });
  // 실패 요청을 주소로 거를 때 고르는 공개 주소입니다.
  const publicAddresses = $derived([...new Set(data.routes.map((route) => route.hostname))].sort());
  let statusRefreshPromise: Promise<void> | null = null;
  let backgroundRefreshBusy = false;

  function refreshConnectorStatus(): Promise<void> {
    if (statusRefreshPromise) return statusRefreshPromise;
    statusRefreshPromise = (async () => {
      const response = await api<{ connectors: Connector[] }>('/api/v1/connectors');
      const connectors = new Map(response.connectors.map((connector) => [connector.id, connector]));
      data = {
        ...data,
        connectors: response.connectors,
        records: data.records.map((record) => ({ ...record, connector: connectors.get(record.connector.id) || record.connector })),
      };
    })().finally(() => { statusRefreshPromise = null; });
    return statusRefreshPromise;
  }

  function currentPage(): PageKey {
    const legacy: Record<string, PageKey> = { '/routes': 'connectors', '/services': 'connectors' };
    const path = stripLocale(window.location.pathname).replace(/\/+$/, '');
    // 감사 로그와 접근 지표는 설정·접근 보호의 탭에서 관찰 화면으로 옮겼습니다. 이전 주소는 같은 탭의 새 주소로 바꿉니다.
    const tab = new URLSearchParams(window.location.search).get('tab');
    const replacement = (path === '/settings' && tab === 'audit') || (path === '/access' && tab === 'metrics') ? 'observe' : legacy[path];
    if (replacement) {
      window.history.replaceState(null, '', `${pageHref(replacement)}${window.location.search}${window.location.hash}`);
    }
    return pageFromPath(window.location.pathname);
  }

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
        clearStoredEnrollments();
        connectorState.selectedId = '';
        connectorState.enrollment = null;
        connectorState.enrollmentWasReset = false;
        authMode = 'login';
      } else {
        loadError = t().loadFailed(code);
      }
    } finally {
      if (!background) loading = false;
    }
  }

  async function navigate(nextView: PageKey): Promise<void> {
    // 화면을 옮길 때는 query를 이어 가지 않고 깨끗한 주소로 갑니다(뒤로 가기는 이전 query를 되살립니다).
    if (window.location.pathname !== pageHref(nextView) || window.location.search) {
      window.history.pushState(null, '', pageHref(nextView));
    }
    syncUrlState();
    view = nextView;
    connectorDetailId = null;
    domainDetail = null;
    if (nextView === 'connectors') await refresh();
  }

  // search는 '?tab=usage'처럼 상세 화면을 특정 탭으로 열 때만 넘깁니다.
  function openConnectorDetail(id: string, search = ''): void {
    window.history.pushState(null, '', `${connectorDetailPath(id)}${search}`);
    syncUrlState();
    view = 'connectors';
    connectorDetailId = id;
  }

  function openDomainDetail(domain: string): void {
    window.history.pushState(null, '', domainDetailPath(domain));
    syncUrlState();
    view = 'domains';
    domainDetail = domain;
  }

  function closeDomainDetail(): void {
    window.history.pushState(null, '', pageHref('domains'));
    syncUrlState();
    view = 'domains';
    domainDetail = null;
  }

  function closeConnectorDetail(): void {
    window.history.pushState(null, '', pageHref('connectors'));
    syncUrlState();
    view = 'connectors';
    connectorDetailId = null;
  }

  function handlePopState(): void {
    syncUrlState();
    setLocale(localeFromPath(window.location.pathname));
    view = currentPage();
    connectorDetailId = connectorIdFromPath(window.location.pathname);
    domainDetail = domainFromPath(window.location.pathname);
    if (view === 'connectors' && authMode === 'dashboard') void refresh();
  }

  async function logout() {
    // SSO 세션이면 공급자의 로그아웃 주소를 받아 공급자 세션까지 종료합니다.
    const result = await api<{ logout_url?: string }>('/api/v1/session', { method: 'DELETE' });
    if (result.logout_url) {
      window.location.assign(result.logout_url);
      return;
    }
    clearStoredEnrollments();
    connectorState.selectedId = '';
    connectorState.enrollment = null;
    connectorState.enrollmentWasReset = false;
    authMode = 'login';
  }

  onMount(() => {
    window.addEventListener('popstate', handlePopState);
    void (async () => {
      try {
        const status = await api<{ setup_complete: boolean; sign_in?: SignInOptions }>('/api/v1/setup/status');
        if (status.sign_in) signIn = status.sign_in;
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
    let pollCount = 0;
    const timer = window.setInterval(() => {
      if (authMode !== 'dashboard' || loading) return;
      pollCount++;
      if (pollCount % 5 === 0) {
        if (backgroundRefreshBusy) return;
        backgroundRefreshBusy = true;
        void refresh(true).finally(() => { backgroundRefreshBusy = false; });
      } else if (view === 'connectors' && connectorDetailId && data.connectors.find((connector) => connector.id === connectorDetailId)?.status !== 'connected' && !backgroundRefreshBusy) {
        void refreshConnectorStatus().catch(() => {});
      }
    }, connectionPollMs);
    return () => {
      window.clearInterval(timer);
      window.removeEventListener('popstate', handlePopState);
    };
  });
</script>

{#snippet brand()}
  <div class="flex min-w-0 items-center gap-2.5">
    <BrandMark class="size-7 shrink-0" />
    <span class="text-sm font-semibold">Meerkat <span class="font-medium text-muted-foreground">Outlet</span></span>
  </div>
{/snippet}

<Tooltip.Provider>
  {#if authMode === 'setup'}
    <AuthPage mode="setup" onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
  {:else if authMode === 'login'}
    <AuthPage mode="login" {signIn} onLogin={refresh} onSetup={() => { authMode = 'login'; }} />
  {:else if authMode === 'loading'}
    <main class="grid min-h-svh place-items-center"><Spinner class="text-muted-foreground" /></main>
  {:else if authMode === 'unavailable'}
    <main class="grid min-h-svh place-items-center bg-muted p-6">
      <Card.Root class="w-full max-w-md">
        <Card.Header>{@render brand()}</Card.Header>
        <Card.Content>
          <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Title>{t().unavailableTitle}</Alert.Title><Alert.Description>{t().unavailableBody}</Alert.Description></Alert.Root>
        </Card.Content>
        <Card.Footer class="justify-end"><Button onclick={() => location.reload()}>{t().retry}</Button></Card.Footer>
      </Card.Root>
    </main>
  {:else}
    <Sidebar.Provider>
      <AppSidebar {view} domain={data.outlet.domain} version={data.outlet.version} onNavigate={(page) => { void navigate(page); }} onLogout={logout} />
      <Sidebar.Inset class="min-w-0 bg-muted dark:bg-background">
        <header class="sticky top-0 z-20 flex h-12 items-center gap-2 border-b bg-background px-3 md:hidden">
          <Sidebar.Trigger />
          {@render brand()}
          <div class="ms-auto flex items-center gap-0.5">
            <LocaleSwitch align="end" />
            <ThemeToggle align="end" />
            <IconButton label={t().logout} icon={ArrowExit} side="bottom" align="end" onclick={logout} />
          </div>
        </header>
        <div class="mx-auto w-full max-w-5xl min-w-0 flex-1 px-4 pt-4 pb-8 lg:px-6 lg:pt-6" data-content>
          {#if loadError}<Alert.Root variant="destructive" class="mb-4"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>{/if}
          {#if loading}<Spinner class="fixed right-4 bottom-4 z-30 text-muted-foreground" />{/if}
          {#if view === 'overview'}
            <OverviewPage {data} {navigate} onOpenDetail={openConnectorDetail} />
          {:else if view === 'domains'}
            <DomainsPage outlet={data.outlet} records={data.records} onSaved={refresh} detail={domainDetail} onOpenDetail={openDomainDetail} onBack={closeDomainDetail} />
          {:else if view === 'connectors'}
            <ConnectorsPage {data} onChanged={refresh} onCheckConnection={refreshConnectorStatus} detailId={connectorDetailId} onOpenDetail={openConnectorDetail} onBack={closeConnectorDetail} />
          {:else if view === 'access'}
            <AccessPage />
          {:else if view === 'observe'}
            <ObservePage addresses={publicAddresses} />
          {:else if view === 'diagnostics'}
            <DiagnosticsPage />
          {:else if view === 'settings'}
            <SettingsPage />
          {/if}
        </div>
        <div class="mx-auto w-full max-w-5xl px-4 pb-6 lg:px-6"><AppFooter version={data.outlet.version} /></div>
      </Sidebar.Inset>
    </Sidebar.Provider>
  {/if}
</Tooltip.Provider>

<Toaster position="bottom-right" />
