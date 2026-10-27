<script lang="ts">
  import { onMount } from 'svelte';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { api } from '../api';
  import { translations } from '../i18n.svelte';
  import { History20 } from '../icons';
  import { accessDecisions, type AccessDecision, type AccessLogEntry, type AccessPoliciesView, type AccessPolicy, type AccessToken, type AccessTokensView } from '../types';
  import { oneOf, param, setParams } from '../urlState.svelte';
  import LogCard from './LogCard.svelte';
  import TruncateText from './TruncateText.svelte';

  // 관찰의 접근 기록 탭. 접근 정책이 거부하거나 로그인을 요구한 요청과, 로그인한 사용자·토큰으로 통과한 요청을 보여 줍니다.
  // 판정과 정책으로 거르며(?decision=&policy=), 정책과 토큰 ID는 이름으로 바꿔 보여 줍니다(지워진 정책은 "삭제됨").
  const t = translations({
    title: 'Access log',
    tip: 'Every request access policies refused or sent to sign in, and those let in by a signed-in person or a token. Requests let in anonymously are only counted in the metrics. Kept in a file of its own within the size limit and retention period, the oldest going first. Requests are never held up by logging; when it can\'t keep up, entries are given up.',
    emptyTitle: 'No requests logged',
    offTitle: 'Logging is disabled',
    offHint: 'Enable it to keep the requests access policies decide.',
    decision: 'Decision',
    policy: 'Policy',
    allDecisions: 'All decisions',
    allPolicies: 'All policies',
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
    decision: '판정',
    policy: '정책',
    allDecisions: '모든 판정',
    allPolicies: '모든 정책',
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

  const decisionOptions = ['', ...accessDecisions] as const;
  const decision = $derived(oneOf('decision', decisionOptions, ''));
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
</script>

<LogCard
  kind="access"
  title={t().title}
  tip={t().tip}
  emptyTitle={t().emptyTitle}
  offTitle={t().offTitle}
  offHint={t().offHint}
  emptyIcon={History20}
  loadFailed={t().loadFailed}
  filters={{ decision, policy }}
  columns={[{ label: t().request }, { label: t().decision, class: 'w-52' }, { label: t().who, class: 'w-48' }]}
>
  {#snippet filterControls()}
    <NativeSelect.Root size="sm" class="w-full sm:w-40" aria-label={t().decision} bind:value={() => decision, (next) => { setParams({ decision: next, page: null }); }}>
      <NativeSelect.Option value="">{t().allDecisions}</NativeSelect.Option>
      {#each accessDecisions as item (item)}<NativeSelect.Option value={item}>{t().decisions[item]}</NativeSelect.Option>{/each}
    </NativeSelect.Root>
    <NativeSelect.Root size="sm" class="w-full sm:w-56" aria-label={t().policy} bind:value={() => policy, (next) => { setParams({ policy: next, page: null }); }}>
      <NativeSelect.Option value="">{t().allPolicies}</NativeSelect.Option>
      {#each policies ?? [] as item (item.id)}<NativeSelect.Option value={item.id}>{item.name}</NativeSelect.Option>{/each}
      {#if unknownPolicy}<NativeSelect.Option value={unknownPolicy}>{unknownPolicy}</NativeSelect.Option>{/if}
    </NativeSelect.Root>
  {/snippet}
  {#snippet cells(entry: AccessLogEntry)}
    <Table.Cell class="align-top">
      <div class="grid gap-0.5">
        <TruncateText text={entry.host} class="font-mono" />
        <span class="flex min-w-0 items-center gap-1.5 font-mono text-muted-foreground"><span>{entry.method}</span><TruncateText text={entry.path} /></span>
      </div>
    </Table.Cell>
    <Table.Cell class="align-top">
      <div class="grid gap-0.5">
        <span class="flex min-w-0 items-center gap-1.5">
          <Badge variant={badge(entry.decision)} class="shrink-0 select-none">{t().decisions[entry.decision]}</Badge>
          <TruncateText text={t().reasons[entry.reason] ?? entry.reason} class="text-muted-foreground" />
        </span>
        <TruncateText text={policyName(entry.policy)} class="text-muted-foreground" />
      </div>
    </Table.Cell>
    <Table.Cell class="pr-4 align-top">
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
    </Table.Cell>
  {/snippet}
</LogCard>
