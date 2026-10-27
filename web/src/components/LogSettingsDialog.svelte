<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { Warning } from '../icons';
  import type { LogSettings, LogView } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';

  // 요청 기록 하나의 용량 상한과 보관 기간을 정합니다. 기록을 켜고 끄는 스위치는 LogCard 머리글에 있고,
  // 저장하면 지금의 스위치 값과 함께 PUT /api/v1/observe/{kind}-log로 바로 서버에 반영합니다. 응답은 onSaved로 넘깁니다.
  let { open = $bindable(false), kind, settings, onSaved }: {
    open?: boolean;
    kind: 'access' | 'failure' | 'request';
    settings: LogSettings;
    onSaved: (view: LogView<unknown>) => void;
  } = $props();

  const t = translations({
    title: 'Log settings',
    maxSize: 'Size limit',
    maxSizeTip: 'The oldest entries are deleted once the file exceeds this size.',
    retention: 'Retention',
    retentionTip: 'Entries older than this are deleted.',
    mib: 'MiB',
    days: 'days',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values. The size limit is 16–4096 MiB and the retention is 1–365 days.',
    failed: (code: string) => `Couldn't save the log settings: ${code}`,
  }, {
    title: '기록 설정',
    maxSize: '용량 상한',
    maxSizeTip: '파일 크기가 상한을 초과하면 오래된 기록부터 삭제됩니다.',
    retention: '보관 기간',
    retentionTip: '보관 기간이 지난 기록은 삭제됩니다.',
    mib: 'MiB',
    days: '일',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '입력값이 올바르지 않습니다. 용량 상한은 16–4096MiB, 보관 기간은 1–365일이어야 합니다.',
    failed: (code: string) => `기록 설정 저장 중 오류가 발생했습니다: ${code}`,
  });

  let maxMib = $state<number | null>(256);
  let retentionDays = $state<number | null>(30);
  let busy = $state(false);
  let error = $state('');

  // 열릴 때만 서버 값으로 채웁니다.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      maxMib = settings.max_mib;
      retentionDays = settings.retention_days;
      error = '';
    }
    wasOpen = open;
  });

  async function save(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    busy = true;
    error = '';
    try {
      const view = await api<LogView<unknown>>(`/api/v1/observe/${kind}-log`, {
        method: 'PUT',
        body: JSON.stringify({ enabled: settings.enabled, max_mib: maxMib, retention_days: retentionDays }),
      });
      onSaved(view);
      toast.success(t().saved);
      open = false;
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_log_settings' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open={() => open, (next) => { if (!busy) open = next; }}>
  <Dialog.Content class="sm:max-w-sm">
    <form class="grid gap-4" onsubmit={save}>
      <Dialog.Header>
        <Dialog.Title>{t().title}</Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field>
          <div class="flex items-center gap-1"><Field.Label for={`${kind}-log-max-mib`}>{t().maxSize}</Field.Label><InfoTooltip id={`${kind}-log-max-mib-tip`} label={t().maxSize} description={t().maxSizeTip} /></div>
          <InputGroup.Root>
            <InputGroup.Input id={`${kind}-log-max-mib`} type="number" min="16" max="4096" step="1" required bind:value={maxMib} class="tabular-nums" aria-describedby={`${kind}-log-max-mib-range`} />
            <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().mib}</InputGroup.Text></InputGroup.Addon>
          </InputGroup.Root>
          <Field.Description id={`${kind}-log-max-mib-range`}>16–4096</Field.Description>
        </Field.Field>
        <Field.Field>
          <div class="flex items-center gap-1"><Field.Label for={`${kind}-log-retention`}>{t().retention}</Field.Label><InfoTooltip id={`${kind}-log-retention-tip`} label={t().retention} description={t().retentionTip} /></div>
          <InputGroup.Root>
            <InputGroup.Input id={`${kind}-log-retention`} type="number" min="1" max="365" step="1" required bind:value={retentionDays} class="tabular-nums" aria-describedby={`${kind}-log-retention-range`} />
            <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().days}</InputGroup.Text></InputGroup.Addon>
          </InputGroup.Root>
          <Field.Description id={`${kind}-log-retention-range`}>1–365</Field.Description>
        </Field.Field>
      </Field.Group>
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        <Button variant="outline" disabled={busy} onclick={() => { open = false; }}>{t().cancel}</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
