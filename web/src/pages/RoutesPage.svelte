<script lang="ts">
  import PageHead from '../components/PageHead.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import { statusLabel } from '../api';
  import type { DashboardData, PageKey } from '../types';

  let { data, onChanged, navigate }: { data: DashboardData; onChanged: () => Promise<void>; navigate: (view: PageKey) => Promise<void> } = $props();
</script>

<PageHead kicker="운영 / 공개 서비스" title="공개 서비스" description="내부 앱과 인터넷 주소가 연결된 현황입니다.">
  {#snippet action()}
    <button class="btn btn-outline" onclick={() => navigate('connectors')}>앱 연결 관리</button>
  {/snippet}
</PageHead>

{#if data.records.length}
  <div class="dashboard-grid route-groups">
    {#each data.connectors as connector}
      {@const records = data.records.filter((record) => record.connector.id === connector.id)}
      {#if records.length}
        <section class="panel card bg-base-100 border-base-300">
          <div class="panel-head"><div><h2>{connector.name}</h2><p>{statusLabel(connector.status)}</p></div></div>
          {#each records as record}
            <ServiceBlock {record} outlet={data.outlet} onChanged={onChanged} />
          {/each}
        </section>
      {/if}
    {/each}
  </div>
{:else}
  <div class="empty"><h2>아직 공개 서비스가 없습니다</h2><p>Connector를 만들고 내부 앱을 등록한 뒤, 원하는 공개 주소를 추가하세요.</p><button class="btn btn-primary" onclick={() => navigate('connectors')}>앱 연결 관리</button></div>
{/if}

<style>
  .route-groups { grid-template-columns: 1fr; }
</style>
