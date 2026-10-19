<script lang="ts">
  import type { ServiceRecord } from '../types';
  import LinkButton from './ui/LinkButton.svelte';

  let { tunnelName, records }: { tunnelName: string; records: ServiceRecord[] } = $props();

  const paths = $derived(records.flatMap((record) => record.routes.map((route) => ({ record, route }))));
  const graphHeight = $derived(Math.max(190, Math.max(paths.length, records.length) * 76 + 64));
  const centerY = $derived(graphHeight / 2);

  function nodeY(index: number, count: number): number {
    if (count < 2) return graphHeight / 2;
    return 65 + (index * (graphHeight - 130)) / (count - 1);
  }

  function inboundPath(index: number): string {
    const y = nodeY(index, paths.length);
    return `M 310 ${y} C 360 ${y}, 360 ${centerY}, 410 ${centerY}`;
  }

  function outboundPath(index: number): string {
    const y = nodeY(index, records.length);
    return `M 610 ${centerY} C 665 ${centerY}, 665 ${y}, 730 ${y}`;
  }
</script>

<div class="graph-scroll" role="region" aria-label={`${tunnelName} 터널의 요청 경로 연결도`} tabindex="0">
  <div class="route-graph" style:height={`${graphHeight}px`}>
    <svg class="graph-lines" viewBox={`0 0 1000 ${graphHeight}`} preserveAspectRatio="none" aria-hidden="true">
      {#each paths as path, index (path.route.id)}
        <path d={inboundPath(index)} class:inactive={!path.route.enabled} />
      {/each}
      {#each records as record, index (record.service.id)}
        <path d={outboundPath(index)} class:inactive={!record.service.enabled} />
      {/each}
    </svg>

    <div class="graph-column-label address-label">공개 주소</div>
    <div class="graph-column-label tunnel-label">터널</div>
    <div class="graph-column-label service-label">연결 서비스</div>

    {#if paths.length}
      {#each paths as path, index (path.route.id)}
        <div class="graph-node address-node" class:inactive={!path.route.enabled} style:top={`${nodeY(index, paths.length)}px`}>
          <LinkButton href={`https://${path.route.hostname}`} target="_blank" rel="noopener noreferrer" variant="text" size="sm" class="graph-address-link" aria-label={`${path.route.hostname} 공개 주소 열기, 연결 서비스 ${path.record.service.name}`}>
            <span class="node-address"><strong>{path.route.hostname}</strong><small>→ {path.record.service.name}</small></span><span aria-hidden="true">↗</span>
          </LinkButton>
        </div>
      {/each}
    {:else}
      <div class="graph-empty-address" style:top={`${centerY}px`}>공개 주소 없음</div>
    {/if}

    <div class="graph-node tunnel-node" style:top={`${centerY}px`}>
      <svg class="tunnel-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="4" y="3" width="16" height="18" rx="5" /><path d="M8 8h8M8 13h8M8 17h.01M16 17h.01" /></svg><strong>{tunnelName}</strong>
    </div>

    {#each records as record, index (record.service.id)}
      <div class="graph-node service-node" class:inactive={!record.service.enabled} style:top={`${nodeY(index, records.length)}px`}>
        <strong>{record.service.name}</strong><code>{record.service.target.host}:{record.service.target.port}</code>
      </div>
    {/each}
  </div>
</div>

<style>
  .graph-scroll { overflow-x: auto; background-color: #fffdfa; background-image: radial-gradient(#ebe4dd 1px, transparent 1px); background-size: 20px 20px; }
  .graph-scroll:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
  .route-graph { position: relative; width: 100%; min-width: 760px; max-width: 1080px; margin: 0 auto; }
  .graph-lines { position: absolute; inset: 0; width: 100%; height: 100%; overflow: visible; pointer-events: none; }
  .graph-lines path { fill: none; stroke: #c8c1bb; stroke-width: 2; vector-effect: non-scaling-stroke; }
  .graph-lines path.inactive { stroke: #d8d4d0; stroke-dasharray: 5 5; }
  .graph-column-label { position: absolute; top: 19px; z-index: 1; color: var(--muted); font-size: 11px; font-weight: 700; }
  .address-label { left: 3%; }
  .tunnel-label { left: 41%; }
  .service-label { left: 73%; }
  .graph-node { position: absolute; z-index: 1; box-sizing: border-box; display: flex; align-items: center; min-width: 0; height: 46px; transform: translateY(-50%); border: 1px solid #d9d3cc; border-radius: 24px; background: #fffdfa; box-shadow: 0 2px 6px rgba(55, 44, 34, .06); }
  .graph-node.inactive { opacity: .55; }
  .address-node { left: 3%; width: 28%; }
  .address-node :global(.graph-address-link) { display: flex; justify-content: flex-start; gap: 8px; width: 100%; height: 100%; min-width: 0; overflow: hidden; padding: 0 14px; color: var(--accent-dark); font-size: 12px; font-weight: 650; }
  .node-address { display: grid; flex: 1; min-width: 0; overflow: hidden; }
  .node-address strong, .node-address small { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .node-address strong { font-size: 12px; font-weight: 650; }
  .node-address small { color: var(--muted); font-size: 10px; font-weight: 500; }
  .tunnel-node { left: 41%; width: 20%; gap: 8px; border-color: #d6beb0; padding: 0 14px; background: #fff9f4; }
  .tunnel-node strong { overflow: hidden; font-size: 12px; text-overflow: ellipsis; white-space: nowrap; }
  .tunnel-icon { flex: none; width: 16px; height: 16px; color: var(--accent-dark); }
  .service-node { left: 73%; width: 24%; display: grid; align-content: center; gap: 2px; padding: 0 14px; }
  .service-node strong, .service-node code { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
  .service-node strong { font-size: 12px; }
  .service-node code { color: var(--muted); font-size: 11px; }
  .graph-empty-address { position: absolute; left: 3%; box-sizing: border-box; display: grid; width: 28%; height: 46px; place-items: center; transform: translateY(-50%); border: 1px dashed #d9d3cc; border-radius: 24px; background: #fffdfa; color: var(--muted); font-size: 12px; }
</style>
