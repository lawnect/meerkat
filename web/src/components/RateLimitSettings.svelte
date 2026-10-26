<script lang="ts">
  import { onMount } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { translations } from '../i18n.svelte';
  import { Add, ArrowUndo, Delete, Save, Shield20, Warning } from '../icons';
  import type { RateBucket, RateLimitSettings } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';

  // 설정 > 보안의 요청 제한 카드. 고친 값은 되돌리기·저장으로만 반영합니다.
  const t = translations({
    title: 'Rate limiting',
    tip: 'Requests over the limit get 429 Too Many Requests before they reach a tunnel. This blocks request floods and password guessing; volumetric DDoS needs network-level protection such as the Cloudflare proxy.',
    enabled: 'Limit requests',
    perIp: 'Per IP',
    perIpHint: 'One client address.',
    perSubnet: 'Per network',
    perSubnetHint: 'IPv4 /24 or IPv6 /64, so many addresses from one network count together.',
    perAddress: 'Per public address',
    perAddressHint: 'All clients of one hostname together. 0 turns it off.',
    rps: 'Requests per second',
    burst: 'Burst',
    perSecond: '/s',
    burstShort: 'burst',
    endpoints: 'Endpoint rules',
    endpointsTip: 'Stricter limits for specific paths. The longest matching path wins. "All clients" caps the endpoint regardless of where requests come from.',
    host: 'Host',
    path: 'Path',
    scope: 'Counted',
    scopes: { total: 'All clients', ip: 'Per IP' } as Record<'total' | 'ip', string>,
    remove: 'Delete rule',
    add: 'Add rule',
    noRules: 'No endpoint rules',
    revert: 'Revert',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the values: requests per second 1–10,000, burst at least that and at most 100,000 (0 and 0 turn a limit off), host is a hostname or *, path starts with /.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load rate limits: ${code}`,
  }, {
    title: '요청 제한',
    tip: '기준을 넘는 요청은 터널로 보내기 전에 429 Too Many Requests로 거부합니다. 요청 폭주와 비밀번호 대입을 막으며, 회선을 채우는 대용량 DDoS는 Cloudflare 프록시 같은 네트워크 단계의 방어가 필요합니다.',
    enabled: '요청 제한 사용',
    perIp: 'IP별',
    perIpHint: '클라이언트 주소 하나 기준입니다.',
    perSubnet: '대역별',
    perSubnetHint: 'IPv4 /24, IPv6 /64 단위로 묶어 여러 주소에서 오는 요청을 함께 셉니다.',
    perAddress: '공개 주소별',
    perAddressHint: '한 호스트로 오는 모든 요청을 합쳐 셉니다. 0이면 끕니다.',
    rps: '초당 요청',
    burst: '순간 허용',
    perSecond: '/초',
    burstShort: '순간',
    endpoints: '엔드포인트 규칙',
    endpointsTip: '특정 경로에 더 엄격한 기준을 둡니다. 가장 길게 일치하는 경로가 적용됩니다. "전체 합산"은 출발지와 관계없이 그 엔드포인트 전체를 제한합니다.',
    host: '호스트',
    path: '경로',
    scope: '기준',
    scopes: { total: '전체 합산', ip: 'IP별' },
    remove: '규칙 삭제',
    add: '규칙 추가',
    noRules: '엔드포인트 규칙이 없습니다',
    revert: '되돌리기',
    save: '저장',
    saving: '저장 중…',
    saved: '저장했습니다',
    invalid: '값을 확인하세요. 초당 요청은 1–10,000, 순간 허용은 그 이상 100,000 이하입니다(둘 다 0이면 끔). 호스트는 도메인 이름 또는 *, 경로는 /로 시작해야 합니다.',
    failed: (code: string) => `저장하지 못했습니다: ${code}`,
    loadFailed: (code: string) => `요청 제한을 불러오지 못했습니다: ${code}`,
  });

  type BucketKey = 'per_ip' | 'per_subnet' | 'per_address';
  const buckets: Array<{ key: BucketKey; label: () => string; hint: () => string }> = [
    { key: 'per_ip', label: () => t().perIp, hint: () => t().perIpHint },
    { key: 'per_subnet', label: () => t().perSubnet, hint: () => t().perSubnetHint },
    { key: 'per_address', label: () => t().perAddress, hint: () => t().perAddressHint },
  ];

  let saved = $state<RateLimitSettings | null>(null);
  let draft = $state<RateLimitSettings | null>(null);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  const adopt = (next: RateLimitSettings) => { saved = next; draft = structuredClone(next); };
  const dirty = $derived(Boolean(saved && draft) && JSON.stringify(draft) !== JSON.stringify(saved));

  onMount(() => {
    api<RateLimitSettings>('/api/v1/security/rate-limit').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function save(): Promise<void> {
    if (!draft || busy) return;
    busy = true;
    error = '';
    try {
      adopt(await api<RateLimitSettings>('/api/v1/security/rate-limit', { method: 'PUT', body: JSON.stringify($state.snapshot(draft)) }));
      toast.success(t().saved);
    } catch (cause) {
      const code = errorMessage(cause);
      error = code === 'invalid_rate_limit' ? t().invalid : t().failed(code);
    } finally {
      busy = false;
    }
  }

  function revert(): void {
    if (saved) draft = structuredClone($state.snapshot(saved));
    error = '';
  }
</script>

{#snippet bucketInputs(bucket: RateBucket, label: string, disabled: boolean)}
  <InputGroup.Root class="w-32">
    <InputGroup.Input type="number" min="0" max="10000" aria-label={`${label} ${t().rps}`} bind:value={bucket.rps} {disabled} class="tabular-nums" />
    <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().perSecond}</InputGroup.Text></InputGroup.Addon>
  </InputGroup.Root>
  <InputGroup.Root class="w-36">
    <InputGroup.Input type="number" min="0" max="100000" aria-label={`${label} ${t().burst}`} bind:value={bucket.burst} {disabled} class="tabular-nums" />
    <InputGroup.Addon align="inline-end"><InputGroup.Text>{t().burstShort}</InputGroup.Text></InputGroup.Addon>
  </InputGroup.Root>
{/snippet}

<Card.Root size="sm" role="region" aria-labelledby="rate-limit-title">
  <Card.Header>
    <Card.Title id="rate-limit-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="rate-limit-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if draft}
      <Card.Action class="flex items-center gap-2">
        <Field.Label for="rate-limit-enabled" class="text-muted-foreground">{t().enabled}</Field.Label>
        <Switch id="rate-limit-enabled" bind:checked={draft.enabled} />
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !draft}
      <Skeleton class="h-48 w-full" />
    {:else}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      <Field.Group class="gap-0">
        {#each buckets as bucket, index (bucket.key)}
          {#if index}<Field.Separator class="my-3" />{/if}
          <Field.Field orientation="horizontal" class="flex-wrap items-center gap-y-2">
            <Field.Content class="min-w-48">
              <Field.Title>{bucket.label()}</Field.Title>
              <Field.Description>{bucket.hint()}</Field.Description>
            </Field.Content>
            <div class="flex items-center gap-2">{@render bucketInputs(draft[bucket.key], bucket.label(), !draft.enabled)}</div>
          </Field.Field>
        {/each}
      </Field.Group>

      <div class="grid gap-2">
        <div class="flex items-center justify-between gap-2">
          <h3 class="flex items-center gap-1 text-sm font-medium">{t().endpoints}<InfoTooltip id="rate-endpoints-tip" label={t().endpoints} description={t().endpointsTip} /></h3>
          <Button variant="outline" size="sm" disabled={!draft.enabled || draft.endpoints.length >= 50} onclick={() => draft?.endpoints.push({ host: '*', path_prefix: '/', rps: 10, burst: 20, per: 'ip' })}><Add aria-hidden="true" />{t().add}</Button>
        </div>
        {#if draft.endpoints.length}
          <div class="overflow-x-auto border">
            <Table.Root>
              <Table.Header class="bg-muted">
                <Table.Row class="hover:bg-transparent">
                  <Table.Head class="pl-4">{t().host}</Table.Head>
                  <Table.Head>{t().path}</Table.Head>
                  <Table.Head>{t().scope}</Table.Head>
                  <Table.Head>{t().rps}</Table.Head>
                  <Table.Head>{t().burst}</Table.Head>
                  <Table.Head class="pr-4"><span class="sr-only">{t().remove}</span></Table.Head>
                </Table.Row>
              </Table.Header>
              <Table.Body>
                {#each draft.endpoints as rule, index (index)}
                  <Table.Row>
                    <Table.Cell class="pl-4"><Input aria-label={t().host} bind:value={rule.host} disabled={!draft.enabled} class="w-48 font-mono" /></Table.Cell>
                    <Table.Cell><Input aria-label={t().path} bind:value={rule.path_prefix} disabled={!draft.enabled} class="w-40 font-mono" /></Table.Cell>
                    <Table.Cell>
                      <NativeSelect.Root aria-label={t().scope} bind:value={rule.per} disabled={!draft.enabled}>
                        <NativeSelect.Option value="ip">{t().scopes.ip}</NativeSelect.Option>
                        <NativeSelect.Option value="total">{t().scopes.total}</NativeSelect.Option>
                      </NativeSelect.Root>
                    </Table.Cell>
                    <Table.Cell><Input type="number" min="1" max="10000" aria-label={t().rps} bind:value={rule.rps} disabled={!draft.enabled} class="w-24 tabular-nums" /></Table.Cell>
                    <Table.Cell><Input type="number" min="1" max="100000" aria-label={t().burst} bind:value={rule.burst} disabled={!draft.enabled} class="w-24 tabular-nums" /></Table.Cell>
                    <Table.Cell class="pr-4 text-right"><IconButton label={t().remove} icon={Delete} side="left" disabled={!draft.enabled} onclick={() => draft?.endpoints.splice(index, 1)} /></Table.Cell>
                  </Table.Row>
                {/each}
              </Table.Body>
            </Table.Root>
          </div>
        {:else}
          <Empty.Root class="border border-dashed">
            <Empty.Header>
              <Empty.Media variant="icon"><Shield20 class="size-5" aria-hidden="true" /></Empty.Media>
              <Empty.Title>{t().noRules}</Empty.Title>
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
