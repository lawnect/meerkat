<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import { connectorState } from '../connectorState.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import Status from '../components/Status.svelte';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import type { Connector, DashboardData, EnrollmentToken } from '../types';

  let { data, onChanged }: { data: DashboardData; onChanged: () => Promise<void> } = $props();
  let name = $state('');
  let serviceName = $state('');
  let host = $state('');
  let port = $state('');
  let createError = $state('');
  let serviceError = $state('');
  let tokenError = $state('');
  let tokenBusy = $state(false);
  let createBusy = $state(false);
  let serviceBusy = $state(false);
  let deleteBusy = $state(false);
  let deleteError = $state('');
  let deleteName = $state('');
  let deleteTarget = $state<Connector | null>(null);
  let openedInitialConnector = $state(false);
  let searchQuery = $state('');
  let connectionFilter = $state<'all' | 'connected' | 'pending' | 'problem'>('all');
  let createDialog: HTMLDialogElement;
  let deleteDialog: HTMLDialogElement;

  const selected = $derived(data.connectors.find((connector) => connector.id === connectorState.selectedId));
  const records = $derived(data.records.filter((record) => record.connector.id === connectorState.selectedId));
  const service = $derived(records[0]?.service);
  const filtersActive = $derived(Boolean(searchQuery.trim()) || connectionFilter !== 'all');
  const visibleConnectors = $derived.by(() => {
    const terms = searchQuery.trim().toLocaleLowerCase().split(/\s+/).filter(Boolean);
    return data.connectors.filter((connector) => {
      if (connectionFilter === 'connected' && connector.status !== 'connected') return false;
      if (connectionFilter === 'pending' && !['draft', 'connecting', 'unknown'].includes(connector.status)) return false;
      if (connectionFilter === 'problem' && !['stale', 'disconnected', 'error'].includes(connector.status)) return false;
      const connectorRecords = data.records.filter((record) => record.connector.id === connector.id);
      if (!terms.length) return true;
      const searchable = [connector.name, ...connectorRecords.flatMap((record) => [record.service.name, record.service.target.host, String(record.service.target.port), ...record.routes.map((route) => route.hostname)])].join(' ').toLocaleLowerCase();
      return terms.every((term) => searchable.includes(term));
    });
  });

  function clearFilters(): void {
    searchQuery = '';
    connectionFilter = 'all';
  }

  $effect(() => {
    if (!openedInitialConnector && data.connectors.length) {
      openedInitialConnector = true;
      if (!connectorState.selectedId || !data.connectors.some((connector) => connector.id === connectorState.selectedId)) {
        connectorState.selectedId = data.connectors[0].id;
      }
    } else if (connectorState.selectedId && !data.connectors.some((connector) => connector.id === connectorState.selectedId)) {
      connectorState.selectedId = '';
    }
  });

  function toggleConnector(id: string): void {
    connectorState.selectedId = connectorState.selectedId === id ? '' : id;
    connectorState.enrollment = null;
    connectorState.enrollmentWasReset = false;
    tokenError = '';
    deleteError = '';
  }

  function openCreateDialog(): void {
    createError = '';
    createDialog.showModal();
  }

  function closeCreateDialog(): void {
    if (!createBusy) createDialog.close();
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
      connectorState.selectedId = connector.id;
      createDialog.close();
    } catch (cause) {
      createError = `Connector를 만들지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      createBusy = false;
    }
  }

  async function createService(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!selected) return;
    serviceError = '';
    serviceBusy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}/services`, {
        method: 'POST',
        body: JSON.stringify({ name: serviceName, protocol: 'http', target: { host, port: Number(port) } }),
      });
      serviceName = '';
      host = '';
      port = '';
      await onChanged();
    } catch (cause) {
      serviceError = `연결 서비스를 등록하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      serviceBusy = false;
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
      await onChanged();
      deleteDialog.close();
    } catch (cause) {
      deleteError = `Connector를 삭제하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      deleteBusy = false;
    }
  }

  async function issueToken(reset = false) {
    if (!selected) return;
    tokenError = '';
    tokenBusy = true;
    try {
      connectorState.enrollment = await api<EnrollmentToken>(`/api/v1/connectors/${encodeURIComponent(selected.id)}/enrollment-tokens${reset ? '?reset=true' : ''}`, { method: 'POST' });
      connectorState.enrollmentWasReset = reset;
    } catch (cause) {
      tokenError = `토큰을 발급하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      tokenBusy = false;
    }
  }

  async function copy(value: string, label: string): Promise<void> {
    try {
      await navigator.clipboard.writeText(value);
      tokenError = `${label}을(를) 복사했습니다.`;
      setTimeout(() => { if (tokenError.startsWith(label)) tokenError = ''; }, 2500);
    } catch {
      tokenError = '클립보드에 복사하지 못했습니다. 내용을 직접 선택해 복사하세요.';
    }
  }

  function installCommand(): string {
    if (!selected || !service) return '';
    const token = connectorState.enrollment?.token || '';
    const volume = connectorState.enrollmentWasReset ? 'meerkat-data-reset' : 'meerkat-data';
    return [
      `docker run --rm --name meerkat-${selected.name}`,
      `-e MEERKAT_OUTLET=https://${data.outlet.domain}`,
      `-e MEERKAT_NAME=${selected.name}`,
      `-e MEERKAT_TARGET=${service.target.host}:${service.target.port}`,
      `-e MEERKAT_ENROLLMENT_TOKEN='${token}'`,
      `-v "$PWD/${volume}:/var/lib/meerkat"`,
      'ghcr.io/lawnect/meerkat:latest',
    ].join(' ' + String.fromCharCode(92) + '\n');
  }

</script>

<div class="connector-page">
  <div class="connector-toolbar"><h2>Connector <small>{data.connectors.length}</small></h2><Button variant="secondary" size="sm" onclick={openCreateDialog}>+ 새로 만들기</Button></div>

  {#if data.connectors.length}
    <div class="connector-filters" role="search" aria-label="Connector 검색 및 필터">
      <div class="field search-field"><label for="connector-search">검색</label><input class="input input-bordered" id="connector-search" type="search" bind:value={searchQuery} placeholder="Connector, 연결 서비스, 도메인 검색" autocomplete="off" /></div>
      <div class="field"><label for="connection-filter">연결 상태</label><select id="connection-filter" bind:value={connectionFilter}><option value="all">전체 상태</option><option value="connected">연결됨</option><option value="pending">연결 대기</option><option value="problem">연결 문제</option></select></div>
    </div>
    {#if filtersActive}<div class="filter-summary"><span aria-live="polite">{data.connectors.length}개 중 {visibleConnectors.length}개 표시</span><Button variant="text" size="sm" onclick={clearFilters}>초기화</Button></div>{/if}
    {#if visibleConnectors.length}
      <div class="connector-list">
        {#each visibleConnectors as connector (connector.id)}
          <Card as="section" padding="none" class="connector-card" aria-label={`${connector.name} Connector`}>
            <Button variant="plain" class="connector-toggle" aria-expanded={connectorState.selectedId === connector.id} aria-controls={connectorState.selectedId === connector.id ? `connector-body-${connector.id}` : undefined} onclick={() => toggleConnector(connector.id)}>
              <strong>{connector.name}</strong>
              <span class="connector-summary"><Status label={`터널 ${statusLabel(connector.status)}`} value={connector.status} /><svg class:expanded={connectorState.selectedId === connector.id} aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m4 6 4 4 4-4" /></svg></span>
            </Button>
            {#if connectorState.selectedId === connector.id && selected}
              <div class="connector-body" id={`connector-body-${connector.id}`}>
                {#if service}
                  {#key service.id}<ServiceBlock record={records[0]} outlet={data.outlet} onChanged={onChanged} />{/key}
                {:else}
                  <section class="detail-section setup-section">
                    <div class="section-heading"><div><h3>연결 서비스 등록</h3><p>이 Connector가 연결할 서비스의 내부 주소와 포트를 입력하세요.</p></div></div>
                    <form class="form-grid" onsubmit={createService}>
                      <div class="field"><label for="service-name">서비스 이름</label><input class="input input-bordered" id="service-name" bind:value={serviceName} placeholder="예: memo" required /></div>
                      <div class="field"><label for="service-host">내부 호스트</label><input class="input input-bordered" id="service-host" bind:value={host} placeholder="예: memo-app" required /></div>
                      <div class="field"><label for="service-port">내부 포트</label><input class="input input-bordered" id="service-port" bind:value={port} type="number" min="1" max="65535" placeholder="예: 3000" required /></div>
                      <div class="form-actions"><Button type="submit" disabled={serviceBusy}>{serviceBusy ? '등록 중…' : '연결 서비스 등록'}</Button></div>
                      {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
                    </form>
                  </section>
                {/if}
                {#if service && (selected.status !== 'connected' || connectorState.enrollment)}
                  <section class="detail-section token-panel">
                    <div class="section-heading"><div><h3>Connector 연결</h3><p>{selected.status === 'draft' ? '토큰을 발급해 서비스가 실행되는 서버에서 Connector를 실행하세요.' : selected.status === 'connecting' ? 'Connector가 연결 중입니다. 잠시 후 상태를 확인하세요.' : selected.status === 'connected' ? 'Connector가 연결되었습니다. 발급한 토큰은 이 화면을 벗어나면 다시 볼 수 없습니다.' : 'Connector의 응답이 없습니다. 실행 중인 Connector와 네트워크를 확인하세요.'}</p></div></div>
                    {#if !connectorState.enrollment && selected.status === 'draft'}<Button onclick={() => issueToken()} disabled={tokenBusy}>{tokenBusy ? '발급 중…' : '등록 토큰 발급'}</Button>{/if}
                    {#if !connectorState.enrollment && selected.status !== 'draft' && selected.status !== 'connected'}<Button variant="secondary" onclick={() => { if (confirm('기존 Connector 자격 증명을 무효화하고 새 토큰을 발급합니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) issueToken(true); }} disabled={tokenBusy}>{tokenBusy ? '발급 중…' : '연결 초기화 후 새 토큰 발급'}</Button>{/if}
                    {#if tokenError}<p class:error={!tokenError.includes('복사했습니다')} class:success={tokenError.includes('복사했습니다')} role="status">{tokenError}</p>{/if}
                    {#if tokenError.includes('already_enrolled') && selected.status === 'draft'}
                      <Button variant="secondary" class="reset-token-button" onclick={() => { if (confirm('기존 Connector 자격 증명을 무효화하고 새 토큰을 발급합니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) issueToken(true); }}>연결 초기화 후 새 토큰 발급</Button>
                    {/if}
                    {#if connectorState.enrollment}
                      {@const enrollment = connectorState.enrollment}
                      <div class="token-box">
                        <strong>토큰을 지금 복사하세요</strong><p>이 화면을 닫으면 다시 확인할 수 없습니다.</p>
                        <input class="input input-bordered" aria-label="등록 토큰" value={enrollment.token} readonly />
                        <Button class="token-copy" onclick={() => copy(enrollment.token, '토큰')}>토큰 복사</Button>
                        <p>만료: {new Date(enrollment.expires_at).toLocaleString()}</p>
                        <p>서비스가 실행되는 서버에서 실행할 명령</p><pre>{installCommand()}</pre>
                        <Button class="token-copy" onclick={() => copy(installCommand(), '설치 명령')}>설치 명령 복사</Button>
                      </div>
                    {/if}
                  </section>
                {/if}
                <div class="detail-footer"><Button variant="danger" size="sm" onclick={openDeleteDialog}>Connector 삭제</Button></div>
              </div>
            {/if}
          </Card>
        {/each}
      </div>
    {:else}
      <p class="list-empty filter-empty">검색 조건에 맞는 Connector가 없습니다.</p>
    {/if}
  {:else}
    <p class="list-empty">아직 Connector가 없습니다. 새로 만들기를 눌러 시작하세요.</p>
  {/if}
</div>

<dialog bind:this={createDialog} class="connector-dialog" aria-labelledby="create-dialog-title" aria-describedby="create-dialog-description" oncancel={(event) => { if (createBusy) event.preventDefault(); }} onclose={() => { name = ''; createError = ''; }}>
  <form onsubmit={createConnector}>
    <h2 id="create-dialog-title">새 Connector 만들기</h2>
    <p id="create-dialog-description">공개할 서비스마다 Connector를 하나씩 만듭니다.</p>
    <div class="field"><label for="connector-name">Connector 이름</label><input class="input input-bordered" id="connector-name" bind:value={name} placeholder="예: coolify-hello" required /></div>
    {#if createError}<p class="error" role="alert">{createError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" onclick={closeCreateDialog} disabled={createBusy}>취소</Button><Button type="submit" disabled={createBusy}>{createBusy ? '만드는 중…' : 'Connector 만들기'}</Button></div>
  </form>
</dialog>

<dialog bind:this={deleteDialog} class="connector-dialog" aria-labelledby="delete-dialog-title" aria-describedby="delete-dialog-description" oncancel={(event) => { if (deleteBusy) event.preventDefault(); }} onclose={() => { deleteName = ''; deleteError = ''; deleteTarget = null; }}>
  <form onsubmit={deleteConnector}>
    <h2 id="delete-dialog-title">Connector 삭제</h2>
    <p id="delete-dialog-description">연결 서비스의 등록 정보와 공개 주소가 삭제되고 실행 중인 연결이 끊깁니다. 실제 서비스는 삭제되지 않습니다.</p>
    <div class="field"><label for="delete-connector-name">삭제 확인: <strong>{deleteTarget?.name}</strong> 입력</label><input class="input input-bordered" id="delete-connector-name" bind:value={deleteName} autocomplete="off" spellcheck="false" required /></div>
    {#if deleteError}<p class="error" role="alert">{deleteError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" onclick={closeDeleteDialog} disabled={deleteBusy}>취소</Button><Button variant="danger" type="submit" disabled={deleteBusy || deleteName !== deleteTarget?.name}>{deleteBusy ? '삭제 중…' : 'Connector 삭제'}</Button></div>
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
  .connector-list { display: grid; gap: 10px; }
  .connector-list :global(.connector-card) { overflow: visible; }
  .connector-list :global(.connector-toggle) { display: flex; align-items: center; justify-content: space-between; gap: 16px; width: 100%; min-height: 58px; border-radius: 8px; padding: 13px 18px; color: var(--ink); text-align: left; }
  .connector-list :global(.connector-toggle:hover) { background: rgba(246, 239, 232, .7); }
  .connector-list :global(.connector-toggle strong) { font-size: 16px; font-weight: 650; }
  .connector-summary { display: flex; align-items: center; gap: 14px; color: var(--muted); }
  .connector-summary svg { width: 17px; height: 17px; transition: transform .15s ease; }
  .connector-summary svg.expanded { transform: rotate(180deg); }
  .connector-body { border-top: 1px solid var(--line); padding: 16px 18px 14px; }
  .list-empty { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
  .connector-dialog { width: min(440px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .connector-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .connector-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .connector-dialog p:not(.error) { margin: 0 0 16px; color: var(--muted); font-size: 12px; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
  .detail-section { margin-top: 16px; border-top: 1px solid var(--line); padding-top: 15px; }
  .setup-section { margin-top: 0; border-top: 0; padding-top: 0; }
  .section-heading { display: flex; align-items: flex-start; gap: 10px; margin-bottom: 11px; }
  .section-heading h3 { margin: 0 0 4px; font-size: 15px; }
  .section-heading p { font-size: 12px; line-height: 1.5; }
  .form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; }
  .form-grid .form-actions, .form-grid .error { grid-column: 1 / -1; margin-top: 0; }
  .token-panel :global(.reset-token-button) { margin-top: 10px; }
  .token-box { margin-top: 12px; border-radius: 6px; padding: 13px; background: #242a2f; color: #f5f3ec; }
  .token-box p { margin: 6px 0 !important; color: #c2c8c5 !important; font-size: 12px; }
  .token-box input { width: 100%; margin: 10px 0; border: 1px solid #59636a; border-radius: 2px; padding: 10px; background: #171c20; color: #fff; font-family: ui-monospace, SFMono-Regular, monospace; font-size: 12px; }
  .token-box pre { overflow-wrap: anywhere; border: 1px solid #59636a; padding: 12px; background: #171c20; color: #d9e0da; font: 12px/1.5 ui-monospace, SFMono-Regular, monospace; white-space: pre-wrap; }
  .token-box :global(.token-copy) { border-color: #e7a27f; background: #e7a27f; color: #2b201b; }
  .detail-footer { display: flex; justify-content: flex-end; margin-top: 18px; border-top: 1px solid var(--line); padding-top: 10px; }
  @media (max-width: 900px) { .form-grid { grid-template-columns: 1fr; } }
  @media (max-width: 700px) { .connector-filters, .filter-summary { width: 100%; } }
  @media (max-width: 420px) { .connector-filters { grid-template-columns: 1fr; } }
</style>
