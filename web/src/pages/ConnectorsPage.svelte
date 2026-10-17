<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import { connectorState } from '../connectorState.svelte';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
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

  const selected = $derived(data.connectors.find((connector) => connector.id === connectorState.selectedId));
  const records = $derived(data.records.filter((record) => record.connector.id === connectorState.selectedId));
  const services = $derived(records.map((record) => record.service));

  $effect(() => {
    if (!connectorState.selectedId && data.connectors.length) connectorState.selectedId = data.connectors[0].id;
    if (connectorState.selectedId && !data.connectors.some((connector) => connector.id === connectorState.selectedId)) {
      connectorState.selectedId = data.connectors[0]?.id || '';
    }
  });

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
    if (!selected || !services[0]) return '';
    const token = connectorState.enrollment?.token || '';
    const volume = connectorState.enrollmentWasReset ? 'meerkat-data-reset' : 'meerkat-data';
    return `docker run --rm --name meerkat-${selected.name} \\\n+  -e MEERKAT_OUTLET=https://${data.outlet.domain} \\\n+  -e MEERKAT_NAME=${selected.name} \\\n+  -e MEERKAT_TARGET=${services[0].target.host}:${services[0].target.port} \\\n+  -e MEERKAT_ENROLLMENT_TOKEN='${token}' \\\n+  -v "$PWD/${volume}:/var/lib/meerkat" \\\n+  ghcr.io/lawnect/meerkat:latest`;
  }

</script>

<PageHead kicker="운영 / 앱 연결" title="앱 연결" description="내부 앱을 Meerkat에 연결하고 공개 주소를 설정합니다." />
<div class="connector-layout">
  <Panel title="Connector" description="공개할 앱마다 Connector를 만들고 연결합니다.">
    <form onsubmit={createConnector}>
      <div class="field"><label for="connector-name">Connector 이름</label><input class="input input-bordered" id="connector-name" bind:value={name} placeholder="예: coolify-hello" required /></div>
      <div class="form-actions"><Button type="submit" disabled={createBusy}>{createBusy ? '만드는 중…' : 'Connector 만들기'}</Button></div>
      {#if createError}<p class="error" role="alert">{createError}</p>{/if}
    </form>
    <h3 class="section-title">등록된 Connector</h3>
    {#if data.connectors.length}
      <div class="connector-list">
        {#each data.connectors as connector}
          <Button variant="plain" class={connectorState.selectedId === connector.id ? 'connector-item selected' : 'connector-item'} onclick={() => { connectorState.selectedId = connector.id; connectorState.enrollment = null; connectorState.enrollmentWasReset = false; tokenError = ''; }}>
            <strong>{connector.name}</strong><Status label={statusLabel(connector.status)} value={connector.status} />
          </Button>
        {/each}
      </div>
    {:else}
      <Card padding="none" class="empty compact-empty"><p>등록된 Connector가 없습니다.</p></Card>
    {/if}
  </Panel>

  {#if selected}
    <div class="connector-detail">
      <Panel title="연결할 앱과 공개 주소" description="먼저 내부 앱을 등록하고, 그 다음 외부에서 사용할 주소를 추가합니다.">
        <div class="detail-title connector-title"><strong>{selected.name}</strong><Status label={statusLabel(selected.status)} value={selected.status} /><Button variant="danger" onclick={deleteConnector} disabled={deleteBusy}>{deleteBusy ? '삭제 중…' : 'Connector 삭제'}</Button></div>
        {#if deleteError}<p class="error" role="alert">{deleteError}</p>{/if}
        <h3 class="section-title">내부 앱 등록</h3>
        <form class="dashboard-grid form-grid" onsubmit={createService}>
          <div class="field"><label for="service-name">앱 이름</label><input class="input input-bordered" id="service-name" bind:value={serviceName} placeholder="예: memo" required /></div>
          <div class="field"><label for="service-host">내부 호스트</label><input class="input input-bordered" id="service-host" bind:value={host} placeholder="예: memo-app" required /></div>
          <div class="field"><label for="service-port">내부 포트</label><input class="input input-bordered" id="service-port" bind:value={port} type="number" min="1" max="65535" placeholder="예: 3000" required /></div>
          <div class="form-actions"><Button type="submit" disabled={serviceBusy || services.length > 0}>{serviceBusy ? '등록 중…' : '내부 앱 등록'}</Button></div>
          {#if services.length}<p class="error">현재 Connector는 앱 하나만 지원합니다. 다른 앱을 공개하려면 새 Connector를 만드세요.</p>{/if}
          {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
        </form>
        <h3 class="section-title">등록된 앱</h3>
        {#if records.length}
          {#each records as record}
            <ServiceBlock {record} outlet={data.outlet} onChanged={onChanged} />
          {/each}
        {:else}
          <Card padding="none" class="empty"><h2>아직 등록된 앱이 없습니다</h2><p>이 Connector가 연결할 내부 앱의 주소와 포트를 등록하세요.</p></Card>
        {/if}
        {#if services.length === 1}
          <section class="token-panel">
            <h3>Connector 연결</h3>
            <p>등록 토큰을 발급하고 앱이 실행되는 서버에서 Connector를 실행합니다. 토큰은 한 번만 표시됩니다.</p>
            <Button onclick={() => issueToken()} disabled={tokenBusy}>{tokenBusy ? '발급 중…' : '등록 토큰 발급'}</Button>
            {#if tokenError}<p class:error={!tokenError.includes('복사했습니다')} class:success={tokenError.includes('복사했습니다')} role="status">{tokenError}</p>{/if}
            {#if tokenError.includes('already_enrolled')}
              <Button variant="secondary" class="reset-token-button" onclick={() => { if (confirm('기존 Connector 자격 증명을 무효화하고 새 토큰을 발급합니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) issueToken(true); }}>기존 연결 초기화 후 새 토큰 발급</Button>
            {/if}
            {#if connectorState.enrollment}
              {@const enrollment = connectorState.enrollment}
              <Card variant="solid" padding="none" class="token-box">
                <strong>지금 복사하세요</strong><p>이 화면을 닫으면 토큰을 다시 확인할 수 없습니다.</p>
                <input class="input input-bordered" aria-label="등록 토큰" value={enrollment.token} readonly />
                <Button class="token-copy" onclick={() => copy(enrollment.token, '토큰')}>토큰 복사</Button>
                <p>만료: {new Date(enrollment.expires_at).toLocaleString()}</p>
                <p>앱이 실행되는 서버에서 실행할 명령</p><pre>{installCommand()}</pre>
                <Button class="token-copy" onclick={() => copy(installCommand(), '설치 명령')}>설치 명령 복사</Button>
              </Card>
            {/if}
          </section>
        {:else if services.length === 0}
          <section class="token-panel"><h3>Connector 연결</h3><p>토큰을 발급하려면 먼저 이 Connector에 내부 앱 하나를 등록하세요.</p><Button disabled>등록 토큰 발급</Button></section>
        {/if}
      </Panel>
    </div>
  {:else}
    <Card class="empty"><h2>Connector를 선택하세요</h2><p>왼쪽에서 Connector를 선택하면 앱 등록과 설치 안내가 표시됩니다.</p></Card>
  {/if}
</div>

<style>
  .connector-layout { display: grid; grid-template-columns: 270px minmax(0, 1fr); gap: 16px; }
  .connector-list { padding: 0; }
  .connector-list :global(.connector-item) { display: block; width: 100%; border-bottom: 1px solid var(--line); padding: 14px 16px; color: var(--ink); text-align: left; }
  .connector-list :global(.connector-item:first-child) { border-top: 1px solid var(--line); }
  .connector-list :global(.connector-item:hover), .connector-list :global(.connector-item.selected) { background: #f5eee8; }
  .connector-list strong { display: block; font-size: 13px; }
  .connector-detail > :global(.panel) { min-width: 0; }
  .section-title { margin: 24px 0 12px; }
  .form-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  .connector-title { margin-bottom: 12px; }
  .connector-title strong { font-size: 18px; }
  .connector-title { display: flex; align-items: center; gap: 12px; flex-wrap: wrap; }
  .connector-title :global(.ui-button) { margin-left: auto; }
  :global(.compact-empty) { margin-top: 12px; padding: 16px; }
  .token-panel { margin-top: 24px; border-top: 1px solid var(--line); padding-top: 20px; }
  .token-panel h3 { margin: 0 0 5px; }
  .token-panel > p { margin: 0 0 12px; }
  .token-panel :global(.reset-token-button) { margin-top: 10px; }
  :global(.token-box) { margin-top: 16px; border-color: #242a2f; padding: 16px; background: #242a2f; color: #f5f3ec; }
  :global(.token-box p) { color: #c2c8c5; font-size: 12px; }
  :global(.token-box input) { width: 100%; margin: 10px 0; border: 1px solid #59636a; border-radius: 2px; padding: 10px; background: #171c20; color: #fff; font-family: ui-monospace, SFMono-Regular, monospace; font-size: 12px; }
  :global(.token-box pre) { overflow-wrap: anywhere; border: 1px solid #59636a; padding: 12px; background: #171c20; color: #d9e0da; font: 12px/1.5 ui-monospace, SFMono-Regular, monospace; white-space: pre-wrap; }
  :global(.token-box .token-copy) { border-color: #e7a27f; background: #e7a27f; color: #2b201b; }
  @media (max-width: 900px) { .connector-layout, .form-grid { grid-template-columns: 1fr; } }
</style>
