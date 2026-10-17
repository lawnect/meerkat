<script lang="ts">
  import { api, errorMessage } from '../api';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import type { Outlet } from '../types';

  let { outlet, onSaved }: { outlet: Outlet; onSaved: () => Promise<void> } = $props();
  let domain = $state('');
  let error = $state('');
  let busy = $state(false);

  $effect(() => { domain = outlet.domain || ''; });

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await api<Outlet>('/api/v1/outlet', { method: 'PUT', body: JSON.stringify({ domain }) });
      await onSaved();
    } catch (cause) {
      error = `도메인을 저장하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      busy = false;
    }
  }
</script>

<PageHead kicker="설정" title="Outlet 설정" description="공개 주소의 기준과 서버 상태를 확인합니다." />
<Panel title="기본 도메인" description="Caddy가 인증서와 공개 주소를 관리할 기준입니다.">
  <p>모든 공개 서비스는 이 도메인의 하위 주소로 만들어집니다. 예: app.saeoo.com.</p>
  <form onsubmit={submit}>
    <div class="field">
      <label for="domain">기본 도메인</label>
      <input class="input input-bordered" id="domain" bind:value={domain} placeholder="예: saeoo.com" required />
    </div>
    <div class="form-actions"><button class="btn btn-primary" type="submit" disabled={busy}>{busy ? '저장 중…' : outlet.domain ? '저장' : '도메인 설정'}</button></div>
    {#if error}<p class="error" role="alert">{error}</p>{/if}
  </form>
</Panel>
