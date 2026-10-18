<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import { connectorState } from '../connectorState.svelte';
  import type { Outlet, ServiceRecord } from '../types';
  import Status from './Status.svelte';
  import Button from './ui/Button.svelte';

  let { record, outlet, onChanged }: { record: ServiceRecord; outlet: Outlet; onChanged: () => Promise<void> } = $props();
  let hostname = $state('');
  let error = $state('');
  let busy = $state(false);
  let showRouteForm = $state(false);

  async function addRoute(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await api(`/api/v1/services/${encodeURIComponent(record.service.id)}/routes`, {
        method: 'POST',
        body: JSON.stringify({ hostname }),
      });
      hostname = '';
      await onChanged();
      showRouteForm = false;
    } catch (cause) {
      error = `공개 주소를 만들지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      busy = false;
    }
  }

  async function deleteService(): Promise<void> {
    if (!confirm(`내부 앱 ${record.service.name}을(를) 삭제할까요?`)) return;
    error = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, { method: 'DELETE' });
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      await onChanged();
    } catch (cause) {
      error = `내부 앱을 삭제하지 못했습니다: ${errorMessage(cause)}`;
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
    </section>
    <section class="service-column routes-section" aria-label="공개 주소">
      <div class="section-heading"><h3>공개 주소</h3>{#if record.routes.length && !showRouteForm}<Button variant="text" size="sm" onclick={() => showRouteForm = true}>+ 주소 추가</Button>{/if}</div>
      {#if record.routes.length}
        <ul class="route-list">
          {#each record.routes as route (route.id)}
            <li><code>https://{route.hostname}</code><span class="route-status">{routeStatus(route)}</span></li>
          {/each}
        </ul>
      {:else}
        <p class="route-empty">아직 공개 주소가 없습니다.</p>
      {/if}
      {#if !record.routes.length || showRouteForm}
        <form class="route-form" onsubmit={addRoute}>
          <div class="field"><label for="route-{record.service.id}">새 공개 주소</label><input class="input input-bordered" id="route-{record.service.id}" bind:value={hostname} placeholder={`예: app.${outlet.domain || 'saeoo.com'}`} required /></div>
          <div class="form-actions"><Button type="submit" disabled={busy}>{busy ? '추가 중…' : '주소 추가'}</Button>{#if record.routes.length}<Button variant="text" onclick={() => { showRouteForm = false; error = ''; }}>취소</Button>{/if}</div>
        </form>
      {/if}
    </section>
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
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
  .route-form { display: flex; align-items: end; gap: 10px; margin-top: 9px; }
  .route-form .field { flex: 1; min-width: 180px; }
  .route-form .form-actions { flex: none; margin: 0; }
  @media (max-width: 600px) {
    .route-form { align-items: stretch; flex-direction: column; }
    .route-form .field { flex: auto; min-width: 0; width: 100%; }
  }
</style>
