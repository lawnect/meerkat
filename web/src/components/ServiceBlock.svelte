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
  let routeError = $state('');
  let serviceError = $state('');
  let busy = $state(false);
  let showRouteForm = $state(false);
  const baseDomain = $derived(outlet.domain?.trim().toLowerCase() || '');
  const routeHostname = $derived(subdomain.trim().toLowerCase() ? `${subdomain.trim().toLowerCase()}.${baseDomain}` : '');

  async function addRoute(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!baseDomain || !routeHostname) return;
    routeError = '';
    busy = true;
    try {
      await api(`/api/v1/services/${encodeURIComponent(record.service.id)}/routes`, {
        method: 'POST',
        body: JSON.stringify({ hostname: routeHostname }),
      });
      subdomain = '';
      await onChanged();
      showRouteForm = false;
    } catch (cause) {
      const code = errorMessage(cause);
      routeError = code === 'hostname_taken' ? '이미 사용 중인 주소입니다. 다른 서브도메인을 입력하세요.' : code === 'invalid_route' ? '서브도메인을 확인하세요. 영문자, 숫자, 하이픈만 사용할 수 있습니다.' : `공개 주소를 만들지 못했습니다: ${code}`;
    } finally {
      busy = false;
    }
  }

  async function deleteService(): Promise<void> {
    if (!confirm(`내부 앱 ${record.service.name}을(를) 삭제할까요?`)) return;
    serviceError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, { method: 'DELETE' });
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      await onChanged();
    } catch (cause) {
      serviceError = `내부 앱을 삭제하지 못했습니다: ${errorMessage(cause)}`;
    }
  }

  function routeStatus(route: ServiceRecord['routes'][number]): string {
    if (!route.enabled) return '사용 안 함';
    if (route.last_error) return '설정 오류';
    if (route.applied_revision < route.desired_revision) return '반영 중';
    if (!record.service.enabled) return '앱 사용 안 함';
    if (record.connector.status !== 'connected') return 'Connector 연결 대기';
    if (record.service.observed_status !== 'connected') return '앱 응답 대기';
    return '공개 중';
  }
</script>

<article class="service">
  <div class="service-grid">
    <section class="service-column" aria-label="내부 앱">
      <div class="section-heading"><h3>내부 앱</h3><Button variant="text" size="sm" onclick={deleteService}>앱 삭제</Button></div>
      <div class="app-row"><strong>{record.service.name}</strong><code>{record.service.target.host}:{record.service.target.port}</code><Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} /></div>
      {#if serviceError}<p class="error" role="alert">{serviceError}</p>{/if}
    </section>
    <section class="service-column routes-section" aria-label="공개 주소">
      <div class="section-heading"><h3>공개 주소</h3>{#if baseDomain && record.routes.length && !showRouteForm}<Button variant="text" size="sm" onclick={() => showRouteForm = true}>+ 주소 추가</Button>{/if}</div>
      {#if record.routes.length}
        <ul class="route-list">
          {#each record.routes as route (route.id)}
            <li><code>https://{route.hostname}</code><span class="route-status">{routeStatus(route)}</span></li>
          {/each}
        </ul>
      {:else}
        {#if baseDomain}<p class="route-empty">아직 공개 주소가 없습니다.</p>{/if}
      {/if}
      {#if !baseDomain}
        <p class="route-empty">주소를 추가하려면 먼저 기본 도메인을 설정하세요. <LinkButton href={pagePath.settings} variant="text" size="sm">도메인 설정 ↗</LinkButton></p>
      {:else if !record.routes.length || showRouteForm}
        <form class="route-form" onsubmit={addRoute}>
          <div class="field">
            <label for="route-{record.service.id}">서브도메인</label>
            <div class="domain-input"><input id="route-{record.service.id}" aria-describedby="domain-suffix-{record.service.id}" bind:value={subdomain} placeholder="app" pattern="[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?" maxlength="63" autocapitalize="none" spellcheck="false" required /><span class="domain-suffix" id="domain-suffix-{record.service.id}">.{baseDomain}</span></div>
            {#if routeHostname}<small class="route-preview">생성될 주소: https://{routeHostname}</small>{/if}
          </div>
          <div class="form-actions"><Button type="submit" disabled={busy}>{busy ? '추가 중…' : '주소 추가'}</Button>{#if record.routes.length}<Button variant="text" onclick={() => { showRouteForm = false; routeError = ''; subdomain = ''; }}>취소</Button>{/if}</div>
        </form>
      {/if}
      {#if routeError}<p class="error" role="alert">{routeError}</p>{/if}
    </section>
  </div>
</article>

<style>
  .service { min-width: 0; }
  .service-grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(min(100%, 320px), 1fr)); gap: 14px 22px; }
  .service-column { min-width: 0; }
  .section-heading { display: flex; align-items: center; justify-content: space-between; gap: 10px; min-height: 27px; margin-bottom: 7px; }
  .section-heading h3 { margin: 0; font-size: 14px; }
  .app-row { display: flex; align-items: center; flex-wrap: wrap; gap: 5px 10px; min-height: 29px; }
  .app-row strong { font-size: 15px; }
  .app-row code { color: var(--muted); font-size: 12px; }
  .app-row :global(.connection-status) { margin-left: 0; }
  .route-list { list-style: none; margin: 0; padding: 0; }
  .route-list li { display: flex; align-items: baseline; flex-wrap: wrap; gap: 4px 10px; border-top: 1px solid var(--line); padding: 6px 0; }
  .route-list li:first-child { border-top: 0; }
  .route-list code { overflow-wrap: anywhere; color: var(--accent-dark); font-size: 13px; font-weight: 650; }
  .route-status { flex: none; color: var(--muted); font-size: 12px; }
  .route-empty { margin: 0 0 8px !important; color: var(--muted); font-size: 12px; }
  .route-empty :global(.ui-button) { display: inline-flex; }
  .route-form { display: flex; align-items: end; gap: 10px; margin-top: 9px; }
  .route-form .field { flex: 1; min-width: 180px; }
  .domain-input { display: inline-flex; align-items: center; flex-wrap: wrap; gap: 4px 0; width: fit-content; max-width: 100%; min-height: 32px; border: 1px solid #d8d1c9; border-radius: 6px; background: #fffdfa; }
  .domain-input:focus-within { outline: 2px solid #ef9b6f; outline-offset: 2px; }
  .domain-input input { flex: 0 1 14ch; width: 14ch; min-width: 110px; border: 0; border-radius: 6px; padding: 6px 10px; background: transparent; outline: 0; box-shadow: none; }
  .domain-input input:focus-visible { outline: 0; }
  .domain-suffix { flex: none; padding: 6px 10px 6px 4px; color: var(--muted); font-size: 12px; white-space: nowrap; }
  .route-preview { overflow-wrap: anywhere; color: var(--muted); }
  .route-form .form-actions { flex: none; margin: 0; }
  @media (max-width: 600px) {
    .route-form { align-items: stretch; flex-direction: column; }
    .route-form .field { flex: auto; min-width: 0; width: 100%; }
  }
</style>
