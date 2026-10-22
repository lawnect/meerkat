<script lang="ts">
  import { api, errorMessage } from '../api';
  import Button from '../components/ui/Button.svelte';
  import Card from '../components/ui/Card.svelte';
  import ConfirmNameDialog from '../components/ui/ConfirmNameDialog.svelte';
  import { adminHostname } from '../domains';
  import type { DomainDNSCheck, Outlet, TunnelCheck } from '../types';

  type TunnelCheckKey = 'current' | 'draft';

  let { outlet, onSaved }: { outlet: Outlet; onSaved: () => Promise<void> } = $props();
  let selectedDomain = $state('');
  let openedInitialDomain = $state(false);
  let domainInput = $state('');
  let domainDialogMode = $state<'add' | 'primary'>('add');
  let dialogError = $state('');
  let dialogBusy = $state(false);
  let deleteTarget = $state('');
  let deleteOpen = $state(false);
  let deleteError = $state('');
  let deleteBusy = $state(false);
  let dnsChecks = $state<Record<string, DomainDNSCheck>>({});
  let dnsBusy = $state<Record<string, boolean>>({});
  let dnsErrors = $state<Record<string, string>>({});
  let tunnelInput = $state('');
  let tunnelDialogError = $state('');
  let tunnelDialogBusy = $state(false);
  let tunnelChecks = $state<Partial<Record<TunnelCheckKey, TunnelCheck>>>({});
  let tunnelCheckBusy = $state<Partial<Record<TunnelCheckKey, boolean>>>({});
  let tunnelCheckErrors = $state<Partial<Record<TunnelCheckKey, string>>>({});
  let domainDialog: HTMLDialogElement;
  let tunnelDialog: HTMLDialogElement;
  let tunnelDialogSession = 0;

  const tunnel = $derived(outlet.tunnel);
  const tunnelHost = $derived(tunnel?.address ? hostOf(tunnel.address) : '');
  const dialErrors: Record<string, string> = {
    connection_refused: '연결 거부 · 포트가 닫혀 있음',
    timeout: '시간 초과 · 방화벽 차단 가능성',
    unreachable: '네트워크 경로 없음',
  };

  function hostOf(address: string): string {
    return address.replace(/:\d+$/, '').replace(/^\[(.*)\]$/, '$1');
  }

  function portOf(address: string): string {
    return address.match(/:(\d+)$/)?.[1] ?? '';
  }

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

  async function checkDNS(domain: string): Promise<void> {
    if (dnsBusy[domain]) return;
    dnsBusy[domain] = true;
    dnsErrors[domain] = '';
    try {
      dnsChecks[domain] = await api<DomainDNSCheck>(`/api/v1/outlet/domains/${encodeURIComponent(domain)}/check-dns`, { method: 'POST' });
    } catch (cause) {
      dnsErrors[domain] = `DNS를 확인하지 못했습니다: ${errorMessage(cause)}`;
    } finally {
      dnsBusy[domain] = false;
    }
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

  function tunnelError(prefix: string, cause: unknown): string {
    const code = errorMessage(cause);
    return code === 'invalid_tunnel_address' ? '주소 형식을 확인하세요. 예: tunnel.example.com:2333 (127.0.0.1, localhost는 사용할 수 없습니다)' : code === 'tunnel_address_unset' ? '터널 주소가 없습니다. 대표 도메인을 등록하거나 주소를 지정하세요.' : code === 'tunnel_default_unavailable' ? '대표 도메인이 없어 기본값이 없으므로 주소를 비울 수 없습니다.' : `${prefix}: ${code}`;
  }

  async function checkTunnel(key: TunnelCheckKey, address = ''): Promise<void> {
    if (tunnelCheckBusy[key]) return;
    const session = tunnelDialogSession;
    // A draft result from a closed dialog must not appear when it opens again.
    const stale = () => key === 'draft' && session !== tunnelDialogSession;
    tunnelCheckBusy[key] = true;
    tunnelCheckErrors[key] = '';
    try {
      const result = await api<TunnelCheck>('/api/v1/outlet/tunnel/check', { method: 'POST', body: JSON.stringify({ address }) });
      if (!stale()) tunnelChecks[key] = result;
    } catch (cause) {
      if (!stale()) tunnelCheckErrors[key] = tunnelError('접속을 확인하지 못했습니다', cause);
    } finally {
      tunnelCheckBusy[key] = false;
    }
  }

  function openTunnelDialog(): void {
    tunnelDialogSession += 1;
    tunnelInput = tunnel?.custom ? tunnel.address : '';
    tunnelDialogError = '';
    tunnelChecks.draft = undefined;
    tunnelCheckErrors.draft = '';
    tunnelDialog.showModal();
  }

  async function saveTunnel(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (tunnelDialogBusy) return;
    tunnelDialogError = '';
    tunnelDialogBusy = true;
    try {
      await api<Outlet>('/api/v1/outlet/tunnel', { method: 'PUT', body: JSON.stringify({ address: tunnelInput.trim() }) });
      await onSaved();
      tunnelChecks.current = undefined;
      tunnelDialog.close();
    } catch (cause) {
      tunnelDialogError = tunnelError('터널 주소를 저장하지 못했습니다', cause);
    } finally {
      tunnelDialogBusy = false;
    }
  }

  function openDeleteDialog(domain: string): void {
    deleteTarget = domain;
    deleteError = '';
    deleteOpen = true;
  }

  async function removeDomain(): Promise<void> {
    if (!deleteTarget || deleteBusy) return;
    deleteError = '';
    deleteBusy = true;
    try {
      await api<Outlet>(`/api/v1/outlet/domains/${encodeURIComponent(deleteTarget)}`, { method: 'DELETE' });
      await onSaved();
      selectedDomain = '';
      deleteOpen = false;
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
                  <div><h3>용도</h3><p>{#if primary}관리 화면: <code>https://{adminHostname(registeredDomain)}</code><br />이 도메인의 하위 주소로 서비스도 공개할 수 있습니다.{:else}연결 서비스의 공개 주소에 사용하는 도메인입니다.{/if}</p></div>
                  <div><h3>DNS 설정</h3><p><code>*.{registeredDomain}</code>을 Outlet 서버 IP로 연결하세요. {#if primary && tunnelHost === registeredDomain}터널 연결을 위해 <code>{registeredDomain}</code>도 같은 IP로 연결해야 합니다. 루트 주소를 다른 서버에 쓰고 있다면 아래 터널 주소를 바꾸세요.{/if}</p></div>
                </div>
                <p class="dns-help">외부에서 80번(인증서 발급)과 443번(HTTPS 접속) 포트에 접근할 수 있어야 합니다. Cloudflare DNS를 쓴다면 프록시를 끈 ‘DNS 전용’을 권장합니다. 도메인과 연결된 공개 주소의 HTTPS 인증서는 자동으로 발급·갱신됩니다.</p>
                <div class="dns-check">
                  <Button variant="secondary" size="sm" disabled={dnsBusy[registeredDomain]} onclick={() => checkDNS(registeredDomain)}>{dnsBusy[registeredDomain] ? '확인 중…' : 'DNS 연결 확인'}</Button>
                  {#if dnsErrors[registeredDomain]}<p class="error" role="alert">{dnsErrors[registeredDomain]}</p>{/if}
                  {#if dnsChecks[registeredDomain]}
                    {@const check = dnsChecks[registeredDomain]}
                    <div class="dns-results" aria-live="polite">
                      {#each check.records as record (record.hostname)}
                        <div class="dns-result">
                          <code>{record.hostname === registeredDomain || record.hostname === adminHostname(registeredDomain) ? record.hostname : `*.${registeredDomain}`}</code>
                          {#if record.addresses.length}<span class="dns-ok">DNS 응답: {record.addresses.join(', ')}</span>
                          {:else}<span class="dns-fail">DNS 응답 없음</span>{/if}
                        </div>
                      {/each}
                      <p>조회 시각: {new Date(check.checked_at).toLocaleString()}</p>
                      <p>이 검사는 DNS 응답 여부를 확인합니다. Cloudflare 프록시가 켜져 있으면 표시 IP는 서버 IP가 아닌 Cloudflare IP일 수 있습니다. HTTPS 인증서와 실제 접속은 {primary ? `https://${adminHostname(registeredDomain)}` : '등록한 공개 주소'}로 확인하세요.</p>
                    </div>
                  {/if}
                </div>
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

  <Card as="section" padding="none" class="tunnel-card" aria-labelledby="tunnel-title">
    <div class="ui-card-header tunnel-header"><h3 class="ui-card-title" id="tunnel-title">터널 주소</h3><span class="tunnel-source">{tunnel?.custom ? '직접 지정' : '기본값'}</span></div>
    <div class="tunnel-body">
      {#if tunnel?.address}<p class="tunnel-address"><code>{tunnel.address}</code></p>
      {:else}<p class="tunnel-address tunnel-unset">설정되지 않음</p>{/if}
      <p class="dns-help">앱 서버의 터널 컨테이너가 Outlet에 TCP로 접속하는 주소로, 관리 화면 주소와 따로 지정할 수 있습니다. {#if tunnel?.address}이 호스트가 DNS 전용으로 Outlet 서버 IP를 가리키고, TCP {portOf(tunnel.address)} 포트가 외부에 열려 있어야 합니다.{:else}대표 도메인을 등록하거나 주소를 직접 지정하세요.{/if}</p>
      <div class="dns-check">
        <div class="tunnel-actions">
          <Button variant="secondary" size="sm" disabled={!tunnel?.address || tunnelCheckBusy.current} onclick={() => checkTunnel('current')}>{tunnelCheckBusy.current ? '확인 중…' : '접속 테스트'}</Button>
          <Button variant="secondary" size="sm" onclick={openTunnelDialog}>주소 변경</Button>
        </div>
        {#if tunnelCheckErrors.current}<p class="error" role="alert">{tunnelCheckErrors.current}</p>{/if}
        {#if tunnelChecks.current}{@render tunnelResult(tunnelChecks.current)}{/if}
      </div>
    </div>
  </Card>
</div>

{#snippet tunnelResult(check: TunnelCheck)}
  {@const failed = check.results.filter((result) => !result.reachable).length}
  <div class="dns-results" aria-live="polite">
    <p><strong class={check.reachable ? 'dns-ok' : 'dns-fail'}>{check.error === 'dns_lookup_failed' ? `DNS 응답 없음: ${hostOf(check.address)}` : !check.reachable ? '접속 실패' : failed ? '일부 주소만 접속 가능' : '접속 가능'}</strong></p>
    {#each check.results as result (result.address)}
      <div class="dns-result">
        <code>{result.address}</code>
        {#if result.reachable}<span class="dns-ok">접속 성공 · {result.latency_ms ?? 0}ms</span>
        {:else}<span class="dns-fail">{dialErrors[result.error ?? ''] ?? '접속 실패'}</span>{/if}
      </div>
    {/each}
    {#if check.reachable && failed}<p>터널 컨테이너가 실패한 주소를 먼저 시도하면 연결이 늦어지거나 실패할 수 있습니다. 해당 DNS 레코드를 확인하세요. IPv6 주소만 실패했다면 Outlet 서버의 Docker에 IPv6 경로가 없어서일 수도 있습니다.</p>{/if}
    <p>조회 시각: {new Date(check.checked_at).toLocaleString()}</p>
    <p>Outlet 서버에서 <code>{check.address}</code>로 TCP 연결만 시험한 결과입니다. 응답한 서버가 이 Outlet인지, 터널을 실행하는 서버의 방화벽이 허용하는지는 확인하지 않습니다. 그 서버에서도 <code>nc -vz {hostOf(check.address)} {portOf(check.address)}</code>로 확인하세요.</p>
  </div>
{/snippet}

<dialog bind:this={domainDialog} class="domain-dialog" aria-labelledby="domain-dialog-title" aria-describedby="domain-dialog-description" oncancel={(event) => { if (dialogBusy) event.preventDefault(); }} onclose={() => { domainInput = ''; dialogError = ''; }}>
  <form onsubmit={saveDomain}>
    <h2 id="domain-dialog-title">{domainDialogMode === 'primary' ? outlet.domain ? '대표 도메인 변경' : '대표 도메인 설정' : '새 도메인 추가'}</h2>
    <p id="domain-dialog-description">{domainDialogMode === 'primary' ? '관리 화면은 대표 도메인의 meerkat 하위 주소에서 열립니다.' : '추가한 도메인의 하위 주소로 서비스를 공개할 수 있습니다.'}</p>
    <div class="field"><label for="domain-input">도메인</label><input class="input input-bordered" id="domain-input" bind:value={domainInput} placeholder="예: example.com" autocapitalize="none" spellcheck="false" required /></div>
    {#if dialogError}<p class="error" role="alert">{dialogError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" disabled={dialogBusy} onclick={() => domainDialog.close()}>취소</Button><Button type="submit" disabled={dialogBusy}>{dialogBusy ? '저장 중…' : '저장'}</Button></div>
  </form>
</dialog>

<dialog bind:this={tunnelDialog} class="domain-dialog" aria-labelledby="tunnel-dialog-title" aria-describedby="tunnel-dialog-description" oncancel={(event) => { if (tunnelDialogBusy) event.preventDefault(); }} onclose={() => { tunnelInput = ''; tunnelDialogError = ''; }}>
  <form onsubmit={saveTunnel}>
    <h2 id="tunnel-dialog-title">터널 주소 변경</h2>
    <p id="tunnel-dialog-description">앱 서버의 터널 컨테이너가 Outlet에 접속하는 주소입니다. 대표 도메인 루트를 다른 서버에 쓰고 있다면 Outlet 서버를 가리키는 호스트를 지정하세요.</p>
    <div class="field">
      <label for="tunnel-input">터널 주소</label>
      <input class="input input-bordered" id="tunnel-input" bind:value={tunnelInput} placeholder={tunnel?.default_address || 'tunnel.example.com:2333'} autocapitalize="none" spellcheck="false" aria-describedby="tunnel-input-help" />
      <small id="tunnel-input-help">호스트:포트 형식입니다. 포트를 생략하면 Outlet 컨테이너의 터널 포트(기본 2333)를 붙이므로, 서버에서 공개 포트(<code>MEERKAT_TUNNEL_PORT</code>)를 바꿨다면 포트까지 입력하세요. 비워 두면 기본값{tunnel?.default_address ? `(${tunnel.default_address})` : ''}을 사용합니다.</small>
    </div>
    <div class="dns-check">
      <Button variant="secondary" size="sm" disabled={tunnelCheckBusy.draft || !(tunnelInput.trim() || tunnel?.default_address)} onclick={() => checkTunnel('draft', tunnelInput.trim() || tunnel?.default_address)}>{tunnelCheckBusy.draft ? '확인 중…' : '저장 전 접속 테스트'}</Button>
      {#if tunnelCheckErrors.draft}<p class="error" role="alert">{tunnelCheckErrors.draft}</p>{/if}
      {#if tunnelChecks.draft}{@render tunnelResult(tunnelChecks.draft)}{/if}
    </div>
    <p>저장하면 실행 중인 터널이 다음 상태 보고(약 5초) 때 새 설정을 받아 새 주소로 다시 연결합니다.</p>
    {#if tunnelDialogError}<p class="error" role="alert">{tunnelDialogError}</p>{/if}
    <div class="dialog-actions"><Button variant="secondary" disabled={tunnelDialogBusy} onclick={() => tunnelDialog.close()}>취소</Button><Button type="submit" disabled={tunnelDialogBusy}>{tunnelDialogBusy ? '저장 중…' : '저장'}</Button></div>
  </form>
</dialog>

<ConfirmNameDialog id="delete-domain" open={deleteOpen} title="도메인 삭제" description="등록된 도메인을 삭제합니다." warning="삭제 후에는 되돌릴 수 없습니다. 이 도메인을 사용하는 공개 주소가 있으면 삭제할 수 없습니다." targetLabel="삭제할 도메인" targetName={deleteTarget} placeholder="삭제할 도메인의 이름을 입력하세요" submitLabel="도메인 삭제" busy={deleteBusy} error={deleteError} onConfirm={removeDomain} onClose={() => { deleteOpen = false; deleteTarget = ''; deleteError = ''; }} />

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
  .dns-check { display: grid; justify-items: start; gap: 8px; margin-top: 12px; }
  .dns-results { display: grid; gap: 5px; color: var(--muted); font-size: 11px; line-height: 1.5; }
  .dns-results p { margin: 0; }
  .dns-result { display: flex; flex-wrap: wrap; gap: 8px; }
  .dns-ok { color: var(--ok, #39744d); }
  .dns-fail { color: var(--danger, #b4432f); }
  .dns-check :global(.error) { margin: 0; }
  .domain-footer { display: flex; justify-content: flex-end; margin-top: 16px; border-top: 1px solid var(--line); padding-top: 10px; }
  .list-empty { margin: 0; color: var(--muted); font-size: 12px; line-height: 1.6; }
  .domain-page :global(.tunnel-card) { margin-top: 18px; overflow: visible; }
  .tunnel-header { display: flex; align-items: center; justify-content: space-between; gap: 12px; }
  .tunnel-source { color: var(--muted); font-size: 12px; }
  .tunnel-body { padding: 14px 18px; }
  .tunnel-address { margin: 0; overflow-wrap: anywhere; font-size: 15px; font-weight: 650; }
  .tunnel-address code { color: var(--accent-dark); }
  .tunnel-unset { color: var(--muted); font-weight: 500; }
  .tunnel-actions { display: flex; flex-wrap: wrap; gap: 8px; }
  .domain-dialog { width: min(440px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .domain-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .domain-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .domain-dialog p:not(.error) { margin: 0 0 16px; color: var(--muted); font-size: 12px; }
  .dialog-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
  .domain-dialog .dns-check { margin: 12px 0 16px; }
  .domain-dialog .dns-results p { margin: 0; font-size: 11px; }
  @media (max-width: 700px) {
    .domain-details { grid-template-columns: 1fr; gap: 14px; }
    .domain-details > div + div { border-top: 1px solid var(--line); border-left: 0; padding-top: 12px; padding-left: 0; }
  }
</style>
