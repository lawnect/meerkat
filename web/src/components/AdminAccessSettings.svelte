<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { Add12, ArrowUndo, Save, Warning } from '../icons';
  import type { AdminAccessSettings, AdminAccessView } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 설정 > 보안의 관리 콘솔 접근 제한 카드. 공개 관리 주소의 콘솔을 허용 대역에서만 열고, 되돌리기·저장으로만 반영합니다.
  // 현재 IP 추가는 서버가 알려 준 client_range(IPv4는 주소, IPv6는 /64)를 넣으며, 로컬 관리 포트에서는 보이지 않습니다.
  const t = translations({
    title: 'Admin console access',
    tip: 'Limits the console and admin API on the public admin address to the allowed ranges; other addresses get 404. The Connector API and access protection sign-in stay open, and the local admin port over SSH is never limited. If you are locked out without SSH, set MEERKAT_ADMIN_IP_LIMIT=false and restart the Outlet.',
    restricted: 'Allow listed IP ranges only',
    current: (ip: string) => `Current IP ${ip}`,
    local: 'Local admin port, not limited',
    allowed: 'Allowed ranges',
    allowedTip: 'On the public admin address, addresses outside these ranges get 404. Add current IP adds the /64 of an IPv6 address.',
    allowedHint: 'IPv4 or IPv6 address or CIDR, up to 50',
    emptyClosed: 'With no ranges, the console is closed on the public address',
    addCurrent: 'Add current IP',
    lifted: 'MEERKAT_ADMIN_IP_LIMIT=false lifts this limit. Remove it and restart the Outlet to apply the limit.',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    locksOut: 'Your current IP is not in the allowed ranges, so saving would lock you out. Add it with Add current IP.',
    invalid: 'Check the values. Allowed ranges are IPv4 or IPv6 addresses or CIDR ranges, up to 50.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load admin console access: ${code}`,
  }, {
    title: '관리 콘솔 접근 제한',
    tip: '공개 관리 주소의 콘솔과 관리 API를 허용 대역에서만 제공하며, 그 밖의 주소에는 404로 응답합니다. Connector API와 접근 보호 로그인은 제한하지 않으며, SSH로 연결한 로컬 관리 포트에는 적용되지 않습니다. SSH 없이 접속이 차단된 경우 MEERKAT_ADMIN_IP_LIMIT=false를 설정하고 Outlet을 재시작하십시오.',
    restricted: '허용 대역에서만 접속 허용',
    current: (ip: string) => `현재 접속 IP ${ip}`,
    local: '로컬 관리 포트 접속, 제한 미적용',
    allowed: '허용 대역',
    allowedTip: '공개 관리 주소에서 이 대역 밖의 주소에는 404로 응답합니다. 현재 IP 추가 시 IPv6 주소는 /64 단위로 추가됩니다.',
    allowedHint: 'IPv4·IPv6 주소 또는 CIDR, 최대 50개',
    emptyClosed: '대역 미지정 시 공개 주소의 콘솔 접속 차단',
    addCurrent: '현재 IP 추가',
    lifted: 'MEERKAT_ADMIN_IP_LIMIT=false 환경 변수로 제한이 해제되어 있습니다. 제한을 적용하려면 환경 변수를 제거하고 Outlet을 재시작하십시오.',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    locksOut: '현재 접속 IP가 허용 대역에 없어 저장 시 접속이 차단됩니다. 현재 IP 추가로 대역을 추가하십시오.',
    invalid: '입력값이 올바르지 않습니다. 허용 대역은 IPv4·IPv6 주소 또는 CIDR이어야 하며 최대 50개까지 지정할 수 있습니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `관리 콘솔 접근 제한 설정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  let view = $state<AdminAccessView | null>(null);
  let draft = $state<AdminAccessSettings | null>(null);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  const adopt = (next: AdminAccessView) => { view = next; draft = { restricted: next.restricted, allowed: [...next.allowed] }; };
  const dirty = $derived(Boolean(view && draft) && (draft?.restricted !== view?.restricted || JSON.stringify(draft?.allowed) !== JSON.stringify(view?.allowed)));
  const currentAdded = $derived(Boolean(view?.client_range && draft?.allowed.includes(view.client_range)));

  onMount(() => {
    api<AdminAccessView>('/api/v1/security/admin-access').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  function addCurrent(): void {
    if (draft && view?.client_range && !draft.allowed.includes(view.client_range)) draft.allowed = [...draft.allowed, view.client_range];
  }

  async function save(): Promise<void> {
    if (!draft || busy) return;
    busy = true;
    error = '';
    try {
      adopt(await api<AdminAccessView>('/api/v1/security/admin-access', { method: 'PUT', body: JSON.stringify($state.snapshot(draft)) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'admin_access_locks_out' ? t().locksOut : code === 'invalid_admin_access' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (view) draft = { restricted: view.restricted, allowed: [...view.allowed] };
    error = '';
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="admin-access-title">
  <Card.Header>
    <Card.Title id="admin-access-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="admin-access-tip" label={t().title} description={t().tip} /></Card.Title>
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !draft || !view}
      <Skeleton class="h-32 w-full" />
    {:else}
      {#if view.lifted}<Alert.Root><Warning aria-hidden="true" /><Alert.Description>{t().lifted}</Alert.Description></Alert.Root>{/if}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Field.Group class="gap-0">
        <Field.Field orientation="horizontal">
          <Field.Content>
            <Field.Label for="admin-access-restricted">{t().restricted}</Field.Label>
            <Field.Description>{view.client ? t().current(view.client) : t().local}</Field.Description>
          </Field.Content>
          <Switch id="admin-access-restricted" bind:checked={draft.restricted} disabled={busy} />
        </Field.Field>
        <Field.Separator class="my-3" />
        <Field.Field>
          <div class="flex items-center justify-between gap-2">
            <div class="flex items-center gap-1"><Field.Label for="admin-access-allowed">{t().allowed}</Field.Label><InfoTooltip id="admin-access-allowed-tip" label={t().allowed} description={t().allowedTip} /></div>
            {#if view.client_range}
              <Button variant="outline" size="sm" disabled={busy || currentAdded} onclick={addCurrent}><Add12 aria-hidden="true" />{t().addCurrent}</Button>
            {/if}
          </div>
          <TagInput id="admin-access-allowed" ariaLabel={t().allowed} bind:values={draft.allowed} lowercase mono placeholder="203.0.113.0/24, 2001:db8::/48" />
          <Field.Description>{draft.restricted && draft.allowed.length === 0 ? t().emptyClosed : t().allowedHint}</Field.Description>
        </Field.Field>
      </Field.Group>
    {/if}
  </Card.Content>
  {#if dirty || busy}
    <Card.Footer class="justify-end gap-2">
      <Button variant="outline" disabled={busy} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button disabled={busy} onclick={save}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    </Card.Footer>
  {/if}
</Card.Root>
