<script>
  import { onMount } from 'svelte';
  import { api } from '../api.js';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';

  let snapshot = $state(null);
  let error = $state('');
  let loading = $state(false);

  async function refresh() {
    loading = true;
    error = '';
    try {
      snapshot = await api('/api/v1/diagnostics/trace');
    } catch (cause) {
      error = `요청 정보를 불러오지 못했습니다: ${cause.message}`;
    } finally {
      loading = false;
    }
  }

  onMount(refresh);
</script>

<PageHead kicker="운영 / 진단" title="요청 진단" description="현재 관리자 요청이 Outlet에 도착한 경로 정보를 확인합니다.">
  {#snippet action()}
    <button class="button secondary" onclick={refresh} disabled={loading}>{loading ? '새로 고치는 중…' : '새로 고침'}</button>
  {/snippet}
</PageHead>

{#if error}<p class="error" role="alert">{error}</p>{/if}
{#if snapshot}
  <div class="grid two-col diagnostics-grid">
    <Panel title="Outlet 위치" description="지역은 outlet.json에 직접 설정한 값입니다.">
      <dl class="diagnostic-list">
        <div><dt>설정된 지역</dt><dd>{snapshot.outlet.region || '설정되지 않음'}</dd></div>
        <div><dt>호스트 이름</dt><dd><code>{snapshot.outlet.hostname || '확인할 수 없음'}</code></dd></div>
        <div><dt>측정 시각</dt><dd>{new Date(snapshot.observed_at).toLocaleString()}</dd></div>
      </dl>
    </Panel>
    <Panel title="요청이 도착한 경로" description="이 정보는 Outlet이 받은 요청 헤더와 연결 정보입니다.">
      <dl class="diagnostic-list">
        <div><dt>CF-Ray 헤더</dt><dd>{snapshot.request.cloudflare_ray ? '수신됨' : '없음'}</dd></div>
        <div><dt>Cloudflare colo</dt><dd><code>{snapshot.request.cloudflare_colo || '없음'}</code></dd></div>
        <div><dt>CF-Ray</dt><dd><code>{snapshot.request.cloudflare_ray || '없음'}</code></dd></div>
        <div><dt>전달된 클라이언트 주소</dt><dd><code>{snapshot.request.forwarded_for || '없음'}</code></dd></div>
        <div><dt>Outlet과 직접 연결한 주소</dt><dd><code>{snapshot.request.remote_address}</code></dd></div>
        <div><dt>요청 호스트 / 프로토콜</dt><dd><code>{snapshot.request.forwarded_host || snapshot.request.host} · {snapshot.request.forwarded_proto || snapshot.request.protocol}</code></dd></div>
        <div><dt>Cloudflare 국가 코드</dt><dd><code>{snapshot.request.cloudflare_country || '없음'}</code></dd></div>
      </dl>
      <p class="diagnostic-note">CF-Ray 헤더가 있으면 그 값의 접미사로 Cloudflare colo를 표시합니다. 직접 연결에서도 요청 헤더를 넣을 수 있으므로 프록시 경유의 단독 증거로 쓰지 마세요. 전달 주소와 위치 정보는 진단용이며 인증 판단에 사용하지 않습니다.</p>
    </Panel>
  </div>
{:else if !loading && !error}
  <div class="empty"><p>진단 정보를 기다리는 중입니다.</p></div>
{/if}

<style>
  .diagnostics-grid { align-items: start; grid-template-columns: minmax(0, 1.35fr) minmax(280px, .65fr); }
  .diagnostic-list { margin: 0; }
  .diagnostic-list > div { display: grid; grid-template-columns: minmax(135px, .7fr) minmax(0, 1.3fr); gap: 12px; border-bottom: 1px solid var(--line); padding: 10px 0; }
  .diagnostic-list > div:last-child { border-bottom: 0; }
  .diagnostic-list dt { color: var(--muted); font-size: 12px; }
  .diagnostic-list dd { overflow-wrap: anywhere; margin: 0; }
  .diagnostic-list code { color: #345a70; font-size: 12px; }
  .diagnostic-note { margin-top: 16px !important; font-size: 12px; }
  @media (max-width: 900px) { .diagnostics-grid { grid-template-columns: 1fr; } }
</style>
