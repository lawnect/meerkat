<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import { connectorState } from '../connectorState.svelte';
  import Panel from '../components/Panel.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import Status from '../components/Status.svelte';
  import Button from '../components/ui/Button.svelte';
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
  let createDialog: HTMLDialogElement;

  const selected = $derived(data.connectors.find((connector) => connector.id === connectorState.selectedId));
  const records = $derived(data.records.filter((record) => record.connector.id === connectorState.selectedId));
  const service = $derived(records[0]?.service);

  $effect(() => {
    if (!connectorState.selectedId && data.connectors.length) connectorState.selectedId = data.connectors[0].id;
    if (connectorState.selectedId && !data.connectors.some((connector) => connector.id === connectorState.selectedId)) {
      connectorState.selectedId = data.connectors[0]?.id || '';
    }
  });

  function openCreateDialog(): void {
    createError = '';
    createDialog.showModal();
  }

  function closeCreateDialog(): void {
    if (!createBusy) createDialog.close();
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
      serviceError = `내부 앱을 등록하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      serviceBusy = false;
    }
  }

  async function deleteConnector(): Promise<void> {
    if (!selected || deleteBusy) return;
    const connector = selected;
    if (!confirm(`Connector ${connector.name}을(를) 삭제할까요? 등록된 앱과 공개 주소도 삭제되고, 실행 중인 Connector의 연결이 끊깁니다.`)) return;
    deleteError = '';
    deleteBusy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(connector.id)}`, { method: 'DELETE' });
      connectorState.selectedId = '';
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      await onChanged();
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

<div class="connector-layout">
  <Panel title="Connector">
    {#snippet action()}
      <Button variant="text" size="sm" onclick={openCreateDialog}>+ 새로 만들기</Button>
    {/snippet}
    {#if data.connectors.length}
      <div class="connector-list">
        {#each data.connectors as connector}
          <Button variant="plain" aria-current={connectorState.selectedId === connector.id ? 'true' : undefined} class={connectorState.selectedId === connector.id ? 'connector-item selected' : 'connector-item'} onclick={() => { connectorState.selectedId = connector.id; connectorState.enrollment = null; connectorState.enrollmentWasReset = false; tokenError = ''; deleteError = ''; }}>
            <strong>{connector.name}</strong><Status label={statusLabel(connector.status)} value={connector.status} />
          </Button>
        {/each}
      </div>
    {:else}
      <p class="list-empty">아직 Connector가 없습니다. 새로 만들기를 눌러 시작하세요.</p>
    {/if}
  </Panel>

  {#if selected}
    <div class="connector-detail">
      <Panel title={selected.name} description="연결 상태와 공개 설정을 확인하세요.">
        <div class="state-row"><span>Connector 상태</span><Status label={statusLabel(selected.status)} value={selected.status} /></div>
        {#if deleteError}<p class="error" role="alert">{deleteError}</p>{/if}
        {#if service}
          {#key service.id}<ServiceBlock record={records[0]} outlet={data.outlet} onChanged={onChanged} />{/key}
        {:else}
          <section class="detail-section">
            <div class="section-heading"><div><h3>내부 앱 등록</h3><p>이 Connector가 연결할 앱의 내부 주소와 포트를 입력하세요.</p></div></div>
            <form class="form-grid" onsubmit={createService}>
              <div class="field"><label for="service-name">앱 이름</label><input class="input input-bordered" id="service-name" bind:value={serviceName} placeholder="예: memo" required /></div>
              <div class="field"><label for="service-host">내부 호스트</label><input class="input input-bordered" id="service-host" bind:value={host} placeholder="예: memo-app" required /></div>
              <div class="field"><label for="service-port">내부 포트</label><input class="input input-bordered" id="service-port" bind:value={port} type="number" min="1" max="65535" placeholder="예: 3000" required /></div>
              <div class="form-actions"><Button type="submit" disabled={serviceBusy}>{serviceBusy ? '등록 중…' : '내부 앱 등록'}</Button></div>
              {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
            </form>
          </section>
        {/if}
        {#if service && (selected.status !== 'connected' || connectorState.enrollment)}
          <section class="detail-section token-panel">
            <div class="section-heading"><div><h3>Connector 연결</h3><p>{selected.status === 'draft' ? '토큰을 발급해 앱이 실행되는 서버에서 Connector를 실행하세요.' : selected.status === 'connecting' ? 'Connector가 연결 중입니다. 잠시 후 상태를 확인하세요.' : selected.status === 'connected' ? 'Connector가 연결되었습니다. 발급한 토큰은 이 화면을 벗어나면 다시 볼 수 없습니다.' : 'Connector의 응답이 없습니다. 실행 중인 Connector와 네트워크를 확인하세요.'}</p></div></div>
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
                <p>앱이 실행되는 서버에서 실행할 명령</p><pre>{installCommand()}</pre>
                <Button class="token-copy" onclick={() => copy(installCommand(), '설치 명령')}>설치 명령 복사</Button>
              </div>
            {/if}
          </section>
        {/if}
        <div class="detail-footer"><Button variant="text" onclick={deleteConnector} disabled={deleteBusy}>{deleteBusy ? '삭제 중…' : 'Connector 삭제'}</Button></div>
      </Panel>
    </div>
  {/if}
</div>

<dialog bind:this={createDialog} class="create-dialog" aria-labelledby="create-dialog-title" aria-describedby="create-dialog-description" oncancel={(event) => { if (createBusy) event.preventDefault(); }} onclose={() => { name = ''; createError = ''; }}>
  <form onsubmit={createConnector}>
    <h2 id="create-dialog-title">새 Connector 만들기</h2>
    <p id="create-dialog-description">공개할 앱마다 Connector를 하나씩 만듭니다.</p>
    <div class="field"><label for="connector-name">Connector 이름</label><input class="input input-bordered" id="connector-name" bind:value={name} placeholder="예: coolify-hello" required /></div>
    {#if createError}<p class="error" role="alert">{createError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" onclick={closeCreateDialog} disabled={createBusy}>취소</Button><Button type="submit" disabled={createBusy}>{createBusy ? '만드는 중…' : 'Connector 만들기'}</Button></div>
  </form>
</dialog>

<style>
  .connector-layout { display: grid; grid-template-columns: 270px minmax(0, 1fr); align-items: start; gap: 16px; }
  .connector-list { padding: 0; }
  .connector-list :global(.connector-item) { display: block; width: 100%; border-bottom: 1px solid var(--line); padding: 14px 16px; color: var(--ink); text-align: left; }
  .connector-list :global(.connector-item:first-child) { border-top: 1px solid var(--line); }
  .connector-list :global(.connector-item:hover), .connector-list :global(.connector-item.selected) { background: #f5eee8; }
  .connector-list :global(.connector-item.selected) { box-shadow: inset 3px 0 var(--accent); }
  .connector-list strong { display: block; font-size: 13px; }
  .connector-detail > :global(.panel) { min-width: 0; }
  .list-empty { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
  .create-dialog { width: min(440px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 26px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .create-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .create-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .create-dialog p:not(.error) { margin: 0 0 22px; color: var(--muted); font-size: 12px; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 24px; }
  .state-row { display: flex; align-items: center; justify-content: space-between; gap: 12px; color: var(--muted); font-size: 12px; }
  .detail-section { margin-top: 24px; border-top: 1px solid var(--line); padding-top: 22px; }
  .section-heading { display: flex; align-items: flex-start; gap: 12px; margin-bottom: 16px; }
  .section-heading h3 { margin: 0 0 4px; font-size: 15px; }
  .section-heading p { font-size: 12px; line-height: 1.5; }
  .form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  .form-grid .form-actions, .form-grid .error { grid-column: 1 / -1; margin-top: 0; }
  .token-panel :global(.reset-token-button) { margin-top: 10px; }
  .token-box { margin-top: 16px; border-radius: 6px; padding: 16px; background: #242a2f; color: #f5f3ec; }
  .token-box p { margin: 6px 0 !important; color: #c2c8c5 !important; font-size: 12px; }
  .token-box input { width: 100%; margin: 10px 0; border: 1px solid #59636a; border-radius: 2px; padding: 10px; background: #171c20; color: #fff; font-family: ui-monospace, SFMono-Regular, monospace; font-size: 12px; }
  .token-box pre { overflow-wrap: anywhere; border: 1px solid #59636a; padding: 12px; background: #171c20; color: #d9e0da; font: 12px/1.5 ui-monospace, SFMono-Regular, monospace; white-space: pre-wrap; }
  .token-box :global(.token-copy) { border-color: #e7a27f; background: #e7a27f; color: #2b201b; }
  .detail-footer { margin-top: 26px; border-top: 1px solid var(--line); padding-top: 14px; }
  .detail-footer :global(.ui-button) { color: #a64132; }
  @media (max-width: 900px) { .connector-layout, .form-grid { grid-template-columns: 1fr; } }
</style>
