<script lang="ts">
  import { api, errorMessage } from '../api';
  import PageHead from '../components/PageHead.svelte';
  import Panel from '../components/Panel.svelte';
  import Button from '../components/ui/Button.svelte';
  import type { Outlet } from '../types';

  let { outlet, onSaved }: { outlet: Outlet; onSaved: () => Promise<void> } = $props();
  let domain = $state('');
  let newDomain = $state('');
  let error = $state('');
  let busy = $state(false);
  let domainBusy = $state(false);
  const domains = $derived(outlet.domains?.length ? outlet.domains : outlet.domain ? [outlet.domain] : []);

  $effect(() => { domain = outlet.domain || ''; });

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      await api<Outlet>('/api/v1/outlet', { method: 'PUT', body: JSON.stringify({ domain: domain.trim().toLowerCase() }) });
      await onSaved();
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'domain_has_routes' ? '공개 주소가 등록된 상태에서는 대표 도메인을 변경할 수 없습니다.' : code === 'domain_exists' ? '이미 등록된 도메인과 겹칩니다.' : code === 'invalid_domain' ? '도메인 형식을 확인하세요.' : `도메인을 저장하지 못했습니다: ${code}`;
    } finally {
      busy = false;
    }
  }

  async function addDomain(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = '';
    domainBusy = true;
    try {
      await api<Outlet>('/api/v1/outlet/domains', { method: 'POST', body: JSON.stringify({ domain: newDomain.trim().toLowerCase() }) });
      newDomain = '';
      await onSaved();
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'domain_exists' ? '이미 등록된 도메인이거나 기존 도메인의 하위 주소입니다.' : code === 'invalid_domain' ? '도메인 형식을 확인하세요.' : `도메인을 추가하지 못했습니다: ${code}`;
    } finally {
      domainBusy = false;
    }
  }

  async function removeDomain(value: string): Promise<void> {
    if (!confirm(`${value} 도메인을 삭제할까요?`)) return;
    error = '';
    domainBusy = true;
    try {
      await api<Outlet>(`/api/v1/outlet/domains/${encodeURIComponent(value)}`, { method: 'DELETE' });
      await onSaved();
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'domain_has_routes' ? '이 도메인을 사용하는 공개 주소가 있어 삭제할 수 없습니다.' : `도메인을 삭제하지 못했습니다: ${code}`;
    } finally {
      domainBusy = false;
    }
  }
</script>

<PageHead kicker="설정" title="Outlet 설정" description="공개 주소의 기준과 서버 상태를 확인합니다." />
<Panel title="도메인" description="대표 도메인과 공개 주소에 사용할 도메인을 관리합니다.">
  <p>대표 도메인은 Outlet 관리 화면 주소입니다. 각 도메인의 하위 주소로 앱을 공개할 수 있습니다.</p>
  <form onsubmit={submit}>
    <div class="field">
      <label for="domain">대표 도메인</label>
      <input class="input input-bordered" id="domain" bind:value={domain} placeholder="예: saeoo.com" required />
    </div>
    <div class="form-actions"><Button type="submit" disabled={busy}>{busy ? '저장 중…' : outlet.domain ? '저장' : '도메인 설정'}</Button></div>
  </form>
  {#if domains.length}
    <div class="domain-management">
      <h3>등록된 도메인</h3>
      <ul class="domain-list">
        {#each domains as registeredDomain}
          <li><span><strong>{registeredDomain}</strong>{#if registeredDomain === outlet.domain}<small>대표 도메인</small>{/if}</span>{#if registeredDomain !== outlet.domain}<Button variant="text" size="sm" disabled={domainBusy} onclick={() => removeDomain(registeredDomain)}>삭제</Button>{/if}</li>
        {/each}
      </ul>
      <form class="add-domain-form" onsubmit={addDomain}>
        <div class="field"><label for="new-domain">도메인 추가</label><input class="input input-bordered" id="new-domain" bind:value={newDomain} placeholder="예: another.com" autocapitalize="none" spellcheck="false" required /></div>
        <Button type="submit" disabled={domainBusy}>{domainBusy ? '처리 중…' : '추가'}</Button>
      </form>
      <p class="dns-help">각 도메인의 DNS에서 <code>*.도메인</code>을 Outlet 서버 IP로 연결하세요. 대표 도메인은 루트 주소도 연결해야 합니다. Caddy의 Cloudflare API 토큰에도 추가한 도메인 영역의 DNS 권한이 필요합니다.</p>
    </div>
  {/if}
  {#if error}<p class="error" role="alert">{error}</p>{/if}
</Panel>

<style>
  .domain-management { margin-top: 18px; border-top: 1px solid var(--line); padding-top: 14px; }
  .domain-management h3 { margin: 0 0 8px; }
  .domain-list { list-style: none; margin: 0 0 14px; padding: 0; }
  .domain-list li { display: flex; align-items: center; justify-content: space-between; gap: 12px; border-bottom: 1px solid var(--line); padding: 8px 0; }
  .domain-list li span { display: flex; align-items: baseline; flex-wrap: wrap; gap: 8px; }
  .domain-list small { color: var(--muted); }
  .add-domain-form { display: flex; align-items: end; gap: 8px; }
  .add-domain-form .field { flex: 1; max-width: 360px; }
  .dns-help { margin-top: 12px !important; font-size: 12px; }
  @media (max-width: 500px) { .add-domain-form { align-items: stretch; flex-direction: column; } }
</style>
