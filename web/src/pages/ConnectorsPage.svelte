<script lang="ts">
  import { onMount } from 'svelte';
  import { api, errorMessage, mockApiEnabled, statusLabel } from '../api';
  import { connectorState, forgetEnrollment, rememberEnrollment, storedEnrollment } from '../connectorState.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import RouteGraph from '../components/RouteGraph.svelte';
  import Status from '../components/Status.svelte';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import InfoTooltip from '../components/ui/InfoTooltip.svelte';
  import LinkButton from '../components/ui/LinkButton.svelte';
  import { connectorDetailPath, pagePath } from '../navigation';
  import type { Connector, DashboardData, EnrollmentToken } from '../types';

  let { data, onChanged, onCheckConnection, detailId, onOpenDetail, onBack }: { data: DashboardData; onChanged: () => Promise<void>; onCheckConnection: () => Promise<void>; detailId: string | null; onOpenDetail: (id: string) => void; onBack: () => void } = $props();
  let name = $state('');
  let serviceName = $state('');
  let serviceHost = $state('');
  let servicePort = $state('');
  let serviceBusy = $state(false);
  let detailTab = $state<'overview' | 'routes'>('overview');
  let serviceError = $state('');
  let createError = $state('');
  let tokenError = $state('');
  let copiedCode = $state('');
  let copyTimer: ReturnType<typeof setTimeout> | undefined;
  let idCopied = $state(false);
  let idCopyTimer: ReturnType<typeof setTimeout> | undefined;
  let idCopyError = $state('');
  let tokenBusy = $state(false);
  let checkingConnection = $state(false);
  let connectionCheckMessage = $state('');
  let now = $state(Date.now());
  let installMethod = $state<'terminal' | 'compose'>('terminal');
  let composeFileMode = $state<'existing' | 'new'>('existing');
  let createBusy = $state(false);
  let deleteBusy = $state(false);
  let deleteError = $state('');
  let deleteName = $state('');
  let deleteTarget = $state<Connector | null>(null);
  const automaticallyIssued = new Set<string>();
  let searchQuery = $state('');
  let connectionFilter = $state<'all' | 'connected' | 'pending' | 'problem'>('all');
  let createDialog: HTMLDialogElement;
  let serviceDialog: HTMLDialogElement;
  let deleteDialog: HTMLDialogElement;
  const composeUpCommand = 'docker compose -f meerkat-tunnel.yaml up -d';

  const selected = $derived(data.connectors.find((connector) => connector.id === detailId));
  const selectedRecords = $derived(data.records.filter((record) => record.connector.id === detailId));
  const selectedPaths = $derived(selectedRecords.flatMap((record) => record.routes.map((route) => ({ record, route }))));
  const invitationExpired = $derived(Boolean(connectorState.enrollment && now >= Date.parse(connectorState.enrollment.expires_at)));
  const filtersActive = $derived(Boolean(searchQuery.trim()) || connectionFilter !== 'all');
  const visibleConnectors = $derived.by(() => {
    const terms = searchQuery.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
    return data.connectors.filter((connector) => {
      if (connectionFilter === 'connected' && connector.status !== 'connected') return false;
      if (connectionFilter === 'pending' && !['draft', 'connecting', 'unknown'].includes(connector.status)) return false;
      if (connectionFilter === 'problem' && !['stale', 'disconnected', 'error'].includes(connector.status)) return false;
      if (!terms.length) return true;
      const searchable = connector.name.toLocaleLowerCase();
      return terms.every((term) => searchable.includes(term));
    });
  });

  onMount(() => {
    const timer = window.setInterval(() => { now = Date.now(); }, 1000);
    return () => window.clearInterval(timer);
  });

  function clearFilters(): void {
    searchQuery = '';
    connectionFilter = 'all';
  }

  function tunnelError(cause: unknown): string {
    const code = errorMessage(cause);
    const labels: Record<string, string> = {
      invalid_connector: '터널 이름을 확인하세요.',
      connector_name_taken: '이미 사용 중인 터널 이름입니다.',
      connector_not_found: '터널을 찾을 수 없습니다.',
    };
    return labels[code] || code;
  }

  $effect(() => {
    const id = detailId || '';
    if (connectorState.selectedId === id) return;
    connectorState.selectedId = id;
    const saved = id ? storedEnrollment(id) : null;
    connectorState.enrollment = saved?.enrollment || null;
    connectorState.enrollmentWasReset = saved?.reset || false;
    tokenError = '';
    connectionCheckMessage = '';
    copiedCode = '';
    if (copyTimer) clearTimeout(copyTimer);
    if (idCopyTimer) clearTimeout(idCopyTimer);
    deleteError = '';
    if (serviceDialog?.open) serviceDialog.close();
    clearServiceForm();
    detailTab = 'overview';
    idCopied = false;
    idCopyError = '';
  });

  $effect(() => {
    const current = selected;
    if (!current) return;
    if (current.status === 'connected' || current.status === 'connecting') {
      forgetEnrollment(current.id);
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      return;
    }
    if (connectorState.enrollment) return;
    const saved = storedEnrollment(current.id);
    if (saved) {
      connectorState.enrollment = saved.enrollment;
      connectorState.enrollmentWasReset = saved.reset;
      return;
    }
    if (current.status !== 'draft' || tokenBusy || automaticallyIssued.has(current.id)) return;
    automaticallyIssued.add(current.id);
    void issueToken(false, current.id);
  });

  function openDetailFromLink(event: MouseEvent, id: string): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpenDetail(id);
  }

  function backFromLink(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onBack();
  }

  function openCreateDialog(): void {
    createError = '';
    createDialog.showModal();
  }

  function closeCreateDialog(): void {
    if (!createBusy) createDialog.close();
  }

  function clearServiceForm(): void {
    serviceName = '';
    serviceHost = '';
    servicePort = '';
    serviceError = '';
  }

  function openServiceDialog(): void {
    if (!selected || selected.status === 'draft') return;
    clearServiceForm();
    serviceDialog.showModal();
  }

  function closeServiceDialog(): void {
    if (!serviceBusy) serviceDialog.close();
  }

  function openDeleteDialog(): void {
    if (!selected) return;
    deleteTarget = selected;
    deleteName = '';
    deleteError = '';
    deleteDialog.showModal();
  }

  function closeDeleteDialog(): void {
    if (!deleteBusy) deleteDialog.close();
  }

  async function createConnector(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    createError = '';
    createBusy = true;
    try {
      const connector = await api<Connector>('/api/v1/connectors', { method: 'POST', body: JSON.stringify({ name }) });
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      name = '';
      await onChanged();
      clearFilters();
      createDialog.close();
      onOpenDetail(connector.id);
    } catch (cause) {
      createError = `터널을 만들지 못했습니다: ${tunnelError(cause)}`;
    } finally {
      createBusy = false;
    }
  }

  async function deleteConnector(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!deleteTarget || deleteName !== deleteTarget.name || deleteBusy) return;
    const connector = deleteTarget;
    deleteError = '';
    deleteBusy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(connector.id)}`, { method: 'DELETE' });
      connectorState.selectedId = '';
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      forgetEnrollment(connector.id);
      await onChanged();
      deleteDialog.close();
      onBack();
    } catch (cause) {
      deleteError = `터널을 삭제하지 못했습니다: ${tunnelError(cause)}`;
    } finally {
      deleteBusy = false;
    }
  }

  async function createService(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!selected || selected.status === 'draft' || serviceBusy) return;
    serviceError = '';
    serviceBusy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}/services`, {
        method: 'POST',
        body: JSON.stringify({ name: serviceName, protocol: 'http', target: { host: serviceHost.trim(), port: Number(servicePort) } }),
      });
      await onChanged();
      serviceDialog.close();
    } catch (cause) {
      const code = errorMessage(cause);
      const message: Record<string, string> = {
        connector_not_found: '터널을 찾을 수 없습니다.',
        service_name_taken: '이미 사용 중인 서비스 이름입니다.',
        service_target_taken: '같은 터널에 동일한 앱 호스트와 포트가 이미 등록되어 있습니다. 기존 서비스에 공개 주소를 추가하세요.',
        invalid_service: '서비스 이름과 앱 주소를 확인하세요.',
      };
      serviceError = `연결 서비스를 등록하지 못했습니다: ${message[code] || code}`;
    } finally {
      serviceBusy = false;
    }
  }

  async function issueToken(reset = false, id = selected?.id) {
    if (!id) return;
    tokenError = '';
    tokenBusy = true;
    try {
      const enrollment = await api<EnrollmentToken>(`/api/v1/connectors/${encodeURIComponent(id)}/enrollment-tokens${reset ? '?reset=true' : ''}`, { method: 'POST' });
      const useResetVolume = reset || (connectorState.selectedId === id && connectorState.enrollmentWasReset);
      rememberEnrollment(id, enrollment, useResetVolume);
      if (connectorState.selectedId === id) {
        connectorState.enrollment = enrollment;
        connectorState.enrollmentWasReset = useResetVolume;
        now = Date.now();
        installMethod = 'terminal';
      }
    } catch (cause) {
      if (connectorState.selectedId === id) tokenError = `설치 안내를 준비하지 못했습니다: ${tunnelError(cause)}`;
    } finally {
      tokenBusy = false;
    }
  }

  function renewExpiredInvitation(): void {
    if (!selected) return;
    if (selected.status !== 'draft') {
      if (!confirm('기존 터널 자격 증명을 무효화하고 설치 명령을 다시 만듭니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) return;
      void issueToken(true);
      return;
    }
    void issueToken();
  }

  async function checkConnection(): Promise<void> {
    if (checkingConnection) return;
    checkingConnection = true;
    connectionCheckMessage = '';
    try {
      await onCheckConnection();
      connectionCheckMessage = selected?.status === 'connected' ? '터널 연결을 확인했습니다.' : '아직 터널 연결이 확인되지 않았습니다.';
    } catch {
      connectionCheckMessage = '연결 상태를 확인하지 못했습니다. 잠시 후 다시 시도하세요.';
    } finally {
      checkingConnection = false;
    }
  }

  async function mockConnect(): Promise<void> {
    if (!mockApiEnabled || !selected) return;
    await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}/mock-connect`, { method: 'POST' });
    await onChanged();
  }

  async function copy(value: string, label: string, key: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      tokenError = '';
      copiedCode = key;
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => { copiedCode = ''; }, 2500);
    } catch {
      copiedCode = '';
      tokenError = `${label}을(를) 복사하지 못했습니다. 내용을 직접 선택해 복사하세요.`;
    }
  }

  async function copyId(): Promise<void> {
    if (!selected) return;
    try {
      await navigator.clipboard.writeText(selected.id);
      idCopied = true;
      idCopyError = '';
      if (idCopyTimer) clearTimeout(idCopyTimer);
      idCopyTimer = setTimeout(() => { idCopied = false; }, 2500);
    } catch {
      idCopied = false;
      idCopyError = '터널 ID를 복사하지 못했습니다.';
    }
  }

  function connectorVolume(): string {
    return `meerkat-${selected?.name || 'connector'}-${connectorState.enrollmentWasReset ? 'data-reset' : 'data'}`;
  }

  function invitation(): string {
    if (!selected || !connectorState.enrollment) return '';
    const payload = JSON.stringify({ v: 1, outlet: `https://${data.outlet.domain}`, name: selected.name, token: connectorState.enrollment.token });
    return btoa(payload).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
  }

  function installCommand(): string {
    if (!selected) return '';
    return [
      `docker run -d --restart unless-stopped --name meerkat-${selected.name}`,
      `-v ${connectorVolume()}:/var/lib/meerkat`,
      `ghcr.io/lawnect/meerkat:latest connect ${invitation()}`,
    ].join(' ' + String.fromCharCode(92) + '\n');
  }

  function composeFile(): string {
    if (!selected) return '';
    const volume = connectorVolume();
    const lines = [
      'services:',
      '  connector:',
      '    image: ghcr.io/lawnect/meerkat:latest',
      `    container_name: meerkat-${selected.name}`,
      '    restart: unless-stopped',
      '    init: true',
      `    command: ["connect", ${JSON.stringify(invitation())}]`,
      '    volumes:',
      `      - ${volume}:/var/lib/meerkat`,
      '',
      'volumes:',
      `  ${volume}:`,
    ];
    return lines.join('\n');
  }

  function existingComposeService(): string {
    if (!selected) return '';
    const lines = [
      '  connector:',
      '    image: ghcr.io/lawnect/meerkat:latest',
      `    container_name: meerkat-${selected.name}`,
      '    restart: unless-stopped',
      '    init: true',
      `    command: ["connect", ${JSON.stringify(invitation())}]`,
      '    volumes:',
      `      - ${connectorVolume()}:/var/lib/meerkat`,
    ];
    return lines.join('\n');
  }

  function existingComposeVolume(): string {
    return `  ${connectorVolume()}:`;
  }

  function onInstallTabKeydown(event: KeyboardEvent): void {
    const methods = ['terminal', 'compose'] as const;
    let next: typeof methods[number];
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') {
      const direction = event.key === 'ArrowRight' ? 1 : -1;
      next = methods[(methods.indexOf(installMethod) + direction + methods.length) % methods.length];
    }
    else if (event.key === 'Home') next = 'terminal';
    else if (event.key === 'End') next = 'compose';
    else return;
    event.preventDefault();
    installMethod = next;
    document.getElementById(`install-${next}-tab-${selected?.id}`)?.focus();
  }

  function onDetailTabKeydown(event: KeyboardEvent): void {
    let next: 'overview' | 'routes';
    if (event.key === 'ArrowLeft' || event.key === 'ArrowRight') next = detailTab === 'overview' ? 'routes' : 'overview';
    else if (event.key === 'Home') next = 'overview';
    else if (event.key === 'End') next = 'routes';
    else return;
    event.preventDefault();
    detailTab = next;
    document.getElementById(`detail-${next}-tab-${selected?.id}`)?.focus();
  }

  function openServiceRoutes(serviceId: string): void {
    detailTab = 'routes';
    requestAnimationFrame(() => {
      const item = document.getElementById(`service-routes-${serviceId}`);
      item?.focus({ preventScroll: true });
      item?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    });
  }

</script>

<div class="connector-page">
  {#if detailId}
    <div class="detail-toolbar"><LinkButton href={pagePath.connectors} variant="text" size="sm" onclick={backFromLink}>← 터널 목록</LinkButton></div>
    {#if selected}
      <div class="detail-header">
        <h2>{selected.name}</h2>
      </div>
      <div class="detail-tabs" role="tablist" aria-label="터널 상세 화면">
        <Button variant="plain" class={detailTab === 'overview' ? 'detail-tab active' : 'detail-tab'} id={`detail-overview-tab-${selected.id}`} role="tab" aria-selected={detailTab === 'overview'} aria-controls={`detail-overview-panel-${selected.id}`} tabindex={detailTab === 'overview' ? 0 : -1} onclick={() => detailTab = 'overview'} onkeydown={onDetailTabKeydown}>개요</Button>
        <Button variant="plain" class={detailTab === 'routes' ? 'detail-tab active' : 'detail-tab'} id={`detail-routes-tab-${selected.id}`} role="tab" aria-selected={detailTab === 'routes'} aria-controls={`detail-routes-panel-${selected.id}`} tabindex={detailTab === 'routes' ? 0 : -1} onclick={() => detailTab = 'routes'} onkeydown={onDetailTabKeydown}>경로 <small>{selectedPaths.length}</small></Button>
      </div>
      <div role="tabpanel" id={`detail-overview-panel-${selected.id}`} aria-labelledby={`detail-overview-tab-${selected.id}`} hidden={detailTab !== 'overview'}>
      <div class="detail-layout">
        <div class="detail-main">
          <Card as="section" padding="none" class="metrics-card" aria-label="터널 현황">
            <div class="ui-card-header"><h3 class="ui-card-title">터널 현황</h3></div>
            <div class="metric-grid">
              <div class="metric"><span class="ui-meta-label">연결 상태</span><strong><Status label={statusLabel(selected.status)} value={selected.status} /></strong></div>
              <div class="metric"><span class="ui-meta-label">연결 서비스</span><strong>{selectedRecords.length}</strong></div>
              <div class="metric"><span class="ui-meta-label">공개 주소</span><strong>{selectedRecords.reduce((total, record) => total + record.routes.length, 0)}</strong></div>
              <div class="metric"><span class="ui-meta-label">최근 응답</span><strong class="metric-date">{selected.last_seen ? new Date(selected.last_seen).toLocaleString('ko-KR', { dateStyle: 'medium', timeStyle: 'short' }) : '기록 없음'}</strong></div>
            </div>
          </Card>
          {#if selected.status !== 'connected' || connectorState.enrollment}
          <Card as="section" padding="none" class="connector-detail" aria-label={`${selected.name} 터널 설치 안내`}>
            <div class="connector-body">
                  <section class="detail-section token-panel">
                    <div class="section-heading"><div><div class="section-title"><h3 class="ui-meta-label">터널 설치 및 연결</h3><InfoTooltip id={`connector-connection-help-${selected.id}`} label="터널 설치 및 연결" description="서버에서 터널을 실행하고 연결 상태를 확인합니다. 앱 주소는 이 터널의 연결 서비스에서 등록합니다." /></div>{#if selected.status !== 'draft'}<p>{selected.status === 'connecting' ? '터널이 연결을 준비 중입니다.' : selected.status === 'connected' ? '터널이 연결되었습니다.' : '터널의 응답이 없습니다. 설치한 컨테이너와 네트워크를 확인하세요.'}</p>{/if}</div><Button variant="secondary" size="sm" class="connection-check" disabled={checkingConnection} onclick={checkConnection}>{checkingConnection ? '확인 중…' : '연결 확인'}</Button></div>
                    {#if connectionCheckMessage}<p class="connection-check-message" role="status">{connectionCheckMessage}</p>{/if}
                    {#if !connectorState.enrollment && selected.status === 'draft'}
                      {#if tokenBusy}<p role="status">설치 명령을 준비하고 있습니다…</p>{:else}<Button variant="secondary" onclick={() => issueToken()}>설치 안내 다시 만들기</Button>{/if}
                    {/if}
                    {#if !connectorState.enrollment && ['stale', 'disconnected', 'error'].includes(selected.status)}<Button variant="secondary" onclick={() => { if (confirm('기존 터널 자격 증명을 무효화하고 설치 명령을 다시 만듭니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) issueToken(true); }} disabled={tokenBusy}>{tokenBusy ? '준비 중…' : '연결 초기화 후 설치 명령 만들기'}</Button>{/if}
                    {#if tokenError}<p class="error" role="alert">{tokenError}</p>{/if}
                    {#if tokenError.includes('already_enrolled') && selected.status === 'draft'}
                      <Button variant="secondary" class="reset-token-button" onclick={() => { if (confirm('기존 터널 자격 증명을 무효화하고 설치 명령을 다시 만듭니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) issueToken(true); }}>연결 초기화 후 설치 명령 만들기</Button>
                    {/if}
                    {#if connectorState.enrollment}
                      {@const enrollment = connectorState.enrollment}
                      <div class="token-box">
                        <div class:expired={invitationExpired} class="install-validity" role="status">
                          <strong>{invitationExpired ? '설치 명령이 만료되었습니다' : '설치 명령 유효 시간'}</strong>
                          <span>{new Date(enrollment.expires_at).toLocaleString('ko-KR', { dateStyle: 'medium', timeStyle: 'short' })}{invitationExpired ? '에 만료됨' : '까지'}</span>
                        </div>
                        {#if invitationExpired}
                          <p>만료된 설치 명령은 사용할 수 없습니다. 새 명령을 발급받아 Docker 명령이나 Compose 파일의 초대 코드를 교체하세요.</p>
                          <Button variant="secondary" disabled={tokenBusy} onclick={renewExpiredInvitation}>{tokenBusy ? '발급 중…' : '설치 명령 재발급'}</Button>
                        {:else}
                        <div class="install-tabs" role="tablist" aria-label="터널 설치 방법">
                          <Button variant="plain" class={installMethod === 'terminal' ? 'install-tab active' : 'install-tab'} id={`install-terminal-tab-${selected.id}`} role="tab" aria-selected={installMethod === 'terminal'} aria-controls={`install-terminal-panel-${selected.id}`} tabindex={installMethod === 'terminal' ? 0 : -1} onclick={() => installMethod = 'terminal'} onkeydown={onInstallTabKeydown}>Docker 명령</Button>
                          <Button variant="plain" class={installMethod === 'compose' ? 'install-tab active' : 'install-tab'} id={`install-compose-tab-${selected.id}`} role="tab" aria-selected={installMethod === 'compose'} aria-controls={`install-compose-panel-${selected.id}`} tabindex={installMethod === 'compose' ? 0 : -1} onclick={() => installMethod = 'compose'} onkeydown={onInstallTabKeydown}>Docker Compose</Button>
                        </div>
                        <div class="install-panel" role="tabpanel" id={`install-terminal-panel-${selected.id}`} aria-labelledby={`install-terminal-tab-${selected.id}`} tabindex="0" hidden={installMethod !== 'terminal'}>
                          <p>터널을 실행할 서버에서 아래 명령을 실행하세요. 인증 정보는 Docker 볼륨에 보관됩니다.</p>
                          {#if connectorState.enrollmentWasReset}<p>기존 터널 컨테이너를 중지·삭제한 뒤 새 명령을 실행하세요.</p>{/if}
                          <div class="command-heading"><strong class="ui-meta-label">실행 명령</strong></div>
                          <div class="code-block"><pre>{installCommand()}</pre><Button variant="plain" class={copiedCode === 'terminal' ? 'code-copy copied' : 'code-copy'} aria-label={copiedCode === 'terminal' ? '실행 명령 복사 완료' : '실행 명령 복사'} title={copiedCode === 'terminal' ? '복사 완료' : '실행 명령 복사'} onclick={() => copy(installCommand(), '실행 명령', 'terminal')}>{#if copiedCode === 'terminal'}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></div>
                        </div>
                        <div class="install-panel" role="tabpanel" id={`install-compose-panel-${selected.id}`} aria-labelledby={`install-compose-tab-${selected.id}`} tabindex="0" hidden={installMethod !== 'compose'}>
                          <div class="compose-choice" role="group" aria-label="Compose 파일 사용 방법">
                            <Button variant="secondary" size="sm" aria-pressed={composeFileMode === 'existing'} onclick={() => composeFileMode = 'existing'}>기존 파일에 추가</Button>
                            <Button variant="secondary" size="sm" aria-pressed={composeFileMode === 'new'} onclick={() => composeFileMode = 'new'}>새 파일로 만들기</Button>
                          </div>
                          {#if composeFileMode === 'existing'}
                          <p>기존 Compose 파일의 <code>services</code>에 터널 서비스를 추가합니다. 앱 네트워크는 연결 서비스를 등록할 때 설정할 수 있습니다.</p>
                          <div class="command-heading"><strong class="ui-meta-label">기존 파일의 services 아래에 추가</strong></div>
                          <div class="code-block"><pre>{existingComposeService()}</pre><Button variant="plain" class={copiedCode === 'existing-service' ? 'code-copy copied' : 'code-copy'} aria-label={copiedCode === 'existing-service' ? '터널 서비스 설정 복사 완료' : '터널 서비스 설정 복사'} title={copiedCode === 'existing-service' ? '복사 완료' : '터널 서비스 설정 복사'} onclick={() => copy(existingComposeService(), '터널 서비스 설정', 'existing-service')}>{#if copiedCode === 'existing-service'}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></div>
                          <div class="command-heading"><strong class="ui-meta-label">기존 파일의 최상위 volumes 아래에 추가</strong></div>
                          <div class="code-block"><pre>{existingComposeVolume()}</pre><Button variant="plain" class={copiedCode === 'existing-volume' ? 'code-copy copied' : 'code-copy'} aria-label={copiedCode === 'existing-volume' ? '볼륨 설정 복사 완료' : '볼륨 설정 복사'} title={copiedCode === 'existing-volume' ? '복사 완료' : '볼륨 설정 복사'} onclick={() => copy(existingComposeVolume(), '볼륨 설정', 'existing-volume')}>{#if copiedCode === 'existing-volume'}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></div>
                          <p>이미 <code>connector</code> 서비스가 있다면 새로 추가하지 말고 해당 설정을 갱신하세요. <code>volumes:</code>가 없다면 최상위에 먼저 추가하세요. 등록 후에는 일회용 초대 코드가 들어간 command 줄을 삭제하세요.</p>
                          {:else}
                          <p>기존 Compose 파일을 수정하지 않고, 아래 내용을 터널 전용 <code>meerkat-tunnel.yaml</code> 파일로 저장하세요.</p>
                          <div class="command-heading"><strong class="ui-meta-label">meerkat-tunnel.yaml</strong></div>
                          <div class="code-block"><pre>{composeFile()}</pre><Button variant="plain" class={copiedCode === 'compose-file' ? 'code-copy copied' : 'code-copy'} aria-label={copiedCode === 'compose-file' ? 'Compose 파일 복사 완료' : 'Compose 파일 복사'} title={copiedCode === 'compose-file' ? '복사 완료' : 'Compose 파일 복사'} onclick={() => copy(composeFile(), 'Compose 파일', 'compose-file')}>{#if copiedCode === 'compose-file'}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></div>
                          <div class="command-heading"><strong class="ui-meta-label">실행</strong></div>
                          <div class="code-block"><pre>{composeUpCommand}</pre><Button variant="plain" class={copiedCode === 'compose-up' ? 'code-copy copied' : 'code-copy'} aria-label={copiedCode === 'compose-up' ? '실행 명령 복사 완료' : '실행 명령 복사'} title={copiedCode === 'compose-up' ? '복사 완료' : '실행 명령 복사'} onclick={() => copy(composeUpCommand, '실행 명령', 'compose-up')}>{#if copiedCode === 'compose-up'}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></div>
                          <p>등록이 끝나면 파일에서 일회용 초대 코드가 들어간 <code>command</code> 줄을 삭제하고 터널 컨테이너를 다시 시작하세요. 인증 정보는 Docker 볼륨에 남습니다.</p>
                          {/if}
                        </div>
                        <p>설치 명령의 초대 코드는 최초 등록에 한 번만 사용할 수 있습니다. 새 명령을 발급하면 이전 코드는 무효가 됩니다.</p>
                        {#if mockApiEnabled}<div class="mock-connect"><span>미리보기에서는 실제 터널이 연결되지 않습니다.</span><Button variant="secondary" size="sm" onclick={mockConnect}>연결 상태로 전환</Button></div>{/if}
                        {/if}
                      </div>
                    {/if}
                  </section>
            </div>
          </Card>
          {/if}
          {#if selected.status !== 'draft'}
            <Card as="section" padding="none" aria-label="연결 서비스">
              <div class="ui-card-header service-card-header"><div class="section-title"><h3 class="ui-card-title">연결 서비스 <small>{selectedRecords.length}</small></h3><InfoTooltip id={`connector-service-help-${selected.id}`} label="연결 서비스" description="이 터널이 요청을 전달할 앱의 내부 주소입니다. 한 터널에 여러 서비스를 연결할 수 있습니다." /></div><Button variant="secondary" size="sm" onclick={openServiceDialog}>+ 서비스 추가</Button></div>
              <div class="connected-card-body">
                {#if selectedRecords.length}
                  {#each selectedRecords as record (record.service.id)}
                    <div class="service-summary">
                      <ServiceBlock {record} outlet={data.outlet} {onChanged} view="service" />
                      <Button variant="text" size="sm" onclick={() => openServiceRoutes(record.service.id)}>공개 주소 {record.routes.length}개 · 경로 보기 →</Button>
                    </div>
                  {/each}
                {:else}
                  <p>아직 연결된 서비스가 없습니다. 이 터널에서 접속할 앱을 등록하세요.</p>
                {/if}
              </div>
            </Card>
          {/if}
        </div>
        <aside class="detail-sidebar" aria-label="터널 상세 정보와 관리">
          <Card as="section" padding="none" class="info-card">
            <div class="ui-card-header"><h3 class="ui-card-title">터널 상세 정보</h3></div>
            <div class="sidebar-body">
              <dl>
                <div><dt class="ui-meta-label">이름</dt><dd>{selected.name}</dd></div>
                <div><dt class="ui-meta-label">터널 ID</dt><dd class="id-value"><code>{selected.id}</code><Button variant="plain" size="sm" class={idCopied ? 'id-copy copied' : 'id-copy'} aria-label={idCopied ? '터널 ID 복사 완료' : '터널 ID 복사'} title={idCopied ? '복사 완료' : '터널 ID 복사'} onclick={copyId}>{#if idCopied}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></dd>{#if idCopyError}<dd class="error" role="alert">{idCopyError}</dd>{/if}</div>
                <div><dt class="ui-meta-label">생성일</dt><dd>{new Date(selected.created_at).toLocaleString('ko-KR', { dateStyle: 'medium', timeStyle: 'short' })}</dd></div>
              </dl>
            </div>
          </Card>
          <Card as="section" padding="none" class="management-card">
            <div class="ui-card-header"><h3 class="ui-card-title">터널 관리</h3></div>
            <div class="sidebar-body">
              <p>터널을 삭제하면 연결 서비스와 공개 주소도 함께 삭제됩니다.</p>
              <Button variant="danger" size="sm" onclick={openDeleteDialog}>터널 삭제</Button>
            </div>
          </Card>
        </aside>
      </div>
      </div>
      <div role="tabpanel" id={`detail-routes-panel-${selected.id}`} aria-labelledby={`detail-routes-tab-${selected.id}`} hidden={detailTab !== 'routes'}>
        {#if selected.status === 'draft'}
          <Card as="section"><div class="routes-start"><p>터널이 연결되면 서비스를 등록하고 공개 주소를 추가할 수 있습니다.</p><Button variant="secondary" size="sm" onclick={() => detailTab = 'overview'}>설치 안내 보기</Button></div></Card>
        {:else}
          <div class="routes-content">
            <Card as="section" padding="none" class="paths-card" aria-label="요청 경로">
              <div class="ui-card-header"><h3 class="ui-card-title">요청 경로 <small>{selectedPaths.length}</small></h3></div>
              {#if selectedRecords.length}
                <RouteGraph tunnelName={selected.name} records={selectedRecords} />
              {:else}
                <p class="paths-empty">아직 공개 주소가 없습니다. 아래 서비스에서 주소를 추가하세요.</p>
              {/if}
            </Card>
            <Card as="section" padding="none" aria-label="서비스별 공개 주소">
              <div class="ui-card-header"><h3 class="ui-card-title">서비스별 공개 주소 <small>{selectedRecords.length}</small></h3></div>
              <div class="connected-card-body">
                {#if selectedRecords.length}
                  {#each selectedRecords as record (record.service.id)}
                    <div class="route-service-item" id={`service-routes-${record.service.id}`} tabindex="-1"><ServiceBlock {record} outlet={data.outlet} {onChanged} view="routes" /></div>
                  {/each}
                {:else}
                  <p>먼저 연결 서비스를 등록하세요. <Button variant="text" size="sm" onclick={() => detailTab = 'overview'}>개요에서 서비스 추가 →</Button></p>
                {/if}
              </div>
            </Card>
          </div>
        {/if}
      </div>
    {:else}
      <Card as="section" class="detail-missing"><p>터널을 찾을 수 없습니다. 삭제되었거나 주소가 잘못되었습니다.</p><LinkButton href={pagePath.connectors} variant="secondary" size="sm" onclick={backFromLink}>터널 목록으로</LinkButton></Card>
    {/if}
  {:else}
    <div class="connector-toolbar"><h2>터널 <small>{data.connectors.length}</small></h2><Button variant="secondary" size="sm" onclick={openCreateDialog}>+ 새로 만들기</Button></div>
    {#if data.connectors.length}
      <div class="connector-filters" role="search" aria-label="터널 검색 및 필터">
        <div class="field search-field"><label for="connector-search">검색</label><input class="input input-bordered" id="connector-search" type="search" bind:value={searchQuery} placeholder="터널 이름 검색" autocomplete="off" /></div>
        <div class="field"><label for="connection-filter">연결 상태</label><select id="connection-filter" bind:value={connectionFilter}><option value="all">전체 상태</option><option value="connected">연결됨</option><option value="pending">연결 대기</option><option value="problem">연결 문제</option></select></div>
      </div>
      {#if filtersActive}<div class="filter-summary"><span aria-live="polite">{data.connectors.length}개 중 {visibleConnectors.length}개 표시</span><Button variant="text" size="sm" onclick={clearFilters}>초기화</Button></div>{/if}
      {#if visibleConnectors.length}
        <Card as="section" padding="none" class="tunnel-table-card" aria-label="터널 목록">
          <div class="table-scroll">
            <table class="tunnel-table">
              <thead><tr><th scope="col">이름</th><th scope="col">연결 상태</th><th scope="col">연결 서비스</th><th scope="col">최근 응답</th><th scope="col">생성일</th><th scope="col">작업</th></tr></thead>
              <tbody>
                {#each visibleConnectors as connector (connector.id)}
                  {@const serviceCount = data.services.filter((service) => service.connector_id === connector.id).length}
                  <tr>
                    <td><LinkButton href={connectorDetailPath(connector.id)} variant="text" size="sm" class="tunnel-name" onclick={(event) => openDetailFromLink(event, connector.id)}>{connector.name}</LinkButton></td>
                    <td><Status label={statusLabel(connector.status)} value={connector.status} /></td>
                    <td><span class:muted-cell={!serviceCount}>{serviceCount ? `${serviceCount}개` : '미등록'}</span></td>
                    <td><span class:muted-cell={!connector.last_seen}>{connector.last_seen ? new Date(connector.last_seen).toLocaleString('ko-KR', { dateStyle: 'medium', timeStyle: 'short' }) : '—'}</span></td>
                    <td>{new Date(connector.created_at).toLocaleDateString('ko-KR')}</td>
                    <td><LinkButton href={connectorDetailPath(connector.id)} variant="text" size="sm" class="detail-link" aria-label={`${connector.name} 터널 상세 보기`} onclick={(event) => openDetailFromLink(event, connector.id)}>상세 →</LinkButton></td>
                  </tr>
                {/each}
              </tbody>
            </table>
          </div>
        </Card>
      {:else}
        <p class="list-empty filter-empty">검색 조건에 맞는 터널이 없습니다.</p>
      {/if}
    {:else}
      <p class="list-empty">아직 터널이 없습니다. 새로 만들기를 눌러 시작하세요.</p>
    {/if}
  {/if}
</div>

<dialog bind:this={serviceDialog} class="connector-dialog service-dialog" aria-labelledby="service-dialog-title" aria-describedby="service-dialog-description" oncancel={(event) => { if (serviceBusy) event.preventDefault(); }} onclose={clearServiceForm}>
  <form onsubmit={createService}>
    <h2 id="service-dialog-title">연결 서비스 추가</h2>
    <p id="service-dialog-description">{selected?.name || '선택한 터널'}에서 연결할 앱의 내부 주소를 입력하세요. 서비스 이름과 앱 호스트는 서로 다를 수 있습니다.</p>
    <div class="service-fields">
      <div class="field service-name-field"><label for="connector-service-name">서비스 이름</label><input class="input input-bordered" id="connector-service-name" bind:value={serviceName} placeholder="예: app" required /></div>
      <div class="field"><label for="connector-service-host">앱 호스트</label><input class="input input-bordered" id="connector-service-host" bind:value={serviceHost} placeholder="예: app" aria-describedby="connector-service-target-help" required /></div>
      <div class="field"><label for="connector-service-port">내부 포트</label><input class="input input-bordered" id="connector-service-port" type="number" min="1" max="65535" bind:value={servicePort} placeholder="예: 80" aria-describedby="connector-service-target-help" required /></div>
    </div>
    <p id="connector-service-target-help">같은 Compose 파일이면 앱 호스트에 <code>services:</code> 아래 이름(예: <code>app</code>), 내부 포트에 앱이 컨테이너에서 듣는 포트(예: <code>80</code>)를 입력하세요. 터널과 앱은 같은 Docker 네트워크에 있어야 합니다.</p>
    {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" disabled={serviceBusy} onclick={closeServiceDialog}>취소</Button><Button type="submit" disabled={serviceBusy}>{serviceBusy ? '등록 중…' : '서비스 등록'}</Button></div>
  </form>
</dialog>

<dialog bind:this={createDialog} class="connector-dialog" aria-labelledby="create-dialog-title" aria-describedby="create-dialog-description" oncancel={(event) => { if (createBusy) event.preventDefault(); }} onclose={() => { name = ''; createError = ''; }}>
  <form onsubmit={createConnector}>
    <h2 id="create-dialog-title">새 터널 만들기</h2>
    <p id="create-dialog-description">터널을 설치한 뒤 여러 연결 서비스를 등록할 수 있습니다.</p>
    <div class="field"><label for="connector-name">터널 이름</label><input class="input input-bordered" id="connector-name" bind:value={name} placeholder="예: app-tunnel" required /></div>
    {#if createError}<p class="error" role="alert">{createError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" onclick={closeCreateDialog} disabled={createBusy}>취소</Button><Button type="submit" disabled={createBusy}>{createBusy ? '만드는 중…' : '터널 만들기'}</Button></div>
  </form>
</dialog>

<dialog bind:this={deleteDialog} class="connector-dialog" aria-labelledby="delete-dialog-title" aria-describedby="delete-dialog-description" oncancel={(event) => { if (deleteBusy) event.preventDefault(); }} onclose={() => { deleteName = ''; deleteError = ''; deleteTarget = null; }}>
  <form onsubmit={deleteConnector}>
    <h2 id="delete-dialog-title">터널 삭제</h2>
    <p id="delete-dialog-description">연결 서비스의 등록 정보와 공개 주소가 삭제되고 실행 중인 연결이 끊깁니다. 실제 서비스는 삭제되지 않습니다.</p>
    <div class="field"><label for="delete-connector-name">삭제 확인: <strong>{deleteTarget?.name}</strong> 입력</label><input class="input input-bordered" id="delete-connector-name" bind:value={deleteName} autocomplete="off" spellcheck="false" required /></div>
    {#if deleteError}<p class="error" role="alert">{deleteError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" onclick={closeDeleteDialog} disabled={deleteBusy}>취소</Button><Button variant="danger" type="submit" disabled={deleteBusy || deleteName !== deleteTarget?.name}>{deleteBusy ? '삭제 중…' : '터널 삭제'}</Button></div>
  </form>
</dialog>

<style>
  .connector-page { min-width: 0; }
  .connector-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
  .connector-toolbar h2 { margin: 0; font-size: 16px; letter-spacing: -.025em; }
  .connector-toolbar small { margin-left: 5px; color: var(--muted); font-size: 12px; font-weight: 500; }
  .connector-filters { display: grid; grid-template-columns: minmax(0, 2fr) minmax(130px, 1fr); gap: 10px; width: 50%; margin: 0 0 12px auto; }
  .connector-filters input, .connector-filters select { width: 100%; min-height: 34px; }
  .connector-filters select { border: 1px solid #d8d1c9; border-radius: 6px; padding: 5px 8px; background: #fffdfa; color: var(--ink); }
  .connector-filters select:focus-visible { outline: 2px solid #ef9b6f; outline-offset: 2px; }
  .filter-summary { display: flex; align-items: center; justify-content: space-between; gap: 12px; width: 50%; margin: -2px 0 8px auto; color: var(--muted); font-size: 12px; }
  .filter-empty { border: 1px dashed var(--line); border-radius: 8px; padding: 26px 18px; text-align: center; }
  .table-scroll { overflow-x: auto; border-radius: inherit; }
  .tunnel-table { width: 100%; border-collapse: collapse; text-align: left; }
  .tunnel-table th, .tunnel-table td { border-bottom: 1px solid var(--line); padding: 13px 16px; white-space: nowrap; }
  .tunnel-table th { color: var(--muted); font-size: 11px; font-weight: 700; }
  .tunnel-table td { font-size: 12px; }
  .tunnel-table th:first-child, .tunnel-table td:first-child { width: 28%; padding-left: 20px; }
  .tunnel-table th:last-child, .tunnel-table td:last-child { padding-right: 20px; text-align: right; }
  .tunnel-table tbody tr:last-child td { border-bottom: 0; }
  .tunnel-table tbody tr:hover { background: rgba(246, 239, 232, .55); }
  .tunnel-table :global(.tunnel-name) { max-width: 100%; overflow: hidden; padding: 0; color: var(--ink); font-size: 13px; font-weight: 650; text-decoration: underline; text-underline-offset: 3px; text-overflow: ellipsis; }
  .tunnel-table :global(.detail-link) { padding: 2px 0; color: var(--accent-dark); font-size: 12px; }
  .tunnel-table .muted-cell { color: var(--muted); }
  .detail-toolbar { margin-bottom: 10px; }
  .detail-toolbar :global(.ui-button) { padding-left: 0; }
  .detail-header { margin-bottom: 16px; }
  .detail-header h2 { margin: 0; font-size: 20px; letter-spacing: -.025em; }
  .detail-tabs { display: flex; gap: 6px; margin-bottom: 16px; border-bottom: 1px solid var(--line); }
  .detail-tabs :global(.detail-tab) { border-bottom: 2px solid transparent; padding: 9px 16px; color: var(--muted); font-size: 13px; font-weight: 650; }
  .detail-tabs :global(.detail-tab:hover), .detail-tabs :global(.detail-tab.active) { color: var(--accent-dark); }
  .detail-tabs :global(.detail-tab.active) { border-bottom-color: var(--accent); }
  .detail-tabs :global(.detail-tab small) { margin-left: 4px; font-size: 11px; font-weight: 500; }
  .detail-layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(240px, 280px); align-items: start; gap: 16px; }
  .detail-main, .detail-sidebar { display: grid; gap: 14px; min-width: 0; }
  .routes-content { display: grid; gap: 14px; }
  .routes-start { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 12px; }
  .routes-start p { margin: 0; color: var(--muted); font-size: 12px; }
  .metric-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); }
  .metric { display: grid; align-content: center; gap: 7px; min-height: 86px; padding: 14px 18px; }
  .metric + .metric { border-left: 1px solid var(--line); }
  .metric strong { font-size: 19px; font-weight: 700; line-height: 1.3; }
  .metric .metric-date { font-size: 12px; font-weight: 650; }
  .metric :global(.connection-status) { color: var(--ink); font-size: 13px; font-weight: 650; }
  .connector-body { padding: 0 18px 16px; }
  .connector-body .detail-section:first-child { margin-top: 0; border-top: 0; padding-top: 0; }
  .connector-body .token-panel:first-child { padding-top: 18px; }
  .service-card-header { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 8px; }
  .connected-card-body { padding: 16px 18px; }
  .connected-card-body .service-summary { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 10px 16px; }
  .service-summary :global(.service) { flex: 1 1 280px; }
  .service-summary > :global(.ui-button) { margin-left: auto; }
  .connected-card-body .service-summary + .service-summary { border-top: 1px solid var(--line); margin-top: 16px; padding-top: 16px; }
  .connected-card-body .route-service-item + .route-service-item { border-top: 1px solid var(--line); margin-top: 16px; padding-top: 16px; }
  .route-service-item:focus { outline: 2px solid var(--accent); outline-offset: 5px; border-radius: 3px; }
  .connected-card-body p { margin: 0; color: var(--muted); font-size: 12px; }
  .paths-empty { margin: 0; padding: 18px; color: var(--muted); font-size: 12px; }
  :global(.detail-missing) { display: grid; justify-items: start; gap: 12px; }
  :global(.detail-missing p) { margin: 0; }
  .list-empty { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
  .connector-dialog { width: min(440px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .connector-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .connector-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .connector-dialog p:not(.error) { margin: 0 0 16px; color: var(--muted); font-size: 12px; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
  .detail-section { margin-top: 16px; border-top: 1px solid var(--line); padding-top: 15px; }
  .section-heading { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 10px; margin-bottom: 8px; }
  .section-heading :global(.connection-check) { margin-left: auto; }
  .connection-check-message { margin: 0 0 8px; color: var(--muted); font-size: 12px; }
  .section-title { display: flex; align-items: center; gap: 5px; }
  .section-heading h3 { margin: 0; }
  .section-heading p { margin: 4px 0 0; color: var(--muted); font-size: 12px; line-height: 1.5; }
  .service-dialog { width: min(500px, calc(100vw - 32px)); }
  .service-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .service-name-field { grid-column: 1 / -1; }
  .service-fields input { width: 100%; }
  .mock-connect { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--muted); font-size: 12px; }
  .token-panel :global(.reset-token-button) { margin-top: 10px; }
  .token-box { display: grid; gap: 10px; margin-top: 4px; }
  .command-heading { display: flex; align-items: flex-start; justify-content: space-between; flex-wrap: wrap; gap: 8px 16px; }
  .token-box p { margin: 2px 0 0 !important; color: var(--muted); font-size: 12px; }
  .install-validity { display: flex; align-items: baseline; flex-wrap: wrap; gap: 4px 10px; border: 1px solid var(--line); border-radius: 6px; padding: 10px 12px; color: var(--ink); font-size: 12px; }
  .install-validity span { color: var(--muted); }
  .install-validity.expired { border-color: var(--accent); color: var(--accent-dark); }
  .install-tabs { display: flex; flex-wrap: wrap; gap: 6px; margin-top: 4px; border-bottom: 1px solid var(--line); }
  .install-tabs :global(.install-tab) { border-bottom: 2px solid transparent; padding: 7px 12px; color: var(--muted); font-size: 12px; font-weight: 650; }
  .install-tabs :global(.install-tab:hover), .install-tabs :global(.install-tab.active) { color: var(--accent-dark); }
  .install-tabs :global(.install-tab.active) { border-bottom-color: var(--accent); }
  .install-panel { display: grid; gap: 8px; min-width: 0; }
  .install-panel[hidden] { display: none; }
  .compose-choice { display: flex; flex-wrap: wrap; gap: 6px; margin: 4px 0 2px; }
  .compose-choice :global(.ui-button[aria-pressed="true"]) { border-color: var(--accent); background: #fff3eb; color: var(--accent-dark); }
  .install-panel .command-heading { align-items: center; margin-top: 6px; }
  .install-panel code { overflow-wrap: anywhere; font-size: 11px; }
  .code-block { position: relative; min-width: 0; }
  .code-block pre { overflow-x: auto; overflow-wrap: anywhere; margin: 0; border-radius: 6px; padding: 12px 48px 12px 14px; background: #242a2f; color: #f5f3ec; font: 12px/1.55 ui-monospace, SFMono-Regular, monospace; white-space: pre-wrap; }
  .code-block :global(.code-copy) { position: absolute; top: 8px; right: 8px; display: grid; place-items: center; width: 30px; height: 30px; border: 1px solid #59636a; border-radius: 5px; background: #343b40; color: #f5f3ec; cursor: pointer; }
  .code-block :global(.code-copy:hover) { border-color: #aeb7b3; background: #454d51; }
  .code-block :global(.code-copy.copied), .code-block :global(.code-copy.copied:hover) { border-color: #65b48d; background: #234737; color: #a8e7c4; }
  .code-block :global(.code-copy svg) { width: 16px; height: 16px; }
  .sidebar-body { padding: 16px 18px; }
  .sidebar-body dl { display: grid; gap: 14px; margin: 0; }
  .sidebar-body dl > div { min-width: 0; }
  .sidebar-body dt { margin-bottom: 4px; }
  .sidebar-body dd { margin: 0; font-size: 12px; font-weight: 600; }
  .id-value { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .id-value code { overflow-wrap: anywhere; font-size: 11px; font-weight: 500; }
  .id-value :global(.id-copy) { display: grid; flex: none; place-items: center; width: 26px; height: 26px; border: 1px solid var(--line); border-radius: 5px; color: var(--muted); }
  .id-value :global(.id-copy svg) { width: 14px; height: 14px; }
  .id-value :global(.id-copy.copied) { border-color: var(--green); color: var(--green); }
  .sidebar-body > p { margin: 0 0 12px; color: var(--muted); font-size: 11px; line-height: 1.5; }
  @media (max-width: 1000px) { .detail-layout { grid-template-columns: 1fr; } .detail-sidebar { grid-template-columns: repeat(2, minmax(0, 1fr)); } }
  @media (max-width: 700px) { .connector-filters, .filter-summary { width: 100%; } }
  @media (max-width: 480px) { .service-fields { grid-template-columns: 1fr; } }
  @media (max-width: 600px) { .metric-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .metric:nth-child(3) { border-left: 0; } .metric:nth-child(n+3) { border-top: 1px solid var(--line); } .detail-sidebar { grid-template-columns: 1fr; } }
  @media (max-width: 420px) { .connector-filters { grid-template-columns: 1fr; } }
</style>
