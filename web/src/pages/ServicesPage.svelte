<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import Status from '../components/Status.svelte';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import InfoTooltip from '../components/ui/InfoTooltip.svelte';
  import type { DashboardData, PageKey } from '../types';

  let { data, onChanged, navigate }: { data: DashboardData; onChanged: () => Promise<void>; navigate: (view: PageKey) => Promise<void> } = $props();
  let dialog: HTMLDialogElement;
  let tunnelId = $state('');
  let name = $state('');
  let host = $state('');
  let port = $state('');
  let busy = $state(false);
  let error = $state('');
  let copied = $state(false);
  let copyError = $state('');
  let appNetworks = $state<Record<string, string>>({});
  let copiedNetwork = $state('');
  let networkCopyError = $state('');

  const availableTunnels = $derived(data.connectors.filter((connector) => connector.status !== 'draft'));
  const target = $derived.by(() => {
    const address = host.trim();
    const number = String(port).trim();
    if (!address || !/^\d+$/.test(number) || Number(number) < 1 || Number(number) > 65535) return '';
    return `${address.includes(':') ? `[${address}]` : address}:${Number(number)}`;
  });

  function openDialog(): void {
    if (!availableTunnels.length) return;
    tunnelId = availableTunnels[0].id;
    error = '';
    dialog.showModal();
  }

  function closeDialog(): void {
    if (!busy) dialog.close();
  }

  function clearForm(): void {
    tunnelId = '';
    name = '';
    host = '';
    port = '';
    error = '';
    copied = false;
    copyError = '';
  }

  async function createService(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!tunnelId || busy) return;
    error = '';
    busy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(tunnelId)}/services`, {
        method: 'POST',
        body: JSON.stringify({ name, protocol: 'http', target: { host: host.trim(), port: Number(port) } }),
      });
      await onChanged();
      dialog.close();
    } catch (cause) {
      const code = errorMessage(cause);
      const message: Record<string, string> = {
        connector_not_found: '선택한 터널을 찾을 수 없습니다.',
        service_name_taken: '이미 사용 중인 서비스 이름입니다.',
        invalid_service: '서비스 이름과 앱 주소를 확인하세요.',
      };
      error = `연결 서비스를 등록하지 못했습니다: ${message[code] || code}`;
    } finally {
      busy = false;
    }
  }

  async function copyTarget(): Promise<void> {
    if (!target) return;
    try {
      await navigator.clipboard.writeText(target);
      copied = true;
      copyError = '';
    } catch {
      copied = false;
      copyError = '복사하지 못했습니다. 주소를 직접 선택해 복사하세요.';
    }
  }

  function networkCommand(connectorId: string, connectorName: string): string {
    const network = appNetworks[connectorId]?.trim() || 'APP_NETWORK';
    return `docker network connect ${network} meerkat-${connectorName}`;
  }

  async function copyNetworkCommand(connectorId: string, connectorName: string): Promise<void> {
    const network = appNetworks[connectorId]?.trim() || '';
    if (!/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(network)) return;
    try {
      await navigator.clipboard.writeText(networkCommand(connectorId, connectorName));
      copiedNetwork = connectorId;
      networkCopyError = '';
    } catch {
      networkCopyError = '명령을 복사하지 못했습니다. 직접 선택해 복사하세요.';
    }
  }
</script>

<div class="services-page">
  <div class="services-toolbar">
    <h2>연결 서비스 <small>{data.services.length}</small></h2>
    {#if availableTunnels.length}<Button variant="secondary" size="sm" onclick={openDialog}>+ 서비스 등록</Button>{/if}
  </div>

  {#if data.records.length}
    <div class="service-list">
      {#each data.records as record (record.service.id)}
        <Card as="section" padding="none" class="service-card" aria-label={`${record.service.name} 연결 서비스`}>
          <div class="service-header">
            <strong>터널 {record.connector.name}</strong>
            <span><Status label={`터널 ${statusLabel(record.connector.status)}`} value={record.connector.status} /></span>
          </div>
          <div class="service-body"><ServiceBlock {record} outlet={data.outlet} {onChanged} /></div>
          <details class="access-guide">
            <summary>앱과 터널 연결 방법</summary>
            <p>Docker 앱이 터널과 다른 네트워크에 있다면 앱의 네트워크 이름을 입력하고, 앱 서버에서 아래 명령을 실행하세요. 이미 같은 네트워크에 있다면 이 단계는 필요 없습니다.</p>
            <div class="field"><label for={`app-network-${record.connector.id}`}>앱의 Docker 네트워크</label><input id={`app-network-${record.connector.id}`} class="input input-bordered" value={appNetworks[record.connector.id] || ''} oninput={(event) => { appNetworks[record.connector.id] = event.currentTarget.value; copiedNetwork = ''; }} placeholder="예: myapp_default" spellcheck="false" /></div>
            <div class="network-command"><code>{networkCommand(record.connector.id, record.connector.name)}</code><Button variant="secondary" size="sm" disabled={!/^[a-zA-Z0-9][a-zA-Z0-9_.-]*$/.test(appNetworks[record.connector.id]?.trim() || '')} onclick={() => copyNetworkCommand(record.connector.id, record.connector.name)}>{copiedNetwork === record.connector.id ? '복사됨' : '명령 복사'}</Button></div>
            {#if networkCopyError}<p class="error" role="alert">{networkCopyError}</p>{/if}
            <p>Compose로 터널을 관리한다면 다음 재생성 때도 유지되도록 터널 서비스의 <code>networks:</code>에 앱 네트워크를 추가하세요. 기존 외부 통신 네트워크도 유지해야 합니다.</p>
            <p>Linux 서버에서 실행 중인 앱이 <code>127.0.0.1</code>에서만 수신한다면, 터널의 기존 데이터 볼륨을 유지한 채 컨테이너를 호스트 네트워크(<code>--network host</code> 또는 Compose의 <code>network_mode: host</code>)로 다시 만드세요.</p>
          </details>
        </Card>
      {/each}
    </div>
  {:else}
    <Card as="section" class="empty-state">
      <p>아직 등록된 연결 서비스가 없습니다.</p>
      {#if availableTunnels.length}
        <Button onclick={openDialog}>첫 서비스 등록</Button>
      {:else}
        <p>터널을 만들고 연결하면 앱 주소를 등록할 수 있습니다.</p>
        <Button variant="secondary" onclick={() => navigate('connectors')}>터널 관리로 이동</Button>
      {/if}
    </Card>
  {/if}

  {#if data.records.length && !availableTunnels.length}<p class="availability-note">서비스를 더 등록하려면 터널을 연결하세요.</p>{/if}
</div>

<dialog bind:this={dialog} class="service-dialog" aria-labelledby="service-dialog-title" aria-describedby="service-dialog-description" oncancel={(event) => { if (busy) event.preventDefault(); }} onclose={clearForm}>
  <form onsubmit={createService}>
    <h2 id="service-dialog-title">연결 서비스 등록</h2>
    <p id="service-dialog-description">연결된 터널을 선택하고 앱의 내부 주소를 입력하세요.</p>
    <div class="form-fields">
      <div class="field full"><div class="field-label"><label for="service-tunnel">연결할 터널</label><InfoTooltip id="service-tunnel-help" label="연결할 터널" description="앱에 접근할 수 있는 네트워크로 설치한 터널을 선택하세요. 한 터널에 여러 서비스를 등록할 수 있습니다." /></div><select id="service-tunnel" aria-describedby="service-tunnel-help" bind:value={tunnelId} required>{#each availableTunnels as connector}<option value={connector.id}>{connector.name}</option>{/each}</select></div>
      <div class="field full"><div class="field-label"><label for="service-name">서비스 이름</label><InfoTooltip id="service-name-help" label="서비스 이름" description="관리 화면에서 앱을 구별할 이름입니다. 공개 주소와는 별개입니다. 예: app" /></div><input id="service-name" class="input input-bordered" bind:value={name} placeholder="예: app" required /></div>
      <div class="field"><div class="field-label"><label for="service-host">앱 호스트</label><InfoTooltip id="service-host-help" label="앱 호스트" description="터널과 같은 Docker 네트워크의 앱은 컨테이너 이름(예: app)을 입력하세요. Linux 호스트 앱이 127.0.0.1에서만 수신한다면 터널을 호스트 네트워크로 다시 실행해야 합니다." /></div><input id="service-host" class="input input-bordered" bind:value={host} oninput={() => copied = false} placeholder="app 또는 127.0.0.1" required /></div>
      <div class="field"><div class="field-label"><label for="service-port">내부 포트</label><InfoTooltip id="service-port-help" label="내부 포트" description="앱이 실제로 듣는 포트입니다. 서버에 공개한 포트와 다를 수 있습니다. 예: 3000" /></div><input id="service-port" class="input input-bordered" type="number" min="1" max="65535" bind:value={port} oninput={() => copied = false} placeholder="3000" required /></div>
    </div>
    {#if target}<div class="target-preview"><span>연결 대상</span><code>{target}</code><Button variant="secondary" size="sm" class={copied ? 'target-copy copied' : 'target-copy'} aria-label={copied ? '연결 대상 복사 완료' : '연결 대상 복사'} title={copied ? '복사 완료' : '연결 대상 복사'} onclick={copyTarget}>{#if copied}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}</Button></div>{/if}
    {#if copyError}<p class="error" role="alert">{copyError}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" disabled={busy} onclick={closeDialog}>취소</Button><Button type="submit" disabled={busy}>{busy ? '등록 중…' : '서비스 등록'}</Button></div>
  </form>
</dialog>

<style>
  .services-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
  .services-toolbar h2 { margin: 0; font-size: 16px; letter-spacing: -.025em; }
  .services-toolbar small { margin-left: 5px; color: var(--muted); font-size: 12px; font-weight: 500; }
  .service-list { display: grid; gap: 10px; }
  .service-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--line); padding: 14px 18px; }
  .service-header strong { font-size: 16px; }
  .service-header span { display: inline-flex; align-items: center; gap: 8px; color: var(--muted); font-size: 12px; }
  .service-body { padding: 16px 18px; }
  .access-guide { border-top: 1px solid var(--line); padding: 12px 18px 16px; font-size: 12px; }
  .access-guide summary { width: fit-content; color: var(--accent-dark); cursor: pointer; font-weight: 650; }
  .access-guide p { max-width: 780px; margin: 9px 0; color: var(--muted); line-height: 1.5; }
  .access-guide .field { max-width: 360px; margin-top: 12px; }
  .access-guide .field input { width: 100%; }
  .network-command { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 9px; }
  .network-command code { overflow-wrap: anywhere; border: 1px solid var(--line); border-radius: 5px; padding: 7px 10px; background: #f7f5f1; color: var(--ink); }
  .empty-state { display: grid; justify-items: start; gap: 10px; }
  .empty-state p { margin: 0; color: var(--muted); font-size: 12px; }
  .availability-note { margin: 14px 2px 0; color: var(--muted); font-size: 12px; }
  .service-dialog { width: min(560px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .service-dialog::backdrop { background: rgba(35, 30, 26, .38); backdrop-filter: blur(3px); }
  .service-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .service-dialog p:not(.error) { margin: 0 0 16px; color: var(--muted); font-size: 12px; }
  .form-fields { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 12px; }
  .form-fields .full { grid-column: 1 / -1; }
  .field-label { display: flex; align-items: center; gap: 5px; }
  .form-fields input, .form-fields select { width: 100%; }
  .form-fields select { min-height: 38px; border: 1px solid #d8d1c9; border-radius: 6px; padding: 6px 8px; background: #fffdfa; color: var(--ink); }
  .target-preview { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-top: 14px; color: var(--muted); font-size: 12px; }
  .target-preview code { color: var(--ink); font-size: 13px; font-weight: 650; }
  .target-preview :global(.target-copy) { display: grid; place-items: center; width: 30px; min-height: 30px; padding: 0; }
  .target-preview :global(.target-copy svg) { width: 16px; height: 16px; }
  .target-preview :global(.target-copy.copied) { border-color: var(--green); color: var(--green); }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
  @media (max-width: 600px) { .form-fields { grid-template-columns: 1fr; } .service-header { align-items: flex-start; flex-direction: column; } }
</style>
