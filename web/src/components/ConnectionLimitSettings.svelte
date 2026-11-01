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

  // 설정 > 보안의 연결 제한 카드. 고친 값은 되돌리기·저장으로만 반영하며, 0이면 그 제한은 꺼집니다. 도메인별 업로드 크기와 응답 대기 시간은 같은 구성의 표 두 개입니다.
  const t = translations({
    title: 'Connection limits',
    tip: 'Limits on request size, requests in progress, stalled transfers and the wait for services. Requests over a limit get 413, 429 or 503 before they reach a tunnel; stalled transfers are cut, and a service that takes too long to answer gets its request ended with 504. 0 turns a limit off.',
    maxUpload: 'Max upload size',
    maxUploadHint: 'Request body size. Larger uploads get 413.',
    perIp: 'Concurrent requests per IP',
    perIpHint: 'Excess requests get 429. IPv6 counts per /56.',
    total: 'Total concurrent requests',
    totalHint: 'Requests being passed to services. Excess requests get 503.',
    respond: 'Response timeout',
    respondHint: 'Wait for a service to start answering. Longer waits get 504.',
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
    timeoutHosts: 'Response timeout by domain',
    timeoutHostsTip: 'Overrides the response timeout for one hostname. For a service slow to start answering, such as an LLM gateway, set a little more than its own limit. 0 for no limit, which also stops trying the next service.',
    noTimeoutHosts: 'No per-domain timeouts',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values: upload size 0–102,400 MB, concurrent requests 0–1,000,000, idle and response timeouts 0 or 5–3,600 seconds, minimum speed 0–100,000 KB/s, host is a hostname listed once.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load connection limits: ${code}`,
  }, {
    title: '연결 제한',
    tip: '요청 크기, 동시 요청 수, 정체된 전송, 서비스 응답 대기 시간을 제한합니다. 기준을 초과한 요청은 터널로 전달되기 전에 413, 429, 503으로 차단되며, 정체된 전송은 연결이 종료되고, 응답이 늦은 서비스의 요청은 504로 종료됩니다. 0 입력 시 해당 제한이 비활성화됩니다.',
    maxUpload: '최대 업로드 크기',
    maxUploadHint: '요청 본문 기준. 초과 시 413으로 차단',
    perIp: 'IP별 동시 요청',
    perIpHint: '초과 시 429로 차단. IPv6는 /56 단위로 그룹화하여 산정',
    total: '전체 동시 요청',
    totalHint: '서비스로 전달 중인 요청 수 기준. 초과 시 503으로 차단',
    respond: '응답 대기 시간',
    respondHint: '서비스 응답 시작까지의 대기 시간. 초과 시 504로 종료',
    idle: '유휴 제한 시간',
    idleHint: '업로드 또는 다운로드 정체 시 연결 종료',
    minUpload: '최소 업로드 속도',
    minUploadHint: '평균 업로드 속도 미달 시 연결 종료',
    unitMb: 'MB',
    unitRequests: '건',
    unitSeconds: '초',
    unitKbps: 'KB/s',
    hosts: '도메인별 업로드 크기',
    hostsTip: '호스트별 최대 업로드 크기를 개별 지정합니다. 0 입력 시 제한하지 않습니다.',
    host: '호스트',
    size: '최대 업로드 크기',
    remove: '삭제',
    add: '도메인 추가',
    noHosts: '도메인별 설정이 없습니다',
    timeoutHosts: '도메인별 응답 대기 시간',
    timeoutHostsTip: '호스트별 응답 대기 시간을 개별 지정합니다. LLM 게이트웨이처럼 응답 시작이 늦은 서비스는 서비스 자체 시한보다 약간 길게 지정합니다. 0 입력 시 제한하지 않으며, 다음 서비스로의 재시도도 이루어지지 않습니다.',
    noTimeoutHosts: '도메인별 설정이 없습니다',
    revert: '변경 취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '입력값이 올바르지 않습니다. 업로드 크기는 0–102,400MB, 동시 요청은 0–1,000,000, 유휴 제한 시간과 응답 대기 시간은 0 또는 5–3,600초, 최소 업로드 속도는 0–100,000KB/s, 호스트는 목록마다 한 번씩 쓴 도메인 이름이어야 합니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `연결 제한 설정을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  type LimitKey = 'max_upload_mb' | 'per_ip_concurrent' | 'total_concurrent' | 'response_timeout_seconds' | 'idle_timeout_seconds' | 'min_upload_kb_per_second';
  const limits: Array<{ key: LimitKey; label: () => string; hint: () => string; unit: () => string; max: number }> = [
    { key: 'max_upload_mb', label: () => t().maxUpload, hint: () => t().maxUploadHint, unit: () => t().unitMb, max: 102400 },
    { key: 'per_ip_concurrent', label: () => t().perIp, hint: () => t().perIpHint, unit: () => t().unitRequests, max: 1000000 },
    { key: 'total_concurrent', label: () => t().total, hint: () => t().totalHint, unit: () => t().unitRequests, max: 1000000 },
    { key: 'response_timeout_seconds', label: () => t().respond, hint: () => t().respondHint, unit: () => t().unitSeconds, max: 3600 },
    { key: 'idle_timeout_seconds', label: () => t().idle, hint: () => t().idleHint, unit: () => t().unitSeconds, max: 3600 },
    { key: 'min_upload_kb_per_second', label: () => t().minUpload, hint: () => t().minUploadHint, unit: () => t().unitKbps, max: 100000 },
  ];
  const maxHosts = 50;

  // 도메인별 표 두 개(업로드 크기, 응답 대기 시간)는 같은 구성이고 값 칸의 키만 다릅니다.
  type HostListKey = 'hosts' | 'response_timeouts';
  type HostRow = { host: string } & Record<string, string | number>;
  const hostLists: Array<{ key: HostListKey; field: string; title: () => string; tip: () => string; column: () => string; unit: () => string; max: number; fresh: number; empty: () => string }> = [
    { key: 'hosts', field: 'max_upload_mb', title: () => t().hosts, tip: () => t().hostsTip, column: () => t().size, unit: () => t().unitMb, max: 102400, fresh: 1024, empty: () => t().noHosts },
    { key: 'response_timeouts', field: 'response_timeout_seconds', title: () => t().timeoutHosts, tip: () => t().timeoutHostsTip, column: () => t().respond, unit: () => t().unitSeconds, max: 3600, fresh: 600, empty: () => t().noTimeoutHosts },
  ];
  const rowsOf = (current: ConnectionLimits, key: HostListKey) => current[key] as unknown as HostRow[];

  let saved = $state<ConnectionLimits | null>(null);
  let draft = $state<ConnectionLimits | null>(null);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  // 0.7.0 서버(되돌린 뒤 열려 있던 탭)는 응답 대기 시간 키를 보내지 않으므로 기본값으로 채웁니다. 저장은 그 서버가 거부합니다.
  const adopt = (next: ConnectionLimits) => {
    next.response_timeout_seconds ??= 60;
    next.response_timeouts ??= [];
    saved = next;
    draft = structuredClone(next);
  };
  const dirty = $derived(Boolean(saved && draft) && JSON.stringify(draft) !== JSON.stringify(saved));

  onMount(() => {
    api<ConnectionLimits>('/api/v1/security/connection-limits').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  // 비운 숫자 칸은 undefined가 되어 서버가 invalid_request로 거부하므로, 보내기 전에 범위 안내를 보여 줍니다.
  const filled = (value: unknown) => typeof value === 'number' && Number.isFinite(value);
  const complete = (current: ConnectionLimits) =>
    limits.every((limit) => filled(current[limit.key])) && hostLists.every((list) => rowsOf(current, list.key).every((row) => filled(row[list.field])));

  async function save(): Promise<void> {
    if (!draft || busy) return;
    if (!complete(draft)) {
      error = t().invalid;
      return;
    }
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

      {#each hostLists as list (list.key)}
        {@const rows = rowsOf(draft, list.key)}
        <div class="grid gap-2">
          <div class="flex items-center justify-between gap-2">
            <h3 id="connection-limits-{list.key}-title" class="flex items-center gap-1 text-sm font-medium">{list.title()}<InfoTooltip id="connection-limits-{list.key}-tip" label={list.title()} description={list.tip()} /></h3>
            <Button variant="outline" size="sm" aria-describedby="connection-limits-{list.key}-title" disabled={rows.length >= maxHosts} onclick={() => rows.push({ host: '', [list.field]: list.fresh })}><Add12 aria-hidden="true" />{t().add}</Button>
          </div>
          {#if rows.length}
            <div class="overflow-x-auto border">
              <Table.Root>
                <Table.Header class="bg-muted">
                  <Table.Row class="hover:bg-transparent">
                    <Table.Head class="pl-4">{t().host}</Table.Head>
                    <Table.Head>{list.column()}</Table.Head>
                    <Table.Head class="pr-4"><span class="sr-only">{t().remove}</span></Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {#each rows as entry, index (index)}
                    <Table.Row>
                      <Table.Cell class="pl-4"><Input aria-label={t().host} bind:value={entry.host} class="w-48 font-mono" /></Table.Cell>
                      <Table.Cell>
                        <InputGroup.Root class="w-36">
                          <InputGroup.Input type="number" min="0" max={list.max} aria-label={list.column()} bind:value={entry[list.field]} class="tabular-nums" />
                          <InputGroup.Addon align="inline-end"><InputGroup.Text>{list.unit()}</InputGroup.Text></InputGroup.Addon>
                        </InputGroup.Root>
                      </Table.Cell>
                      <Table.Cell class="pr-4 text-right"><IconButton label={t().remove} icon={Delete} side="left" onclick={() => rows.splice(index, 1)} /></Table.Cell>
                    </Table.Row>
                  {/each}
                </Table.Body>
              </Table.Root>
            </div>
          {:else}
            <Empty.Root class="border border-dashed">
              <Empty.Header>
                <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
                <Empty.Title>{list.empty()}</Empty.Title>
              </Empty.Header>
            </Empty.Root>
          {/if}
        </div>
      {/each}
    {/if}
  </Card.Content>
  {#if dirty || busy}
    <Card.Footer class="justify-end gap-2">
      <Button variant="outline" disabled={busy} onclick={revert}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button disabled={busy} onclick={save}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    </Card.Footer>
  {/if}
</Card.Root>
