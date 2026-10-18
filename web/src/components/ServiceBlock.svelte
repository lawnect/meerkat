<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import { connectorState } from '../connectorState.svelte';
  import type { Outlet, ServiceRecord } from '../types';
  import Status from './Status.svelte';
  import Button from './ui/Button.svelte';
  import LinkButton from './ui/LinkButton.svelte';
  import { pagePath } from '../navigation';

  let { record, outlet, onChanged }: { record: ServiceRecord; outlet: Outlet; onChanged: () => Promise<void> } = $props();
  let subdomain = $state('');
  let selectedDomain = $state('');
  let addError = $state('');
  let routeError = $state('');
  let serviceError = $state('');
  let busy = $state(false);
  let deletingRoute = $state('');
  let deletingService = $state(false);
  let routeDialog = $state<HTMLDialogElement>();
  const domains = $derived(outlet.domains?.length ? outlet.domains : outlet.domain ? [outlet.domain] : []);
  const baseDomain = $derived(domains.includes(selectedDomain) ? selectedDomain : domains[0] || '');
  const routeHostname = $derived(subdomain.trim().toLowerCase() && baseDomain ? `${subdomain.trim().toLowerCase()}.${baseDomain}` : '');

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

  async function deleteService(): Promise<void> {
    if (record.routes.length || deletingService) return;
    if (!confirm(`미어캣에서 내부 앱 ${record.service.name}의 등록을 해제할까요? 실제 앱은 삭제되지 않습니다.`)) return;
    serviceError = '';
    deletingService = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, { method: 'DELETE' });
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      await onChanged();
    } catch (cause) {
      const code = errorMessage(cause);
      serviceError = code === 'service_has_routes' ? '공개 주소를 모두 삭제한 뒤 등록을 해제하세요.' : `내부 앱 등록을 해제하지 못했습니다: ${code}`;
    } finally {
      deletingService = false;
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
    if (!record.service.enabled) return { label: '앱 사용 안 함', kind: 'off' };
    if (record.connector.status !== 'connected') return { label: 'Connector 연결 대기', kind: 'waiting' };
    if (record.service.observed_status !== 'connected') return { label: '앱 응답 대기', kind: 'waiting' };
    return { label: '공개 중', kind: 'live' };
  }
</script>

<article class="service">
  <div class="service-grid">
    <section class="service-column" aria-label="내부 앱">
      <div class="section-heading"><h3>내부 앱</h3><Button variant="text" size="sm" disabled={record.routes.length > 0 || deletingService} onclick={deleteService}>{deletingService ? '해제 중…' : '내부 앱 등록 해제'}</Button></div>
      <div class="app-row"><strong>{record.service.name}</strong><code>{record.service.target.host}:{record.service.target.port}</code><Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} /></div>
      {#if record.routes.length}<p class="service-hint">공개 주소 {record.routes.length}개를 삭제하면 내부 앱 등록을 해제할 수 있습니다.</p>{/if}
      {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
    </section>
    <section class="service-column routes-section" aria-label="공개 주소">
      <div class="section-heading"><h3>공개 주소</h3>{#if baseDomain}<Button variant="text" size="sm" onclick={openRouteDialog}>+ 주소 추가</Button>{/if}</div>
      {#if record.routes.length}
        <ul class="route-list">
          {#each record.routes as route (route.id)}
            {@const status = routeStatus(route)}
            <li>
              <code>https://{route.hostname}</code>
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
        <p class="route-empty">주소를 추가하려면 먼저 기본 도메인을 설정하세요. <LinkButton href={pagePath.settings} variant="text" size="sm">도메인 설정 ↗</LinkButton></p>
      {/if}
      {#if routeError}<p class="error" role="alert">{routeError}</p>{/if}
    </section>
  </div>
</article>

{#if baseDomain}
  <dialog bind:this={routeDialog} class="route-dialog" aria-labelledby="route-dialog-title-{record.service.id}" aria-describedby="route-dialog-description-{record.service.id}" oncancel={(event) => { if (busy) event.preventDefault(); }} onclose={() => { subdomain = ''; selectedDomain = ''; addError = ''; }}>
    <form class="route-form" onsubmit={addRoute}>
      <h2 id="route-dialog-title-{record.service.id}">공개 주소 추가</h2>
      <p id="route-dialog-description-{record.service.id}">앱에 연결할 도메인과 서브도메인을 선택하세요.</p>
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
  .service-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr)); gap: 14px 22px; }
  .service-column { min-width: 0; }
  .section-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 27px; margin-bottom: 7px; }
  .section-heading h3 { margin: 0; font-size: 14px; }
  .app-row { display: flex; align-items: center; flex-wrap: wrap; gap: 5px 10px; min-height: 29px; }
  .app-row strong { font-size: 15px; }
  .app-row code { color: var(--muted); font-size: 12px; }
  .service-hint { margin: 7px 0 0; color: var(--muted); font-size: 11px; }
  .app-row :global(.connection-status) { margin-left: 0; }
  .route-list { list-style: none; margin: 0; padding: 0; }
  .route-list li { display: grid; grid-template-columns: minmax(0, 1fr) 24px 30px; align-items: center; gap: 10px; min-height: 40px; border-top: 1px solid var(--line); padding: 5px 0; }
  .route-list li:first-child { border-top: 0; }
  .route-list code { overflow-wrap: anywhere; color: var(--accent-dark); font-size: 13px; font-weight: 650; }
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
</style>
