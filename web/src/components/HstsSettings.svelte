<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { ArrowUndo, Save, Warning } from '../icons';
  import type { HstsSettings } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';

  // 설정 > 보안의 HSTS 카드. 스위치 하나이며 되돌리기·저장으로만 반영합니다.
  const t = translations({
    title: 'HSTS',
    tip: 'Adds Strict-Transport-Security (max-age 1 year) to HTTPS responses on public addresses, so browsers open them only over HTTPS. An app that sends its own header keeps it. The admin console always sends it. Turn it off before moving an address to a server without HTTPS; browsers that already received it keep using HTTPS for up to a year.',
    enabled: 'Apply HSTS to public addresses',
    enabledHint: 'max-age 1 year, subdomains not included',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load HSTS settings: ${code}`,
  }, {
    title: 'HSTS',
    tip: '공개 주소의 HTTPS 응답에 Strict-Transport-Security(max-age 1년)를 추가하여 브라우저가 HTTPS로만 접속하도록 합니다. 앱이 헤더를 직접 지정한 경우 앱의 값을 유지합니다. 관리 콘솔에는 항상 적용됩니다. HTTPS를 제공하지 않는 서버로 주소를 이전하기 전에 비활성화하십시오. 이미 헤더를 수신한 브라우저는 최대 1년간 HTTPS 접속을 유지합니다.',
    enabled: '공개 주소에 HSTS 적용',
    enabledHint: 'max-age 1년, 하위 도메인 미포함',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `HSTS 설정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  let saved = $state<HstsSettings | null>(null);
  let draft = $state<HstsSettings | null>(null);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  const adopt = (next: HstsSettings) => { saved = next; draft = { ...next }; };
  const dirty = $derived(Boolean(saved && draft) && draft?.enabled !== saved?.enabled);

  onMount(() => {
    api<HstsSettings>('/api/v1/security/hsts').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function save(): Promise<void> {
    if (!draft || busy) return;
    busy = true;
    error = '';
    try {
      adopt(await api<HstsSettings>('/api/v1/security/hsts', { method: 'PUT', body: JSON.stringify({ enabled: draft.enabled }) }));
      toast.success(t().saved);
    } catch (cause) {
      error = t().failed(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (saved) draft = { ...saved };
    error = '';
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="hsts-title">
  <Card.Header>
    <Card.Title id="hsts-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="hsts-tip" label={t().title} description={t().tip} /></Card.Title>
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !draft}
      <Skeleton class="h-10 w-full" />
    {:else}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Field.Field orientation="horizontal">
        <Field.Content>
          <Field.Label for="hsts-enabled">{t().enabled}</Field.Label>
          <Field.Description>{t().enabledHint}</Field.Description>
        </Field.Content>
        <Switch id="hsts-enabled" bind:checked={draft.enabled} disabled={busy} />
      </Field.Field>
    {/if}
  </Card.Content>
  {#if dirty || busy}
    <Card.Footer class="justify-end gap-2">
      <Button variant="outline" disabled={busy} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button disabled={busy} onclick={save}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    </Card.Footer>
  {/if}
</Card.Root>
