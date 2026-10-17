<script>
  import { api, statusLabel } from '../api.js';
  import { connectorState } from '../connectorState.svelte.js';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import Status from '../components/Status.svelte';

  let { data, onChanged } = $props();
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

  const selected = $derived(data.connectors.find((connector) => connector.id === connectorState.selectedId));
  const records = $derived(data.records.filter((record) => record.connector.id === connectorState.selectedId));
  const services = $derived(records.map((record) => record.service));

  $effect(() => {
    if (!connectorState.selectedId && data.connectors.length) connectorState.selectedId = data.connectors[0].id;
    if (connectorState.selectedId && !data.connectors.some((connector) => connector.id === connectorState.selectedId)) {
      connectorState.selectedId = data.connectors[0]?.id || '';
    }
  });

  async function createConnector(event) {
    event.preventDefault();
    createError = '';
    createBusy = true;
    try {
      const connector = await api('/api/v1/connectors', { method: 'POST', body: JSON.stringify({ name }) });
      connectorState.selectedId = connector.id;
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      name = '';
      await onChanged();
    } catch (cause) {
      createError = `Connector를 만들지 못했습니다: ${cause.message}`;
    } finally {
      createBusy = false;
    }
  }

  async function createService(event) {
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
      serviceError = `서비스를 등록하지 못했습니다: ${cause.message}`;
    } finally {
      serviceBusy = false;
    }
  }

  async function issueToken(reset = false) {
    if (!selected) return;
    tokenError = '';
    tokenBusy = true;
    try {
      connectorState.enrollment = await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}/enrollment-tokens${reset ? '?reset=true' : ''}`, { method: 'POST' });
      connectorState.enrollmentWasReset = reset;
    } catch (cause) {
      tokenError = `토큰을 발급하지 못했습니다: ${cause.message}`;
    } finally {
      tokenBusy = false;
    }
  }

  async function copy(value, label) {
    try {
      await navigator.clipboard.writeText(value);
      tokenError = `${label}을(를) 복사했습니다.`;
      setTimeout(() => { if (tokenError.startsWith(label)) tokenError = ''; }, 2500);
    } catch {
      tokenError = '클립보드에 복사하지 못했습니다. 내용을 직접 선택해 복사하세요.';
    }
  }

  function installCommand() {
    const token = connectorState.enrollment?.token || '';
    const volume = connectorState.enrollmentWasReset ? 'meerkat-data-reset' : 'meerkat-data';
    return `docker run --rm --name meerkat-${selected.name} \\\n+  -e MEERKAT_OUTLET=https://${data.outlet.domain} \\\n+  -e MEERKAT_NAME=${selected.name} \\\n+  -e MEERKAT_TARGET=${services[0].target.host}:${services[0].target.port} \\\n+  -e MEERKAT_ENROLLMENT_TOKEN='${token}' \\\n+  -v "$PWD/${volume}:/var/lib/meerkat" \\\n+  ghcr.io/lawnect/meerkat:latest`;
  }

</script>

<PageHead kicker="운영 / 연결 서버" title="서버 연결" description="서비스를 실행하는 서버를 등록하고 연결 상태를 관리합니다." />
<div class="connector-layout">
  <Panel title="연결 서버" description="새 서버를 등록하거나 기존 연결을 선택합니다.">
    <form onsubmit={createConnector}>
      <div class="field"><label for="connector-name">새 연결 서버 이름</label><input id="connector-name" bind:value={name} placeholder="예: home-server" required /></div>
      <div class="form-actions"><button class="button" type="submit" disabled={createBusy}>{createBusy ? '만드는 중…' : 'Connector 만들기'}</button></div>
      {#if createError}<p class="error" role="alert">{createError}</p>{/if}
    </form>
    <h3 class="section-title">등록된 연결 서버</h3>
    {#if data.connectors.length}
      <div class="connector-list">
        {#each data.connectors as connector}
          <button class="connector-item" class:selected={connectorState.selectedId === connector.id} onclick={() => { connectorState.selectedId = connector.id; connectorState.enrollment = null; connectorState.enrollmentWasReset = false; tokenError = ''; }}>
            <strong>{connector.name}</strong><Status label={statusLabel(connector.status)} value={connector.status} />
          </button>
        {/each}
      </div>
    {:else}
      <div class="empty compact-empty"><p>등록된 연결 서버가 없습니다.</p></div>
    {/if}
  </Panel>

  {#if selected}
    <div class="connector-detail">
      <Panel title="서비스와 공개 주소" description="먼저 내부 앱을 등록하고, 그 다음 외부에서 사용할 주소를 추가합니다.">
        <div class="detail-title connector-title"><strong>{selected.name}</strong><Status label={statusLabel(selected.status)} value={selected.status} /></div>
        <h3 class="section-title">새 내부 서비스</h3>
        <form class="grid form-grid" onsubmit={createService}>
          <div class="field"><label for="service-name">서비스 이름</label><input id="service-name" bind:value={serviceName} placeholder="예: memo" required /></div>
          <div class="field"><label for="service-host">내부 호스트</label><input id="service-host" bind:value={host} placeholder="예: memo-app" required /></div>
          <div class="field"><label for="service-port">내부 포트</label><input id="service-port" bind:value={port} type="number" min="1" max="65535" placeholder="예: 3000" required /></div>
          <div class="form-actions"><button class="button" type="submit" disabled={serviceBusy || services.length > 0}>{serviceBusy ? '등록 중…' : '내부 서비스 등록'}</button></div>
          {#if services.length}<p class="error">현재 Connector는 서비스 하나만 지원합니다. 기존 서비스를 삭제한 뒤 새 서비스를 등록하세요.</p>{/if}
          {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
        </form>
        <h3 class="section-title">등록된 서비스</h3>
        {#if records.length}
          {#each records as record}
            <ServiceBlock {record} outlet={data.outlet} onChanged={onChanged} />
          {/each}
        {:else}
          <div class="empty"><h2>아직 서비스가 없습니다</h2><p>이 Connector가 연결할 내부 앱의 주소와 포트를 등록하세요.</p></div>
        {/if}
        {#if services.length === 1}
          <section class="token-panel">
            <h3>Connector 연결</h3>
            <p>등록 토큰을 발급하고 Connector 서버에 설치합니다. 토큰은 한 번만 표시됩니다.</p>
            <button class="button" onclick={() => issueToken()} disabled={tokenBusy}>{tokenBusy ? '발급 중…' : '등록 토큰 발급'}</button>
            {#if tokenError}<p class:error={!tokenError.includes('복사했습니다')} class:success={tokenError.includes('복사했습니다')} role="status">{tokenError}</p>{/if}
            {#if tokenError.includes('already_enrolled')}
              <button class="button secondary" onclick={() => { if (confirm('기존 Connector 자격 증명을 무효화하고 새 토큰을 발급합니다. 실행 중인 연결은 끊깁니다. 계속할까요?')) issueToken(true); }}>기존 연결 초기화 후 새 토큰 발급</button>
            {/if}
            {#if connectorState.enrollment}
              <div class="token-box">
                <strong>지금 복사하세요</strong><p>이 화면을 닫으면 토큰을 다시 확인할 수 없습니다.</p>
                <input aria-label="등록 토큰" value={connectorState.enrollment.token} readonly />
                <button class="button" onclick={() => copy(connectorState.enrollment.token, '토큰')}>토큰 복사</button>
                <p>만료: {new Date(connectorState.enrollment.expires_at).toLocaleString()}</p>
                <p>Connector 서버에서 실행할 명령</p><pre>{installCommand()}</pre>
                <button class="button" onclick={() => copy(installCommand(), '설치 명령')}>설치 명령 복사</button>
              </div>
            {/if}
          </section>
        {:else if services.length === 0}
          <section class="token-panel"><h3>Connector 연결</h3><p>토큰 발급에는 Connector당 서비스 하나가 필요합니다. 먼저 서비스를 등록하세요.</p><button class="button" disabled>등록 토큰 발급</button></section>
        {/if}
      </Panel>
    </div>
  {:else}
    <div class="panel"><div class="empty"><h2>연결 서버를 선택하세요</h2><p>왼쪽에서 서버를 선택하면 서비스 등록과 설치 안내가 표시됩니다.</p></div></div>
  {/if}
</div>

<style>
  .connector-layout { display: grid; grid-template-columns: 270px minmax(0, 1fr); gap: 16px; }
  .connector-list { padding: 0; }
  .connector-item { display: block; width: 100%; border: 0; border-bottom: 1px solid var(--line); padding: 14px 16px; background: transparent; color: var(--ink); text-align: left; }
  .connector-item:first-child { border-top: 1px solid var(--line); }
  .connector-item:hover, .connector-item.selected { background: #f5eee8; }
  .connector-item strong { display: block; font-size: 13px; }
  .connector-item small { color: var(--muted); }
  .connector-detail > :global(.panel) { min-width: 0; }
  .section-title { margin: 24px 0 12px; }
  .form-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 14px; }
  .connector-title { margin-bottom: 12px; }
  .connector-title strong { font-size: 18px; }
  .compact-empty { margin-top: 12px; padding: 16px; }
  .token-panel { margin-top: 24px; border-top: 1px solid var(--line); padding-top: 20px; }
  .token-panel h3 { margin: 0 0 5px; }
  .token-panel > p { margin: 0 0 12px; }
  .token-panel > .button.secondary { margin-top: 10px; }
  .token-box { margin-top: 16px; padding: 16px; background: #242a2f; color: #f5f3ec; }
  .token-box p { color: #c2c8c5; font-size: 12px; }
  .token-box input { width: 100%; margin: 10px 0; border: 1px solid #59636a; border-radius: 2px; padding: 10px; background: #171c20; color: #fff; font-family: ui-monospace, SFMono-Regular, monospace; font-size: 12px; }
  .token-box pre { overflow-wrap: anywhere; border: 1px solid #59636a; padding: 12px; background: #171c20; color: #d9e0da; font: 12px/1.5 ui-monospace, SFMono-Regular, monospace; white-space: pre-wrap; }
  .token-box .button { border-color: #e7a27f; background: #e7a27f; color: #2b201b; }
  @media (max-width: 900px) { .connector-layout, .form-grid { grid-template-columns: 1fr; } }
</style>
