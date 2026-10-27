<script lang="ts">
  import { onMount, tick } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
  import { Separator } from '$lib/components/ui/separator/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Add12, Dismiss, Key20, Warning } from '../icons';
  import type { AccessToken, AccessTokenInput, AccessTokensView, CreatedAccessToken } from '../types';
  import CopyText from './CopyText.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 접근 보호 화면의 접근 토큰 카드. 로그인할 수 없는 클라이언트(스크립트, CI, 다른 서비스)가 보내, 토큰이 필요한 접근 정책에 해당하게 합니다.
  // 토큰은 이용 범위(적용 주소, 허용 IP 대역) 안에서만 통하며, 범위와 이름은 행을 눌러 열리는 편집 대화상자에서 바꿉니다. 토큰 값은 바뀌지 않습니다.
  // 토큰 값은 만든 직후 대화상자에만 한 번 보여 주고, 대화상자가 닫히면 지웁니다. 서버에도 값은 남지 않아 다시 볼 수 없습니다.
  // tokens는 접근 토큰 목록(불러오기 전에는 null)으로, 만들거나 고치거나 지울 때마다 갱신해 접근 정책 카드가 토큰 조건의 토큰을 고르게 합니다.
  let { tokens = $bindable(null) }: { tokens?: AccessToken[] | null } = $props();

  const t = translations({
    title: 'Access tokens',
    tip: 'Clients that can\'t sign in, such as scripts, CI and other services, send a token\'s value in the X-Meerkat-Access-Token header to meet the access policies that require it; with Bypass, the token alone lets them in. A token works only on its addresses and from its IP ranges. The app receives the token name in the X-Meerkat-Access-Service header. A token\'s value is shown only once, when it is created.',
    create: 'Create token',
    name: 'Name',
    addresses: 'Addresses',
    lastUsed: 'Last used',
    neverUsed: 'Never used',
    created: 'Created',
    expires: 'Expires',
    never: 'No expiry',
    expired: 'Expired',
    noTokens: 'No access tokens',
    createTitle: 'Create token',
    editTitle: 'Edit token',
    tokenName: 'Token name',
    address: 'Address',
    addAddress: 'Add address',
    removeAddress: 'Remove address',
    hostPlaceholder: 'app.example.com or *.example.com',
    allowedIps: 'Allowed IPs',
    scope: 'Scope',
    anyIp: 'Any IP',
    selectedIps: 'Selected IP ranges',
    ipRanges: 'IP ranges',
    ipHint: 'CIDR or single address',
    needsRange: 'Enter at least one IP range.',
    expiry: 'Expiry',
    expiryOptions: { 0: 'No expiry', 30: '30 days', 90: '90 days', 365: '1 year' } as Record<string, string>,
    policies: 'Policies',
    noPolicies: 'No policy requires it',
    creating: 'Creating…',
    saving: 'Saving…',
    saved: 'Saved',
    createdTitle: 'Token created',
    shownOnce: 'This value is shown only once. Copy it and keep it somewhere safe.',
    header: 'Request header',
    done: 'Done',
    cancel: 'Cancel',
    save: 'Save',
    delete: 'Delete',
    deleteTitle: 'Delete this token?',
    deleted: 'Deleted',
    gone: 'This token no longer exists.',
    inUse: 'A token that a policy names can\'t be deleted. Remove it from the policies first.',
    invalid: 'Check the values: the name can be up to 64 characters, addresses are hostnames or *.domain, IP ranges are CIDR or single addresses, and up to 100 tokens can be created.',
    failed: (code: string) => `Couldn't create: ${code}`,
    saveFailed: (code: string) => `Couldn't save: ${code}`,
    deleteFailed: (code: string) => `Couldn't delete: ${code}`,
    loadFailed: (code: string) => `Couldn't load access tokens: ${code}`,
  }, {
    title: '접근 토큰',
    tip: '로그인할 수 없는 클라이언트(스크립트, CI, 다른 서비스 등)는 X-Meerkat-Access-Token 헤더로 토큰 값을 보내 해당 토큰이 필요한 접근 정책에 해당하며, 우회 정책이면 토큰만으로 통과합니다. 토큰은 지정한 주소와 IP 대역에서만 사용할 수 있습니다. 앱에는 토큰 이름이 X-Meerkat-Access-Service 헤더로 전달됩니다. 토큰 값은 생성할 때 한 번만 표시됩니다.',
    create: '토큰 생성',
    name: '이름',
    addresses: '적용 주소',
    lastUsed: '마지막 사용',
    neverUsed: '사용 기록 없음',
    created: '생성일',
    expires: '만료',
    never: '만료 없음',
    expired: '만료됨',
    noTokens: '접근 토큰이 없습니다',
    createTitle: '토큰 생성',
    editTitle: '토큰 편집',
    tokenName: '토큰 이름',
    address: '주소',
    addAddress: '주소 추가',
    removeAddress: '주소 삭제',
    hostPlaceholder: 'app.example.com 또는 *.example.com',
    allowedIps: '허용 IP',
    scope: '이용 범위',
    anyIp: '모든 IP',
    selectedIps: '지정한 IP 대역',
    ipRanges: 'IP 대역',
    ipHint: 'CIDR 또는 단일 주소',
    needsRange: 'IP 대역을 하나 이상 입력하십시오.',
    expiry: '만료 기간',
    expiryOptions: { 0: '만료 없음', 30: '30일', 90: '90일', 365: '1년' },
    policies: '적용 정책',
    noPolicies: '이 토큰이 필요한 정책 없음',
    creating: '생성 중…',
    saving: '저장 중…',
    saved: '저장되었습니다',
    createdTitle: '토큰이 생성되었습니다',
    shownOnce: '이 값은 지금만 표시됩니다. 복사하여 안전한 곳에 보관하십시오.',
    header: '요청 헤더',
    done: '완료',
    cancel: '취소',
    save: '저장',
    delete: '삭제',
    deleteTitle: '이 토큰을 삭제하시겠습니까?',
    deleted: '삭제되었습니다',
    gone: '이미 삭제된 토큰입니다.',
    inUse: '정책에 지정된 토큰은 삭제할 수 없습니다. 정책에서 먼저 제외하십시오.',
    invalid: '입력값이 올바르지 않습니다. 이름은 64자 이하, 주소는 호스트 이름 또는 *.도메인, IP 대역은 CIDR 또는 단일 주소여야 하며, 토큰은 최대 100개까지 만들 수 있습니다.',
    failed: (code: string) => `생성 중 오류가 발생했습니다: ${code}`,
    saveFailed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    deleteFailed: (code: string) => `삭제 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `접근 토큰을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const maxTokens = 100;
  const maxHosts = 100;
  const maxRanges = 50;
  const expiryDays = ['0', '30', '90', '365'];
  const createPrefix = 'access-token-create';
  const editPrefix = 'access-token-edit';

  // 날짜는 현재 언어로, 시각을 넣으면 분 단위까지 보여 줍니다. 읽을 수 없는 값은 그대로 둡니다.
  const formatDate = (value: string, withTime = false): string => {
    const time = Date.parse(value);
    if (Number.isNaN(time)) return value;
    return new Intl.DateTimeFormat(intlLocale(), withTime ? { dateStyle: 'medium', timeStyle: 'short' } : { dateStyle: 'medium' }).format(time);
  };
  const isExpired = (token: AccessToken): boolean => Boolean(token.expires_at) && Date.parse(token.expires_at) <= Date.now();

  // 대화상자의 주소 줄은 지워도 입력 칸이 섞이지 않도록 고유한 key를 가집니다.
  type HostRow = { key: number; value: string };
  let nextKey = 0;
  const newHost = (value = ''): HostRow => ({ key: nextKey++, value });

  let addresses = $state<string[]>([]);
  let loadError = $state('');
  let busy = $state(false);

  // 만들기와 편집 대화상자는 같은 입력 항목을 씁니다(동시에 하나만 열림). 주소 줄은 항상 하나 이상 남깁니다.
  // scope가 'any'면 모든 IP, 'selected'면 ranges에 적은 대역에서만 쓸 수 있습니다.
  let form = $state<{ name: string; hosts: HostRow[]; scope: 'any' | 'selected'; ranges: string[]; days: string }>({
    name: '', hosts: [], scope: 'any', ranges: [], days: '90',
  });
  let formError = $state('');
  // 지정한 IP 대역을 고르고 대역을 하나도 넣지 않은 채 저장하려 하면 켜고, 대역을 넣거나 선택을 바꾸면 끕니다.
  let rangeMissing = $state(false);

  // 만들기 대화상자: 입력 화면에서 만들면 같은 대화상자가 값 화면으로 바뀝니다. value가 비어 있으면 입력 화면입니다.
  let createOpen = $state(false);
  let createdValue = $state('');

  // 편집 대화상자: 열 때의 토큰을 복사해 두어, 삭제 뒤 닫히는 동안에도 내용이 남게 합니다.
  let editOpen = $state(false);
  let editing = $state<AccessToken | null>(null);
  let confirmOpen = $state(false);

  const header = $derived(`X-Meerkat-Access-Token: ${createdValue}`);

  onMount(() => {
    api<AccessTokensView>('/api/v1/security/access-tokens').then((view) => {
      tokens = view.tokens;
      addresses = view.addresses ?? [];
    }).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  function openCreate(): void {
    form = { name: '', hosts: [newHost()], scope: 'any', ranges: [], days: '90' };
    formError = '';
    rangeMissing = false;
    createdValue = '';
    createOpen = true;
  }

  // 값은 대화상자가 완전히 닫힌 뒤에 지워, 닫히는 동안 입력 화면으로 바뀌어 보이지 않게 합니다.
  function createClosed(open: boolean): void {
    if (!open) createdValue = '';
  }

  async function addHost(prefix: string): Promise<void> {
    const row = newHost();
    form.hosts.push(row);
    await tick();
    document.getElementById(`${prefix}-host-${row.key}`)?.focus();
  }

  // 입력값을 서버 형식으로 바꿉니다. 주소는 소문자로 맞추고 중복을 뺍니다. 올바르지 않으면 오류를 보여 주고 null을 돌려줍니다.
  function scopeInput(prefix: string): Pick<AccessTokenInput, 'name' | 'hosts' | 'ranges'> | null {
    const hosts = [...new Set(form.hosts.map((row) => row.value.trim().toLowerCase()).filter(Boolean))];
    const ranges = form.scope === 'selected' ? [...form.ranges] : [];
    if (form.scope === 'selected' && !ranges.length) {
      rangeMissing = true;
      document.getElementById(`${prefix}-ranges`)?.focus();
      return null;
    }
    if (hosts.length > maxHosts || ranges.length > maxRanges) {
      formError = t().invalid;
      return null;
    }
    return { name: form.name.trim(), hosts, ranges };
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!tokens || busy) return;
    const scope = scopeInput(createPrefix);
    if (!scope) return;
    busy = true;
    formError = '';
    try {
      const body: AccessTokenInput = { ...scope, expires_days: Number(form.days) };
      const result = await api<CreatedAccessToken>('/api/v1/security/access-tokens', { method: 'POST', body: JSON.stringify(body) });
      tokens = [...tokens, result.token];
      createdValue = result.value;
      // 입력 화면의 버튼이 사라지므로, 다음에 할 일인 복사 버튼으로 초점을 옮깁니다.
      await tick();
      document.querySelector<HTMLElement>('#access-token-header button')?.focus();
    } catch (cause) {
      const code = errorMessage(cause);
      formError = code === 'invalid_access_token' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function openEdit(token: AccessToken): void {
    editing = { ...token, policies: [...token.policies] };
    form = { name: token.name, hosts: token.hosts.map((host) => newHost(host)), scope: token.ranges.length ? 'selected' : 'any', ranges: [...token.ranges], days: '90' };
    formError = '';
    rangeMissing = false;
    editOpen = true;
    // 목록을 불러온 뒤 정책이 바뀌었을 수 있으므로, 정책 카드와 어긋나지 않게 읽기 전용 항목만 다시 읽어 옵니다.
    api<AccessTokensView>('/api/v1/security/access-tokens').then((view) => {
      const fresh = view.tokens.find((entry) => entry.id === token.id);
      if (fresh && editing?.id === fresh.id) editing = { ...editing, policies: fresh.policies, last_used_at: fresh.last_used_at };
    }).catch(() => {});
  }

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!tokens || !editing || busy) return;
    const scope = scopeInput(editPrefix);
    if (!scope) return;
    const id = editing.id;
    busy = true;
    formError = '';
    try {
      const updated = await api<AccessToken>(`/api/v1/security/access-tokens/${encodeURIComponent(id)}`, { method: 'PUT', body: JSON.stringify(scope) });
      tokens = tokens.map((token) => (token.id === id ? updated : token));
      editOpen = false;
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      if (code === 'access_token_not_found') {
        // 이미 지워진 토큰은 목록에서만 빼면 됩니다.
        tokens = tokens.filter((token) => token.id !== id);
        editOpen = false;
        toast.error(t().gone);
      } else {
        formError = code === 'invalid_access_token' ? t().invalid : t().saveFailed(code);
      }
    } finally {
      busy = false;
    }
  }

  async function remove(): Promise<void> {
    if (!tokens || !editing || busy) return;
    const id = editing.id;
    busy = true;
    formError = '';
    try {
      try {
        await api(`/api/v1/security/access-tokens/${encodeURIComponent(id)}`, { method: 'DELETE' });
      } catch (cause) {
        // 이미 지워진 토큰은 목록에서만 빼면 됩니다.
        if (errorMessage(cause) !== 'access_token_not_found') throw cause;
      }
      tokens = tokens.filter((token) => token.id !== id);
      confirmOpen = false;
      editOpen = false;
      toast.success(t().deleted);
    } catch (cause) {
      const code = errorMessage(cause);
      confirmOpen = false;
      formError = code === 'access_token_in_use' ? t().inUse : t().deleteFailed(code);
    } finally {
      busy = false;
    }
  }
</script>

<!-- 만들기와 편집 대화상자가 함께 쓰는 입력 항목: 이름과 이용 범위(적용 주소, 허용 IP). prefix는 두 대화상자의 id를 구분합니다. -->
{#snippet scopeFields(prefix: string)}
  <Field.Field>
    <Field.Label for={`${prefix}-name`}>{t().tokenName}</Field.Label>
    <Input id={`${prefix}-name`} bind:value={form.name} maxlength={64} pattern=".*\S.*" autocomplete="off" required />
  </Field.Field>
  <!-- 적용 주소와 허용 IP는 함께 토큰의 이용 범위라 한데 묶고, 항목마다 상자로 둡니다. -->
  <Field.Set>
    <Field.Legend>{t().scope}</Field.Legend>
    <Item.Group>
      <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
        <Field.Set>
          <Field.Legend variant="label">{t().addresses}</Field.Legend>
          <div class="grid gap-2">
            {#each form.hosts as row, index (row.key)}
              <div class="flex items-center gap-2">
                <Input
                  id={`${prefix}-host-${row.key}`}
                  class="min-w-0 flex-1 font-mono"
                  list={`${prefix}-addresses`}
                  bind:value={row.value}
                  placeholder={t().hostPlaceholder}
                  aria-label={`${t().address} ${index + 1}`}
                  autocomplete="off"
                  autocapitalize="none"
                  spellcheck={false}
                  pattern=".*\S.*"
                  required
                />
                <IconButton label={t().removeAddress} icon={Dismiss} disabled={form.hosts.length <= 1} onclick={() => form.hosts.splice(index, 1)} />
              </div>
            {/each}
          </div>
          <!-- 등록된 공개 주소를 자동 완성 후보로 보여 줍니다. 직접 입력과 와일드카드도 받습니다. -->
          <datalist id={`${prefix}-addresses`}>
            {#each addresses as address (address)}<option value={address}></option>{/each}
          </datalist>
          <div class="flex justify-end">
            <Button variant="outline" size="sm" disabled={form.hosts.length >= maxHosts} onclick={() => addHost(prefix)}><Add12 aria-hidden="true" />{t().addAddress}</Button>
          </div>
        </Field.Set>
      </Item.Root>
      <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
        <Field.Set>
          <Field.Legend variant="label">{t().allowedIps}</Field.Legend>
          <RadioGroup.Root value={form.scope} onValueChange={(value) => { form.scope = value === 'selected' ? 'selected' : 'any'; rangeMissing = false; }} orientation="horizontal" class="flex flex-wrap gap-x-6 gap-y-2">
            <Field.Field orientation="horizontal" class="w-auto">
              <RadioGroup.Item value="any" id={`${prefix}-ip-any`} />
              <Field.Label for={`${prefix}-ip-any`} class="font-normal">{t().anyIp}</Field.Label>
            </Field.Field>
            <Field.Field orientation="horizontal" class="w-auto">
              <RadioGroup.Item value="selected" id={`${prefix}-ip-selected`} />
              <Field.Label for={`${prefix}-ip-selected`} class="font-normal">{t().selectedIps}</Field.Label>
            </Field.Field>
          </RadioGroup.Root>
          {#if form.scope === 'selected'}
            <TagInput id={`${prefix}-ranges`} ariaLabel={t().ipRanges} bind:values={form.ranges} lowercase mono placeholder="203.0.113.0/24, 2001:db8::/48" />
            {#if rangeMissing && !form.ranges.length}
              <Field.Error>{t().needsRange}</Field.Error>
            {:else}
              <Field.Description>{t().ipHint}</Field.Description>
            {/if}
          {/if}
        </Field.Set>
      </Item.Root>
    </Item.Group>
  </Field.Set>
{/snippet}

<Card.Root size="sm" role="region" aria-labelledby="access-tokens-title">
  <Card.Header>
    <Card.Title id="access-tokens-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="access-tokens-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if tokens}
      <Card.Action>
        <Button variant="outline" size="sm" disabled={tokens.length >= maxTokens} onclick={openCreate}><Add12 aria-hidden="true" />{t().create}</Button>
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{loadError}</Alert.Description></Alert.Root>
    {:else if !tokens}
      <Skeleton class="h-24 w-full" />
    {:else if tokens.length}
      <div class="overflow-x-auto border">
        <Table.Root>
          <Table.Header class="bg-muted">
            <Table.Row class="hover:bg-transparent">
              <Table.Head class="pl-4">{t().name}</Table.Head>
              <Table.Head class="hidden sm:table-cell">{t().addresses}</Table.Head>
              <Table.Head class="hidden lg:table-cell">{t().lastUsed}</Table.Head>
              <Table.Head class="pr-4">{t().expires}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {#each tokens as token (token.id)}
              <Table.Row class="relative cursor-pointer">
                <Table.Cell class="pl-4">
                  <button type="button" class="block max-w-40 truncate text-left text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50 sm:max-w-56" onclick={() => openEdit(token)}>{token.name}</button>
                </Table.Cell>
                <Table.Cell class="hidden sm:table-cell">
                  <span class="flex items-center gap-1.5">
                    <span class="max-w-48 truncate font-mono">{token.hosts[0]}</span>
                    {#if token.hosts.length > 1}<span class="text-muted-foreground tabular-nums">+{(token.hosts.length - 1).toLocaleString(intlLocale())}</span>{/if}
                  </span>
                </Table.Cell>
                <Table.Cell class="hidden tabular-nums lg:table-cell">
                  {#if token.last_used_at}
                    {formatDate(token.last_used_at, true)}
                  {:else}
                    <span class="text-muted-foreground">{t().neverUsed}</span>
                  {/if}
                </Table.Cell>
                <Table.Cell class="pr-4">
                  {#if !token.expires_at}
                    <span class="text-muted-foreground">{t().never}</span>
                  {:else}
                    <span class="flex items-center gap-1.5">
                      <span class="tabular-nums">{formatDate(token.expires_at)}</span>
                      {#if isExpired(token)}<Badge variant="destructive" class="select-none">{t().expired}</Badge>{/if}
                    </span>
                  {/if}
                </Table.Cell>
              </Table.Row>
            {/each}
          </Table.Body>
        </Table.Root>
      </div>
    {:else}
      <Empty.Root class="border border-dashed">
        <Empty.Header>
          <Empty.Media variant="icon"><Key20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().noTokens}</Empty.Title>
        </Empty.Header>
      </Empty.Root>
    {/if}
  </Card.Content>
</Card.Root>

<Dialog.Root bind:open={() => createOpen, (open) => { if (!busy) createOpen = open; }} onOpenChangeComplete={createClosed}>
  <!-- 값 화면에서는 바깥을 눌러 실수로 닫아 값을 잃지 않도록, 완료·Esc로만 닫습니다. -->
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg" onInteractOutside={(event) => { if (createdValue) event.preventDefault(); }}>
    {#if createdValue}
      <div class="grid gap-4">
        <Dialog.Header>
          <Dialog.Title>{t().createdTitle}</Dialog.Title>
        </Dialog.Header>
        <Alert.Root><Warning aria-hidden="true" /><Alert.Description class="break-keep">{t().shownOnce}</Alert.Description></Alert.Root>
        <Field.Set>
          <Field.Legend variant="label">{t().header}</Field.Legend>
          <div id="access-token-header" class="border bg-muted p-3 text-xs">
            <CopyText value={header} class="wrap-anywhere text-clip whitespace-normal" />
          </div>
        </Field.Set>
        <Dialog.Footer>
          <Button onclick={() => (createOpen = false)}>{t().done}</Button>
        </Dialog.Footer>
      </div>
    {:else}
      <form class="grid gap-4" onsubmit={submit}>
        <Dialog.Header>
          <Dialog.Title>{t().createTitle}</Dialog.Title>
        </Dialog.Header>
        <Field.Group>
          {@render scopeFields(createPrefix)}
          <Field.Field>
            <Field.Label for="access-token-expiry">{t().expiry}</Field.Label>
            <NativeSelect.Root id="access-token-expiry" bind:value={form.days}>
              {#each expiryDays as days (days)}
                <NativeSelect.Option value={days}>{t().expiryOptions[days]}</NativeSelect.Option>
              {/each}
            </NativeSelect.Root>
          </Field.Field>
        </Field.Group>
        {#if formError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{formError}</Alert.Description></Alert.Root>{/if}
        <Dialog.Footer>
          <Button variant="outline" disabled={busy} onclick={() => (createOpen = false)}>{t().cancel}</Button>
          <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().creating}{:else}{t().create}{/if}</Button>
        </Dialog.Footer>
      </form>
    {/if}
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={() => editOpen, (open) => { if (!busy) editOpen = open; }}>
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
    {#if editing}
      <form class="grid gap-4" onsubmit={save}>
        <Dialog.Header>
          <Dialog.Title>{t().editTitle}</Dialog.Title>
        </Dialog.Header>
        <Field.Group>
          {@render scopeFields(editPrefix)}
        </Field.Group>
        <Separator />
        <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-2 text-sm">
          <dt class="text-muted-foreground">{t().created}</dt>
          <dd class="min-w-0 tabular-nums">{formatDate(editing.created_at, true)}</dd>
          <dt class="text-muted-foreground">{t().expires}</dt>
          <dd class="flex min-w-0 flex-wrap items-center gap-1.5">
            {#if !editing.expires_at}
              <span class="text-muted-foreground">{t().never}</span>
            {:else}
              <span class="tabular-nums">{formatDate(editing.expires_at, true)}</span>
              {#if isExpired(editing)}<Badge variant="destructive" class="select-none">{t().expired}</Badge>{/if}
            {/if}
          </dd>
          <dt class="text-muted-foreground">{t().lastUsed}</dt>
          <dd class="min-w-0 tabular-nums">
            {#if editing.last_used_at}
              {formatDate(editing.last_used_at, true)}
            {:else}
              <span class="text-muted-foreground">{t().neverUsed}</span>
            {/if}
          </dd>
          <dt class="text-muted-foreground">{t().policies}</dt>
          <dd class="flex min-w-0 flex-wrap items-center gap-1.5">
            {#each editing.policies as policy (policy.id)}
              <Badge variant="outline" class="max-w-full"><span class="truncate">{policy.name}</span></Badge>
            {:else}
              <span class="text-muted-foreground">{t().noPolicies}</span>
            {/each}
          </dd>
        </dl>
        {#if formError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{formError}</Alert.Description></Alert.Root>{/if}
        <Dialog.Footer>
          <Button variant="destructive" disabled={busy} onclick={() => (confirmOpen = true)}>{t().delete}</Button>
          <div class="flex flex-col-reverse gap-2 sm:ml-auto sm:flex-row">
            <Button variant="outline" disabled={busy} onclick={() => (editOpen = false)}>{t().cancel}</Button>
            <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
          </div>
        </Dialog.Footer>
      </form>
    {/if}
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={() => confirmOpen, (open) => { if (!busy) confirmOpen = open; }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{t().deleteTitle}</AlertDialog.Title>
      <AlertDialog.Description>{editing?.name}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>{t().cancel}</AlertDialog.Cancel>
      <Button variant="destructive" disabled={busy} onclick={remove}>{#if busy}<Spinner aria-hidden="true" />{/if}{t().delete}</Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
