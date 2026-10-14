<script>
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';

  let { data, navigate } = $props();
  const connected = $derived(data.connectors.filter((connector) => connector.status === 'connected').length);
  const next = $derived(data.connectors.length === 0
    ? 'Connector를 하나 만들어 내부 서비스를 연결하세요.'
    : connected === 0
      ? 'Connector를 실행하고 등록 토큰으로 연결하세요.'
      : '공개 서비스의 상태를 확인하세요.');
  const items = $derived([
    ['도메인 확인', Boolean(data.outlet.domain), '설정에서 변경', 'settings'],
    ['Connector 만들기', data.connectors.length > 0, '서버 등록', 'connectors'],
    ['Connector 연결', connected > 0, '등록 토큰 발급', 'connectors'],
    ['공개 서비스 만들기', data.routes.length > 0, '서비스 추가', 'routes'],
  ]);
</script>

<PageHead kicker="MEERKAT OUTLET" title="오늘의 운영 상태" description={next}>
  {#snippet action()}
    <button class="button" onclick={() => navigate(data.connectors.length ? 'routes' : 'connectors')}>{data.connectors.length ? '공개 서비스 보기' : 'Connector 시작'}</button>
  {/snippet}
</PageHead>

<div class="grid summary-grid">
  <div class="stat"><div class="stat-label">Connector</div><div class="stat-value">{data.connectors.length}</div><div class="stat-note">등록된 서버</div></div>
  <div class="stat"><div class="stat-label">연결됨</div><div class="stat-value">{connected}</div><div class="stat-note">heartbeat 기준</div></div>
  <div class="stat"><div class="stat-label">내부 서비스</div><div class="stat-value">{data.services.length}</div><div class="stat-note">등록된 앱</div></div>
  <div class="stat"><div class="stat-label">공개 주소</div><div class="stat-value">{data.routes.length}</div><div class="stat-note">생성된 Route</div></div>
</div>

<div class="grid two-col">
  <Panel title="시작하기" description="Meerkat에서 서비스를 공개하는 순서입니다.">
    <div class="steps">
      {#each items as [label, done, action, view], i}
        <button class:done class="step" onclick={() => navigate(view)}>
          <span class="step-no">{done ? '✓' : i + 1}</span>
          <span><strong>{label}</strong><small>{done ? '완료됨' : action}</small></span>
          <span class="step-action">{done ? '완료' : action}</span>
        </button>
      {/each}
    </div>
  </Panel>
  <div>
    <div class="callout"><strong>현재 할 일</strong><span>{next}</span></div>
    <div class="panel help-panel"><h2>용어 안내</h2><p>Connector는 서비스를 실행하는 서버입니다. 공개 서비스는 서버 안의 앱 하나와 인터넷 주소 하나를 연결합니다.</p></div>
  </div>
</div>
