<script lang="ts">
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { api, errorMessage } from '../api';
  import { untrack } from 'svelte';
  import { intlLocale, translations } from '../i18n.svelte';
  import { intParam, oneOf, param, setParams } from '../urlState.svelte';
  import { ArrowReset20, History20, Search, Warning } from '../icons';
  import type { AuditPage } from '../types';
  import PaginationBar from './PaginationBar.svelte';
  import TruncateText from './TruncateText.svelte';

  // 감사 로그. 서버가 페이지를 나눠 주므로 표 아래 Pagination이 offset을 바꿔 다시 불러옵니다.
  // 기록은 서버에서 지울 수 없게 막혀 있어 이 화면에도 삭제 기능이 없습니다.
  const t = translations({
    search: 'Search by user, target or detail',
    category: 'Category',
    categories: { '': 'All actions', 'session.': 'Sign-in', 'connector.': 'Tunnels', 'service.': 'Services', 'route.': 'Public addresses', 'domain.': 'Domains', 'tunnel.': 'Tunnel address', 'telegram.': 'Telegram', 'alerts.': 'Alerts', 'security.': 'Security' } as Record<string, string>,
    reset: 'Reset filters',
    colTime: 'Time',
    colActor: 'User',
    colAction: 'Action',
    colTarget: 'Target',
    colSource: 'From',
    local: 'Local port',
    actions: {
      'session.login': 'Signed in', 'session.login_failed': 'Sign-in failed', 'session.second_factor': 'Passed two-step verification', 'session.second_factor_failed': 'Two-step verification failed', 'session.logout': 'Signed out', 'setup.admin': 'Created administrator',
      'connector.create': 'Created tunnel', 'connector.delete': 'Deleted tunnel', 'connector.description': 'Changed tunnel description', 'connector.token': 'Issued tunnel token', 'connector.token_reset': 'Replaced tunnel token',
      'service.create': 'Added service', 'service.update': 'Changed service target', 'service.delete': 'Deleted service',
      'route.create': 'Added public address', 'route.delete': 'Deleted public address',
      'domain.set': 'Set primary domain', 'domain.add': 'Added domain', 'domain.remove': 'Deleted domain', 'domain.mode': 'Changed DNS records mode',
      'tunnel.address': 'Changed tunnel address',
      'telegram.save': 'Saved Telegram settings', 'telegram.test': 'Sent Telegram test', 'telegram.delete': 'Disconnected Telegram', 'alerts.rules': 'Changed alert rules', 'security.second_factor': 'Changed two-step verification',
    } as Record<string, string>,
    empty: 'No matching records',
    loadFailed: (code: string) => `Couldn't load the audit log: ${code}`,
  }, {
    search: '사용자, 대상, 내용 검색',
    category: '분류',
    categories: { '': '모든 작업', 'session.': '로그인', 'connector.': '터널', 'service.': '서비스', 'route.': '공개 주소', 'domain.': '도메인', 'tunnel.': '터널 주소', 'telegram.': '텔레그램', 'alerts.': '알림', 'security.': '보안' },
    reset: '필터 초기화',
    colTime: '시각',
    colActor: '사용자',
    colAction: '작업',
    colTarget: '대상',
    colSource: '접속',
    local: '로컬 포트',
    actions: {
      'session.login': '로그인', 'session.login_failed': '로그인 실패', 'session.second_factor': '2단계 인증 통과', 'session.second_factor_failed': '2단계 인증 실패', 'session.logout': '로그아웃', 'setup.admin': '관리자 생성',
      'connector.create': '터널 만들기', 'connector.delete': '터널 삭제', 'connector.description': '터널 설명 변경', 'connector.token': '터널 토큰 발급', 'connector.token_reset': '터널 토큰 교체',
      'service.create': '서비스 추가', 'service.update': '서비스 대상 변경', 'service.delete': '서비스 삭제',
      'route.create': '공개 주소 추가', 'route.delete': '공개 주소 삭제',
      'domain.set': '대표 도메인 설정', 'domain.add': '도메인 추가', 'domain.remove': '도메인 삭제', 'domain.mode': 'DNS 방식 변경',
      'tunnel.address': '터널 주소 변경',
      'telegram.save': '텔레그램 설정 저장', 'telegram.test': '텔레그램 테스트', 'telegram.delete': '텔레그램 연결 해제', 'alerts.rules': '알림 규칙 변경', 'security.second_factor': '2단계 인증 설정 변경',
    },
    empty: '조건에 맞는 기록이 없습니다',
    loadFailed: (code: string) => `감사 로그를 불러오지 못했습니다: ${code}`,
  });

  const perPage = 20;
  const categories = ['', 'session.', 'connector.', 'service.', 'route.', 'domain.', 'tunnel.', 'telegram.', 'alerts.', 'security.'];
  // 검색어·분류·페이지는 주소(?q=&category=&page=)가 단일 출처입니다. 입력칸은 잠시 멈춘 뒤에 주소에 반영합니다.
  const page = $derived(intParam('page', 1));
  const action = $derived(oneOf('category', categories, ''));
  const query = $derived(param('q') ?? '');
  let q = $state(untrack(() => param('q') ?? ''));
  let result = $state<AuditPage | null>(null);
  let error = $state('');

  $effect(() => {
    const fromUrl = query;
    if (untrack(() => q.trim()) !== fromUrl) q = fromUrl;
  });

  $effect(() => {
    const value = q.trim();
    const timer = window.setTimeout(() => { if (value !== untrack(() => query)) setParams({ q: value, page: 1 }, { page: 1 }); }, 250);
    return () => window.clearTimeout(timer);
  });

  $effect(() => {
    const params = new URLSearchParams({ limit: String(perPage), offset: String((page - 1) * perPage) });
    if (action) params.set('action', action);
    if (query) params.set('q', query);
    let active = true;
    error = '';
    api<AuditPage>(`/api/v1/audit?${params}`)
      .then((next) => {
        if (!active) return;
        result = next;
        // 범위를 벗어난 페이지 번호는 첫 페이지로 되돌립니다.
        if (next.total > 0 && next.events.length === 0 && page > 1) setParams({ page: 1 }, { page: 1 });
      })
      .catch((cause) => { if (active) error = t().loadFailed(errorMessage(cause)); });
    return () => { active = false; };
  });

  const setPage = (next: number): void => setParams({ page: next }, { page: 1 });
  const failed = (name: string) => name.endsWith('_failed');
  const timeLabel = (value: string) => new Date(value).toLocaleString(intlLocale(), { dateStyle: 'medium', timeStyle: 'medium' });

  function clearFilters(): void {
    q = '';
    setParams({ q: '', category: '', page: 1 }, { page: 1 });
  }
</script>

<div class="grid min-w-0 gap-4">
  <div class="flex flex-wrap items-center gap-2" role="search" aria-label={t().search}>
    <InputGroup.Root class="min-w-48 grow sm:max-w-sm sm:grow-0">
      <InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
      <InputGroup.Input type="search" bind:value={q} placeholder={t().search} aria-label={t().search} autocomplete="off" />
    </InputGroup.Root>
    <NativeSelect.Root class="w-full sm:w-44" aria-label={t().category} bind:value={() => action, (next) => { setParams({ category: next, page: 1 }, { page: 1 }); }}>
      {#each categories as category (category)}
        <NativeSelect.Option value={category}>{t().categories[category]}</NativeSelect.Option>
      {/each}
    </NativeSelect.Root>
    {#if q || action}
      <Tooltip.Root>
        <Tooltip.Trigger>
          {#snippet child({ props })}
            <Button {...props} variant="outline" size="icon" aria-label={t().reset} onclick={clearFilters}><ArrowReset20 class="size-5" aria-hidden="true" /></Button>
          {/snippet}
        </Tooltip.Trigger>
        <Tooltip.Content>{t().reset}</Tooltip.Content>
      </Tooltip.Root>
    {/if}
  </div>

  {#if error}
    <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>
  {:else if !result}
    <Skeleton class="h-96 w-full" />
  {:else if result.events.length}
    <PaginationBar total={result.total} {perPage} bind:page={() => page, setPage} placement="top" />
    <div class="overflow-hidden border bg-card">
      <Table.Root>
        <Table.Header class="bg-muted">
          <Table.Row class="hover:bg-transparent">
            <Table.Head class="pl-4">{t().colTime}</Table.Head>
            <Table.Head>{t().colActor}</Table.Head>
            <Table.Head>{t().colAction}</Table.Head>
            <Table.Head>{t().colTarget}</Table.Head>
            <Table.Head class="pr-4">{t().colSource}</Table.Head>
          </Table.Row>
        </Table.Header>
        <Table.Body>
          {#each result.events as event (event.id)}
            <Table.Row>
              <Table.Cell class="pl-4 text-muted-foreground tabular-nums">{timeLabel(event.created_at)}</Table.Cell>
              <Table.Cell class="max-w-48"><TruncateText text={event.actor_email ?? '—'} /></Table.Cell>
              <Table.Cell>
                <Tooltip.Root>
                  <Tooltip.Trigger class="inline-flex outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
                    <Badge variant={failed(event.action) ? 'destructive' : 'secondary'} class="cursor-default select-none">{t().actions[event.action] ?? event.action}</Badge>
                  </Tooltip.Trigger>
                  <Tooltip.Content class="max-w-sm font-mono">{event.action}{event.detail && event.detail !== '{}' ? ` ${event.detail}` : ''}</Tooltip.Content>
                </Tooltip.Root>
              </Table.Cell>
              <Table.Cell class="max-w-56 font-mono"><TruncateText text={event.resource_id} /></Table.Cell>
              <Table.Cell class="pr-4">
                {#if event.source === 'local'}
                  <Badge variant="outline" class="cursor-default select-none">{t().local}</Badge>
                {:else}
                  <span class="flex min-w-0 items-center gap-2"><span class="font-mono">{event.ip ?? '—'}</span>{#if event.user_agent}<span class="truncate text-muted-foreground">{event.user_agent}</span>{/if}</span>
                {/if}
              </Table.Cell>
            </Table.Row>
          {/each}
        </Table.Body>
      </Table.Root>
    </div>
    <PaginationBar total={result.total} {perPage} bind:page={() => page, setPage} />
  {:else}
    <Empty.Root class="border border-dashed bg-card">
      <Empty.Header>
        <Empty.Media variant="icon"><History20 class="size-5" aria-hidden="true" /></Empty.Media>
        <Empty.Title>{t().empty}</Empty.Title>
      </Empty.Header>
      {#if q || action}<Empty.Content><Button variant="outline" onclick={clearFilters}><ArrowReset20 class="size-5" aria-hidden="true" />{t().reset}</Button></Empty.Content>{/if}
    </Empty.Root>
  {/if}
</div>
