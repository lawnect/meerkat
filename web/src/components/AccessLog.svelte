<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { ArrowSync, History20, Warning } from '../icons';
  import type { AccessDecision, AccessLogEntry, AccessLogView, AccessPolicy, AccessToken } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';

  // 지표 탭의 요청 기록. 스위치로 켜고 끄며, 판정과 정책으로 걸러 최신순으로 보고 "더 보기"로 이어 받습니다.
  // policies·tokens는 ID를 이름으로 보여 주는 데 씁니다(지워진 것은 ID 그대로).
  let { policies, tokens }: { policies: AccessPolicy[] | null; tokens: AccessToken[] | null } = $props();

  const t = translations({
    title: 'Request log',
    tip: 'Every request access policies refused or sent to sign in, and those let in by a signed-in person or a token. Requests let in anonymously are only counted. Kept in a file of its own for 30 days, at most 500,000 entries and 256 MiB, the oldest going first; requests are never held up by it, and when it can\'t keep up, entries are given up.',
    enabled: 'Logging',
    refresh: 'Refresh',
    decision: 'Decision',
    policy: 'Policy',
    allDecisions: 'All decisions',
    allPolicies: 'All policies',
    decisions: { passed: 'Passed', challenged: 'Sign-in required', denied: 'Denied' } as Record<AccessDecision, string>,
    reasons: {
      bypass: 'Bypass with token', session: 'Signed in', no_session: 'Not signed in', blocked: 'Block policy', not_allowed: 'Not allowed',
      token_invalid: 'Invalid token', token_expired: 'Expired token', token_out_of_scope: 'Token out of scope', token_not_allowed: 'Token not taken', unavailable: 'Access SSO off or failing',
    } as Record<string, string>,
    time: 'Time',
    request: 'Request',
    who: 'Who',
    removed: 'Deleted',
    more: 'Show more',
    off: 'Logging is off',
    offHint: 'Turn it on to keep the requests policies decide.',
    empty: 'No requests logged',
    unavailable: 'The log file couldn\'t be opened, so requests aren\'t logged.',
    dropped: (n: string) => `${n} requests weren't logged because the queue was full (since the Outlet started).`,
    saved: (on: boolean): string => (on ? 'Logging on' : 'Logging off'),
    failed: (code: string) => `Couldn't load the request log: ${code}`,
  }, {
    title: '요청 기록',
    tip: '접근 정책이 거부하거나 로그인을 요구한 요청과, 로그인한 사용자·토큰으로 통과한 요청을 기록합니다. 익명으로 통과한 요청은 지표로만 셉니다. 별도 파일에 30일, 최대 50만 건·256MiB까지 보관하며 오래된 것부터 지웁니다. 기록 때문에 요청이 지연되지 않으며, 처리량을 넘으면 기록을 포기합니다.',
    enabled: '기록',
    refresh: '새로 고침',
    decision: '판정',
    policy: '정책',
    allDecisions: '모든 판정',
    allPolicies: '모든 정책',
    decisions: { passed: '통과', challenged: '로그인 요구', denied: '거부' },
    reasons: {
      bypass: '토큰 우회', session: '로그인', no_session: '로그인 안 됨', blocked: '차단 정책', not_allowed: '허용 대상 아님',
      token_invalid: '유효하지 않은 토큰', token_expired: '만료된 토큰', token_out_of_scope: '이용 범위 밖 토큰', token_not_allowed: '정책이 받지 않는 토큰', unavailable: '접근 보호 SSO 비활성화 또는 오류',
    },
    time: '시각',
    request: '요청',
    who: '요청자',
    removed: '삭제됨',
    more: '더 보기',
    off: '요청 기록이 꺼져 있습니다',
    offHint: '켜면 정책이 판정한 요청을 기록합니다.',
    empty: '기록된 요청이 없습니다',
    unavailable: '기록 파일을 열 수 없어 요청을 기록하지 않습니다.',
    dropped: (n: string) => `대기열이 넘쳐 ${n}건을 기록하지 못했습니다(Outlet 시작 이후).`,
    saved: (on: boolean) => (on ? '요청 기록을 켰습니다' : '요청 기록을 껐습니다'),
    failed: (code: string) => `요청 기록을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const decisionList: AccessDecision[] = ['passed', 'challenged', 'denied'];
  const badge = (decision: AccessDecision) => (decision === 'denied' ? 'destructive' : decision === 'challenged' ? 'secondary' : 'outline');

  let view = $state<AccessLogView | null>(null);
  let entries = $state<AccessLogEntry[]>([]);
  let decision = $state('');
  let policy = $state('');
  let busy = $state(false);
  let loadError = $state('');

  async function load(before = 0): Promise<void> {
    const query = new URLSearchParams();
    if (decision) query.set('decision', decision);
    if (policy) query.set('policy', policy);
    if (before) query.set('before', String(before));
    busy = true;
    try {
      const next = await api<AccessLogView>(`/api/v1/security/access-log?${query}`);
      entries = before ? [...entries, ...next.entries] : next.entries;
      view = next;
      loadError = '';
    } catch (cause) {
      loadError = t().failed(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  $effect(() => {
    void decision;
    void policy;
    void load();
  });

  async function toggle(enabled: boolean): Promise<void> {
    busy = true;
    try {
      view = await api<AccessLogView>('/api/v1/security/access-log', { method: 'PUT', body: JSON.stringify({ enabled }) });
      toast.success(t().saved(enabled));
    } catch (cause) {
      toast.error(t().failed(errorMessage(cause)));
    } finally {
      busy = false;
    }
  }

  const policyName = (id: string) => policies?.find((entry) => entry.id === id)?.name;
  const tokenName = (id: string) => tokens?.find((entry) => entry.id === id)?.name ?? id;
  const time = (at: string) => new Date(at).toLocaleString(intlLocale(), { month: 'numeric', day: 'numeric', hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23' });
</script>

<Card.Root size="sm" role="region" aria-labelledby="access-log-title">
  <Card.Header>
    <Card.Title id="access-log-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="access-log-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if view}
      <Card.Action class="flex items-center gap-2">
        <IconButton label={t().refresh} icon={ArrowSync} disabled={busy} onclick={() => load()} />
        <Field.Label for="access-log-enabled" class="text-muted-foreground">{t().enabled}</Field.Label>
        <Switch id="access-log-enabled" checked={view.enabled} disabled={busy || !view.available} onCheckedChange={toggle} />
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !view}
      <div class="flex justify-center py-8"><Spinner /></div>
    {:else}
      {#if !view.available}
        <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{t().unavailable}</Alert.Description></Alert.Root>
      {:else if view.dropped}
        <Alert.Root><Warning aria-hidden="true" /><Alert.Description>{t().dropped(view.dropped.toLocaleString(intlLocale()))}</Alert.Description></Alert.Root>
      {/if}
      <div class="flex flex-wrap justify-end gap-2">
        <NativeSelect.Root size="sm" aria-label={t().decision} bind:value={decision}>
          <NativeSelect.Option value="">{t().allDecisions}</NativeSelect.Option>
          {#each decisionList as item (item)}<NativeSelect.Option value={item}>{t().decisions[item]}</NativeSelect.Option>{/each}
        </NativeSelect.Root>
        <NativeSelect.Root size="sm" aria-label={t().policy} bind:value={policy}>
          <NativeSelect.Option value="">{t().allPolicies}</NativeSelect.Option>
          {#each policies ?? [] as item (item.id)}<NativeSelect.Option value={item.id}>{item.name}</NativeSelect.Option>{/each}
        </NativeSelect.Root>
      </div>
      {#if entries.length}
        <div class="overflow-x-auto border">
          <Table.Root>
            <Table.Header class="bg-muted">
              <Table.Row class="hover:bg-transparent">
                <Table.Head class="pl-4">{t().time}</Table.Head>
                <Table.Head>{t().request}</Table.Head>
                <Table.Head>{t().decision}</Table.Head>
                <Table.Head class="pr-4">{t().who}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each entries as entry (entry.id)}
                <Table.Row>
                  <Table.Cell class="pl-4 align-top text-muted-foreground tabular-nums">{time(entry.at)}</Table.Cell>
                  <Table.Cell class="align-top">
                    <div class="grid gap-0.5">
                      <span class="max-w-56 truncate font-mono">{entry.host}</span>
                      <span class="flex items-center gap-1.5 text-muted-foreground"><span class="font-mono">{entry.method}</span><span class="max-w-48 truncate font-mono">{entry.path}</span></span>
                    </div>
                  </Table.Cell>
                  <Table.Cell class="align-top">
                    <div class="grid gap-0.5">
                      <span class="flex items-center gap-1.5">
                        <Badge variant={badge(entry.decision)} class="select-none">{t().decisions[entry.decision]}</Badge>
                        <span class="text-muted-foreground">{t().reasons[entry.reason] ?? entry.reason}</span>
                      </span>
                      <span class="max-w-48 truncate text-muted-foreground">{policyName(entry.policy) ?? t().removed}</span>
                    </div>
                  </Table.Cell>
                  <Table.Cell class="pr-4 align-top">
                    <div class="grid gap-0.5">
                      {#if entry.email}<span class="max-w-48 truncate font-mono">{entry.email}</span>{/if}
                      {#if entry.token}<span class="max-w-48 truncate">{tokenName(entry.token)}</span>{/if}
                      <span class="font-mono text-muted-foreground">{entry.client}</span>
                    </div>
                  </Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        </div>
        {#if view.next}
          <div class="flex justify-end">
            <Button variant="outline" size="sm" disabled={busy} onclick={() => load(view?.next ?? 0)}>{#if busy}<Spinner aria-hidden="true" />{/if}{t().more}</Button>
          </div>
        {/if}
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><History20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{view.enabled ? t().empty : t().off}</Empty.Title>
            {#if !view.enabled}<Empty.Description>{t().offHint}</Empty.Description>{/if}
          </Empty.Header>
        </Empty.Root>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>
