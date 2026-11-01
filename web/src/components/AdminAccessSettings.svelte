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
  import { isIpRange, joinScope, splitScope, tagSuggestions, unknownTags } from '../ipScope.svelte';
  import type { AdminAccessSettings, AdminAccessView, IpAddressesView, IpTagUse } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 설정 > 보안의 관리 콘솔 접근 제한 카드. 공개 관리 주소의 콘솔을 허용 IP에서만 열고, 되돌리기·저장으로만 반영합니다.
  // 허용 IP는 한 입력칸(TagInput)에 IP 주소록의 태그와 IP·CIDR을 함께 받고(태그는 secondary 칩, 제안은 주소록의 태그),
  // 저장할 때 splitScope로 allowed_tags와 allowed로 나눕니다. 주소록에 없는 태그와 개수 초과는 저장 전에 알려 줍니다.
  // 현재 IP 추가는 서버가 알려 준 client_range(IPv4는 주소, IPv6는 /64)를 넣으며, 로컬 관리 포트에서는 보이지 않습니다.
  // 허용 IP 미지정 안내는 빈 목록을 저장할 수 있는 로컬 관리 포트에서만 보입니다(공개 주소에서는 409).
  const t = translations({
    title: 'Admin console access',
    tip: 'Limits the console and admin API on the public admin address to the allowed ranges; other addresses get 404. The Connector API and access protection sign-in stay open, and the local admin port over SSH is never limited. If you are locked out without SSH, set MEERKAT_ADMIN_IP_LIMIT=false in compose.env and run docker compose up -d again.',
    restricted: 'Allow listed IPs only',
    current: (ip: string) => `Current IP ${ip}`,
    local: 'Local admin port, not limited',
    allowed: 'Allowed IPs',
    allowedTip: 'On the public admin address, addresses outside these get 404. Tags stand for every address in the IP address book that carries them. Add current IP adds the /64 of an IPv6 address.',
    allowedHint: 'Tags from the IP address book, or IPv4 or IPv6 addresses or CIDR',
    placeholder: 'Tag, IP or CIDR',
    unknownTag: (tag: string) => `Not a tag in the IP address book: ${tag}`,
    tooMany: 'Choose up to 20 tags and 50 IPs or CIDR ranges.',
    emptyClosed: 'With nothing listed, the console is closed on the public address',
    addCurrent: 'Add current IP',
    lifted: 'MEERKAT_ADMIN_IP_LIMIT=false lifts this limit. Remove it and run docker compose up -d again to apply the limit.',
    shared: 'The current IP looks like a proxy, a private network or a CDN rather than your own. Allowing it lets in every client that comes through it.',
    damaged: 'The saved limit could not be read, so the console is closed on the public address. Save the settings again.',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    locksOut: 'Your current IP is not among the allowed IPs, so saving would lock you out. Add it with Add current IP.',
    invalid: 'Check the values. Allowed IPs are tags from the IP address book or IPv4 or IPv6 addresses or CIDR ranges, up to 20 tags and 50 ranges.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load admin console access: ${code}`,
  }, {
    title: '관리 콘솔 접근 제한',
    tip: '공개 관리 주소의 콘솔과 관리 API를 허용 대역에서만 제공하며, 그 밖의 주소에는 404로 응답합니다. Connector API와 접근 보호 로그인은 제한하지 않으며, SSH로 연결한 로컬 관리 포트에는 적용되지 않습니다. SSH 없이 접속이 차단된 경우 compose.env에 MEERKAT_ADMIN_IP_LIMIT=false를 설정하고 docker compose up -d를 다시 실행하십시오.',
    restricted: '허용 IP에서만 접속 허용',
    current: (ip: string) => `현재 접속 IP ${ip}`,
    local: '로컬 관리 포트 접속, 제한 미적용',
    allowed: '허용 IP',
    allowedTip: '공개 관리 주소에서 이 범위 밖의 주소에는 404로 응답합니다. 태그는 IP 주소록에서 그 태그가 붙은 모든 주소를 의미합니다. 현재 IP 추가 시 IPv6 주소는 /64 단위로 추가됩니다.',
    allowedHint: 'IP 주소록의 태그, IPv4·IPv6 주소 또는 CIDR',
    placeholder: '태그, IP 또는 CIDR',
    unknownTag: (tag: string) => `IP 주소록에 없는 태그입니다: ${tag}`,
    tooMany: '태그는 최대 20개, IP·CIDR은 최대 50개까지 지정할 수 있습니다.',
    emptyClosed: '미지정 시 공개 주소의 콘솔 접속 차단',
    addCurrent: '현재 IP 추가',
    lifted: 'MEERKAT_ADMIN_IP_LIMIT=false 환경 변수로 제한이 해제되어 있습니다. 제한을 적용하려면 환경 변수를 제거하고 docker compose up -d를 다시 실행하십시오.',
    shared: '현재 접속 IP가 본인 주소가 아닌 프록시, 사설망 또는 CDN의 주소로 보입니다. 이 주소를 허용하면 같은 경로로 들어오는 모든 클라이언트가 허용됩니다.',
    damaged: '저장된 제한 설정을 읽을 수 없어 공개 주소의 콘솔 접속이 차단되었습니다. 설정을 다시 저장하십시오.',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    locksOut: '현재 접속 IP가 허용 IP에 없어 저장 시 접속이 차단됩니다. 현재 IP 추가로 추가하십시오.',
    invalid: '입력값이 올바르지 않습니다. 허용 IP는 IP 주소록의 태그 또는 IPv4·IPv6 주소나 CIDR이어야 하며, 태그 최대 20개, 대역 최대 50개까지 지정할 수 있습니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `관리 콘솔 접근 제한 설정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  // 입력칸 하나에 태그와 IP·CIDR이 함께 들어 있으므로 초안은 values 하나로 두고, 저장할 때 나눕니다.
  let view = $state<AdminAccessView | null>(null);
  let draft = $state<{ restricted: boolean; values: string[] } | null>(null);
  let ipTags = $state<IpTagUse[] | null>(null);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  const draftOf = (next: AdminAccessView) => ({ restricted: next.restricted, values: joinScope(next.allowed_tags, next.allowed) });
  const adopt = (next: AdminAccessView) => { view = next; draft = draftOf(next); };
  const scope = $derived(splitScope(draft?.values ?? []));
  const dirty = $derived(Boolean(view && draft) && (draft?.restricted !== view?.restricted || JSON.stringify(scope.tags) !== JSON.stringify(view?.allowed_tags) || JSON.stringify(scope.ranges) !== JSON.stringify(view?.allowed)));
  const currentAdded = $derived(Boolean(view?.client_range && draft?.values.some((value) => value.toLowerCase() === view?.client_range?.toLowerCase())));

  onMount(() => {
    api<AdminAccessView>('/api/v1/security/admin-access').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
    // 제안과 사전 확인에만 쓰므로, 주소록을 불러오지 못하면 제안 없이 서버의 확인에 맡깁니다.
    api<IpAddressesView>('/api/v1/security/ip-addresses').then((book) => { ipTags = book.tags; }).catch(() => {});
  });

  function addCurrent(): void {
    if (draft && view?.client_range && !currentAdded) draft.values = [...draft.values, view.client_range];
  }

  async function save(): Promise<void> {
    if (!draft || busy) return;
    error = '';
    const { tags, ranges } = scope;
    const unknown = ipTags ? unknownTags(tags, ipTags) : [];
    if (unknown.length) { error = t().unknownTag(unknown[0]); return; }
    if (tags.length > 20 || ranges.length > 50) { error = t().tooMany; return; }
    busy = true;
    try {
      const body: AdminAccessSettings = { restricted: draft.restricted, allowed: ranges, allowed_tags: tags };
      adopt(await api<AdminAccessView>('/api/v1/security/admin-access', { method: 'PUT', body: JSON.stringify(body) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'admin_access_locks_out' ? t().locksOut : code === 'invalid_admin_access' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (view) draft = draftOf(view);
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
      {#if view.damaged}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{t().damaged}</Alert.Description></Alert.Root>{/if}
      {#if view.client_shared}<Alert.Root><Warning aria-hidden="true" /><Alert.Description>{t().shared}</Alert.Description></Alert.Root>{/if}
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
          <TagInput id="admin-access-allowed" ariaLabel={t().allowed} bind:values={draft.values} mono placeholder={t().placeholder} suggestions={tagSuggestions(ipTags)} autoselect secondary={(value) => !isIpRange(value)} />
          <Field.Description>{draft.restricted && draft.values.length === 0 && !view.client ? t().emptyClosed : t().allowedHint}</Field.Description>
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
