<script lang="ts">
  import * as Tabs from '$lib/components/ui/tabs/index.js';
  import AccessLog from '../components/AccessLog.svelte';
  import AccessMetrics from '../components/AccessMetrics.svelte';
  import AuditLog from '../components/AuditLog.svelte';
  import FailureLog from '../components/FailureLog.svelte';
  import LatencyMetrics from '../components/LatencyMetrics.svelte';
  import PageHead from '../components/PageHead.svelte';
  import RequestLog from '../components/RequestLog.svelte';
  import { translations } from '../i18n.svelte';
  import { ArrowBidirectionalUpDown12, ArrowTrending12, Clock12, ErrorCircle12, Shield12 } from '../icons';
  import { oneOf, setParams } from '../urlState.svelte';

  // 관찰 화면. 접근 정책의 판정 지표와 지연 시간, 요청 기록(목록과 엔드포인트·클라이언트별 통계), 접근 기록, 실패 요청, 감사 로그를 탭으로 나눕니다. 탭은 주소(?tab=access)가 단일 출처이고,
  // 각 탭의 기간·필터·페이지는 그 탭의 컴포넌트가 같은 방식으로 주소에 씁니다. addresses는 요청 기록과 실패 요청을 주소로 거를 때 고르는 공개 주소입니다.
  let { addresses }: { addresses: string[] } = $props();

  const t = translations({
    title: 'Observe',
    tabsAria: 'Observe sections',
    tabMetrics: 'Metrics',
    tabRequests: 'Requests',
    tabAccess: 'Access log',
    tabFailures: 'Failed requests',
    tabAudit: 'Audit log',
  }, {
    title: '관찰',
    tabsAria: '관찰 항목',
    tabMetrics: '지표',
    tabRequests: '요청 기록',
    tabAccess: '접근 기록',
    tabFailures: '실패 요청',
    tabAudit: '감사 로그',
  });

  const tabs = ['metrics', 'requests', 'access', 'failures', 'audit'] as const;
  const tab = $derived(oneOf('tab', tabs, 'metrics'));

  // Tabs.Content는 숨겨진 탭도 그려 두므로, 탭마다 같은 주소 query(page, 필터)를 읽는 컴포넌트는 고른 탭에서만 그립니다.
  // 탭을 옮기면 이전 탭의 기간·필터·페이지·검색어는 주소에서 지웁니다.
  const tabParams = ['unit', 'date', 'live', 'sort', 'page', 'q', 'decision', 'policy', 'who', 'cause', 'host', 'category', 'view', 'class', 'path', 'source', 'period'];
  function setTab(next: (typeof tabs)[number]): void {
    setParams({ ...Object.fromEntries(tabParams.map((name) => [name, null])), tab: next }, { tab: 'metrics' }, { push: true });
  }
</script>

<PageHead title={t().title} />

<Tabs.Root bind:value={() => tab, setTab} class="min-w-0 gap-4">
  <Tabs.List aria-label={t().tabsAria}>
    <Tabs.Trigger value="metrics"><ArrowTrending12 class="size-3" aria-hidden="true" />{t().tabMetrics}</Tabs.Trigger>
    <Tabs.Trigger value="requests"><ArrowBidirectionalUpDown12 class="size-3" aria-hidden="true" />{t().tabRequests}</Tabs.Trigger>
    <Tabs.Trigger value="access"><Shield12 class="size-3" aria-hidden="true" />{t().tabAccess}</Tabs.Trigger>
    <Tabs.Trigger value="failures"><ErrorCircle12 class="size-3" aria-hidden="true" />{t().tabFailures}</Tabs.Trigger>
    <Tabs.Trigger value="audit"><Clock12 class="size-3" aria-hidden="true" />{t().tabAudit}</Tabs.Trigger>
  </Tabs.List>
  <Tabs.Content value="metrics" class="grid min-w-0 gap-4">
    {#if tab === 'metrics'}<AccessMetrics /><LatencyMetrics />{/if}
  </Tabs.Content>
  <Tabs.Content value="requests" class="grid min-w-0 gap-4">
    {#if tab === 'requests'}<RequestLog {addresses} />{/if}
  </Tabs.Content>
  <Tabs.Content value="access" class="grid min-w-0 gap-4">
    {#if tab === 'access'}<AccessLog />{/if}
  </Tabs.Content>
  <Tabs.Content value="failures" class="grid min-w-0 gap-4">
    {#if tab === 'failures'}<FailureLog {addresses} />{/if}
  </Tabs.Content>
  <Tabs.Content value="audit" class="grid min-w-0 gap-4">
    {#if tab === 'audit'}<AuditLog />{/if}
  </Tabs.Content>
</Tabs.Root>
