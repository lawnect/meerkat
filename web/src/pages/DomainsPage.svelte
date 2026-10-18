<script lang="ts">
  import { api, errorMessage } from '../api';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import type { Outlet } from '../types';

  let { outlet, onSaved }: { outlet: Outlet; onSaved: () => Promise<void> } = $props();
  let selectedDomain = $state('');
  let openedInitialDomain = $state(false);
  let domainInput = $state('');
  let domainDialogMode = $state<'add' | 'primary'>('add');
  let dialogError = $state('');
  let dialogBusy = $state(false);
  let deleteTarget = $state('');
  let deleteInput = $state('');
  let deleteError = $state('');
  let deleteBusy = $state(false);
  let domainDialog: HTMLDialogElement;
  let deleteDialog: HTMLDialogElement;

  const domains = $derived.by(() => {
    const registered = outlet.domains?.length ? outlet.domains : outlet.domain ? [outlet.domain] : [];
    return outlet.domain ? [outlet.domain, ...registered.filter((domain) => domain !== outlet.domain)] : registered;
  });

  $effect(() => {
    if (!openedInitialDomain && domains.length) {
      selectedDomain = outlet.domain || domains[0];
      openedInitialDomain = true;
    } else if (selectedDomain && !domains.includes(selectedDomain)) {
      selectedDomain = '';
    }
  });

  function openDomainDialog(mode: 'add' | 'primary'): void {
    domainDialogMode = mode;
    domainInput = mode === 'primary' ? outlet.domain || '' : '';
    dialogError = '';
    domainDialog.showModal();
  }

  async function saveDomain(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (dialogBusy) return;
    const value = domainInput.trim().toLowerCase();
    const primary = domainDialogMode === 'primary' || !outlet.domain;
    dialogError = '';
    dialogBusy = true;
    try {
      await api<Outlet>(primary ? '/api/v1/outlet' : '/api/v1/outlet/domains', {
        method: primary ? 'PUT' : 'POST',
        body: JSON.stringify({ domain: value }),
      });
      await onSaved();
      selectedDomain = value;
      domainDialog.close();
    } catch (cause) {
      const code = errorMessage(cause);
      dialogError = code === 'domain_has_routes' ? '공개 주소가 등록된 상태에서는 대표 도메인을 변경할 수 없습니다.' : code === 'domain_exists' ? '이미 등록된 도메인이거나 기존 도메인의 하위 주소입니다.' : code === 'invalid_domain' ? '도메인 형식을 확인하세요.' : `도메인을 저장하지 못했습니다: ${code}`;
    } finally {
      dialogBusy = false;
    }
  }

  function openDeleteDialog(domain: string): void {
    deleteTarget = domain;
    deleteInput = '';
    deleteError = '';
    deleteDialog.showModal();
  }

  async function removeDomain(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!deleteTarget || deleteInput !== deleteTarget || deleteBusy) return;
    deleteError = '';
    deleteBusy = true;
    try {
      await api<Outlet>(`/api/v1/outlet/domains/${encodeURIComponent(deleteTarget)}`, { method: 'DELETE' });
      await onSaved();
      selectedDomain = '';
      deleteDialog.close();
    } catch (cause) {
      const code = errorMessage(cause);
      deleteError = code === 'domain_has_routes' ? '이 도메인을 사용하는 공개 주소가 있어 삭제할 수 없습니다.' : `도메인을 삭제하지 못했습니다: ${code}`;
    } finally {
      deleteBusy = false;
    }
  }
</script>

<div class="domain-page">
  <div class="domain-toolbar"><h2>도메인 <small>{domains.length}</small></h2><Button variant="secondary" size="sm" onclick={() => openDomainDialog(outlet.domain ? 'add' : 'primary')}>+ 도메인 추가</Button></div>

  {#if domains.length}
    <div class="domain-list">
      {#each domains as registeredDomain (registeredDomain)}
          {@const primary = registeredDomain === outlet.domain}
          <Card as="section" padding="none" class="domain-card" aria-label={`${registeredDomain} 도메인`}>
            <Button variant="plain" class="domain-toggle" aria-expanded={selectedDomain === registeredDomain} aria-controls={selectedDomain === registeredDomain ? `domain-body-${registeredDomain}` : undefined} onclick={() => selectedDomain = selectedDomain === registeredDomain ? '' : registeredDomain}>
              <strong>{registeredDomain}</strong>
              <span class="domain-summary"><span>{primary ? '대표 도메인' : '추가 도메인'}</span><svg class:expanded={selectedDomain === registeredDomain} aria-hidden="true" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="m4 6 4 4 4-4" /></svg></span>
            </Button>
            {#if selectedDomain === registeredDomain}
              <div class="domain-body" id={`domain-body-${registeredDomain}`}>
                <div class="domain-details">
                  <div><h3>용도</h3><p>{primary ? 'Outlet 관리 화면과 공개 주소에 사용하는 도메인입니다.' : '연결 서비스의 공개 주소에 사용하는 도메인입니다.'}</p></div>
                  <div><h3>DNS 설정</h3><p><code>*.{registeredDomain}</code>을 Outlet 서버 IP로 연결하세요.{#if primary} <code>{registeredDomain}</code>도 연결해야 합니다.{/if}</p></div>
                </div>
                <p class="dns-help">Caddy의 Cloudflare API 토큰에 이 도메인 영역의 DNS 권한이 필요합니다.</p>
                <div class="domain-footer">
                  {#if primary}<Button variant="secondary" size="sm" onclick={() => openDomainDialog('primary')}>대표 도메인 변경</Button>
                  {:else}<Button variant="danger" size="sm" onclick={() => openDeleteDialog(registeredDomain)}>도메인 삭제</Button>{/if}
                </div>
              </div>
            {/if}
          </Card>
      {/each}
    </div>
  {:else}
    <p class="list-empty">아직 도메인이 없습니다. 도메인을 추가해 시작하세요.</p>
  {/if}
</div>

<dialog bind:this={domainDialog} class="domain-dialog" aria-labelledby="domain-dialog-title" aria-describedby="domain-dialog-description" oncancel={(event) => { if (dialogBusy) event.preventDefault(); }} onclose={() => { domainInput = ''; dialogError = ''; }}>
  <form onsubmit={saveDomain}>
    <h2 id="domain-dialog-title">{domainDialogMode === 'primary' ? outlet.domain ? '대표 도메인 변경' : '대표 도메인 설정' : '새 도메인 추가'}</h2>
    <p id="domain-dialog-description">{domainDialogMode === 'primary' ? '대표 도메인은 Outlet 관리 화면 주소입니다.' : '추가한 도메인의 하위 주소로 서비스를 공개할 수 있습니다.'}</p>
    <div class="field"><label for="domain-input">도메인</label><input class="input input-bordered" id="domain-input" bind:value={domainInput} placeholder="예: example.com" autocapitalize="none" spellcheck="false" required /></div>
    {#if dialogError}<p class="error" role="alert">{dialogError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" disabled={dialogBusy} onclick={() => domainDialog.close()}>취소</Button><Button type="submit" disabled={dialogBusy}>{dialogBusy ? '저장 중…' : '저장'}</Button></div>
  </form>
</dialog>

<dialog bind:this={deleteDialog} class="domain-dialog" aria-labelledby="delete-dialog-title" aria-describedby="delete-dialog-description" oncancel={(event) => { if (deleteBusy) event.preventDefault(); }} onclose={() => { deleteTarget = ''; deleteInput = ''; deleteError = ''; }}>
  <form onsubmit={removeDomain}>
    <h2 id="delete-dialog-title">도메인 삭제</h2>
    <p id="delete-dialog-description">{deleteTarget} 도메인을 삭제합니다. 이 도메인을 사용하는 공개 주소가 있으면 삭제할 수 없습니다.</p>
    <div class="field"><label for="delete-domain-input">삭제 확인: <strong>{deleteTarget}</strong> 입력</label><input class="input input-bordered" id="delete-domain-input" bind:value={deleteInput} autocomplete="off" spellcheck="false" required /></div>
    {#if deleteError}<p class="error" role="alert">{deleteError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" disabled={deleteBusy} onclick={() => deleteDialog.close()}>취소</Button><Button variant="danger" type="submit" disabled={deleteBusy || deleteInput !== deleteTarget}>{deleteBusy ? '삭제 중…' : '도메인 삭제'}</Button></div>
  </form>
</dialog>

<style>
  .domain-page { min-width: 0; }
  .domain-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; margin-bottom: 12px; }
  .domain-toolbar h2 { margin: 0; font-size: 16px; letter-spacing: -.025em; }
  .domain-toolbar small { margin-left: 5px; color: var(--muted); font-size: 12px; font-weight: 500; }
  .domain-list { display: grid; gap: 10px; }
  .domain-list :global(.domain-card) { overflow: visible; }
  .domain-list :global(.domain-toggle) { display: flex; align-items: center; justify-content: space-between; gap: 16px; width: 100%; min-height: 58px; border-radius: 8px; padding: 13px 18px; color: var(--ink); text-align: left; }
  .domain-list :global(.domain-toggle:hover) { background: rgba(246, 239, 232, .7); }
  .domain-list :global(.domain-toggle strong) { overflow-wrap: anywhere; font-size: 16px; font-weight: 650; }
  .domain-summary { display: flex; align-items: center; gap: 14px; flex: none; color: var(--muted); font-size: 12px; }
  .domain-summary svg { width: 17px; height: 17px; transition: transform .15s ease; }
  .domain-summary svg.expanded { transform: rotate(180deg); }
  .domain-body { border-top: 1px solid var(--line); padding: 16px 18px 14px; }
  .domain-details { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); }
  .domain-details > div { min-width: 0; }
  .domain-details > div + div { border-left: 1px solid var(--line); padding-left: 20px; }
  .domain-details h3 { margin: 0 0 8px; color: var(--muted); font-size: 12px; font-weight: 700; letter-spacing: .02em; }
  .domain-details p { margin: 0; font-size: 13px; line-height: 1.5; }
  .domain-details code { overflow-wrap: anywhere; color: var(--accent-dark); }
  .dns-help { margin: 12px 0 0; color: var(--muted); font-size: 11px; }
  .domain-footer { display: flex; justify-content: flex-end; margin-top: 16px; border-top: 1px solid var(--line); padding-top: 10px; }
  .list-empty { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
  .domain-dialog { width: min(440px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .domain-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .domain-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .domain-dialog p:not(.error) { margin: 0 0 16px; color: var(--muted); font-size: 12px; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
  @media (max-width: 700px) {
    .domain-details { grid-template-columns: 1fr; gap: 14px; }
    .domain-details > div + div { border-top: 1px solid var(--line); border-left: 0; padding-top: 12px; padding-left: 0; }
  }
</style>
