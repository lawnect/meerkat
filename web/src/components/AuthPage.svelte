<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import * as InputOTP from '$lib/components/ui/input-otp/index.js';
  import { REGEXP_ONLY_DIGITS } from 'bits-ui';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { ArrowEnter, ArrowLeft, CheckmarkCircle, Key, LockClosed, Mail, PersonAdd, ShieldKeyhole, Warning } from '../icons';
  import { api, errorMessage } from '../api';
  import { pageHref } from '../navigation';
  import { localizePath, translations } from '../i18n.svelte';
  import type { SignInOptions } from '../types';
  import { syncUrlState } from '../urlState.svelte';
  import BrandMark from './BrandMark.svelte';
  import LocaleSwitch from './LocaleSwitch.svelte';
  import ThemeToggle from './ThemeToggle.svelte';

  const t = translations({
    setupTitle: 'First-time setup',
    loginTitle: 'Sign in',
    setupIntro: 'Create the administrator account for this server.',
    setupCode: 'Setup code',
    setupCodePlaceholder: 'The admin.token value on the server',
    email: 'Email',
    emailPlaceholder: 'you@example.com',
    password: 'Password',
    passwordHint: 'At least 12 characters',
    passwordPlaceholder: 'Password',
    busy: 'Working…',
    createAdmin: 'Create administrator',
    submitLogin: 'Sign in',
    invalidSetup: 'Check the setup code, email, and password. The password must be at least 12 characters.',
    setupFailed: (code: string) => `Setup failed: ${code}`,
    loginFailed: 'Sign-in failed.',
    rateLimited: 'Too many sign-in attempts. Try again in a few minutes.',
    codeTitle: 'Enter the code',
    codeIntro: 'We sent a 6-digit code to Telegram.',
    codeLabel: 'Verification code',
    verify: 'Verify',
    backToLogin: 'Sign in again',
    codeInvalid: 'The code is wrong or has expired.',
    codeLocked: 'Too many attempts. Sign in again to get a new code.',
    telegramUnreachable: 'Couldn\'t send the code to Telegram. Sign in over the SSH-forwarded local admin port instead.',
    ssoSignIn: 'Sign in with SSO',
    or: 'or',
    ssoUnavailable: 'SSO sign-in is unavailable.',
    ssoProviderUnreachable: 'Couldn\'t reach the SSO provider.',
    ssoInvalidState: 'The sign-in request expired. Try again.',
    ssoProviderDenied: 'The SSO provider declined the sign-in.',
    ssoTokenFailed: 'Couldn\'t verify the SSO sign-in.',
    ssoNotAllowed: 'This account isn\'t allowed to sign in.',
    ssoBusy: 'Try again in a moment.',
    passwordLoginDisabled: 'Password sign-in is disabled. Sign in with SSO.',
    recoverLink: 'Recover admin access',
    recoverTitle: 'Recover admin access',
    recoverIntro: 'Set an administrator\'s password with the server\'s admin.token. Available on the local admin port only.',
    newPasswordPlaceholder: 'New password',
    setPassword: 'Set password',
    recovered: 'Password set. Sign in with the new password.',
    recoverFailed: (code: string) => `Recovery failed: ${code}`,
  }, {
    setupTitle: '초기 설정',
    loginTitle: '로그인',
    setupIntro: '이 서버의 관리자 계정을 생성합니다.',
    setupCode: '설정 코드',
    setupCodePlaceholder: '서버의 admin.token 값',
    email: '이메일',
    emailPlaceholder: 'you@example.com',
    password: '비밀번호',
    passwordHint: '12자 이상',
    passwordPlaceholder: '비밀번호',
    busy: '처리 중…',
    createAdmin: '관리자 생성',
    submitLogin: '로그인',
    invalidSetup: '설정 코드, 이메일 또는 비밀번호가 올바르지 않습니다. 비밀번호는 12자 이상이어야 합니다.',
    setupFailed: (code: string) => `초기 설정 중 오류가 발생했습니다: ${code}`,
    loginFailed: '로그인에 실패했습니다.',
    rateLimited: '로그인 시도 횟수를 초과했습니다. 잠시 후 다시 시도하십시오.',
    codeTitle: '인증 코드 입력',
    codeIntro: 'Telegram으로 6자리 인증 코드가 전송되었습니다.',
    codeLabel: '인증 코드',
    verify: '확인',
    backToLogin: '로그인 화면으로 이동',
    codeInvalid: '인증 코드가 올바르지 않거나 만료되었습니다.',
    codeLocked: '입력 시도 횟수를 초과했습니다. 다시 로그인하여 새 인증 코드를 받으십시오.',
    telegramUnreachable: 'Telegram으로 인증 코드를 전송할 수 없습니다. SSH로 연결한 로컬 관리 포트에서 로그인하십시오.',
    ssoSignIn: 'SSO로 로그인',
    or: '또는',
    ssoUnavailable: 'SSO 로그인을 사용할 수 없습니다.',
    ssoProviderUnreachable: 'SSO 공급자에 연결할 수 없습니다.',
    ssoInvalidState: '로그인 요청이 만료되었습니다. 다시 시도하십시오.',
    ssoProviderDenied: 'SSO 공급자에서 로그인이 거부되었습니다.',
    ssoTokenFailed: 'SSO 로그인 확인 중 오류가 발생했습니다.',
    ssoNotAllowed: '이 계정에는 접근 권한이 없습니다.',
    ssoBusy: '잠시 후 다시 시도하십시오.',
    passwordLoginDisabled: '비밀번호 로그인이 비활성화되어 있습니다. SSO로 로그인하십시오.',
    recoverLink: '관리자 계정 복구',
    recoverTitle: '관리자 계정 복구',
    recoverIntro: '서버의 admin.token으로 관리자 비밀번호를 설정합니다. 로컬 관리 포트에서만 사용할 수 있습니다.',
    newPasswordPlaceholder: '새 비밀번호',
    setPassword: '비밀번호 설정',
    recovered: '비밀번호가 설정되었습니다. 새 비밀번호로 로그인하십시오.',
    recoverFailed: (code: string) => `계정 복구 중 오류가 발생했습니다: ${code}`,
  });

  let { mode, signIn = { password: true, sso: false }, onLogin, onSetup }: { mode: 'setup' | 'login'; signIn?: SignInOptions; onLogin: () => Promise<void>; onSetup: () => void } = $props();
  let code = $state('');
  let email = $state('');
  let password = $state('');
  // 로컬 관리 포트에서 admin.token으로 관리자 비밀번호를 설정하는 복구 양식입니다.
  let recovering = $state(false);
  let notice = $state('');
  // 설정 코드(admin.token)를 묻는 양식: 초기 설정과 계정 복구.
  const askCode = $derived(mode === 'setup' || recovering);
  type FailureKind = 'invalidSetup' | 'setupFailed' | 'recoverFailed' | 'loginFailed' | 'rateLimited' | 'codeInvalid' | 'codeLocked' | 'telegramUnreachable'
    | 'ssoUnavailable' | 'ssoProviderUnreachable' | 'ssoInvalidState' | 'ssoProviderDenied' | 'ssoTokenFailed' | 'ssoNotAllowed' | 'ssoBusy' | 'passwordLoginDisabled';
  let failure = $state<{ kind: FailureKind; code: string } | null>(null);
  // 2단계 인증을 켜 둔 Outlet은 비밀번호 확인 뒤 텔레그램 코드를 한 번 더 받습니다.
  let challenge = $state('');
  let otp = $state('');
  let busy = $state(false);
  const error = $derived(!failure ? '' : failure.kind === 'setupFailed' ? t().setupFailed(failure.code) : failure.kind === 'recoverFailed' ? t().recoverFailed(failure.code) : t()[failure.kind]);

  // SSO는 주소 이동으로 시작하고, 돌아올 때는 언어별 앱 루트('/' 또는 '/ko/')로 옵니다. 실패하면 ?sso_error=코드가 붙습니다.
  const ssoHref = $derived(`/api/v1/session/sso/start?return=${encodeURIComponent(localizePath('/').replace(/\/?$/, '/'))}`);
  const ssoErrors: Record<string, FailureKind> = {
    sso_unavailable: 'ssoUnavailable',
    provider_unreachable: 'ssoProviderUnreachable',
    invalid_state: 'ssoInvalidState',
    provider_denied: 'ssoProviderDenied',
    token_failed: 'ssoTokenFailed',
    not_allowed: 'ssoNotAllowed',
    sso_busy: 'ssoBusy',
  };
  const showPassword = $derived(askCode || signIn.password || !signIn.sso);
  const ssoFirst = $derived(mode === 'login' && signIn.sso && !recovering);

  onMount(() => {
    if (mode !== 'login') return;
    const params = new URLSearchParams(window.location.search);
    const code = params.get('sso_error');
    if (code === null) return;
    params.delete('sso_error');
    const query = params.toString();
    window.history.replaceState(window.history.state, '', `${window.location.pathname}${query ? `?${query}` : ''}${window.location.hash}`);
    syncUrlState();
    failure = { kind: ssoErrors[code] ?? 'loginFailed', code };
  });

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    failure = null;
    notice = '';
    busy = true;
    try {
      if (recovering) {
        await api<{ status: string }>('/api/v1/setup/recover', {
          method: 'POST',
          headers: { Authorization: `Bearer ${code.trim()}` },
          body: JSON.stringify({ email, password }),
        });
        recovering = false;
        code = '';
        password = '';
        notice = t().recovered;
      } else if (mode === 'setup') {
        await api<{ status: string }>('/api/v1/setup/admin', {
          method: 'POST',
          headers: { Authorization: `Bearer ${code.trim()}` },
          body: JSON.stringify({ email, password }),
        });
        onSetup();
      } else {
        const response = await api<{ second_factor?: 'telegram'; challenge?: string }>('/api/v1/session', {
          method: 'POST',
          body: JSON.stringify({ email, password }),
        });
        if (response.second_factor && response.challenge) {
          challenge = response.challenge;
          otp = '';
          return;
        }
        await onLogin();
      }
    } catch (cause) {
      const reason = errorMessage(cause);
      if (askCode && reason === 'invalid_setup_credential') {
        failure = { kind: 'invalidSetup', code: reason };
      } else if (askCode && reason === 'rate_limited') {
        failure = { kind: 'rateLimited', code: reason };
      } else if (recovering) {
        failure = { kind: 'recoverFailed', code: reason };
      } else if (mode === 'setup' && reason === 'invalid_setup_credential') {
        failure = { kind: 'invalidSetup', code: reason };
      } else if (mode === 'setup') {
        failure = { kind: 'setupFailed', code: reason };
      } else if (reason === 'rate_limited') {
        failure = { kind: 'rateLimited', code: reason };
      } else if (reason === 'telegram_unreachable') {
        failure = { kind: 'telegramUnreachable', code: reason };
      } else if (reason === 'password_login_disabled') {
        failure = { kind: 'passwordLoginDisabled', code: reason };
      } else {
        failure = { kind: 'loginFailed', code: reason };
      }
    } finally {
      busy = false;
    }
  }

  async function verify(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy || otp.length !== 6) return;
    failure = null;
    busy = true;
    try {
      await api('/api/v1/session/second-factor', { method: 'POST', body: JSON.stringify({ challenge, code: otp }) });
      await onLogin();
    } catch (cause) {
      const reason = errorMessage(cause);
      otp = '';
      if (reason === 'too_many_attempts') {
        challenge = '';
        failure = { kind: 'codeLocked', code: reason };
      } else if (reason === 'rate_limited') {
        failure = { kind: 'rateLimited', code: reason };
      } else {
        failure = { kind: 'codeInvalid', code: reason };
      }
    } finally {
      busy = false;
    }
  }

  function toggleRecovery(next: boolean): void {
    recovering = next;
    code = '';
    password = '';
    failure = null;
    notice = '';
  }

  function restart(): void {
    challenge = '';
    otp = '';
    password = '';
    failure = null;
  }
</script>

{#snippet errorAlert()}
  {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{error}</Alert.Description></Alert.Root>{/if}
  {#if notice}<Alert.Root><CheckmarkCircle aria-hidden="true" /><Alert.Description class="break-keep">{notice}</Alert.Description></Alert.Root>{/if}
{/snippet}

<main class="relative flex min-h-svh flex-col items-center justify-center gap-6 bg-muted p-6 md:p-10">
  <div class="absolute end-3 top-3 flex items-center gap-0.5">
    <LocaleSwitch />
    <ThemeToggle />
  </div>
  <div class="flex w-full max-w-sm flex-col gap-6">
    <a href={pageHref('overview')} class="flex items-center gap-2 self-center text-sm font-semibold outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
      <BrandMark class="size-8 shrink-0" />
      <span>Meerkat <span class="font-medium text-muted-foreground">Outlet</span></span>
    </a>
    <Card.Root>
      <Card.Header class="text-center">
        <Card.Title class="text-xl" role="heading" aria-level={1}>{challenge ? t().codeTitle : recovering ? t().recoverTitle : mode === 'setup' ? t().setupTitle : t().loginTitle}</Card.Title>
        {#if challenge}<Card.Description>{t().codeIntro}</Card.Description>{:else if recovering}<Card.Description class="break-keep">{t().recoverIntro}</Card.Description>{:else if mode === 'setup'}<Card.Description>{t().setupIntro}</Card.Description>{/if}
      </Card.Header>
      <Card.Content>
        {#if challenge}
          <form onsubmit={verify}>
            <Field.Group>
              <Field.Field class="items-center">
                <Field.Label for="login-code" class="sr-only">{t().codeLabel}</Field.Label>
                <InputOTP.Root id="login-code" class="justify-center" maxlength={6} pattern={REGEXP_ONLY_DIGITS} bind:value={otp} autocomplete="one-time-code" inputmode="numeric">
                  {#snippet children({ cells })}
                    <InputOTP.Group>
                      {#each cells.slice(0, 3) as cell (cell)}
                        <InputOTP.Slot {cell} />
                      {/each}
                    </InputOTP.Group>
                    <InputOTP.Separator />
                    <InputOTP.Group>
                      {#each cells.slice(3, 6) as cell (cell)}
                        <InputOTP.Slot {cell} />
                      {/each}
                    </InputOTP.Group>
                  {/snippet}
                </InputOTP.Root>
              </Field.Field>
              {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{error}</Alert.Description></Alert.Root>{/if}
              <Field.Field>
                <Button type="submit" disabled={busy || otp.length !== 6}>{#if busy}<Spinner aria-hidden="true" />{t().busy}{:else}<ArrowEnter aria-hidden="true" />{t().verify}{/if}</Button>
                <Button variant="outline" disabled={busy} onclick={restart}><ArrowLeft aria-hidden="true" />{t().backToLogin}</Button>
              </Field.Field>
            </Field.Group>
          </form>
        {:else}
        <form onsubmit={submit}>
          <Field.Group>
            {#if ssoFirst}
              {@render errorAlert()}
              <Field.Field>
                <Button variant="outline" href={ssoHref}><ShieldKeyhole aria-hidden="true" />{t().ssoSignIn}</Button>
              </Field.Field>
              {#if signIn.password}<Field.Separator class="*:data-[slot=field-separator-content]:bg-card">{t().or}</Field.Separator>{/if}
            {/if}
            {#if showPassword}
              {#if askCode}
                <Field.Field>
                  <Field.Label for="setup-code" class="sr-only">{t().setupCode}</Field.Label>
                  <InputGroup.Root>
                    <InputGroup.Addon><Key aria-hidden="true" /></InputGroup.Addon>
                    <InputGroup.Input id="setup-code" bind:value={code} placeholder={t().setupCodePlaceholder} autocomplete="off" required />
                  </InputGroup.Root>
                </Field.Field>
              {/if}
              <Field.Field>
                <Field.Label for="email" class="sr-only">{t().email}</Field.Label>
                <InputGroup.Root>
                  <InputGroup.Addon><Mail aria-hidden="true" /></InputGroup.Addon>
                  <InputGroup.Input id="email" bind:value={email} type="email" placeholder={t().emailPlaceholder} autocomplete="username" required />
                </InputGroup.Root>
              </Field.Field>
              <Field.Field>
                <Field.Label for="password" class="sr-only">{t().password}</Field.Label>
                <InputGroup.Root>
                  <InputGroup.Addon><LockClosed aria-hidden="true" /></InputGroup.Addon>
                  <InputGroup.Input id="password" bind:value={password} type="password" placeholder={recovering ? t().newPasswordPlaceholder : t().passwordPlaceholder} autocomplete={askCode ? 'new-password' : 'current-password'} minlength={askCode ? 12 : undefined} aria-describedby={askCode ? 'password-hint' : undefined} required />
                </InputGroup.Root>
                {#if askCode}<Field.Description id="password-hint">{t().passwordHint}</Field.Description>{/if}
              </Field.Field>
              {#if !ssoFirst}{@render errorAlert()}{/if}
              <Field.Field>
                <Button type="submit" disabled={busy}>
                  {#if busy}
                    <Spinner aria-hidden="true" />{t().busy}
                  {:else if recovering}
                    <Key aria-hidden="true" />{t().setPassword}
                  {:else if mode === 'setup'}
                    <PersonAdd aria-hidden="true" />{t().createAdmin}
                  {:else}
                    <ArrowEnter aria-hidden="true" />{t().submitLogin}
                  {/if}
                </Button>
                {#if recovering}
                  <Button variant="outline" disabled={busy} onclick={() => toggleRecovery(false)}><ArrowLeft aria-hidden="true" />{t().backToLogin}</Button>
                {:else if mode === 'login' && signIn.recovery}
                  <Button variant="link" class="text-muted-foreground" disabled={busy} onclick={() => toggleRecovery(true)}>{t().recoverLink}</Button>
                {/if}
              </Field.Field>
            {/if}
          </Field.Group>
        </form>
        {/if}
      </Card.Content>
    </Card.Root>
  </div>
</main>
