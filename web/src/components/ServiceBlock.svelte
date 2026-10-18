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
  <div class="section-heading">
    <div><h3>내부 앱</h3><p>이 Connector가 연결하는 앱입니다.</p></div>
    <Button variant="text" size="sm" onclick={deleteService}>앱 삭제</Button>
  </div>
  <div class="app-row"><strong>{record.service.name}</strong><code>{record.service.target.host}:{record.service.target.port}</code><Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} /></div>
  <div class="routes-section">
    <div class="section-heading"><div><h3>공개 주소</h3><p>외부에서 이 앱에 접속할 주소입니다.</p></div>{#if record.routes.length && !showRouteForm}<Button variant="text" size="sm" onclick={() => showRouteForm = true}>+ 주소 추가</Button>{/if}</div>
    {#if record.routes.length}
      <ul class="route-list">
        {#each record.routes as route (route.id)}
          <li><code>https://{route.hostname}</code><span class="route-status">{routeStatus(route)}</span></li>
        {/each}
      </ul>
    {:else}
      <p class="route-empty">아직 공개 주소가 없습니다. 앱에 사용할 주소를 추가하세요.</p>
    {/if}
    {#if !record.routes.length || showRouteForm}
      <form class="route-form" onsubmit={addRoute}>
        <div class="field"><label for="route-{record.service.id}">새 공개 주소</label><input class="input input-bordered" id="route-{record.service.id}" bind:value={hostname} placeholder={`예: app.${outlet.domain || 'saeoo.com'}`} required /></div>
        <div class="form-actions"><Button type="submit" disabled={busy}>{busy ? '추가 중…' : '주소 추가'}</Button>{#if record.routes.length}<Button variant="text" onclick={() => { showRouteForm = false; error = ''; }}>취소</Button>{/if}</div>
      </form>
    {/if}
  </div>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</article>

<style>
  .service { margin-top: 22px; border-top: 1px solid var(--line); padding-top: 22px; }
  .section-heading { display: flex; align-items: flex-start; justify-content: space-between; gap: 14px; margin-bottom: 12px; }
  .section-heading h3 { margin: 0 0 4px; font-size: 15px; }
  .section-heading p { margin: 0; color: var(--muted); font-size: 12px; }
  .app-row { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 14px; padding: 8px 0; }
  .app-row strong { font-size: 15px; }
  .app-row code { color: var(--muted); font-size: 12px; }
  .app-row :global(.connection-status) { margin-left: auto; }
  .routes-section { margin-top: 24px; border-top: 1px solid var(--line); padding-top: 22px; }
  .route-list { list-style: none; margin: 0; padding: 0; }
  .route-list li { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-top: 1px solid var(--line); padding: 13px 0; }
  .route-list li:first-child { border-top: 0; }
  .route-list code { overflow-wrap: anywhere; color: var(--accent-dark); font-size: 13px; font-weight: 650; }
  .route-status { flex: none; color: var(--muted); font-size: 12px; }
  .route-empty { margin: 0 0 15px !important; color: var(--muted); font-size: 12px; }
  .route-form { display: flex; align-items: end; gap: 12px; margin-top: 12px; }
  .route-form .field { flex: 1; min-width: 180px; }
  .route-form .form-actions { flex: none; margin: 0; }
  @media (max-width: 600px) {
    .route-list li { align-items: flex-start; flex-direction: column; gap: 5px; }
    .route-form { align-items: stretch; flex-direction: column; }
    .route-form .field { flex: auto; min-width: 0; width: 100%; }
  }
</style>
