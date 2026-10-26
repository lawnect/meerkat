<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { ArrowExit, ArrowUndo, PlugConnected, Save, Warning } from '../icons';
  import type { AccessProviderView } from '../types';
  import CopyText from './CopyText.svelte';
  import InfoTooltip from './InfoTooltip.svelte';

  // 설정 > 보안의 접근 보호 SSO 카드. 공개 주소를 보호하는 OpenID Connect 공급자로, 관리자 SSO와 별개입니다.
  // 고친 값은 되돌리기·저장으로만 반영하고, 저장한 공급자가 켜져 있는지는 `enabled`로 알려 접근 정책 카드가 안내를 띄우게 합니다.
  let { enabled = $bindable(null) }: { enabled?: boolean | null } = $props();

  const t = translations({
    title: 'Access SSO',
    tip: 'Protected public addresses ask visitors to sign in with this OpenID Connect provider before requests reach a tunnel. It is separate from the SSO for the admin console, so people who are not administrators can be let in. Who may pass is set in access policies.',
    enabled: 'Use access SSO',
    issuer: 'Issuer URL',
    test: 'Test',
    providerFound: 'Provider found',
    providerUnreachable: 'Couldn\'t reach the provider.',
    issuerInvalid: 'Enter an https URL.',
    clientId: 'Client ID',
    clientSecret: 'Client secret',
    secretKeep: (hint: string) => `Saved (…${hint}). Enter a new secret to replace it.`,
    secretKeepShort: 'Saved. Enter a new secret to replace it.',
    redirectUri: 'Redirect URI',
    redirectHint: 'Register this URI with the provider.',
    redirectPending: 'Appears after you add a domain.',
    sessionHours: 'Session length',
    sessionHoursHint: '1–720 hours',
    hours: 'hours',
    sessions: 'Sign-in sessions',
    sessionsTip: 'Ends every sign-in on all protected addresses. People have to sign in again on their next request.',
    revoke: 'Sign everyone out',
    revokeTitle: 'Sign everyone out?',
    revokeDescription: 'Everyone is signed out of every protected address and has to sign in again.',
    revoked: 'Everyone has been signed out',
    revokeFailed: (code: string) => `Couldn't sign everyone out: ${code}`,
    cancel: 'Cancel',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values: turning access SSO on needs an https issuer URL and a client ID, and the session length must be 1–720 hours.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load access SSO settings: ${code}`,
  }, {
    title: '접근 보호 SSO',
    tip: '보호 대상 공개 주소는 요청이 터널로 전달되기 전에 이 OpenID Connect 공급자로 로그인해야 합니다. 관리 콘솔의 SSO와는 별도로 구성되므로 관리자가 아닌 사용자도 허용할 수 있습니다. 접근을 허용할 대상은 접근 정책에서 지정합니다.',
    enabled: '접근 보호 SSO 활성화',
    issuer: '발급자 URL',
    test: '연결 테스트',
    providerFound: '공급자 연결이 확인되었습니다',
    providerUnreachable: '공급자에 연결할 수 없습니다.',
    issuerInvalid: 'https URL을 입력하십시오.',
    clientId: '클라이언트 ID',
    clientSecret: '클라이언트 시크릿',
    secretKeep: (hint: string) => `저장됨(…${hint}). 변경하려면 새 시크릿을 입력하십시오.`,
    secretKeepShort: '저장됨. 변경하려면 새 시크릿을 입력하십시오.',
    redirectUri: '리디렉션 URI',
    redirectHint: '공급자의 유효한 리디렉션 URI로 등록',
    redirectPending: '도메인 등록 후 표시',
    sessionHours: '세션 유지 시간',
    sessionHoursHint: '1~720시간',
    hours: '시간',
    sessions: '로그인 세션',
    sessionsTip: '모든 보호 대상 주소에서 로그인한 사용자의 세션을 종료합니다. 종료된 사용자는 다음 요청 시 다시 로그인해야 합니다.',
    revoke: '모든 세션 종료',
    revokeTitle: '모든 세션을 종료하시겠습니까?',
    revokeDescription: '모든 보호 대상 주소에서 로그인한 사용자가 로그아웃되며, 다시 로그인해야 접근할 수 있습니다.',
    revoked: '모든 세션이 종료되었습니다',
    revokeFailed: (code: string) => `세션 종료 중 오류가 발생했습니다: ${code}`,
    cancel: '취소',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '입력값이 올바르지 않습니다. 접근 보호 SSO를 활성화하려면 https 발급자 URL과 클라이언트 ID가 필요하며, 세션 유지 시간은 1~720시간이어야 합니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `접근 보호 SSO 설정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  // 서버 값과 입력 모양이 다른 칸은 범위(공백으로 구분한 한 줄), 클라이언트 시크릿(입력했을 때만 보냄), 세션 시간(비우면 null)입니다.
  interface Draft {
    enabled: boolean;
    label: string;
    issuer: string;
    client_id: string;
    client_secret: string;
    scopes: string;
    groups_claim: string;
    session_hours: number | null;
  }

  const toDraft = (view: AccessProviderView): Draft => ({
    enabled: view.enabled,
    label: view.label,
    issuer: view.issuer,
    client_id: view.client_id,
    client_secret: '',
    scopes: view.scopes.join(' '),
    groups_claim: view.groups_claim,
    session_hours: view.session_hours,
  });

  // PUT 본문. 시크릿은 입력했을 때만 넣습니다.
  const toBody = (value: Draft) => ({
    enabled: value.enabled,
    label: value.label.trim(),
    issuer: value.issuer.trim(),
    client_id: value.client_id.trim(),
    ...(value.client_secret.trim() ? { client_secret: value.client_secret.trim() } : {}),
    scopes: value.scopes.split(/\s+/).filter(Boolean),
    groups_claim: value.groups_claim.trim(),
    session_hours: Number(value.session_hours),
  });

  let saved = $state<AccessProviderView | null>(null);
  let draft = $state<Draft | null>(null);
  let loadError = $state('');
  let error = $state('');
  let issuerError = $state('');
  let busy = $state(false);
  let testing = $state(false);
  let revokeOpen = $state(false);
  let revoking = $state(false);

  const adopt = (next: AccessProviderView) => { saved = next; draft = toDraft(next); enabled = next.enabled; };
  const dirty = $derived(Boolean(saved && draft) && JSON.stringify(toBody($state.snapshot(draft!))) !== JSON.stringify(toBody(toDraft(saved!))));

  onMount(() => {
    api<AccessProviderView>('/api/v1/security/access-provider').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!draft || busy || !dirty) return;
    busy = true;
    error = '';
    try {
      adopt(await api<AccessProviderView>('/api/v1/security/access-provider', { method: 'PUT', body: JSON.stringify(toBody($state.snapshot(draft))) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_access_provider' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (saved) draft = toDraft($state.snapshot(saved) as AccessProviderView);
    error = '';
    issuerError = '';
  }

  async function testIssuer(): Promise<void> {
    if (!draft || testing) return;
    testing = true;
    issuerError = '';
    try {
      await api<{ authorization_endpoint: string }>('/api/v1/security/access-provider/test', { method: 'POST', body: JSON.stringify({ issuer: draft.issuer.trim() }) });
      toast.success(t().providerFound);
    } catch (cause) {
      issuerError = errorMessage(cause) === 'invalid_issuer' ? t().issuerInvalid : t().providerUnreachable;
    } finally {
      testing = false;
    }
  }

  async function revoke(): Promise<void> {
    if (revoking) return;
    revoking = true;
    error = '';
    try {
      await api('/api/v1/security/access/sessions/revoke', { method: 'POST' });
      revokeOpen = false;
      toast.success(t().revoked);
    } catch (cause) {
      revokeOpen = false;
      error = t().revokeFailed(errorMessage(cause));
    } finally {
      revoking = false;
    }
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="access-provider-title">
  <form class="contents" onsubmit={save}>
    <Card.Header>
      <Card.Title id="access-provider-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="access-provider-tip" label={t().title} description={t().tip} /></Card.Title>
      {#if draft}
        <Card.Action class="flex items-center gap-2">
          <Field.Label for="access-provider-enabled" class="text-muted-foreground">{t().enabled}</Field.Label>
          <Switch id="access-provider-enabled" bind:checked={draft.enabled} />
        </Card.Action>
      {/if}
    </Card.Header>
    <Card.Content class="grid gap-4">
      {#if loadError}
        <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
      {:else if !draft || !saved}
        <Skeleton class="h-64 w-full" />
      {:else}
        {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
        <Field.Group class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Field.Field>
            <Field.Label for="access-provider-issuer">{t().issuer}</Field.Label>
            <ButtonGroup.Root class="w-full">
              <Input id="access-provider-issuer" inputmode="url" bind:value={draft.issuer} oninput={() => { issuerError = ''; }} autocomplete="off" autocapitalize="off" spellcheck={false} placeholder="https://accounts.google.com" aria-invalid={issuerError ? true : undefined} aria-describedby={issuerError ? 'access-provider-issuer-error' : undefined} class="font-mono" />
              <Button variant="outline" disabled={testing || !draft.issuer.trim()} onclick={testIssuer}>{#if testing}<Spinner aria-hidden="true" />{:else}<PlugConnected aria-hidden="true" />{/if}{t().test}</Button>
            </ButtonGroup.Root>
            {#if issuerError}<Field.Error id="access-provider-issuer-error">{issuerError}</Field.Error>{/if}
          </Field.Field>
          <Field.Field>
            <Field.Label for="access-provider-client-id">{t().clientId}</Field.Label>
            <Input id="access-provider-client-id" bind:value={draft.client_id} autocomplete="off" autocapitalize="off" spellcheck={false} class="font-mono" />
          </Field.Field>
          <Field.Field>
            <Field.Label for="access-provider-client-secret">{t().clientSecret}</Field.Label>
            <Input id="access-provider-client-secret" type="password" bind:value={draft.client_secret} autocomplete="off" spellcheck={false} placeholder={saved.client_secret_set ? (saved.client_secret_hint ? t().secretKeep(saved.client_secret_hint) : t().secretKeepShort) : ''} />
          </Field.Field>
          <Field.Field class="md:col-span-2">
            <Field.Label id="access-provider-redirect-label">{t().redirectUri}</Field.Label>
            <div class="flex min-h-8 min-w-0 items-center text-xs" role="group" aria-labelledby="access-provider-redirect-label">
              {#if saved.available && saved.redirect_uri}
                <CopyText value={saved.redirect_uri} />
              {:else}
                <span class="text-muted-foreground">{t().redirectPending}</span>
              {/if}
            </div>
            {#if saved.available && saved.redirect_uri}<Field.Description>{t().redirectHint}</Field.Description>{/if}
          </Field.Field>
          <!-- 범위와 그룹 클레임은 기본값(openid email profile, groups)을 그대로 보내며 화면에 두지 않습니다. -->
          <Field.Field>
            <Field.Label for="access-provider-session-hours">{t().sessionHours}</Field.Label>
            <InputGroup.Root>
              <InputGroup.Input id="access-provider-session-hours" type="number" inputmode="numeric" min="1" max="720" step="1" required bind:value={draft.session_hours} class="tabular-nums" />
              <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().hours}</InputGroup.Text></InputGroup.Addon>
            </InputGroup.Root>
            <Field.Description>{t().sessionHoursHint}</Field.Description>
          </Field.Field>
        </Field.Group>
        <Field.Field orientation="horizontal">
          <Field.Content>
            <Field.Title>{t().sessions}<InfoTooltip id="access-provider-sessions-tip" label={t().sessions} description={t().sessionsTip} /></Field.Title>
          </Field.Content>
          <Button variant="outline" class="text-destructive hover:text-destructive" disabled={revoking} onclick={() => { revokeOpen = true; }}><ArrowExit aria-hidden="true" />{t().revoke}</Button>
        </Field.Field>
      {/if}
    </Card.Content>
    {#if dirty || busy}
      <Card.Footer class="justify-end gap-2">
        <Button variant="outline" disabled={busy} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
      </Card.Footer>
    {/if}
  </form>
</Card.Root>

<AlertDialog.Root bind:open={() => revokeOpen, (open) => { if (!revoking) revokeOpen = open; }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{t().revokeTitle}</AlertDialog.Title>
      <AlertDialog.Description>{t().revokeDescription}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={revoking}>{t().cancel}</AlertDialog.Cancel>
      <Button variant="destructive" disabled={revoking} onclick={revoke}>{#if revoking}<Spinner aria-hidden="true" />{/if}{t().revoke}</Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
