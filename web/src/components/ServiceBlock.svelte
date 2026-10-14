<script>
  import { api, statusLabel } from '../api.js';
  import { connectorState } from '../connectorState.svelte.js';
  import Status from './Status.svelte';

  let { record, outlet, onChanged } = $props();
  let hostname = $state('');
  let error = $state('');
  let busy = $state(false);

  async function addRoute(event) {
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
      error = `공개 주소를 만들지 못했습니다: ${cause.message}`;
    } finally {
      busy = false;
    }
  }

  async function deleteService() {
    if (!confirm(`서비스 ${record.service.name} 을(를) 삭제할까요?`)) return;
    error = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(record.connector.id)}/services/${encodeURIComponent(record.service.id)}`, { method: 'DELETE' });
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      await onChanged();
    } catch (cause) {
      error = `서비스를 삭제하지 못했습니다: ${cause.message}`;
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
    <button class="button secondary" onclick={deleteService}>서비스 삭제</button>
  {/if}
  {#each record.routes as route}
    <div class="route-line"><code>https://{route.hostname}</code><span>연결 후 자동 공개</span></div>
  {/each}
  <form class="form-actions route-form" onsubmit={addRoute}>
    <div class="field"><label for="route-{record.service.id}">공개 주소</label><input id="route-{record.service.id}" bind:value={hostname} placeholder={`예: app.${outlet.domain || 'saeoo.com'}`} required /></div>
    <button class="button" type="submit" disabled={busy}>{busy ? '추가 중…' : '공개 주소 추가'}</button>
  </form>
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</article>
