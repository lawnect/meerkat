<script lang="ts">
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import { translations } from '../i18n.svelte';
  import { Info12, Search12 } from '../icons';
  import type { AccessDecision, LogEvidence } from '../types';
  import { setParams } from '../urlState.svelte';
  import CopyText from './CopyText.svelte';
  import IconButton from './IconButton.svelte';
  import TruncateText from './TruncateText.svelte';

  let { entry }: { entry: LogEvidence & { method: string; path: string; block_rule?: string; access_decision?: AccessDecision | ''; access_reason?: string; access_policy?: string } } = $props();
  const t = translations({
    requestId: 'Request ID', search: 'Search this log for this request ID', escaped: 'Escaped path',
    pathTruncated: 'The decoded path was truncated.', escapedTruncated: 'The escaped path was truncated.',
    rule: (id: string) => `Block rule: ${id}`, policy: (id: string) => `Access policy: ${id}`,
    decisions: { passed: 'Passed', challenged: 'Sign-in required', denied: 'Denied' } as Record<AccessDecision, string>,
    reasons: { no_policy: 'No matching policy', bypass: 'Bypass', session: 'Signed in', no_session: 'Not signed in', blocked: 'Block policy', not_allowed: 'Not allowed', token_invalid: 'Invalid token', token_expired: 'Expired token', token_out_of_scope: 'Token out of scope', token_not_allowed: 'Token not taken', unavailable: 'Access SSO off or failing' } as Record<string, string>,
  }, {
    requestId: '요청 ID', search: '이 요청 ID로 현재 기록 검색', escaped: '이스케이프된 경로',
    pathTruncated: '디코딩한 경로가 잘렸습니다.', escapedTruncated: '이스케이프된 경로가 잘렸습니다.',
    rule: (id: string) => `차단 규칙: ${id}`, policy: (id: string) => `접근 정책: ${id}`,
    decisions: { passed: '통과', challenged: '로그인 요구', denied: '거부' },
    reasons: { no_policy: '해당 정책 없음', bypass: '우회', session: '로그인', no_session: '로그인 안 됨', blocked: '차단 정책', not_allowed: '허용 대상 아님', token_invalid: '유효하지 않은 토큰', token_expired: '만료된 토큰', token_out_of_scope: '이용 범위 밖 토큰', token_not_allowed: '정책이 받지 않는 토큰', unavailable: '접근 보호 SSO 비활성화 또는 오류' },
  });

  function search(): void {
    setParams({ q: entry.request_id ?? null, page: null, class: null, host: null, path: null, source: null, cause: null, policy: null, decision: null, who: null });
  }
</script>

<span class="flex min-w-0 items-center gap-1.5 font-mono text-muted-foreground">
  <span>{entry.method}</span><TruncateText text={entry.path} />
  {#if entry.escaped_path || entry.path_truncated || entry.escaped_path_truncated}
    <Tooltip.Root>
      <Tooltip.Trigger>
        {#snippet child({ props })}
          <Button {...props} type="button" variant="ghost" size="icon-xs" aria-label={t().escaped}><Info12 class="size-3" aria-hidden="true" /></Button>
        {/snippet}
      </Tooltip.Trigger>
      <Tooltip.Content class="max-w-lg break-all">
        <div class="grid gap-1"><span>{t().escaped}</span><span class="font-mono">{entry.escaped_path}</span>
          {#if entry.path_truncated}<span>{t().pathTruncated}</span>{/if}
          {#if entry.escaped_path_truncated}<span>{t().escapedTruncated}</span>{/if}
        </div>
      </Tooltip.Content>
    </Tooltip.Root>
  {/if}
</span>
{#if entry.request_id}
  <span class="flex min-w-0 items-center gap-1.5 text-muted-foreground"><span class="shrink-0">{t().requestId}</span><CopyText value={entry.request_id} /><IconButton size="icon-xs" icon={Search12} label={t().search} onclick={search} /></span>
{/if}
{#if entry.access_decision}
  <TruncateText text={`${t().decisions[entry.access_decision]} · ${t().reasons[entry.access_reason ?? ''] ?? entry.access_reason ?? ''}`} class="text-muted-foreground" />
  {#if entry.access_policy}<TruncateText text={t().policy(entry.access_policy)} class="text-muted-foreground" />{/if}
{/if}
{#if entry.block_rule}<TruncateText text={t().rule(entry.block_rule)} class="text-muted-foreground" />{/if}
