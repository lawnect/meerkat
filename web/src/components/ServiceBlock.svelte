<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import type { Outlet, ServiceRecord } from '../types';
  import Status from './Status.svelte';
  import Button from './ui/Button.svelte';
  import InfoTooltip from './ui/InfoTooltip.svelte';
  import LinkButton from './ui/LinkButton.svelte';
  import { pagePath } from '../navigation';

  let { record, outlet, onChanged, view = 'both' }: { record: ServiceRecord; outlet: Outlet; onChanged: () => Promise<void>; view?: 'both' | 'service' | 'routes' } = $props();
  let subdomain = $state('');
  let selectedDomain = $state('');
  let addError = $state('');
  let routeError = $state('');
  let busy = $state(false);
  let deletingRoute = $state('');
  let targetCopied = $state(false);
  let targetCopyTimer: ReturnType<typeof setTimeout> | undefined;
  let targetCopyError = $state('');
  let editingTarget = $state(false);
  let editHost = $state('');
  let editPort = $state('');
  let editBusy = $state(false);
  let editError = $state('');
  let deletingService = $state(false);
  let serviceError = $state('');
  let routeDialog = $state<HTMLDialogElement>();
  const domains = $derived(outlet.domains?.length ? outlet.domains : outlet.domain ? [outlet.domain] : []);
  const baseDomain = $derived(domains.includes(selectedDomain) ? selectedDomain : domains[0] || '');
  const routeHostname = $derived(subdomain.trim().toLowerCase() && baseDomain ? `${subdomain.trim().toLowerCase()}.${baseDomain}` : '');
  const targetAddress = $derived(`${record.service.target.host.includes(':') ? `[${record.service.target.host}]` : record.service.target.host}:${record.service.target.port}`);

  async function copyTarget(): Promise<void> {
    try {
      await navigator.clipboard.writeText(targetAddress);
      targetCopied = true;
      targetCopyError = '';
      if (targetCopyTimer) clearTimeout(targetCopyTimer);
      targetCopyTimer = setTimeout(() => { targetCopied = false; }, 2500);
    } catch {
      targetCopied = false;
      targetCopyError = '연결 대상을 복사하지 못했습니다. 주소를 직접 선택해 복사하세요.';
    }
  }

  function startTargetEdit(): void {
    editHost = record.service.target.host;
    editPort = String(record.service.target.port);
    editError = '';
    editingTarget = true;
  }

  async function saveTarget(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (editBusy) return;
    editBusy = true;
    editError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, {
        method: 'PATCH',
        body: JSON.stringify({ target: { host: editHost.trim(), port: Number(editPort) } }),
      });
      editingTarget = false;
      targetCopied = false;
      await onChanged();
    } catch (cause) {
      const code = errorMessage(cause);
      editError = code === 'invalid_service'
        ? '앱 호스트와 내부 포트를 확인하세요.'
        : code === 'service_target_taken'
          ? '같은 터널의 다른 서비스가 이 앱 호스트와 포트를 사용 중입니다.'
          : `연결 대상을 저장하지 못했습니다: ${code}`;
    } finally {
      editBusy = false;
    }
  }

  async function deleteService(): Promise<void> {
    if (deletingService || record.routes.length || !confirm(`${record.service.name} 연결 서비스를 삭제할까요? 같은 터널의 다른 서비스도 잠시 다시 연결될 수 있습니다.`)) return;
    deletingService = true;
    serviceError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, { method: 'DELETE' });
      await onChanged();
    } catch (cause) {
      serviceError = `연결 서비스를 삭제하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      deletingService = false;
    }
  }

  function openRouteDialog(): void {
    addError = '';
    routeDialog?.showModal();
  }

  function closeRouteDialog(): void {
    if (!busy) routeDialog?.close();
  }

  async function addRoute(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!baseDomain || !routeHostname) return;
    addError = '';
    busy = true;
    try {
      await api(`/api/v1/services/${encodeURIComponent(record.service.id)}/routes`, {
        method: 'POST',
        body: JSON.stringify({ hostname: routeHostname }),
      });
      await onChanged();
      routeDialog?.close();
    } catch (cause) {
      const code = errorMessage(cause);
      addError = code === 'hostname_taken' ? '이미 사용 중인 주소입니다. 다른 서브도메인을 입력하세요.' : code === 'invalid_route' ? '서브도메인을 확인하세요. 영문자, 숫자, 하이픈만 사용할 수 있습니다.' : `공개 주소를 만들지 못했습니다: ${code}`;
    } finally {
      busy = false;
    }
  }

  async function deleteRoute(route: ServiceRecord['routes'][number]): Promise<void> {
    if (deletingRoute || !confirm(`${route.hostname} 공개 주소를 삭제할까요? 이 주소로의 접속이 중단됩니다.`)) return;
    routeError = '';
    deletingRoute = route.id;
    try {
      await api(`/api/v1/services/${encodeURIComponent(record.service.id)}/routes/${encodeURIComponent(route.id)}`, { method: 'DELETE' });
      await onChanged();
    } catch (cause) {
      routeError = `공개 주소를 삭제하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      deletingRoute = '';
    }
  }

  function routeStatus(route: ServiceRecord['routes'][number]): { label: string; kind: 'live' | 'waiting' | 'error' | 'off' } {
    if (!route.enabled) return { label: '사용 안 함', kind: 'off' };
    if (route.last_error) return { label: '설정 오류', kind: 'error' };
    if (route.applied_revision < route.desired_revision) return { label: '반영 중', kind: 'waiting' };
    if (!record.service.enabled) return { label: '서비스 사용 안 함', kind: 'off' };
    if (record.connector.status !== 'connected') return { label: '터널 연결 대기', kind: 'waiting' };
    if (record.service.observed_status !== 'connected') return { label: '서비스 응답 대기', kind: 'waiting' };
    return { label: '공개 중', kind: 'live' };
  }
</script>

<article class="service">
  <div class:single={view !== 'both'} class="service-grid">
    {#if view !== 'routes'}
    <section class="service-column" aria-label="연결 서비스">
      {#if view === 'both'}
      <div class="section-heading"><div class="section-title"><h3 class="ui-meta-label">연결 서비스</h3><InfoTooltip id={`service-help-${record.service.id}`} label="연결 서비스" description="터널이 요청을 전달하는 앱과 내부 주소입니다. 상태는 해당 앱 포트의 응답을 나타냅니다." /></div></div>
      {/if}
      <div class="app-row">
        <div class="app-summary"><strong>{record.service.name}</strong><Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} /></div>
        <div class="target-row"><span class="ui-meta-label">연결 대상</span><code>{targetAddress}</code><Button variant="plain" size="sm" class={targetCopied ? 'target-copy copied' : 'target-copy'} aria-label={targetCopied ? '연결 대상 복사 완료' : '연결 대상 복사'} title={targetCopied ? '복사 완료' : '연결 대상 복사'} onclick={copyTarget}>
          {#if targetCopied}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}
        </Button><Button variant="text" size="sm" onclick={startTargetEdit}>수정</Button><Button variant="text" size="sm" disabled={deletingService || record.routes.length > 0} title={record.routes.length ? '공개 주소를 먼저 삭제하세요' : '연결 서비스 삭제'} onclick={deleteService}>삭제</Button></div>
        {#if targetCopyError}<p class="error" role="alert">{targetCopyError}</p>{/if}
        {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
        {#if editingTarget}
          <form class="target-edit" onsubmit={saveTarget}>
            <div class="field"><label for={`edit-host-${record.service.id}`}>앱 호스트</label><input id={`edit-host-${record.service.id}`} class="input input-bordered" bind:value={editHost} required /></div>
            <div class="field"><label for={`edit-port-${record.service.id}`}>내부 포트</label><input id={`edit-port-${record.service.id}`} class="input input-bordered" type="number" min="1" max="65535" bind:value={editPort} required /></div>
            <p class="target-edit-note">저장하면 터널 연결이 잠시 끊길 수 있습니다.</p>
            {#if editError}<p class="error" role="alert">{editError}</p>{/if}
            <div class="target-edit-actions"><Button variant="secondary" size="sm" disabled={editBusy} onclick={() => editingTarget = false}>취소</Button><Button type="submit" size="sm" disabled={editBusy}>{editBusy ? '저장 중…' : '저장'}</Button></div>
          </form>
        {/if}
      </div>
    </section>
    {/if}
    {#if view !== 'service'}
    <section class="service-column routes-section" aria-label="공개 주소">
      <div class="section-heading">{#if view === 'both'}<div class="section-title"><h3 class="ui-meta-label">공개 주소</h3><InfoTooltip id={`routes-help-${record.service.id}`} label="공개 주소" description="인터넷에서 연결 서비스에 접속할 HTTPS 주소입니다. 등록된 도메인의 서브도메인을 선택해 추가할 수 있습니다." /></div>{:else}<strong class="route-service-name">{record.service.name}</strong>{/if}{#if baseDomain}<Button variant="text" size="sm" onclick={openRouteDialog}>+ 주소 추가</Button>{/if}</div>
      {#if record.routes.length}
        <ul class="route-list">
          {#each record.routes as route (route.id)}
            {@const status = routeStatus(route)}
            <li>
              <LinkButton href={`https://${route.hostname}`} target="_blank" rel="noopener noreferrer" variant="text" size="sm" class="route-link" aria-label={`https://${route.hostname} 새 탭에서 열기`}><code>https://{route.hostname}</code></LinkButton>
              <span class="route-indicator" data-kind={status.kind} data-tooltip={status.label} role="img" aria-label={status.label} tabindex="0">
                {#if status.kind === 'live'}
                  <svg aria-hidden="true" viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><circle cx="10" cy="10" r="7" /><path d="M3 10h14M10 3c2 2 3 4.3 3 7s-1 5-3 7c-2-2-3-4.3-3-7s1-5 3-7Z" /></svg>
                {:else if status.kind === 'waiting'}
                  <span aria-hidden="true">…</span>
                {:else if status.kind === 'error'}
                  <span aria-hidden="true">!</span>
                {:else}
                  <span aria-hidden="true">–</span>
                {/if}
              </span>
              <Button variant="plain" size="sm" class="route-delete" aria-label={`${route.hostname} 주소 삭제`} title={`${route.hostname} 주소 삭제`} disabled={Boolean(deletingRoute)} onclick={() => deleteRoute(route)}>
                <svg aria-hidden="true" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h16M9 7V4h6v3m3 0-1 13H7L6 7m4 4v5m4-5v5" /></svg>
              </Button>
            </li>
          {/each}
        </ul>
      {:else}
        {#if baseDomain}<p class="route-empty">아직 공개 주소가 없습니다.</p>{/if}
      {/if}
      {#if !baseDomain}
        <p class="route-empty">주소를 추가하려면 먼저 기본 도메인을 설정하세요. <LinkButton href={pagePath.domains} variant="text" size="sm">도메인 관리 ↗</LinkButton></p>
      {/if}
      {#if routeError}<p class="error" role="alert">{routeError}</p>{/if}
    </section>
    {/if}
  </div>
</article>

{#if baseDomain && view !== 'service'}
  <dialog bind:this={routeDialog} class="route-dialog" aria-labelledby="route-dialog-title-{record.service.id}" aria-describedby="route-dialog-description-{record.service.id}" oncancel={(event) => { if (busy) event.preventDefault(); }} onclose={() => { subdomain = ''; selectedDomain = ''; addError = ''; }}>
    <form class="route-form" onsubmit={addRoute}>
      <h2 id="route-dialog-title-{record.service.id}">공개 주소 추가</h2>
      <p id="route-dialog-description-{record.service.id}">연결 서비스에 사용할 도메인과 서브도메인을 선택하세요.</p>
      <div class="field domain-choice"><label for="domain-{record.service.id}">도메인 선택</label><select id="domain-{record.service.id}" value={baseDomain} onchange={(event) => selectedDomain = event.currentTarget.value}>{#each domains as domain}<option value={domain}>{domain}</option>{/each}</select></div>
      <div class="field">
        <label for="route-{record.service.id}">서브도메인</label>
        <div class="domain-input"><input id="route-{record.service.id}" aria-describedby="domain-suffix-{record.service.id}" bind:value={subdomain} placeholder="app" pattern={'[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?'} maxlength="63" autocapitalize="none" spellcheck="false" required /><span class="domain-suffix" id="domain-suffix-{record.service.id}">.{baseDomain}</span></div>
      </div>
      {#if routeHostname}<p class="route-preview">생성될 주소: <strong>https://{routeHostname}</strong></p>{/if}
      {#if addError}<p class="error" role="alert">{addError}</p>{/if}
      <div class="dialog-actions"><Button variant="secondary" disabled={busy} onclick={closeRouteDialog}>취소</Button><Button type="submit" disabled={busy}>{busy ? '추가 중…' : '주소 추가'}</Button></div>
    </form>
  </dialog>
{/if}

<style>
  .service { min-width: 0; }
  .service-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .service-grid.single { grid-template-columns: 1fr; }
  .service-column { min-width: 0; }
  .routes-section { border-left: 1px solid var(--line); padding-left: 20px; }
  .service-grid.single .routes-section { border-left: 0; padding-left: 0; }
  .service-grid.single .routes-section .section-heading { justify-content: space-between; }
  .section-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 24px; margin-bottom: 8px; }
  .section-title { display: flex; align-items: center; gap: 5px; }
  .section-heading h3 { margin: 0; }
  .route-service-name { font-size: 14px; }
  .app-row { display: grid; gap: 5px; min-width: 0; padding: 3px 0; }
  .app-summary, .target-row { display: flex; align-items: center; flex-wrap: wrap; gap: 5px 10px; min-width: 0; }
  .app-summary strong { font-size: 16px; }
  .target-row { gap: 5px 8px; color: var(--muted); font-size: 11px; }
  .target-row code { overflow-wrap: anywhere; color: var(--ink); font-size: 12px; font-weight: 600; user-select: all; }
  .target-row :global(.target-copy) { display: grid; place-items: center; width: 27px; height: 27px; border: 1px solid var(--line); border-radius: 5px; color: var(--muted); }
  .target-row :global(.target-copy:hover) { border-color: #b9aba0; background: #f6f1ec; color: var(--ink); }
  .target-row :global(.target-copy.copied) { border-color: var(--green); color: var(--green); }
  .target-row :global(.target-copy svg) { width: 15px; height: 15px; }
  .target-edit { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 10px; margin-top: 10px; }
  .target-edit input { width: 100%; }
  .target-edit .error, .target-edit-note, .target-edit-actions { grid-column: 1 / -1; }
  .target-edit-note { margin: 0; color: var(--muted); font-size: 11px; }
  .target-edit-actions { display: flex; justify-content: flex-end; gap: 6px; }
  .app-row :global(.connection-status) { margin-left: 0; }
  .route-list { list-style: none; margin: 0; padding: 0; }
  .route-list li { display: grid; grid-template-columns: minmax(0, 1fr) 24px 30px; align-items: center; gap: 10px; min-height: 40px; border-top: 1px solid var(--line); padding: 5px 0; }
  .route-list li:first-child { border-top: 0; }
  .route-list :global(.route-link) { flex: initial; min-width: 0; max-width: 100%; justify-content: flex-start; text-align: left; white-space: normal; }
  .route-list code { overflow-wrap: anywhere; color: inherit; font-size: 14px; font-weight: 650; }
  .route-indicator { position: relative; display: grid; place-items: center; width: 24px; height: 24px; border: 1px solid #eacbb7; border-radius: 6px; background: #fbefe5; color: var(--accent-dark); font-size: 15px; font-weight: 700; line-height: 1; }
  .route-indicator[data-kind='waiting'] { border-color: #ddd6ce; background: #f4f1ec; color: #82766d; }
  .route-indicator[data-kind='error'] { border-color: #e5b6aa; background: #fbede9; color: #aa4939; }
  .route-indicator[data-kind='off'] { border-color: #e2dfda; background: #f6f5f2; color: #99918a; }
  .route-indicator svg { width: 16px; height: 16px; }
  .route-indicator:focus-visible { outline: 2px solid #ef9b6f; outline-offset: 2px; }
  .route-indicator::after { position: absolute; z-index: 2; bottom: calc(100% + 8px); left: 50%; width: max-content; max-width: 190px; border-radius: 5px; padding: 5px 8px; background: #37312e; color: #fff; content: attr(data-tooltip); font-size: 11px; font-weight: 500; line-height: 1.3; pointer-events: none; transform: translateX(-50%); visibility: hidden; }
  .route-indicator:hover::after, .route-indicator:focus-visible::after { visibility: visible; }
  .route-list :global(.route-delete) { display: grid; place-items: center; width: 30px; height: 30px; border: 1px solid transparent; border-radius: 6px; color: #9b8b80; }
  .route-list :global(.route-delete:hover:not(:disabled)) { border-color: #e8c5b8; background: #faf0eb; color: #a5432c; }
  .route-list :global(.route-delete svg) { width: 16px; height: 16px; }
  .route-empty { margin: 0 0 8px !important; color: var(--muted); font-size: 12px; }
  .route-empty :global(.ui-button) { display: inline-flex; }
  .route-dialog { width: min(480px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .route-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .route-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .route-dialog p:not(.error) { margin: 0; color: var(--muted); font-size: 12px; }
  .route-form { display: grid; gap: 13px; }
  .domain-choice select { width: 100%; min-height: 36px; border: 1px solid #d8d1c9; border-radius: 6px; padding: 5px 8px; background: #fffdfa; color: inherit; }
  .domain-input { display: flex; align-items: center; gap: 4px 0; width: 100%; min-height: 36px; border: 1px solid #d8d1c9; border-radius: 6px; background: #fffdfa; }
  .domain-input:focus-within { outline: 2px solid #ef9b6f; outline-offset: 2px; }
  .domain-input input { flex: 1 1 110px; width: 100%; min-width: 0; border: 0; border-radius: 6px; padding: 6px 10px; background: transparent; outline: 0; box-shadow: none; }
  .domain-input input:focus-visible { outline: 0; }
  .domain-suffix { flex: none; padding: 6px 10px 6px 4px; color: var(--muted); font-size: 12px; white-space: nowrap; }
  .route-preview { overflow-wrap: anywhere; }
  .route-preview strong { color: var(--accent-dark); font-weight: 650; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 5px; }
  @media (max-width: 700px) {
    .service-grid { grid-template-columns: 1fr; gap: 14px; }
    .routes-section { border-top: 1px solid var(--line); border-left: 0; padding-top: 12px; padding-left: 0; }
    .service-grid.single .routes-section { border-top: 0; padding-top: 0; }
    .target-edit { grid-template-columns: 1fr; }
  }
</style>
