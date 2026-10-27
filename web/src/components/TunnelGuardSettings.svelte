<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { formatClock, formatDay } from '../format';
  import { intlLocale, translations } from '../i18n.svelte';
  import { ArrowSync, ArrowUndo, PlugConnected20, Save, Warning } from '../icons';
  import type { TunnelGuardMode, TunnelGuardSettings, TunnelGuardView } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 설정 > 보안의 터널 포트 보호 카드. Outlet이 터널 포트를 직접 받아 연결을 선별한 뒤에야 터널 서버에 전달합니다.
  // 방식과 허용 범위는 되돌리기·저장으로만 반영하고, 연결 수와 알려진 커넥터는 읽기 전용이라 새로 고침으로 다시 받습니다.
  const t = translations({
    title: 'Tunnel port protection',
    tip: 'Anyone can open a tunnel handshake, so a flood of anonymous connections can exhaust the tunnel server and drop every tunnel. The Outlet now filters connections on the tunnel port before they reach it.',
    mode: 'Mode',
    modes: { limited: 'Apply limits', strict: 'Connector IPs only' } as Record<TunnelGuardMode, string>,
    modeTips: {
      limited: 'Addresses seen making authenticated connector requests connect freely. Other addresses are limited per address and overall.',
      strict: 'Only known connector addresses and the allowed ranges connect; every other address is refused. A connector whose tunnel connection leaves from a different address than its HTTPS requests (for example IPv6 for one and IPv4 for the other) is refused, so add its range to the allowed ranges.',
    } as Record<TunnelGuardMode, string>,
    allowed: 'Allowed ranges',
    allowedTip: 'Clients at these addresses always connect without limits, in either mode.',
    allowedHint: 'IPv4 or IPv6 address or CIDR, up to 50',
    connectors: 'Known connectors',
    connectorsTip: 'Addresses that made an authenticated Connector API request in the last 15 minutes. They connect to the tunnel port without limits.',
    address: 'IP',
    lastSeen: 'Last authenticated',
    noConnectors: 'No known connectors',
    stats: (active: string, accepted: string, refused: string) => `${active} active · ${accepted} accepted · ${refused} refused`,
    statsTip: (rate: string, busy: string, idle: string, strict: string) => `Accepted and refused are counted since the Outlet started; active is the current number of open connections. Refused: ${rate} over the connection rate, ${busy} over the concurrent limit, ${idle} idle before sending data, ${strict} by connector IPs only mode.`,
    refresh: 'Refresh',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values. Allowed ranges are IPv4 or IPv6 addresses or CIDR ranges, up to 50.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load tunnel port protection: ${code}`,
  }, {
    title: '터널 포트 보호',
    tip: '터널 포트는 누구나 핸드셰이크를 시작할 수 있어, 익명 연결이 폭주하면 터널 서버가 마비되어 모든 터널의 연결이 끊어질 수 있습니다. Outlet이 터널 포트로 들어오는 연결을 먼저 선별한 뒤 터널 서버에 전달합니다.',
    mode: '방식',
    modes: { limited: '한도 적용', strict: '커넥터 IP만 허용' },
    modeTips: {
      limited: '인증된 커넥터 API 요청이 확인된 주소는 제한 없이 연결되며, 그 밖의 주소에는 주소별 및 전체 연결 한도가 적용됩니다.',
      strict: '알려진 커넥터 주소와 허용 범위만 연결되고, 그 밖의 주소는 거부됩니다. 터널 연결이 HTTPS 요청과 다른 주소(예: 한쪽은 IPv6, 다른 쪽은 IPv4)로 나가는 커넥터는 거부되므로 해당 대역을 허용 범위에 추가하십시오.',
    },
    allowed: '허용 범위',
    allowedTip: '지정한 IP와 범위의 클라이언트는 어느 방식에서든 제한 없이 연결됩니다.',
    allowedHint: 'IPv4·IPv6 주소 또는 CIDR, 최대 50개',
    connectors: '알려진 커넥터',
    connectorsTip: '최근 15분 이내에 인증된 커넥터 API 요청을 보낸 주소이며, 터널 포트에 제한 없이 연결됩니다.',
    address: 'IP',
    lastSeen: '마지막 인증',
    noConnectors: '알려진 커넥터가 없습니다',
    stats: (active: string, accepted: string, refused: string) => `활성 연결 ${active} · 허용 ${accepted} · 거부 ${refused}`,
    statsTip: (rate: string, busy: string, idle: string, strict: string) => `허용과 거부는 Outlet 시작 이후의 누계이며, 활성 연결은 현재 열려 있는 연결 수입니다. 거부 내역: 연결 빈도 초과 ${rate}건, 동시 연결 초과 ${busy}건, 첫 데이터 미수신 ${idle}건, 커넥터 IP만 허용 ${strict}건.`,
    refresh: '새로 고침',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '입력값이 올바르지 않습니다. 허용 범위는 IPv4·IPv6 주소 또는 CIDR이어야 하며 최대 50개까지 지정할 수 있습니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `터널 포트 보호 설정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const modes: TunnelGuardMode[] = ['limited', 'strict'];
  const endpoint = '/api/v1/security/tunnel-guard';

  let saved = $state<TunnelGuardSettings | null>(null);
  let draft = $state<TunnelGuardSettings | null>(null);
  let stats = $state<TunnelGuardView['stats'] | null>(null);
  let connectors = $state<TunnelGuardView['connectors']>([]);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  // 새로 고침은 연결 수와 알려진 커넥터만 바꾸고, 고치던 방식·허용 범위는 그대로 둡니다.
  const adoptLive = (next: TunnelGuardView) => { stats = next.stats; connectors = next.connectors ?? []; };
  const adopt = (next: TunnelGuardView) => {
    saved = { mode: next.settings.mode, allowed: next.settings.allowed ?? [] };
    draft = structuredClone($state.snapshot(saved));
    adoptLive(next);
  };
  const dirty = $derived(Boolean(saved && draft) && JSON.stringify(draft) !== JSON.stringify(saved));

  onMount(() => {
    api<TunnelGuardView>(endpoint).then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function refresh(): Promise<void> {
    try {
      adoptLive(await api<TunnelGuardView>(endpoint));
    } catch (cause) {
      toast.error(t().loadFailed(errorMessage(cause)));
    }
  }

  async function save(): Promise<void> {
    if (!draft || busy) return;
    busy = true;
    error = '';
    try {
      adopt(await api<TunnelGuardView>(endpoint, { method: 'PUT', body: JSON.stringify($state.snapshot(draft)) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_tunnel_guard' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (saved) draft = structuredClone($state.snapshot(saved));
    error = '';
  }

  const number = (value: number): string => value.toLocaleString(intlLocale());
  const statsLine = $derived(stats ? t().stats(number(stats.active), number(stats.accepted), number(stats.refused.rate + stats.refused.busy + stats.refused.idle + stats.refused.strict)) : '');
  const statsTip = $derived(stats ? t().statsTip(number(stats.refused.rate), number(stats.refused.busy), number(stats.refused.idle), number(stats.refused.strict)) : '');
</script>

<Card.Root size="sm" role="region" aria-labelledby="tunnel-guard-title">
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id="tunnel-guard-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="tunnel-guard-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if stats}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <div class="flex items-center gap-2">
          <span class="flex items-center gap-1 text-xs text-muted-foreground tabular-nums">{statsLine}<InfoTooltip id="tunnel-guard-stats-tip" label={statsLine} description={statsTip} /></span>
          <IconButton label={t().refresh} icon={ArrowSync} onclick={refresh} />
        </div>
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !draft}
      <Skeleton class="h-48 w-full" />
    {:else}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Field.Group class="gap-0">
        <Field.Field orientation="horizontal" class="flex-wrap items-center gap-y-2">
          <Field.Content class="min-w-48">
            <Field.Title id="tunnel-guard-mode-title">{t().mode}</Field.Title>
          </Field.Content>
          <!-- 선택지는 한 줄로 두고, 각 방식의 뜻은 마우스를 올리면 툴팁으로, 화면 낭독기에는 설명으로 알립니다. -->
          <RadioGroup.Root aria-labelledby="tunnel-guard-mode-title" value={draft.mode} onValueChange={(value) => { if (draft && modes.includes(value as TunnelGuardMode)) draft.mode = value as TunnelGuardMode; }} orientation="horizontal" class="flex w-auto flex-wrap gap-x-6 gap-y-2">
            {#each modes as mode (mode)}
              <Tooltip.Root>
                <Tooltip.Trigger>
                  {#snippet child({ props })}
                    <Field.Field orientation="horizontal" class="w-auto" {...props} tabindex={-1}>
                      <RadioGroup.Item value={mode} id={`tunnel-guard-mode-${mode}`} aria-describedby={`tunnel-guard-mode-${mode}-hint`} />
                      <Field.Label for={`tunnel-guard-mode-${mode}`} class="font-normal">{t().modes[mode]}</Field.Label>
                      <span id={`tunnel-guard-mode-${mode}-hint`} class="sr-only">{t().modeTips[mode]}</span>
                    </Field.Field>
                  {/snippet}
                </Tooltip.Trigger>
                <Tooltip.Content class="max-w-xs">{t().modeTips[mode]}</Tooltip.Content>
              </Tooltip.Root>
            {/each}
          </RadioGroup.Root>
        </Field.Field>
        <Field.Separator class="my-3" />
        <Field.Field>
          <div class="flex items-center gap-1"><Field.Label for="tunnel-guard-allowed">{t().allowed}</Field.Label><InfoTooltip id="tunnel-guard-allowed-tip" label={t().allowed} description={t().allowedTip} /></div>
          <TagInput id="tunnel-guard-allowed" ariaLabel={t().allowed} bind:values={draft.allowed} lowercase mono placeholder="203.0.113.0/24, 2001:db8::/48" />
          <Field.Description>{t().allowedHint}</Field.Description>
        </Field.Field>
      </Field.Group>

      <div class="grid gap-2">
        <h3 class="flex items-center gap-1 text-sm font-medium">{t().connectors}<InfoTooltip id="tunnel-guard-connectors-tip" label={t().connectors} description={t().connectorsTip} /></h3>
        {#if connectors.length}
          <div class="max-h-56 overflow-y-auto border">
            <Table.Root aria-label={t().connectors}>
              <Table.Header class="bg-muted">
                <Table.Row class="hover:bg-transparent">
                  <Table.Head class="pl-4">{t().address}</Table.Head>
                  <Table.Head class="pr-4">{t().lastSeen}</Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {#each connectors as connector (connector.client)}
                  <Table.Row>
                    <Table.Cell class="pl-4 font-mono">{connector.client}</Table.Cell>
                    <Table.Cell class="pr-4 text-muted-foreground tabular-nums">{formatClock(connector.last_seen)} {formatDay(connector.last_seen)}</Table.Cell>
                  </Table.Row>
                {/each}
              </Table.Body>
            </Table.Root>
          </div>
        {:else}
          <Empty.Root class="border border-dashed">
            <Empty.Header>
              <Empty.Media variant="icon"><PlugConnected20 class="size-5" aria-hidden="true" /></Empty.Media>
              <Empty.Title>{t().noConnectors}</Empty.Title>
            </Empty.Header>
          </Empty.Root>
        {/if}
      </div>
    {/if}
  </Card.Content>
  {#if dirty || busy}
    <Card.Footer class="justify-end gap-2">
      <Button variant="outline" disabled={busy} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button disabled={busy} onclick={save}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    </Card.Footer>
  {/if}
</Card.Root>
