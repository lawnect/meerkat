<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { Add12, ArrowUndo, Delete, Globe20, Save, Warning } from '../icons';
  import type { ConnectionLimits } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';

  // 설정 > 보안의 연결 제한 카드. 고친 값은 되돌리기·저장으로만 반영하며, 0이면 그 제한은 꺼집니다.
  const t = translations({
    title: 'Connection limits',
    tip: 'Limits on request size, requests in progress and stalled transfers. Requests over a limit get 413, 429 or 503 before they reach a tunnel; stalled transfers are cut. 0 turns a limit off.',
    maxUpload: 'Max upload size',
    maxUploadHint: 'Request body size. Larger uploads get 413.',
    perIp: 'Concurrent requests per IP',
    perIpHint: 'Excess requests get 429. IPv6 counts per /56.',
    total: 'Total concurrent requests',
    totalHint: 'Requests being passed to services. Excess requests get 503.',
    idle: 'Idle timeout',
    idleHint: 'Closes uploads and downloads that stop making progress.',
    minUpload: 'Minimum upload speed',
    minUploadHint: 'Closes uploads slower than this on average.',
    unitMb: 'MB',
    unitRequests: 'req',
    unitSeconds: 's',
    unitKbps: 'KB/s',
    hosts: 'Upload size by domain',
    hostsTip: 'Overrides the max upload size for one hostname. 0 for no limit.',
    host: 'Host',
    size: 'Max upload size',
    remove: 'Delete',
    add: 'Add domain',
    noHosts: 'No per-domain sizes',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values: upload size 0–102,400 MB, concurrent requests 0–1,000,000, idle timeout 0 or 5–3,600 seconds, minimum speed 0–100,000 KB/s, host is a hostname.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load connection limits: ${code}`,
  }, {
    title: '연결 제한',
    tip: '요청 크기, 동시 요청 수, 멈춘 전송을 제한합니다. 기준을 넘는 요청은 터널로 보내기 전에 413, 429, 503으로 거부하고, 멈춘 전송은 끊습니다. 0이면 해당 제한을 사용하지 않습니다.',
    maxUpload: '최대 업로드 크기',
    maxUploadHint: '요청 본문 크기입니다. 초과하면 413으로 차단합니다.',
    perIp: 'IP별 동시 요청',
    perIpHint: '초과하면 429로 차단합니다. IPv6는 /56 단위로 셉니다.',
    total: '전체 동시 요청',
    totalHint: '서비스로 전달 중인 요청 수입니다. 초과하면 503으로 차단합니다.',
    idle: '유휴 제한 시간',
    idleHint: '업로드나 다운로드가 멈추면 연결을 끊습니다.',
    minUpload: '최소 업로드 속도',
    minUploadHint: '평균 속도가 이보다 낮으면 업로드를 끊습니다.',
    unitMb: 'MB',
    unitRequests: '건',
    unitSeconds: '초',
    unitKbps: 'KB/s',
    hosts: '도메인별 업로드 크기',
    hostsTip: '호스트 하나의 최대 업로드 크기를 따로 정합니다. 0이면 제한하지 않습니다.',
    host: '호스트',
    size: '최대 업로드 크기',
    remove: '삭제',
    add: '도메인 추가',
    noHosts: '도메인별 설정이 없습니다',
    revert: '되돌리기',
    save: '저장',
    saving: '저장 중…',
    saved: '저장했습니다',
    invalid: '값을 확인하세요. 업로드 크기는 0–102,400MB, 동시 요청은 0–1,000,000, 유휴 시간은 0 또는 5–3,600초, 최소 속도는 0–100,000KB/s이고, 호스트는 도메인 이름이어야 합니다.',
    failed: (code: string) => `저장하지 못했습니다: ${code}`,
    loadFailed: (code: string) => `연결 제한을 불러오지 못했습니다: ${code}`,
  });

  type LimitKey = 'max_upload_mb' | 'per_ip_concurrent' | 'total_concurrent' | 'idle_timeout_seconds' | 'min_upload_kb_per_second';
  const limits: Array<{ key: LimitKey; label: () => string; hint: () => string; unit: () => string; max: number }> = [
    { key: 'max_upload_mb', label: () => t().maxUpload, hint: () => t().maxUploadHint, unit: () => t().unitMb, max: 102400 },
    { key: 'per_ip_concurrent', label: () => t().perIp, hint: () => t().perIpHint, unit: () => t().unitRequests, max: 1000000 },
    { key: 'total_concurrent', label: () => t().total, hint: () => t().totalHint, unit: () => t().unitRequests, max: 1000000 },
    { key: 'idle_timeout_seconds', label: () => t().idle, hint: () => t().idleHint, unit: () => t().unitSeconds, max: 3600 },
    { key: 'min_upload_kb_per_second', label: () => t().minUpload, hint: () => t().minUploadHint, unit: () => t().unitKbps, max: 100000 },
  ];
  const maxHosts = 50;

  let saved = $state<ConnectionLimits | null>(null);
  let draft = $state<ConnectionLimits | null>(null);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  const adopt = (next: ConnectionLimits) => { saved = next; draft = structuredClone(next); };
  const dirty = $derived(Boolean(saved && draft) && JSON.stringify(draft) !== JSON.stringify(saved));

  onMount(() => {
    api<ConnectionLimits>('/api/v1/security/connection-limits').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function save(): Promise<void> {
    if (!draft || busy) return;
    busy = true;
    error = '';
    try {
      adopt(await api<ConnectionLimits>('/api/v1/security/connection-limits', { method: 'PUT', body: JSON.stringify($state.snapshot(draft)) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_connection_limits' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (saved) draft = structuredClone($state.snapshot(saved));
    error = '';
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="connection-limits-title">
  <Card.Header>
    <Card.Title id="connection-limits-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="connection-limits-tip" label={t().title} description={t().tip} /></Card.Title>
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !draft}
      <Skeleton class="h-48 w-full" />
    {:else}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Field.Group class="gap-0">
        {#each limits as limit, index (limit.key)}
          {#if index}<Field.Separator class="my-3" />{/if}
          <Field.Field orientation="horizontal" class="flex-wrap items-center gap-y-2">
            <Field.Content class="min-w-48">
              <Field.Title>{limit.label()}</Field.Title>
              <Field.Description>{limit.hint()}</Field.Description>
            </Field.Content>
            <InputGroup.Root class="w-36">
              <InputGroup.Input type="number" min="0" max={limit.max} aria-label={limit.label()} bind:value={draft[limit.key]} class="tabular-nums" />
              <InputGroup.Addon align="inline-end"><InputGroup.Text>{limit.unit()}</InputGroup.Text></InputGroup.Addon>
            </InputGroup.Root>
          </Field.Field>
        {/each}
      </Field.Group>

      <div class="grid gap-2">
        <div class="flex items-center justify-between gap-2">
          <h3 class="flex items-center gap-1 text-sm font-medium">{t().hosts}<InfoTooltip id="connection-limits-hosts-tip" label={t().hosts} description={t().hostsTip} /></h3>
          <Button variant="outline" size="sm" disabled={draft.hosts.length >= maxHosts} onclick={() => draft?.hosts.push({ host: '', max_upload_mb: 1024 })}><Add12 aria-hidden="true" />{t().add}</Button>
        </div>
        {#if draft.hosts.length}
          <div class="overflow-x-auto border">
            <Table.Root>
              <Table.Header class="bg-muted">
                <Table.Row class="hover:bg-transparent">
                  <Table.Head class="pl-4">{t().host}</Table.Head>
                  <Table.Head>{t().size}</Table.Head>
                  <Table.Head class="pr-4"><span class="sr-only">{t().remove}</span></Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {#each draft.hosts as entry, index (index)}
                  <Table.Row>
                    <Table.Cell class="pl-4"><Input aria-label={t().host} bind:value={entry.host} class="w-48 font-mono" /></Table.Cell>
                    <Table.Cell>
                      <InputGroup.Root class="w-36">
                        <InputGroup.Input type="number" min="0" max="102400" aria-label={t().size} bind:value={entry.max_upload_mb} class="tabular-nums" />
                        <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().unitMb}</InputGroup.Text></InputGroup.Addon>
                      </InputGroup.Root>
                    </Table.Cell>
                    <Table.Cell class="pr-4 text-right"><IconButton label={t().remove} icon={Delete} side="left" onclick={() => draft?.hosts.splice(index, 1)} /></Table.Cell>
                  </Table.Row>
                {/each}
              </Table.Body>
            </Table.Root>
          </div>
        {:else}
          <Empty.Root class="border border-dashed">
            <Empty.Header>
              <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
              <Empty.Title>{t().noHosts}</Empty.Title>
            </Empty.Header>
          </Empty.Root>
        {/if}
      </div>
    {/if}
  </Card.Content>
  {#if dirty || busy}
    <Card.Footer class="justify-end gap-2">
      <Button variant="outline" disabled={busy} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button disabled={busy} onclick={save}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    </Card.Footer>
  {/if}
</Card.Root>
