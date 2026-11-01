<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { Add12, Globe20, Warning } from '../icons';
  import { tagSuggestions } from '../ipScope.svelte';
  import type { IpAddressEntry, IpAddressInput, IpAddressesView, IpTagUse } from '../types';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 접근 보호 화면의 IP 주소록 카드. 클라이언트 주소(IP 또는 CIDR)를 메모·태그와 함께 한 번 등록하고,
  // 접근 정책과 관리 콘솔 접근 제한은 태그로 그 태그가 붙은 모든 주소를 가리킵니다. 행을 누르면 열리는 대화상자에서 고치고 지웁니다.
  // tags는 주소록의 태그별 항목 수와 사용처(불러오기 전에는 null)로, 불러오거나 바꿀 때마다 갱신해 정책 카드와 관리 콘솔 접근 제한이 태그를 제안하게 합니다.
  // refresh가 바뀌면(정책 카드가 정책을 저장했을 때) 태그의 사용처가 달라졌을 수 있어 다시 불러옵니다.
  let { tags = $bindable(null), refresh = 0 }: { tags?: IpTagUse[] | null; refresh?: number } = $props();

  const t = translations({
    title: 'IP address book',
    tip: 'Register client addresses once and use their tags in access policies and the admin console limit. A tag stands for every address that carries it.',
    add: 'Add IP',
    addTitle: 'Add IP',
    editTitle: 'Edit IP',
    range: 'IP or CIDR',
    rangeHint: 'IPv4 or IPv6 address or CIDR',
    note: 'Note',
    noteHint: 'Up to 100 characters',
    tags: 'Tags',
    tagsPlaceholder: 'Tag name',
    tagsHint: 'Up to 10, Enter or comma to add',
    empty: 'No IP addresses',
    usedBy: (list: string) => `Tags used by ${list}`,
    adminConsole: 'admin console limit',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    delete: 'Delete',
    deleteTitle: 'Delete IP',
    deleteConfirm: (range: string) => `Delete ${range} from the address book?`,
    deleted: 'Deleted',
    damaged: 'The saved address book could not be read, so tags match no address. Add the addresses again.',
    invalid: 'Check the values. Enter an IPv4 or IPv6 address or CIDR, a note of up to 100 characters, and up to 10 tags of 1–32 characters without commas that are not addresses.',
    taken: 'This IP or range is already in the address book.',
    tagInUse: 'A tag used by an access policy or the admin console limit must keep at least one IP.',
    locksOut: 'This change would cut your current IP off from the admin console. Make it over SSH or keep your address tagged.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load the IP address book: ${code}`,
  }, {
    title: 'IP 주소록',
    tip: '클라이언트 주소를 한 번 등록하고, 접근 정책과 관리 콘솔 접근 제한에서 태그로 지정합니다. 태그는 그 태그가 붙은 모든 주소를 의미합니다.',
    add: 'IP 추가',
    addTitle: 'IP 추가',
    editTitle: 'IP 편집',
    range: 'IP 또는 CIDR',
    rangeHint: 'IPv4·IPv6 주소 또는 CIDR',
    note: '메모',
    noteHint: '최대 100자',
    tags: '태그',
    tagsPlaceholder: '태그 이름',
    tagsHint: '최대 10개, Enter 또는 쉼표로 추가',
    empty: '등록된 IP가 없습니다',
    usedBy: (list: string) => `태그 사용처: ${list}`,
    adminConsole: '관리 콘솔 접근 제한',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    delete: '삭제',
    deleteTitle: 'IP 삭제',
    deleteConfirm: (range: string) => `${range}을(를) 주소록에서 삭제하시겠습니까?`,
    deleted: '삭제되었습니다',
    damaged: '저장된 주소록을 읽을 수 없어 태그가 어떤 주소에도 일치하지 않습니다. 주소를 다시 등록하십시오.',
    invalid: '입력값이 올바르지 않습니다. IPv4·IPv6 주소 또는 CIDR, 최대 100자의 메모, 쉼표 없이 1–32자이며 주소 형식이 아닌 태그를 최대 10개까지 지정할 수 있습니다.',
    taken: '이미 주소록에 있는 IP 또는 대역입니다.',
    tagInUse: '접근 정책이나 관리 콘솔 접근 제한에서 사용 중인 태그에는 IP가 하나 이상 있어야 합니다.',
    locksOut: '현재 접속 IP의 관리 콘솔 접근이 차단됩니다. SSH로 연결해 변경하거나 이 주소의 태그를 유지하십시오.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `IP 주소록을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const path = '/api/v1/security/ip-addresses';
  const maxTags = 10;

  let view = $state<IpAddressesView | null>(null);
  let loadError = $state('');
  let busy = $state(false);

  // 추가와 편집 대화상자는 같은 입력 항목을 씁니다. editing이 null이면 추가입니다.
  // 편집 대화상자는 열 때의 항목을 복사해 두어, 삭제 뒤 닫히는 동안에도 내용이 남게 합니다.
  let open = $state(false);
  let editing = $state<IpAddressEntry | null>(null);
  let confirmOpen = $state(false);
  let form = $state<IpAddressInput>({ range: '', note: '', tags: [] });
  let formError = $state('');

  // 늦게 도착한 이전 응답이 새 응답을 덮어쓰지 않도록 요청마다 번호를 매깁니다.
  let loadSeq = 0;

  async function load(): Promise<void> {
    const seq = ++loadSeq;
    try {
      const next = await api<IpAddressesView>(path);
      if (seq !== loadSeq) return;
      view = next;
      tags = next.tags;
      loadError = '';
    } catch (cause) {
      if (seq === loadSeq) loadError = t().loadFailed(errorMessage(cause));
    }
  }

  $effect(() => {
    void refresh;
    void load();
  });

  // 편집하는 항목의 태그를 쓰는 정책 이름과 관리 콘솔 접근 제한을 쉼표로 이어 줍니다. 쓰는 곳이 없으면 빈 문자열입니다.
  const usage = $derived.by(() => {
    if (!editing) return '';
    const names: string[] = [];
    let admin = false;
    for (const name of editing.tags) {
      const use = view?.tags.find((item) => item.name.toLowerCase() === name.toLowerCase());
      if (!use) continue;
      for (const policy of use.policies) if (!names.includes(policy)) names.push(policy);
      if (use.admin_access) admin = true;
    }
    if (admin) names.push(t().adminConsole);
    return names.join(', ');
  });

  function openCreate(): void {
    editing = null;
    form = { range: '', note: '', tags: [] };
    formError = '';
    open = true;
  }

  function openEdit(entry: IpAddressEntry): void {
    editing = { ...entry, tags: [...entry.tags] };
    form = { range: entry.range, note: entry.note, tags: [...entry.tags] };
    formError = '';
    open = true;
  }

  function errorText(code: string): string {
    switch (code) {
      case 'invalid_ip_address': return t().invalid;
      case 'ip_address_taken': return t().taken;
      case 'ip_tag_in_use': return t().tagInUse;
      case 'admin_access_locks_out': return t().locksOut;
      default: return t().failed(code);
    }
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (busy) return;
    const body: IpAddressInput = { range: form.range.trim(), note: form.note.trim(), tags: [...form.tags] };
    if (body.tags.length > maxTags) {
      formError = t().invalid;
      return;
    }
    const id = editing?.id;
    busy = true;
    formError = '';
    try {
      await api(id ? `${path}/${encodeURIComponent(id)}` : path, { method: id ? 'PUT' : 'POST', body: JSON.stringify(body) });
      open = false;
      toast.success(t().saved);
      await load();
    } catch (cause) {
      const code = errorMessage(cause);
      formError = errorText(code);
      // 이미 지워진 항목이면 목록을 다시 받아 맞춥니다.
      if (code === 'ip_address_not_found') void load();
    } finally {
      busy = false;
    }
  }

  async function remove(): Promise<void> {
    if (!editing || busy) return;
    const id = editing.id;
    busy = true;
    formError = '';
    try {
      try {
        await api(`${path}/${encodeURIComponent(id)}`, { method: 'DELETE' });
      } catch (cause) {
        // 이미 지워진 항목은 목록을 다시 받으면 됩니다.
        if (errorMessage(cause) !== 'ip_address_not_found') throw cause;
      }
      confirmOpen = false;
      open = false;
      toast.success(t().deleted);
      await load();
    } catch (cause) {
      confirmOpen = false;
      formError = errorText(errorMessage(cause));
    } finally {
      busy = false;
    }
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="ip-addresses-title">
  <Card.Header>
    <Card.Title id="ip-addresses-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="ip-addresses-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if view}
      <Card.Action>
        <Button variant="outline" size="sm" onclick={openCreate}><Add12 aria-hidden="true" />{t().add}</Button>
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{loadError}</Alert.Description></Alert.Root>
    {:else if !view}
      <Skeleton class="h-24 w-full" />
    {:else}
      {#if view.damaged}
        <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{t().damaged}</Alert.Description></Alert.Root>
      {/if}
      {#if view.entries.length}
        <div class="overflow-x-auto border">
          <Table.Root>
            <Table.Header class="bg-muted">
              <Table.Row class="hover:bg-transparent">
                <Table.Head class="pl-4">{t().range}</Table.Head>
                <Table.Head class="hidden sm:table-cell">{t().note}</Table.Head>
                <Table.Head class="pr-4">{t().tags}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each view.entries as entry (entry.id)}
                <Table.Row class="relative cursor-pointer">
                  <Table.Cell class="pl-4">
                    <button type="button" class="block max-w-48 truncate text-left font-mono text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50" aria-label={`${t().editTitle}: ${entry.range}`} onclick={() => openEdit(entry)}>{entry.range}</button>
                  </Table.Cell>
                  <Table.Cell class="hidden sm:table-cell">
                    <span class="block max-w-64 truncate">{entry.note}</span>
                  </Table.Cell>
                  <Table.Cell class="pr-4">
                    <div class="flex flex-wrap gap-1">
                      {#each entry.tags as tag (tag)}
                        <Badge variant="secondary" class="max-w-full"><span class="truncate">{tag}</span></Badge>
                      {/each}
                    </div>
                  </Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        </div>
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{t().empty}</Empty.Title>
          </Empty.Header>
        </Empty.Root>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>

<Dialog.Root bind:open={() => open, (next) => { if (!busy) open = next; }}>
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-lg">
    <form class="grid gap-4" onsubmit={submit}>
      <Dialog.Header>
        <Dialog.Title>{editing ? t().editTitle : t().addTitle}</Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field>
          <Field.Label for="ip-address-range">{t().range}</Field.Label>
          <Input id="ip-address-range" class="font-mono" bind:value={form.range} pattern=".*\S.*" autocomplete="off" autocapitalize="off" spellcheck={false} required />
          <Field.Description>{t().rangeHint}</Field.Description>
        </Field.Field>
        <Field.Field>
          <Field.Label for="ip-address-note">{t().note}</Field.Label>
          <Input id="ip-address-note" bind:value={form.note} maxlength={100} autocomplete="off" />
          <Field.Description>{t().noteHint}</Field.Description>
        </Field.Field>
        <Field.Field>
          <Field.Label for="ip-address-tags">{t().tags}</Field.Label>
          <TagInput id="ip-address-tags" ariaLabel={t().tags} bind:values={form.tags} suggestions={tagSuggestions(view?.tags ?? null)} secondary={() => true} placeholder={t().tagsPlaceholder} />
          <Field.Description>{t().tagsHint}</Field.Description>
        </Field.Field>
      </Field.Group>
      {#if usage}<p class="text-sm text-muted-foreground wrap-anywhere">{t().usedBy(usage)}</p>{/if}
      {#if formError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="break-keep">{formError}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        {#if editing}
          <Button variant="destructive" disabled={busy} onclick={() => (confirmOpen = true)}>{t().delete}</Button>
        {/if}
        <div class="flex flex-col-reverse gap-2 sm:ml-auto sm:flex-row">
          <Button variant="outline" disabled={busy} onclick={() => (open = false)}>{t().cancel}</Button>
          <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
        </div>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={() => confirmOpen, (next) => { if (!busy) confirmOpen = next; }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{t().deleteTitle}</AlertDialog.Title>
      <AlertDialog.Description>{editing ? t().deleteConfirm(editing.range) : ''}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>{t().cancel}</AlertDialog.Cancel>
      <Button variant="destructive" disabled={busy} onclick={remove}>{#if busy}<Spinner aria-hidden="true" />{/if}{t().delete}</Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
