<script lang="ts">
  import { statusLabel } from '../api';
  import type { DashboardData, PageKey, Route } from '../types';
  import Button from './ui/Button.svelte';
  import Card from './ui/Card.svelte';

  let { data, navigate, onOpenDetail }: { data: DashboardData; navigate: (view: PageKey) => Promise<void>; onOpenDetail: (id: string) => void } = $props();

  const visible = $derived(data.records.slice(0, 4));
  const hasConnectedTunnel = $derived(data.connectors.some((connector) => connector.status === 'connected'));

  function openFirstTunnel(): void {
    const connector = data.connectors.find((item) => item.status === 'connected') ?? data.connectors[0];
    if (connector) onOpenDetail(connector.id);
    else void navigate('connectors');
  }

  function routeReady(route?: Route): boolean {
    return Boolean(route?.enabled && route.applied_revision >= route.desired_revision && !route.last_error);
  }

  function routeStatus(route?: Route): string {
    if (!route) return '미설정';
    if (!route.enabled) return '사용 안 함';
    if (route.last_error) return '오류';
    if (route.applied_revision < route.desired_revision) return '반영 중';
    return '반영됨';
  }
</script>

<section class="connection-section" aria-labelledby="connection-heading">
  <div class="connection-heading">
    <div>
      <p class="section-kicker">CONNECTION MAP</p>
      <h2 id="connection-heading">서비스 연결 경로</h2>
      <p>외부 주소에서 터널을 거쳐 연결 서비스로 이어지는 경로입니다.</p>
    </div>
    <Button variant="text" size="sm" class="map-link" onclick={() => navigate('connectors')}>터널 관리 <span aria-hidden="true">↗</span></Button>
  </div>

  {#if visible.length}
    <div class="flow-list">
      {#each visible as record (record.service.id)}
        {@const route = record.routes[0]}
        <Card as="article" padding="none" class="path-card">
          <div class="path-step">
            <span class="step-label">외부 주소</span>
            <div class="step-name">
              <strong title={route?.hostname || '공개 주소 미설정'}>{route?.hostname || '공개 주소 미설정'}</strong>
              {#if record.routes.length > 1}<span class="extra-routes">+{record.routes.length - 1}</span>{/if}
            </div>
            <span class:active={routeReady(route)} class="step-status"><span class="status-dot" aria-hidden="true"></span>{routeStatus(route)}</span>
          </div>
          <span class="path-arrow" aria-hidden="true">→</span>
          <div class="path-step">
            <span class="step-label">터널</span>
            <div class="step-name"><strong title={record.connector.name}>{record.connector.name}</strong></div>
            <span class:active={record.connector.status === 'connected'} class="step-status"><span class="status-dot" aria-hidden="true"></span>{statusLabel(record.connector.status)}</span>
          </div>
          <span class="path-arrow" aria-hidden="true">→</span>
          <div class="path-step">
            <span class="step-label">연결 서비스</span>
            <div class="step-name"><strong title={record.service.name}>{record.service.name}</strong></div>
            <span class:active={record.service.enabled && record.service.observed_status === 'connected'} class="step-status"><span class="status-dot" aria-hidden="true"></span>{record.service.enabled ? statusLabel(record.service.observed_status) : '사용 안 함'} <span class="target-address">· {record.service.target.host}:{record.service.target.port}</span></span>
          </div>
        </Card>
      {/each}
    </div>
    {#if data.records.length > visible.length}<p class="more-records">이 외 {data.records.length - visible.length}개의 연결이 더 있습니다.</p>{/if}
  {:else}
    <Card variant="solid" padding="none" class="map-empty">
      <p>아직 표시할 연결이 없습니다.</p>
      <Button variant="text" size="sm" onclick={openFirstTunnel}>{hasConnectedTunnel ? '첫 서비스 등록' : '첫 터널 만들기'} <span aria-hidden="true">↗</span></Button>
    </Card>
  {/if}
</section>

<style>
  .connection-section { margin: 0 0 20px; }
  .connection-heading { display: flex; align-items: end; justify-content: space-between; gap: 16px; margin-bottom: 10px; }
  .section-kicker { margin: 0 0 5px; color: var(--accent-dark); font-size: 10px; font-weight: 750; letter-spacing: .14em; }
  .connection-heading h2 { margin: 0 0 3px; font-size: 18px; font-weight: 700; letter-spacing: -.035em; }
  .connection-heading p:not(.section-kicker) { margin: 0; color: var(--muted); font-size: 12px; }
  .connection-heading :global(.map-link) { flex: none; }
  .flow-list { display: grid; gap: 8px; }
  .flow-list :global(.path-card) { display: grid; grid-template-columns: minmax(0, 1fr) 24px minmax(0, 1fr) 24px minmax(0, 1fr); align-items: center; gap: 10px; padding: 12px 16px; }
  .path-step { min-width: 0; }
  .step-label { display: block; margin-bottom: 3px; color: #81766f; font-size: 11px; font-weight: 650; }
  .step-name { display: flex; align-items: baseline; gap: 7px; min-width: 0; color: var(--ink); font-size: 15px; line-height: 1.35; }
  .step-name strong { min-width: 0; overflow: hidden; font-weight: 650; text-overflow: ellipsis; white-space: nowrap; }
  .extra-routes { flex: none; color: var(--muted); font-size: 11px; }
  .step-status { display: flex; align-items: center; gap: 5px; min-width: 0; margin-top: 4px; color: #8a817a; font-size: 11px; }
  .step-status.active { color: #4d8069; }
  .status-dot { flex: none; width: 6px; height: 6px; border-radius: 50%; background: currentColor; }
  .target-address { min-width: 0; overflow: hidden; color: #8a817a; text-overflow: ellipsis; white-space: nowrap; }
  .path-arrow { color: #b49e8d; font-size: 19px; text-align: center; }
  .more-records { margin: 9px 0 0; color: #82766d; font-size: 11px; text-align: center; }
  :global(.map-empty) { display: grid; min-height: 84px; align-content: center; justify-items: center; gap: 4px; border: 1px dashed #d4c2b4; padding: 14px; background: transparent; color: #766d65; }
  :global(.map-empty) p { margin: 0; }
  @media (max-width: 700px) {
    .connection-heading { align-items: start; }
    .flow-list :global(.path-card) { grid-template-columns: 1fr; gap: 4px; padding: 12px 14px; }
    .path-arrow { padding-left: 2px; font-size: 17px; line-height: 1; text-align: left; transform: rotate(90deg); transform-origin: 9px center; }
    .path-step { padding: 3px 0; }
  }
  @media (max-width: 470px) {
    .connection-heading { display: block; }
    .connection-heading :global(.map-link) { margin-top: 7px; }
  }
</style>
