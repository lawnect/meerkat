<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { translations } from '../i18n.svelte';
  import { Warning } from '../icons';
  import CopyText from './CopyText.svelte';

  // 삭제·토큰 교체처럼 되돌리기 어려운 작업을 대상 이름 입력으로 확인합니다.
  // 제목과 같은 말을 되풀이하는 설명·입력 안내는 두지 않고, 경고 한 줄과 확인용 이름 입력만 보여줍니다.
  let { id, open, title, warning, targetName, submitLabel, busy, error, onConfirm, onClose }: {
    id: string;
    open: boolean;
    title: string;
    warning: string;
    targetName: string;
    submitLabel: string;
    busy: boolean;
    error: string;
    onConfirm: () => void | Promise<void>;
    onClose: () => void;
  } = $props();

  const t = translations({ cancel: 'Cancel', working: 'Working…', confirm: 'Type to confirm' }, { cancel: '취소', working: '처리 중…', confirm: '확인을 위해 입력' });

  let enteredName = $state('');
  $effect(() => { if (!open) enteredName = ''; });

  function submit(event: SubmitEvent): void {
    event.preventDefault();
    if (busy || !targetName || enteredName !== targetName) return;
    void onConfirm();
  }
</script>

<AlertDialog.Root {open} onOpenChange={(next) => { if (!next && !busy) onClose(); }}>
  <AlertDialog.Content class="sm:max-w-md">
    <form class="grid gap-4" onsubmit={submit}>
      <AlertDialog.Header>
        <AlertDialog.Title>{title}</AlertDialog.Title>
        <AlertDialog.Description class="sr-only">{warning}</AlertDialog.Description>
      </AlertDialog.Header>
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{warning}</Alert.Description></Alert.Root>
      <Field.Group>
        <Field.Field>
          <Field.Label for={`confirm-input-${id}`} class="flex-wrap gap-x-2">{t().confirm}: <CopyText value={targetName} class="font-semibold" /></Field.Label>
          <Input id={`confirm-input-${id}`} bind:value={enteredName} autocomplete="off" spellcheck={false} required />
        </Field.Field>
      </Field.Group>
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <AlertDialog.Footer>
        <Button variant="outline" disabled={busy} onclick={onClose}>{t().cancel}</Button>
        <Button type="submit" variant="destructive" disabled={busy || !targetName || enteredName !== targetName}>{#if busy}<Spinner />{t().working}{:else}{submitLabel}{/if}</Button>
      </AlertDialog.Footer>
    </form>
  </AlertDialog.Content>
</AlertDialog.Root>
