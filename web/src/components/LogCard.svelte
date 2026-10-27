<script lang="ts" generics="Entry extends { id: number; at: string }">
  import type { Snippet } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { formatClock, formatDay } from '../format';
  import { intlLocale, translations } from '../i18n.svelte';
  import { FilterDismiss, ArrowSync, Settings, Warning, type IconComponent } from '../icons';
  import type { LogView } from '../types';
  import { intParam, setParams } from '../urlState.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import LogSettingsDialog from './LogSettingsDialog.svelte';
  import PaginationBar from './PaginationBar.svelte';

  // 관찰의 요청 기록 카드(접근 기록, 실패 요청). 머리글에 사용 중인 크기, 새로 고침, 기록 설정, 기록 스위치를 두고,
  // 본문은 필터 줄, 표, 위·아래 PaginationBar입니다. 기록은 서버(GET /api/v1/observe/{kind}-log)가 페이지로 나눠 주며
  // page·per_page와 필터는 주소의 query가 단일 출처입니다. 필터를 바꾸면 부모가 page를 비웁니다.
  // 부모는 필터 값(filters), 필터 입력(filterControls), 첫 열(시각) 다음의 열(columns)과 한 줄의 셀(cells)을 채웁니다.
  let { kind, title, tip, emptyTitle, offTitle, offHint, emptyIcon: EmptyIcon, loadFailed, filters, filterControls, columns, cells }: {
    kind: 'access' | 'failure';
    title: string;
    tip: string;
    emptyTitle: string;
    offTitle: string;
    offHint: string;
    emptyIcon: IconComponent;
    loadFailed: (code: string) => string;
    // 지금 적용 중인 필터 값(주소 query의 이름 → 값). 빈 문자열은 필터 없음입니다.
    filters: Record<string, string>;
    filterControls: Snippet;
    columns: Array<{ label: string; class?: string }>;
    cells: Snippet<[Entry]>;
  } = $props();

  const t = translations({
    enabled: 'Logging',
    refresh: 'Refresh',
    settings: 'Log settings',
    time: 'Time',
    reset: 'Reset filters',
    filteredEmpty: 'No matching entries',
    unavailable: 'The log file couldn\'t be opened, so nothing is logged.',
    dropped: (count: string) => `${count} entries weren't logged because the queue was full (since the Outlet started).`,
    on: 'Logging enabled',
    off: 'Logging disabled',
    errors: { log_unavailable: 'The log file couldn\'t be opened.' } as Record<string, string>,
    failed: (code: string) => `Couldn't change logging: ${code}`,
  }, {
    enabled: '기록',
    refresh: '새로 고침',
    settings: '기록 설정',
    time: '시각',
    reset: '필터 초기화',
    filteredEmpty: '조건에 해당하는 기록이 없습니다',
    unavailable: '기록 파일을 열 수 없어 기록하지 않습니다.',
    dropped: (count: string) => `대기열 초과로 ${count}건을 기록하지 못했습니다(Outlet 시작 이후).`,
    on: '기록이 활성화되었습니다',
    off: '기록이 비활성화되었습니다',
    errors: { log_unavailable: '기록 파일을 열 수 없습니다.' },
    failed: (code: string) => `기록 설정 변경 중 오류가 발생했습니다: ${code}`,
  });

  const id = $derived(`${kind}-log`);
  const endpoint = $derived(`/api/v1/observe/${kind}-log`);
  const defaultPerPage = 20;

  const page = $derived(intParam('page', 1));
  const perPage = $derived(intParam('per_page', defaultPerPage, 1, 200));
  const filtered = $derived(Object.values(filters).some(Boolean));
  const query = $derived.by(() => {
    const params = new URLSearchParams({ page: String(page), per_page: String(perPage) });
    for (const [name, value] of Object.entries(filters)) if (value) params.set(name, value);
    return params.toString();
  });

  let view = $state<LogView<Entry> | null>(null);
  let loadError = $state('');
  let reloads = $state(0);
  let busy = $state(false);
  let settingsOpen = $state(false);

  $effect(() => {
    void reloads;
    const current = query;
    let active = true;
    api<LogView<Entry>>(`${endpoint}?${current}`)
      .then((next) => {
        if (!active) return;
        // Go는 빈 목록을 null로 보낼 수 있습니다.
        view = { ...next, entries: next.entries ?? [] };
        loadError = '';
        // 범위를 벗어난 페이지 번호는 첫 페이지로 되돌립니다.
        if (next.total > 0 && !next.entries?.length && page > 1) setParams({ page: null });
      })
      .catch((cause) => { if (active) loadError = loadFailed(errorMessage(cause)); });
    return () => { active = false; };
  });

  // 설정을 저장한 응답은 필터 없는 첫 페이지이므로, 설정과 크기만 반영하고 목록은 지금 조건으로 다시 받습니다.
  function adopt(next: LogView<unknown>): void {
    if (view) view = { ...view, settings: next.settings, available: next.available, size_bytes: next.size_bytes, dropped: next.dropped };
    reloads += 1;
  }

  async function toggle(enabled: boolean): Promise<void> {
    if (!view || busy) return;
    busy = true;
    try {
      adopt(await api<LogView<unknown>>(endpoint, { method: 'PUT', body: JSON.stringify({ ...view.settings, enabled }) }));
      toast.success(enabled ? t().on : t().off);
    } catch (cause) {
      const code = errorMessage(cause);
      toast.error(t().errors[code] ?? t().failed(code));
    } finally {
      busy = false;
    }
  }

  const setPage = (next: number): void => setParams({ page: next }, { page: 1 });

  function resetFilters(): void {
    setParams({ ...Object.fromEntries(Object.keys(filters).map((name) => [name, null])), page: null });
  }

  const number = (value: number): string => value.toLocaleString(intlLocale());
  // 크기는 MiB 한 자리까지만 보여 줍니다.
  const mib = (bytes: number): string => (bytes / 1048576).toLocaleString(intlLocale(), { maximumFractionDigits: 1 });
</script>

<Card.Root size="sm" role="region" aria-labelledby={`${id}-title`}>
  <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
    <Card.Title id={`${id}-title`} class="flex items-center gap-1">{title}<InfoTooltip id={`${id}-tip`} label={title} description={tip} /></Card.Title>
    {#if view}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <div class="flex items-center gap-2">
          <span class="me-1 text-xs text-muted-foreground tabular-nums">{mib(view.size_bytes)} MiB / {number(view.settings.max_mib)} MiB</span>
          <IconButton label={t().refresh} icon={ArrowSync} onclick={() => { reloads += 1; }} />
          <IconButton label={t().settings} icon={Settings} onclick={() => { settingsOpen = true; }} />
          <Field.Label for={`${id}-enabled`} class="text-muted-foreground">{t().enabled}</Field.Label>
          <Switch id={`${id}-enabled`} size="sm" bind:checked={() => view?.settings.enabled ?? false, toggle} disabled={busy || !view.available} />
        </div>
      </Card.Action>
    {:else if loadError}
      <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-start-2 @max-md/card-header:justify-self-start">
        <IconButton label={t().refresh} icon={ArrowSync} onclick={() => { reloads += 1; }} />
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid min-w-0 gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !view}
      <Skeleton class="h-96 w-full" />
    {:else}
      {#if !view.available}
        <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{t().unavailable}</Alert.Description></Alert.Root>
      {:else if view.dropped > 0}
        <Alert.Root><Warning aria-hidden="true" /><Alert.Description>{t().dropped(number(view.dropped))}</Alert.Description></Alert.Root>
      {/if}
      <div class="flex flex-wrap items-center gap-2">
        {@render filterControls()}
        {#if filtered}<IconButton variant="outline" label={t().reset} icon={FilterDismiss} onclick={resetFilters} />{/if}
      </div>
      {#if view.entries.length}
        <PaginationBar total={view.total} {perPage} bind:page={() => page, setPage} placement="top" />
        <div class="min-w-0 border">
          <Table.Root class="min-w-160 lg:table-fixed">
            <Table.Header class="bg-muted">
              <Table.Row class="hover:bg-transparent">
                <Table.Head class="w-28 pl-4">{t().time}</Table.Head>
                {#each columns as column, index (index)}
                  <Table.Head class={[column.class, index === columns.length - 1 && 'pr-4']}>{column.label}</Table.Head>
                {/each}
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each view.entries as entry (entry.id)}
                <Table.Row>
                  <Table.Cell class="pl-4 align-top">
                    <div class="grid gap-0.5 tabular-nums">
                      <span>{formatClock(entry.at)}</span>
                      <span class="text-muted-foreground">{formatDay(entry.at)}</span>
                    </div>
                  </Table.Cell>
                  {@render cells(entry)}
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        </div>
        <PaginationBar total={view.total} {perPage} bind:page={() => page, setPage} />
      {:else if view.total > 0}
        <Skeleton class="h-96 w-full" />
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><EmptyIcon class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{filtered ? t().filteredEmpty : view.settings.enabled ? emptyTitle : offTitle}</Empty.Title>
            {#if !filtered && !view.settings.enabled}<Empty.Description>{offHint}</Empty.Description>{/if}
          </Empty.Header>
          {#if filtered}<Empty.Content><Button variant="outline" onclick={resetFilters}><FilterDismiss aria-hidden="true" />{t().reset}</Button></Empty.Content>{/if}
        </Empty.Root>
      {/if}
    {/if}
  </Card.Content>
</Card.Root>

{#if view}
  <LogSettingsDialog bind:open={settingsOpen} {kind} settings={view.settings} onSaved={adopt} />
{/if}
