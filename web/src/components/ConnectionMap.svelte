<script lang="ts">
  import type { DashboardData, PageKey } from '../types';
  import Button from './ui/Button.svelte';
  import Card from './ui/Card.svelte';

  let { data, navigate }: { data: DashboardData; navigate: (view: PageKey) => Promise<void> } = $props();

  const visible = $derived(data.records.slice(0, 4));
  const connected = $derived(data.connectors.filter((connector) => connector.status === 'connected').length);
  const appliedRoutes = $derived(data.routes.filter((route) => route.enabled && route.applied_revision >= route.desired_revision && !route.last_error).length);
  const healthyServices = $derived(data.services.filter((service) => service.observed_status === 'connected').length);

  function routeLabel(hostname?: string): string {
    return hostname || '공개 주소 미설정';
  }
</script>

<section class="connection-section" aria-labelledby="connection-heading">
  <div class="connection-heading">
    <div>
      <p class="section-kicker">CONNECTION MAP</p>
      <h2 id="connection-heading">서비스 연결 구조</h2>
      <p>공개 주소에서 Connector를 거쳐 내부 앱까지 이어지는 경로입니다.</p>
    </div>
    <Button variant="text" size="sm" class="map-link" onclick={() => navigate('routes')}>공개 서비스 보기 <span aria-hidden="true">↗</span></Button>
  </div>

  <Card padding="none" class="map-canvas">
    <div class="signal-track" aria-hidden="true"></div>
    <div class="glass-metrics">
      <Card variant="glass" padding="sm" class="glass-card">
        <span class="glass-label">공개 주소</span>
        <strong>{appliedRoutes} / {data.routes.length}</strong>
        <small>설정이 반영된 주소</small>
      </Card>
      <Card variant="glass" padding="sm" class="glass-card">
        <span class="glass-label">Connector</span>
        <strong>{connected} / {data.connectors.length}</strong>
        <small>현재 연결된 Connector</small>
      </Card>
      <Card variant="glass" padding="sm" class="glass-card">
        <span class="glass-label">내부 앱</span>
        <strong>{healthyServices} / {data.services.length}</strong>
        <small>정상 응답 중인 앱</small>
      </Card>
    </div>

    <div class="column-headings" aria-hidden="true">
      <span>외부 주소</span><span>CONNECTOR</span><span>내부 앱</span>
    </div>

    {#if visible.length}
      <div class="flow-list">
        {#each visible as record (record.service.id)}
          <Card padding="none" class="flow-row">
            <Card padding="none" class={record.routes.length ? 'flow-node' : 'flow-node missing'}>
              <span class="mobile-label">외부 주소</span>
              <span class="node-dot" aria-hidden="true"></span>
              <span class="node-text">{routeLabel(record.routes[0]?.hostname)}</span>
              {#if record.routes.length > 1}<small>+{record.routes.length - 1}</small>{/if}
            </Card>
            <span class="flow-line" aria-hidden="true"></span>
            <Card padding="none" class={record.connector.status === 'connected' ? 'flow-node connector-node connected' : 'flow-node connector-node'}>
              <span class="mobile-label">Connector</span>
              <span class="node-dot" aria-hidden="true"></span>
              <span class="node-text">{record.connector.name}</span>
            </Card>
            <span class="flow-line" aria-hidden="true"></span>
            <Card padding="none" class="flow-node">
              <span class="mobile-label">내부 앱</span>
              <span class="node-dot" aria-hidden="true"></span>
              <span class="node-text">{record.service.name}</span>
              <small>{record.service.target.port}</small>
            </Card>
          </Card>
        {/each}
      </div>
      {#if data.records.length > visible.length}<p class="more-records">이 외 {data.records.length - visible.length}개의 연결이 더 있습니다.</p>{/if}
    {:else}
      <Card padding="none" class="map-empty">
        <p>아직 표시할 연결이 없습니다.</p>
        <Button variant="text" size="sm" onclick={() => navigate('connectors')}>첫 Connector 만들기 <span aria-hidden="true">↗</span></Button>
      </Card>
    {/if}
  </Card>
</section>

<style>
  .connection-section { margin: 0 0 28px; }
  .connection-heading { display: flex; align-items: end; justify-content: space-between; gap: 20px; margin-bottom: 15px; }
  .section-kicker { margin: 0 0 5px; color: var(--accent-dark); font-size: 10px; font-weight: 750; letter-spacing: .14em; }
  .connection-heading h2 { margin: 0 0 3px; font-size: 18px; font-weight: 700; letter-spacing: -.035em; }
  .connection-heading p:not(.section-kicker) { margin: 0; color: var(--muted); font-size: 12px; }
  .connection-heading :global(.map-link) { flex: none; }
  :global(.map-canvas) { position: relative; overflow: hidden; border-color: #eadbd0; border-radius: 10px; padding: 30px 30px 22px; background: #f6eee7; }
  .signal-track { position: absolute; top: 86px; left: 0; right: 0; height: 1px; background: #c9b9ab; }
  .signal-track::before, .signal-track::after { content: ''; position: absolute; top: -3px; width: 7px; height: 7px; border: 1px solid #bda999; border-radius: 50%; background: #f6eee7; }
  .signal-track::before { left: 7%; }
  .signal-track::after { right: 7%; }
  .glass-metrics { position: relative; z-index: 1; display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 18px; max-width: 760px; margin: 0 auto 32px; }
  .glass-metrics :global(.glass-card) { min-height: 104px; }
  .glass-label, .glass-metrics small { display: block; color: #756c65; font-size: 11px; }
  .glass-metrics strong { display: block; color: #282522; font-size: 25px; line-height: 1.25; font-weight: 650; letter-spacing: -.045em; }
  .column-headings { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 10%; margin-bottom: 10px; color: #706761; font-size: 10px; font-weight: 700; letter-spacing: .09em; text-transform: uppercase; }
  .column-headings span:nth-child(2) { text-align: center; }
  .column-headings span:nth-child(3) { text-align: right; }
  .flow-list { display: grid; gap: 9px; }
  :global(.flow-row) { display: grid; grid-template-columns: minmax(0,1fr) 10% minmax(0,1fr) 10% minmax(0,1fr); align-items: center; border: 0; border-radius: 0; background: transparent; }
  :global(.flow-node) { display: flex; align-items: center; gap: 8px; min-width: 0; min-height: 41px; border-color: #e9ddd4; border-radius: 6px; padding: 7px 10px; background: rgba(255,255,255,.76); color: #36312d; font-size: 12px; }
  :global(.flow-node) .node-dot { flex: none; width: 6px; height: 6px; border-radius: 50%; background: #8b918e; }
  :global(.flow-node.missing) { color: #8b8179; }
  :global(.flow-node.missing) .node-dot { background: #b9b0a7; }
  :global(.flow-node) .node-text { min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  :global(.flow-node) small { margin-left: auto; color: #9d938b; font-size: 10px; }
  :global(.connector-node) { justify-content: center; }
  :global(.connector-node.connected) { border-color: #f3c5ac; }
  :global(.connector-node.connected) .node-dot { background: var(--accent); box-shadow: 0 0 0 3px #fde3d2; }
  .flow-line { height: 1px; background: #c7bdb4; }
  .more-records { margin: 12px 0 0; color: #82766d; font-size: 11px; text-align: center; }
  :global(.map-empty) { display: grid; min-height: 115px; align-content: center; justify-items: center; gap: 4px; border: 1px dashed #d4c2b4; border-radius: 6px; background: transparent; color: #766d65; }
  :global(.map-empty) p { margin: 0; }
  .mobile-label { display: none; }
  @media (max-width: 700px) {
    .connection-heading { align-items: start; }
    :global(.map-canvas) { padding: 18px; }
    .signal-track { top: 70px; }
    .glass-metrics { gap: 7px; margin-bottom: 24px; }
    .glass-metrics :global(.glass-card) { min-height: 88px; padding: 10px; }
    .glass-metrics strong { font-size: 21px; }
    .glass-metrics small { font-size: 10px; line-height: 1.25; }
    .column-headings { display: none; }
    .flow-list { gap: 10px; }
    :global(.flow-row) { grid-template-columns: 1fr; gap: 0; border: 1px solid #e9ddd4; border-radius: 7px; padding: 9px; background: rgba(255,255,255,.35); }
    :global(.flow-node) { min-height: 30px; border: 0; padding: 2px 4px; background: transparent; }
    .flow-line { width: 1px; height: 12px; margin-left: 7px; }
    :global(.connector-node) { justify-content: start; }
    .mobile-label { display: inline; flex: none; width: 66px; color: #8d8178; font-size: 10px; }
  }
  @media (max-width: 470px) {
    .connection-heading { display: block; }
    .connection-heading :global(.map-link) { margin-top: 7px; }
    .glass-metrics { grid-template-columns: 1fr 1fr; }
    .glass-metrics :global(.glass-card:first-child) { grid-column: 1 / -1; }
  }
</style>
