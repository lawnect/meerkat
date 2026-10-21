<script lang="ts">
  import { onDestroy } from 'svelte';
  import Button from './Button.svelte';

  let { id, open, title, description, warning, targetLabel, targetName, placeholder, submitLabel, busy, error, onConfirm, onClose }: {
    id: string;
    open: boolean;
    title: string;
    description: string;
    warning: string;
    targetLabel: string;
    targetName: string;
    placeholder: string;
    submitLabel: string;
    busy: boolean;
    error: string;
    onConfirm: () => void | Promise<void>;
    onClose: () => void;
  } = $props();

  let dialog = $state<HTMLDialogElement>();
  let enteredName = $state('');
  let copied = $state(false);
  let copyError = $state('');
  let copyTimer: ReturnType<typeof setTimeout> | undefined;

  $effect(() => {
    if (!dialog) return;
    if (open && !dialog.open) dialog.showModal();
    else if (!open && dialog.open) dialog.close();
  });

  onDestroy(() => { if (copyTimer) clearTimeout(copyTimer); });

  async function copyName(): Promise<void> {
    try {
      await navigator.clipboard.writeText(targetName);
      copied = true;
      copyError = '';
      if (copyTimer) clearTimeout(copyTimer);
      copyTimer = setTimeout(() => { copied = false; }, 2500);
    } catch {
      copied = false;
      copyError = '이름을 복사하지 못했습니다. 직접 선택해 복사하세요.';
    }
  }

  function close(): void {
    if (!busy) onClose();
  }

  function submit(event: SubmitEvent): void {
    event.preventDefault();
    if (busy || !targetName || enteredName !== targetName) return;
    void onConfirm();
  }
</script>

<dialog bind:this={dialog} class="confirm-dialog" aria-labelledby={`confirm-title-${id}`} aria-describedby={`confirm-description-${id} confirm-warning-${id}`} oncancel={(event) => { if (busy) event.preventDefault(); }} onclose={() => { enteredName = ''; copied = false; copyError = ''; onClose(); }}>
  <form onsubmit={submit}>
    <h2 id={`confirm-title-${id}`}>{title}</h2>
    <p id={`confirm-description-${id}`}>{description}</p>
    <div id={`confirm-warning-${id}`} class="confirm-warning" role="note">{warning}</div>
    <div class="field">
      <div class="confirm-target">
        <label for={`confirm-input-${id}`}>{targetLabel}:</label>
        <strong title={targetName}>{targetName}</strong>
        <Button variant="plain" size="sm" class={copied ? 'confirm-copy copied' : 'confirm-copy'} aria-label={copied ? '이름 복사 완료' : '이름 복사'} title={copied ? '복사 완료' : '이름 복사'} disabled={busy} onclick={copyName}>
          {#if copied}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="m5 12 4 4L19 6" /></svg>{:else}<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><rect x="8" y="8" width="12" height="12" rx="2" /><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2" /></svg>{/if}
        </Button>
      </div>
      <input class="input input-bordered" id={`confirm-input-${id}`} bind:value={enteredName} {placeholder} autocomplete="off" spellcheck="false" required />
    </div>
    {#if copyError}<p class="error" role="alert">{copyError}</p>{/if}
    {#if error}<p class="error" role="alert">{error}</p>{/if}
    <div class="confirm-actions"><Button variant="secondary" disabled={busy} onclick={close}>취소</Button><Button variant="danger" type="submit" disabled={busy || !targetName || enteredName !== targetName}>{busy ? '처리 중…' : submitLabel}</Button></div>
  </form>
</dialog>

<style>
  .confirm-dialog { width: min(440px, calc(100vw - 32px)); max-width: none; margin: auto; border: 1px solid var(--line); border-radius: 10px; padding: 22px; background: var(--panel); color: var(--ink); box-shadow: 0 22px 70px rgba(48, 36, 28, .18); }
  .confirm-dialog::backdrop { background: rgba(35, 30, 26, .38); -webkit-backdrop-filter: blur(3px); backdrop-filter: blur(3px); }
  .confirm-dialog h2 { margin: 0 0 4px; font-size: 20px; letter-spacing: -.025em; }
  .confirm-dialog > form > p:not(.error) { margin: 0 0 16px; color: var(--muted); font-size: 12px; }
  .confirm-warning { margin: 0 0 18px; padding: 12px 14px; border-radius: 8px; background: #fff8e7; color: #a65112; font-size: 12px; line-height: 1.6; }
  .confirm-target { display: flex; align-items: center; gap: 8px; min-width: 0; }
  .confirm-target label { flex: none; white-space: nowrap; }
  .confirm-target strong { min-width: 0; overflow: hidden; white-space: nowrap; text-overflow: ellipsis; }
  .confirm-target :global(.confirm-copy) { display: grid; flex: none; place-items: center; width: 28px; height: 28px; border: 1px solid var(--line); border-radius: 5px; color: var(--muted); }
  .confirm-target :global(.confirm-copy svg) { width: 14px; height: 14px; }
  .confirm-target :global(.confirm-copy.copied) { border-color: var(--green); color: var(--green); }
  .confirm-actions { display: flex; justify-content: flex-end; gap: 8px; margin-top: 18px; }
</style>
