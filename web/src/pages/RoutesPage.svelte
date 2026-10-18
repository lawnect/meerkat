<script lang="ts">
  import PageHead from '../components/PageHead.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import Panel from '../components/Panel.svelte';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import { statusLabel } from '../api';
  import type { DashboardData, PageKey } from '../types';

  let { data, onChanged, navigate }: { data: DashboardData; onChanged: () => Promise<void>; navigate: (view: PageKey) => Promise<void> } = $props();
</script>

<PageHead kicker="운영 / 공개 서비스" title="공개 서비스" description="내부 앱과 인터넷 주소가 연결된 현황입니다.">
  {#snippet action()}
    <Button variant="secondary" onclick={() => navigate('connectors')}>Connector 관리</Button>
  {/snippet}
</PageHead>

{#if data.records.length}
  <div class="dashboard-grid route-groups">
    {#each data.connectors as connector}
      {@const records = data.records.filter((record) => record.connector.id === connector.id)}
      {#if records.length}
        <Panel title={connector.name} description={statusLabel(connector.status)}>
          {#each records as record}
            <ServiceBlock {record} outlet={data.outlet} onChanged={onChanged} />
          {/each}
        </Panel>
      {/if}
    {/each}
  </div>
{:else}
  <Card class="empty"><h2>아직 공개 서비스가 없습니다</h2><p>Connector를 만들고 내부 앱을 등록한 뒤, 원하는 공개 주소를 추가하세요.</p><Button onclick={() => navigate('connectors')}>Connector 관리</Button></Card>
{/if}

<style>
  .route-groups { grid-template-columns: 1fr; }
</style>
