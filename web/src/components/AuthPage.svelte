<script lang="ts">
  import { api, errorMessage } from '../api';

  let { mode, onLogin, onSetup }: { mode: 'setup' | 'login'; onLogin: () => Promise<void>; onSetup: () => void } = $props();
  let code = $state('');
  let email = $state('');
  let password = $state('');
  let error = $state('');
  let busy = $state(false);

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    error = '';
    busy = true;
    try {
      if (mode === 'setup') {
        await api<{ status: string }>('/api/v1/setup/admin', {
          method: 'POST',
          headers: { Authorization: `Bearer ${code.trim()}` },
          body: JSON.stringify({ email, password }),
        });
        onSetup();
      } else {
        await api<{ admin: { email: string }; expires_at: string }>('/api/v1/session', {
          method: 'POST',
          body: JSON.stringify({ email, password }),
        });
        await onLogin();
      }
    } catch (cause) {
      const code = errorMessage(cause);
      if (mode === 'setup' && code === 'invalid_setup_credential') {
        error = '설정 코드, 이메일 또는 비밀번호를 확인하세요. 비밀번호는 12자 이상이어야 합니다.';
      } else if (mode === 'setup') {
        error = `설정에 실패했습니다: ${code}`;
      } else {
        error = '이메일 또는 비밀번호가 올바르지 않습니다.';
      }
    } finally {
      busy = false;
    }
  }
</script>

<main class="auth-wrap">
  <section class="auth">
    <div class="auth-brand">MEERKAT / OUTLET</div>
    <h1>{mode === 'setup' ? '처음 한 번만 설정하세요' : '로그인'}</h1>
    <p class="auth-intro">{mode === 'setup' ? '이 서버의 관리자가 사용할 계정을 만듭니다. 설정 코드는 서버에서만 확인할 수 있습니다.' : 'Meerkat Outlet 운영 콘솔에 접속합니다.'}</p>
    <form onsubmit={submit}>
      {#if mode === 'setup'}
        <div class="field">
          <label for="setup-code">설정 코드</label>
          <input class="input input-bordered" id="setup-code" bind:value={code} placeholder="서버의 admin.token 값" autocomplete="off" required />
        </div>
      {/if}
      <div class="field">
        <label for="email">이메일</label>
        <input class="input input-bordered" id="email" bind:value={email} type="email" placeholder="you@example.com" autocomplete="username" required />
      </div>
      <div class="field">
        <label for="password">비밀번호</label>
        <input class="input input-bordered" id="password" bind:value={password} type="password" placeholder={mode === 'setup' ? '12자 이상' : '비밀번호'} autocomplete={mode === 'setup' ? 'new-password' : 'current-password'} minlength={mode === 'setup' ? 12 : undefined} required />
      </div>
      <div class="form-actions">
        <button class="btn btn-primary" type="submit" disabled={busy}>{busy ? '처리 중…' : mode === 'setup' ? '관리자 만들기' : '로그인'}</button>
      </div>
      {#if error}<p class="error" role="alert">{error}</p>{/if}
    </form>
  </section>
</main>

<style>
  :global(.auth-wrap) { display: grid; min-height: 100vh; place-items: center; padding: 24px; background: var(--paper); }
  :global(.auth) { width: min(520px, 100%); border: 1px solid var(--line); padding: 34px; background: var(--panel); }
  :global(.auth-brand) { margin-bottom: 42px; font-size: 13px; font-weight: 800; letter-spacing: .1em; }
  :global(.auth h1) { margin: 0 0 8px; font-size: 27px; letter-spacing: -.04em; }
  :global(.auth-intro) { margin: 0 0 25px; color: var(--muted); }
  :global(.auth .field) { margin-top: 14px; }
</style>
