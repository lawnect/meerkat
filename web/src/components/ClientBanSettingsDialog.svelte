<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { Warning } from '../icons';
  import type { ClientBanSettings, ClientBansView } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 클라이언트 차단 시간과 제외 대상을 정합니다. 저장하면 PUT /api/v1/security/client-bans로 바로 서버에 반영하고, 응답은 onSaved로 넘깁니다.
  let { open = $bindable(false), settings, onSaved }: {
    open?: boolean;
    settings: ClientBanSettings;
    onSaved: (view: ClientBansView) => void;
  } = $props();

  const t = translations({
    title: 'Ban settings',
    minutes: 'Ban duration',
    minutesTip: 'How long a client stays banned after it sends a request that matches a ban rule.',
    unit: 'min',
    exempt: 'Exempt IPs and ranges',
    exemptTip: 'Clients at these addresses are never banned. Saving releases any of them that are banned now.',
    exemptHint: 'IPv4 or IPv6 address or CIDR, up to 50',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values. The ban duration is 1–10080 minutes, and exempt entries are IPv4 or IPv6 addresses or CIDR ranges, up to 50.',
    failed: (code: string) => `Couldn't save: ${code}`,
  }, {
    title: '차단 설정',
    minutes: '차단 시간',
    minutesTip: '클라이언트 차단 규칙에 해당하는 요청을 보낸 시점부터 차단을 유지하는 시간입니다.',
    unit: '분',
    exempt: '제외 대상 IP·범위',
    exemptTip: '지정한 IP와 범위의 클라이언트는 차단하지 않습니다. 저장하면 이미 차단된 해당 클라이언트는 즉시 해제됩니다.',
    exemptHint: 'IPv4·IPv6 주소 또는 CIDR, 최대 50개',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '입력값이 올바르지 않습니다. 차단 시간은 1–10080분, 제외 대상은 IPv4·IPv6 주소 또는 CIDR 범위이며 최대 50개까지 지정할 수 있습니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
  });

  let minutes = $state<number | null>(60);
  let exempt = $state<string[]>([]);
  let busy = $state(false);
  let error = $state('');

  // 열릴 때만 서버 값으로 채웁니다.
  let wasOpen = false;
  $effect(() => {
    if (open && !wasOpen) {
      minutes = settings.minutes;
      exempt = [...(settings.exempt ?? [])];
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
      const view = await api<ClientBansView>('/api/v1/security/client-bans', { method: 'PUT', body: JSON.stringify({ minutes, exempt: $state.snapshot(exempt) }) });
      onSaved(view);
      toast.success(t().saved);
      open = false;
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_client_bans' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }
</script>

<Dialog.Root bind:open={() => open, (next) => { if (!busy) open = next; }}>
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-md">
    <form class="grid gap-4" onsubmit={save}>
      <Dialog.Header>
        <Dialog.Title>{t().title}</Dialog.Title>
      </Dialog.Header>
      <Item.Group>
        <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
          <Field.Field>
            <div class="flex items-center gap-1"><Field.Label for="client-ban-minutes">{t().minutes}</Field.Label><InfoTooltip id="client-ban-minutes-tip" label={t().minutes} description={t().minutesTip} /></div>
            <InputGroup.Root>
              <InputGroup.Input id="client-ban-minutes" type="number" min="1" max="10080" step="1" required bind:value={minutes} class="tabular-nums" aria-describedby="client-ban-minutes-range" />
              <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().unit}</InputGroup.Text></InputGroup.Addon>
            </InputGroup.Root>
            <Field.Description id="client-ban-minutes-range">1–10080</Field.Description>
          </Field.Field>
        </Item.Root>
        <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
          <Field.Field>
            <div class="flex items-center gap-1"><Field.Label for="client-ban-exempt">{t().exempt}</Field.Label><InfoTooltip id="client-ban-exempt-tip" label={t().exempt} description={t().exemptTip} /></div>
            <TagInput id="client-ban-exempt" ariaLabel={t().exempt} bind:values={exempt} lowercase mono placeholder="203.0.113.0/24, 2001:db8::/48" />
            <Field.Description>{t().exemptHint}</Field.Description>
          </Field.Field>
        </Item.Root>
      </Item.Group>
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        <Button variant="outline" disabled={busy} onclick={() => { open = false; }}>{t().cancel}</Button>
        <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>
