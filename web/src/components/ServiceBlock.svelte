<script lang="ts">
  import { api, errorMessage, statusLabel } from '../api';
  import { connectorState } from '../connectorState.svelte';
  import type { Outlet, ServiceRecord } from '../types';
  import Status from './Status.svelte';
  import Button from './ui/Button.svelte';
  import Card from './ui/Card.svelte';

  let { record, outlet, onChanged }: { record: ServiceRecord; outlet: Outlet; onChanged: () => Promise<void> } = $props();
  let hostname = $state('');
  let error = $state('');
  let busy = $state(false);

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
</script>

<article class="service">
  <div class="detail-title">
    <div>
      <h3>{record.service.name}</h3>
      <div class="service-meta">{record.connector.name} · {record.service.protocol} · {record.service.target.host}:{record.service.target.port}</div>
    </div>
    <Status label={statusLabel(record.service.observed_status)} value={record.service.observed_status} />
  </div>
  {#if record.routes.length === 0}
    <p class="muted service-empty">아직 공개 주소가 없습니다.</p>
    <Button variant="danger" onclick={deleteService}>내부 앱 삭제</Button>
  {/if}
  {#each record.routes as route}
    <Card variant="solid" padding="none" class="route-line"><code>https://{route.hostname}</code><span>{record.connector.status === 'connected' && record.service.observed_status === 'connected' ? '공개 중' : '연결 후 자동 공개'}</span></Card>
  {/each}
  <form class="form-actions route-form" onsubmit={addRoute}>
    <div class="field"><label for="route-{record.service.id}">공개 주소</label><input class="input input-bordered" id="route-{record.service.id}" bind:value={hostname} placeholder={`예: app.${outlet.domain || 'saeoo.com'}`} required /></div>
    <Button type="submit" disabled={busy}>{busy ? '추가 중…' : '공개 주소 추가'}</Button>
  </form>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</article>

<style>
  .detail-title { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; }
  .service { border-top: 1px solid var(--line); padding: 17px 0; }
  .service:first-of-type { border-top: 0; padding-top: 0; }
  .service-meta { color: var(--muted); font-size: 12px; }
  .service-empty { margin: 12px 0 !important; }
  :global(.route-line) { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-top: 10px; border-radius: 5px; padding: 10px 12px; }
  :global(.route-line code) { overflow-wrap: anywhere; color: #345a70; font-size: 12px; }
  .route-form { align-items: end; }
  .route-form .field { flex: 1; min-width: 180px; }
  @media (max-width: 600px) {
    :global(.route-line) { align-items: flex-start; flex-direction: column; }
    .route-form { align-items: stretch; flex-direction: column; }
    .route-form .field { flex: auto; min-width: 0; width: 100%; }
  }
</style>
