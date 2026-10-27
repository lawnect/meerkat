<script lang="ts">
  import { onMount } from 'svelte';
  import { createColumnHelper, renderSnippet } from '@tanstack/svelte-table';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { api } from '../api';
  import { translations } from '../i18n.svelte';
  import { History20 } from '../icons';
  import { accessDecisions, type AccessDecision, type AccessLogEntry, type AccessPoliciesView, type AccessPolicy, type AccessToken, type AccessTokensView } from '../types';
  import { param } from '../urlState.svelte';
  import type { DataTableFeatures } from './data-table/features';
  import LogCard from './LogCard.svelte';
  import type { LogFilter } from './logFilter';
  import TruncateText from './TruncateText.svelte';

  // 관찰의 접근 기록 탭. 접근 정책이 거부하거나 로그인을 요구한 요청과, 로그인한 사용자·토큰으로 통과한 요청을 보여 줍니다.
  // 검색어와 판정, 정책, 요청자(로그인·토큰 또는 익명)로 거르며(?q=&decision=&policy=&who=), 정책과 토큰 ID는 이름으로 바꿔 보여 줍니다(지워진 정책은 "삭제됨").
  const t = translations({
    title: 'Access log',
    tip: 'Every request access policies refused or sent to sign in, and those let in by a signed-in person or a token. Requests let in anonymously are only counted in the metrics. Kept in a file of its own within the size limit and retention period, the oldest going first. Requests are never held up by logging; when it can\'t keep up, entries are given up.',
    emptyTitle: 'No requests logged',
    offTitle: 'Logging is disabled',
    offHint: 'Enable it to keep the requests access policies decide.',
    suppressedTip: 'Anonymous requests are logged up to 60 per minute per client. Anything over that is only counted, and the count is since the Outlet started.',
    search: 'Search by address, path, client or requester',
    decision: 'Decision',
    policy: 'Policy',
    allDecisions: 'All decisions',
    allPolicies: 'All policies',
    allWho: 'All requesters',
    named: 'Signed in or token',
    anonymous: 'Anonymous',
    decisions: { passed: 'Passed', challenged: 'Sign-in required', denied: 'Denied' } as Record<AccessDecision, string>,
    reasons: {
      bypass: 'Bypass with token', session: 'Signed in', no_session: 'Not signed in', blocked: 'Block policy', not_allowed: 'Not allowed',
      token_invalid: 'Invalid token', token_expired: 'Expired token', token_out_of_scope: 'Token out of scope', token_not_allowed: 'Token not taken', unavailable: 'Access SSO off or failing',
    } as Record<string, string>,
    request: 'Request',
    who: 'Requester',
    removed: 'Deleted',
    loadFailed: (code: string) => `Couldn't load the access log: ${code}`,
  }, {
    title: '접근 기록',
    tip: '접근 정책이 거부하거나 로그인을 요구한 요청과, 로그인한 사용자·토큰으로 통과한 요청을 기록합니다. 익명으로 통과한 요청은 지표로만 집계됩니다. 별도 파일에 용량 상한과 보관 기간 범위 내에서 보관하며, 오래된 기록부터 삭제됩니다. 기록 때문에 요청이 지연되지 않으며, 처리량을 초과하면 기록을 포기합니다.',
    emptyTitle: '기록된 요청이 없습니다',
    offTitle: '기록이 비활성화되어 있습니다',
    offHint: '활성화하면 접근 정책이 판정한 요청을 기록합니다.',
    suppressedTip: '로그인하지 않은 익명 요청은 클라이언트별로 분당 60건까지만 기록합니다. 초과분은 건수만 집계하며, Outlet 시작 이후의 누계입니다.',
    search: '주소, 경로, 클라이언트, 요청자 검색',
    decision: '판정',
    policy: '정책',
    allDecisions: '모든 판정',
    allPolicies: '모든 정책',
    allWho: '모든 요청자',
    named: '로그인·토큰',
    anonymous: '익명',
    decisions: { passed: '통과', challenged: '로그인 요구', denied: '거부' },
    reasons: {
      bypass: '토큰 우회', session: '로그인', no_session: '로그인 안 됨', blocked: '차단 정책', not_allowed: '허용 대상 아님',
      token_invalid: '유효하지 않은 토큰', token_expired: '만료된 토큰', token_out_of_scope: '이용 범위 밖 토큰', token_not_allowed: '정책이 받지 않는 토큰', unavailable: '접근 보호 SSO 비활성화 또는 오류',
    },
    request: '요청',
    who: '요청자',
    removed: '삭제됨',
    loadFailed: (code: string) => `접근 기록을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const policy = $derived(param('policy') ?? '');

  // 이름을 보여 주는 데만 쓰므로 한 번만 불러오고, 실패하면 정책은 "삭제됨", 토큰은 ID로 보여 줍니다.
  let policies = $state<AccessPolicy[] | null>(null);
  let tokens = $state<AccessToken[] | null>(null);
  onMount(() => {
    api<AccessPoliciesView>('/api/v1/security/access-policies').then((view) => { policies = view.policies; }).catch(() => { policies = []; });
    api<AccessTokensView>('/api/v1/security/access-tokens').then((view) => { tokens = view.tokens; }).catch(() => { tokens = []; });
  });

  const badge = (value: AccessDecision) => (value === 'denied' ? 'destructive' : value === 'challenged' ? 'secondary' : 'outline');
  const policyName = (id: string) => (policies ? policies.find((entry) => entry.id === id)?.name ?? t().removed : '');
  const tokenName = (id: string) => tokens?.find((entry) => entry.id === id)?.name ?? id;
  const unknownPolicy = $derived(policy && policies && !policies.some((entry) => entry.id === policy) ? policy : '');

  const filters = $derived<LogFilter[]>([
    { param: 'decision', label: t().decision, class: 'sm:w-40', options: [{ value: '', label: t().allDecisions }, ...accessDecisions.map((value) => ({ value, label: t().decisions[value] }))] },
    {
      param: 'policy',
      label: t().policy,
      class: 'sm:w-56',
      free: true,
      options: [{ value: '', label: t().allPolicies }, ...(policies ?? []).map((item) => ({ value: item.id, label: item.name })), ...(unknownPolicy ? [{ value: unknownPolicy, label: unknownPolicy }] : [])],
    },
    { param: 'who', label: t().who, class: 'sm:w-44', options: [{ value: '', label: t().allWho }, { value: 'named', label: t().named }, { value: 'anonymous', label: t().anonymous }] },
  ]);

  const columnHelper = createColumnHelper<DataTableFeatures, AccessLogEntry>();
  const columns = columnHelper.columns([
    columnHelper.display({ id: 'request', header: () => t().request, cell: ({ row }) => renderSnippet(requestCell, row.original), enableSorting: false }),
    columnHelper.display({ id: 'decision', header: () => t().decision, cell: ({ row }) => renderSnippet(decisionCell, row.original), enableSorting: false, meta: { class: 'w-52' } }),
    columnHelper.display({ id: 'who', header: () => t().who, cell: ({ row }) => renderSnippet(whoCell, row.original), enableSorting: false, meta: { class: 'w-48' } }),
  ]);
</script>

{#snippet requestCell(entry: AccessLogEntry)}
  <div class="grid gap-0.5">
    <TruncateText text={entry.host} class="font-mono" />
    <span class="flex min-w-0 items-center gap-1.5 font-mono text-muted-foreground"><span>{entry.method}</span><TruncateText text={entry.path} /></span>
  </div>
{/snippet}

{#snippet decisionCell(entry: AccessLogEntry)}
  <div class="grid gap-0.5">
    <span class="flex min-w-0 items-center gap-1.5">
      <Badge variant={badge(entry.decision)} class="shrink-0 select-none">{t().decisions[entry.decision]}</Badge>
      <TruncateText text={t().reasons[entry.reason] ?? entry.reason} class="text-muted-foreground" />
    </span>
    <TruncateText text={policyName(entry.policy)} class="text-muted-foreground" />
  </div>
{/snippet}

{#snippet whoCell(entry: AccessLogEntry)}
  <div class="grid gap-0.5">
    {#if entry.email}
      <TruncateText text={entry.email} class="font-mono" />
    {:else if entry.token}
      <TruncateText text={tokenName(entry.token)} />
    {:else}
      <span class="text-muted-foreground">—</span>
    {/if}
    <span class="font-mono text-muted-foreground">{entry.client}</span>
  </div>
{/snippet}

<LogCard
  kind="access"
  title={t().title}
  tip={t().tip}
  suppressedTip={t().suppressedTip}
  searchLabel={t().search}
  emptyTitle={t().emptyTitle}
  offTitle={t().offTitle}
  offHint={t().offHint}
  emptyIcon={History20}
  loadFailed={t().loadFailed}
  {filters}
  {columns}
/>
