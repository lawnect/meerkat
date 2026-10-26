<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { ArrowUndo, PlugConnected, Save, Warning } from '../icons';
  import type { SsoSettings } from '../types';
  import CopyText from './CopyText.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 설정 > 보안의 SSO(OpenID Connect) 카드. 고친 값은 되돌리기·저장으로만 반영합니다.
  const t = translations({
    title: 'SSO',
    tip: 'Sign in to the admin console with an OpenID Connect provider such as Google Workspace, Entra ID, Okta or Keycloak. Sign-ins through SSO skip the Telegram code; use the provider\'s multi-factor authentication.',
    fromEnvironment: 'Set by environment',
    overridesEnvironment: 'Overrides environment',
    revertToEnvironment: 'Revert to environment',
    reverted: 'Reverted to the environment settings',
    enabled: 'Use SSO',
    label: 'Provider name',
    labelHint: 'Shown on the sign-in button.',
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
    scopes: 'Scopes',
    allowedUsers: 'Allowed users',
    allowedUsersHint: 'Email addresses or @domain.',
    allowedGroups: 'Allowed groups',
    allowedGroupsHint: 'With allowed users also set, both must match.',
    groupsClaim: 'Groups claim',
    passwordLogin: 'Password sign-in',
    passwordLoginHint: 'When off, public addresses accept only SSO. The local admin port over SSH still accepts passwords.',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values: turning SSO on needs an https issuer URL, a client ID, and at least one allowed user or group.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load SSO settings: ${code}`,
  }, {
    title: 'SSO',
    tip: 'Google Workspace, Entra ID, Okta, Keycloak 같은 OpenID Connect 공급자로 관리 콘솔에 로그인합니다. SSO 로그인에는 텔레그램 코드를 묻지 않으니 공급자의 다단계 인증을 사용하세요.',
    fromEnvironment: '환경 변수로 설정됨',
    overridesEnvironment: '환경 변수 덮어씀',
    revertToEnvironment: '환경 변수 값으로 되돌리기',
    reverted: '환경 변수 값으로 되돌렸습니다',
    enabled: 'SSO 사용',
    label: '공급자 이름',
    labelHint: '로그인 버튼에 표시됩니다.',
    issuer: '발급자 URL',
    test: '연결 확인',
    providerFound: '공급자를 확인했습니다',
    providerUnreachable: '공급자에 연결하지 못했습니다.',
    issuerInvalid: 'https URL을 입력하세요.',
    clientId: '클라이언트 ID',
    clientSecret: '클라이언트 시크릿',
    secretKeep: (hint: string) => `저장됨(…${hint}). 바꾸려면 새 시크릿을 입력하세요.`,
    secretKeepShort: '저장됨. 바꾸려면 새 시크릿을 입력하세요.',
    redirectUri: '리디렉션 URI',
    redirectHint: '공급자에 이 URI를 등록하세요.',
    redirectPending: '도메인을 등록하면 표시됩니다.',
    scopes: '범위',
    allowedUsers: '허용 사용자',
    allowedUsersHint: '이메일 주소 또는 @도메인',
    allowedGroups: '허용 그룹',
    allowedGroupsHint: '허용 사용자와 함께 쓰면 둘 다 충족해야 합니다.',
    groupsClaim: '그룹 클레임',
    passwordLogin: '비밀번호 로그인',
    passwordLoginHint: '끄면 공개 주소에서는 SSO로만 로그인합니다. SSH로 연결한 로컬 관리 포트는 예외입니다.',
    revert: '되돌리기',
    save: '저장',
    saving: '저장 중…',
    saved: '저장했습니다',
    invalid: '값을 확인하세요. SSO를 사용하려면 https 발급자 URL, 클라이언트 ID, 허용 사용자나 그룹이 하나 이상 필요합니다.',
    failed: (code: string) => `저장하지 못했습니다: ${code}`,
    loadFailed: (code: string) => `SSO 설정을 불러오지 못했습니다: ${code}`,
  });

  // 서버 값과 입력 모양이 다른 칸은 범위(공백으로 구분한 한 줄)와 클라이언트 시크릿(입력했을 때만 보냄)입니다.
  interface Draft {
    enabled: boolean;
    label: string;
    issuer: string;
    client_id: string;
    client_secret: string;
    scopes: string;
    groups_claim: string;
    allowed_users: string[];
    allowed_groups: string[];
    password_login: boolean;
  }

  const toDraft = (view: SsoSettings): Draft => ({
    enabled: view.enabled,
    label: view.label,
    issuer: view.issuer,
    client_id: view.client_id,
    client_secret: '',
    scopes: view.scopes.join(' '),
    groups_claim: view.groups_claim,
    allowed_users: [...view.allowed_users],
    allowed_groups: [...view.allowed_groups],
    password_login: view.password_login,
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
    allowed_users: [...value.allowed_users],
    allowed_groups: [...value.allowed_groups],
    password_login: value.password_login,
  });

  let saved = $state<SsoSettings | null>(null);
  let draft = $state<Draft | null>(null);
  let loadError = $state('');
  let error = $state('');
  let issuerError = $state('');
  let busy = $state(false);
  let testing = $state(false);

  const adopt = (next: SsoSettings) => { saved = next; draft = toDraft(next); };
  const dirty = $derived(Boolean(saved && draft) && JSON.stringify(toBody($state.snapshot(draft!))) !== JSON.stringify(toBody(toDraft(saved!))));

  onMount(() => {
    api<SsoSettings>('/api/v1/security/sso').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!draft || busy || !dirty) return;
    busy = true;
    error = '';
    try {
      adopt(await api<SsoSettings>('/api/v1/security/sso', { method: 'PUT', body: JSON.stringify(toBody($state.snapshot(draft))) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_sso' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  // 관리 화면에서 저장한 값을 지워 환경 변수 값을 다시 씁니다.
  async function resetToEnvironment(): Promise<void> {
    if (busy) return;
    busy = true;
    error = '';
    try {
      adopt(await api<SsoSettings>('/api/v1/security/sso', { method: 'DELETE' }));
      toast.success(t().reverted);
    } catch (cause) {
      error = t().failed(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (saved) draft = toDraft($state.snapshot(saved) as SsoSettings);
    error = '';
    issuerError = '';
  }

  async function testIssuer(): Promise<void> {
    if (!draft || testing) return;
    testing = true;
    issuerError = '';
    try {
      await api<{ authorization_endpoint: string }>('/api/v1/security/sso/test', { method: 'POST', body: JSON.stringify({ issuer: draft.issuer.trim() }) });
      toast.success(t().providerFound);
    } catch (cause) {
      issuerError = errorMessage(cause) === 'invalid_issuer' ? t().issuerInvalid : t().providerUnreachable;
    } finally {
      testing = false;
    }
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="sso-title">
  <form class="contents" onsubmit={save}>
    <Card.Header>
      <Card.Title id="sso-title" class="flex items-center gap-2">
        <span class="flex items-center gap-1">{t().title}<InfoTooltip id="sso-tip" label={t().title} description={t().tip} /></span>
        {#if saved?.source === 'environment'}
          <Badge variant="outline" class="select-none">{t().fromEnvironment}</Badge>
        {:else if saved?.source === 'database' && saved.environment}
          <Badge variant="outline" class="select-none">{t().overridesEnvironment}</Badge>
        {/if}
      </Card.Title>
      {#if draft}
        <Card.Action class="flex items-center gap-2">
          {#if saved?.source === 'database' && saved.environment}
            <Button variant="outline" size="sm" disabled={busy} onclick={resetToEnvironment}><ArrowUndo aria-hidden="true" />{t().revertToEnvironment}</Button>
          {/if}
          <Field.Label for="sso-enabled" class="text-muted-foreground">{t().enabled}</Field.Label>
          <Switch id="sso-enabled" bind:checked={draft.enabled} />
        </Card.Action>
      {/if}
    </Card.Header>
    <Card.Content class="grid gap-4">
      {#if loadError}
        <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
      {:else if !draft || !saved}
        <Skeleton class="h-72 w-full" />
      {:else}
        {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
        <Field.Group class="grid grid-cols-1 gap-4 md:grid-cols-2">
          <Field.Field>
            <Field.Label for="sso-label">{t().label}</Field.Label>
            <Input id="sso-label" bind:value={draft.label} maxlength={32} autocomplete="off" placeholder="Google" />
            <Field.Description>{t().labelHint}</Field.Description>
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-issuer">{t().issuer}</Field.Label>
            <ButtonGroup.Root class="w-full">
              <Input id="sso-issuer" inputmode="url" bind:value={draft.issuer} oninput={() => { issuerError = ''; }} autocomplete="off" autocapitalize="off" spellcheck={false} placeholder="https://accounts.google.com" aria-invalid={issuerError ? true : undefined} aria-describedby={issuerError ? 'sso-issuer-error' : undefined} class="font-mono" />
              <Button variant="outline" disabled={testing || !draft.issuer.trim()} onclick={testIssuer}>{#if testing}<Spinner aria-hidden="true" />{:else}<PlugConnected aria-hidden="true" />{/if}{t().test}</Button>
            </ButtonGroup.Root>
            {#if issuerError}<Field.Error id="sso-issuer-error">{issuerError}</Field.Error>{/if}
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-client-id">{t().clientId}</Field.Label>
            <Input id="sso-client-id" bind:value={draft.client_id} autocomplete="off" autocapitalize="off" spellcheck={false} class="font-mono" />
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-client-secret">{t().clientSecret}</Field.Label>
            <Input id="sso-client-secret" type="password" bind:value={draft.client_secret} autocomplete="off" spellcheck={false} placeholder={saved.client_secret_set ? (saved.client_secret_hint ? t().secretKeep(saved.client_secret_hint) : t().secretKeepShort) : ''} />
          </Field.Field>
          <Field.Field class="md:col-span-2">
            <Field.Label id="sso-redirect-label">{t().redirectUri}</Field.Label>
            <div class="flex min-h-8 min-w-0 items-center text-xs" role="group" aria-labelledby="sso-redirect-label">
              {#if saved.available && saved.redirect_uri}
                <CopyText value={saved.redirect_uri} />
              {:else}
                <span class="text-muted-foreground">{t().redirectPending}</span>
              {/if}
            </div>
            {#if saved.available && saved.redirect_uri}<Field.Description>{t().redirectHint}</Field.Description>{/if}
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-scopes">{t().scopes}</Field.Label>
            <Input id="sso-scopes" bind:value={draft.scopes} autocomplete="off" autocapitalize="off" spellcheck={false} placeholder="openid email profile" class="font-mono" />
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-groups-claim">{t().groupsClaim}</Field.Label>
            <Input id="sso-groups-claim" bind:value={draft.groups_claim} autocomplete="off" autocapitalize="off" spellcheck={false} placeholder="groups" class="font-mono" />
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-users">{t().allowedUsers}</Field.Label>
            <TagInput id="sso-users" ariaLabel={t().allowedUsers} bind:values={draft.allowed_users} lowercase mono placeholder="admin@example.com, @example.com" />
            <Field.Description>{t().allowedUsersHint}</Field.Description>
          </Field.Field>
          <Field.Field>
            <Field.Label for="sso-groups">{t().allowedGroups}</Field.Label>
            <TagInput id="sso-groups" ariaLabel={t().allowedGroups} bind:values={draft.allowed_groups} mono placeholder="meerkat-admins" />
            <Field.Description>{t().allowedGroupsHint}</Field.Description>
          </Field.Field>
        </Field.Group>
        <Field.Field orientation="horizontal">
          <Field.Content>
            <Field.Label for="sso-password-login">{t().passwordLogin}</Field.Label>
            <Field.Description>{t().passwordLoginHint}</Field.Description>
          </Field.Content>
          <Switch id="sso-password-login" bind:checked={draft.password_login} disabled={!draft.enabled} />
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
