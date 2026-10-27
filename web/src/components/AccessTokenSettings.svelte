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
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Add12, Key20, Warning } from '../icons';
  import type { AccessToken, AccessTokensView, CreatedAccessToken } from '../types';
  import CopyText from './CopyText.svelte';
  import InfoTooltip from './InfoTooltip.svelte';

  // 설정 > 접근 보호의 접근 토큰 카드. 로그인할 수 없는 클라이언트(스크립트, CI, 다른 서비스)가 토큰을 허용한 접근 정책을 통과하는 데 씁니다.
  // 토큰 값은 만든 직후 대화상자에만 한 번 보여 주고, 대화상자가 닫히면 지웁니다. 서버에도 값은 남지 않아 다시 볼 수 없습니다.
  // tokens는 접근 토큰 목록(불러오기 전에는 null)으로, 만들거나 지울 때마다 갱신해 접근 정책 카드가 허용할 토큰을 고르게 합니다.
  let { tokens = $bindable(null) }: { tokens?: AccessToken[] | null } = $props();

  const t = translations({
    title: 'Access tokens',
    tip: 'Clients that can\'t sign in, such as scripts, CI and other services, pass the access policies that allow a token by sending its value in the X-Meerkat-Access-Token header. The app receives the token name in the X-Meerkat-Access-Service header. A token\'s value is shown only once, when it is created.',
    create: 'Create token',
    name: 'Name',
    created: 'Created',
    expires: 'Expires',
    never: 'No expiry',
    expired: 'Expired',
    noTokens: 'No access tokens',
    createTitle: 'Create token',
    tokenName: 'Token name',
    expiry: 'Expiry',
    expiryOptions: { 0: 'No expiry', 30: '30 days', 90: '90 days', 365: '1 year' } as Record<string, string>,
    creating: 'Creating…',
    createdTitle: 'Token created',
    shownOnce: 'This value is shown only once. Copy it and keep it somewhere safe.',
    header: 'Request header',
    done: 'Done',
    detailTitle: 'Access token',
    close: 'Close',
    cancel: 'Cancel',
    delete: 'Delete',
    deleteTitle: 'Delete this token?',
    deleted: 'Deleted',
    inUse: 'A token that a policy allows can\'t be deleted. Remove it from the policies first.',
    invalid: 'Check the values: the name can be up to 64 characters, and up to 100 tokens can be created.',
    failed: (code: string) => `Couldn't create: ${code}`,
    deleteFailed: (code: string) => `Couldn't delete: ${code}`,
    loadFailed: (code: string) => `Couldn't load access tokens: ${code}`,
  }, {
    title: '접근 토큰',
    tip: '로그인할 수 없는 클라이언트(스크립트, CI, 다른 서비스 등)는 X-Meerkat-Access-Token 헤더로 토큰 값을 보내 해당 토큰을 허용한 접근 정책을 통과할 수 있습니다. 앱에는 토큰 이름이 X-Meerkat-Access-Service 헤더로 전달됩니다. 토큰 값은 생성할 때 한 번만 표시됩니다.',
    create: '토큰 생성',
    name: '이름',
    created: '생성일',
    expires: '만료',
    never: '만료 없음',
    expired: '만료됨',
    noTokens: '접근 토큰이 없습니다',
    createTitle: '토큰 생성',
    tokenName: '토큰 이름',
    expiry: '만료 기간',
    expiryOptions: { 0: '만료 없음', 30: '30일', 90: '90일', 365: '1년' },
    creating: '생성 중…',
    createdTitle: '토큰이 생성되었습니다',
    shownOnce: '이 값은 지금만 표시됩니다. 복사하여 안전한 곳에 보관하십시오.',
    header: '요청 헤더',
    done: '완료',
    detailTitle: '접근 토큰',
    close: '닫기',
    cancel: '취소',
    delete: '삭제',
    deleteTitle: '이 토큰을 삭제하시겠습니까?',
    deleted: '삭제되었습니다',
    inUse: '정책에서 허용 중인 토큰은 삭제할 수 없습니다. 정책에서 먼저 제외하십시오.',
    invalid: '입력값이 올바르지 않습니다. 이름은 64자 이하여야 하며, 토큰은 최대 100개까지 만들 수 있습니다.',
    failed: (code: string) => `생성 중 오류가 발생했습니다: ${code}`,
    deleteFailed: (code: string) => `삭제 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `접근 토큰을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const maxTokens = 100;
  const expiryDays = ['0', '30', '90', '365'];

  // 날짜는 현재 언어로, 시각을 넣으면 분 단위까지 보여 줍니다. 읽을 수 없는 값은 그대로 둡니다.
  const formatDate = (value: string, withTime = false): string => {
    const time = Date.parse(value);
    if (Number.isNaN(time)) return value;
    return new Intl.DateTimeFormat(intlLocale(), withTime ? { dateStyle: 'medium', timeStyle: 'short' } : { dateStyle: 'medium' }).format(time);
  };
  const isExpired = (token: AccessToken): boolean => Boolean(token.expires_at) && Date.parse(token.expires_at) <= Date.now();

  let loadError = $state('');
  let busy = $state(false);

  // 만들기 대화상자: 입력 화면에서 만들면 같은 대화상자가 값 화면으로 바뀝니다. value가 비어 있으면 입력 화면입니다.
  let createOpen = $state(false);
  let createName = $state('');
  let createDays = $state('0');
  let createError = $state('');
  let createdValue = $state('');

  // 상세 대화상자: 열 때의 토큰을 복사해 두어, 삭제 뒤 닫히는 동안에도 내용이 남게 합니다.
  let detailOpen = $state(false);
  let detail = $state<AccessToken | null>(null);
  let detailError = $state('');
  let confirmOpen = $state(false);

  const header = $derived(`X-Meerkat-Access-Token: ${createdValue}`);

  onMount(() => {
    api<AccessTokensView>('/api/v1/security/access-tokens').then((view) => { tokens = view.tokens; }).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  function openCreate(): void {
    createName = '';
    createDays = '0';
    createError = '';
    createdValue = '';
    createOpen = true;
  }

  // 값은 대화상자가 완전히 닫힌 뒤에 지워, 닫히는 동안 입력 화면으로 바뀌어 보이지 않게 합니다.
  function createClosed(open: boolean): void {
    if (!open) createdValue = '';
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!tokens || busy) return;
    busy = true;
    createError = '';
    try {
      const result = await api<CreatedAccessToken>('/api/v1/security/access-tokens', { method: 'POST', body: JSON.stringify({ name: createName.trim(), expires_days: Number(createDays) }) });
      tokens = [...tokens, result.token];
      createdValue = result.value;
      // 입력 화면의 버튼이 사라지므로, 다음에 할 일인 복사 버튼으로 초점을 옮깁니다.
      await tick();
      document.querySelector<HTMLElement>('#access-token-header button')?.focus();
    } catch (cause) {
      const code = errorMessage(cause);
      createError = code === 'invalid_access_token' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function openDetail(token: AccessToken): void {
    detail = { ...token };
    detailError = '';
    detailOpen = true;
  }

  async function remove(): Promise<void> {
    if (!tokens || !detail || busy) return;
    const id = detail.id;
    busy = true;
    detailError = '';
    try {
      try {
        await api(`/api/v1/security/access-tokens/${encodeURIComponent(id)}`, { method: 'DELETE' });
      } catch (cause) {
        // 이미 지워진 토큰은 목록에서만 빼면 됩니다.
        if (errorMessage(cause) !== 'access_token_not_found') throw cause;
      }
      tokens = tokens.filter((token) => token.id !== id);
      confirmOpen = false;
      detailOpen = false;
      toast.success(t().deleted);
    } catch (cause) {
      const code = errorMessage(cause);
      confirmOpen = false;
      detailError = code === 'access_token_in_use' ? t().inUse : t().deleteFailed(code);
    } finally {
      busy = false;
    }
  }
</script>

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
              <Table.Head class="hidden sm:table-cell">{t().created}</Table.Head>
              <Table.Head class="pr-4">{t().expires}</Table.Head>
            </Table.Row>
          </Table.Header>
          <Table.Body>
            {#each tokens as token (token.id)}
              <Table.Row class="relative cursor-pointer">
                <Table.Cell class="pl-4">
                  <button type="button" class="block max-w-40 truncate text-left text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50 sm:max-w-56" onclick={() => openDetail(token)}>{token.name}</button>
                </Table.Cell>
                <Table.Cell class="hidden tabular-nums sm:table-cell">{formatDate(token.created_at)}</Table.Cell>
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
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md" onInteractOutside={(event) => { if (createdValue) event.preventDefault(); }}>
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
          <Field.Field>
            <Field.Label for="access-token-name">{t().tokenName}</Field.Label>
            <Input id="access-token-name" bind:value={createName} maxlength={64} pattern=".*\S.*" autocomplete="off" required />
          </Field.Field>
          <Field.Field>
            <Field.Label for="access-token-expiry">{t().expiry}</Field.Label>
            <NativeSelect.Root id="access-token-expiry" bind:value={createDays}>
              {#each expiryDays as days (days)}
                <NativeSelect.Option value={days}>{t().expiryOptions[days]}</NativeSelect.Option>
              {/each}
            </NativeSelect.Root>
          </Field.Field>
        </Field.Group>
        {#if createError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{createError}</Alert.Description></Alert.Root>{/if}
        <Dialog.Footer>
          <Button variant="outline" disabled={busy} onclick={() => (createOpen = false)}>{t().cancel}</Button>
          <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().creating}{:else}{t().create}{/if}</Button>
        </Dialog.Footer>
      </form>
    {/if}
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={() => detailOpen, (open) => { if (!busy) detailOpen = open; }}>
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md">
    {#if detail}
      <div class="grid gap-4">
        <Dialog.Header>
          <Dialog.Title>{t().detailTitle}</Dialog.Title>
        </Dialog.Header>
        <dl class="grid grid-cols-[auto_minmax(0,1fr)] gap-x-6 gap-y-2 text-sm">
          <dt class="text-muted-foreground">{t().name}</dt>
          <dd class="min-w-0 wrap-anywhere">{detail.name}</dd>
          <dt class="text-muted-foreground">{t().created}</dt>
          <dd class="min-w-0 tabular-nums">{formatDate(detail.created_at, true)}</dd>
          <dt class="text-muted-foreground">{t().expires}</dt>
          <dd class="flex min-w-0 flex-wrap items-center gap-1.5">
            {#if !detail.expires_at}
              <span class="text-muted-foreground">{t().never}</span>
            {:else}
              <span class="tabular-nums">{formatDate(detail.expires_at, true)}</span>
              {#if isExpired(detail)}<Badge variant="destructive" class="select-none">{t().expired}</Badge>{/if}
            {/if}
          </dd>
        </dl>
        {#if detailError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{detailError}</Alert.Description></Alert.Root>{/if}
        <Dialog.Footer class="sm:justify-between">
          <Button variant="destructive" disabled={busy} onclick={() => (confirmOpen = true)}>{t().delete}</Button>
          <Button variant="outline" disabled={busy} onclick={() => (detailOpen = false)}>{t().close}</Button>
        </Dialog.Footer>
      </div>
    {/if}
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={() => confirmOpen, (open) => { if (!busy) confirmOpen = open; }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{t().deleteTitle}</AlertDialog.Title>
      <AlertDialog.Description>{detail?.name}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>{t().cancel}</AlertDialog.Cancel>
      <Button variant="destructive" disabled={busy} onclick={remove}>{#if busy}<Spinner aria-hidden="true" />{/if}{t().delete}</Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
