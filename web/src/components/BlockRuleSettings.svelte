<script lang="ts">
  import { onMount, tick } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Add12, Dismiss, Shield20, Warning } from '../icons';
  import type { BlockCondition, BlockOperator, BlockRule, BlockRulesView } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';

  // 설정 > 보안의 차단 규칙 카드. 경로가 조건과 일치하는 요청을 403으로 거부합니다.
  // 스위치와 대화상자의 저장·삭제는 모두 바로 서버에 저장하고, 응답으로 목록을 다시 채웁니다.
  const t = translations({
    title: 'Block rules',
    tip: 'Requests for public addresses whose path matches a rule get 403 before they reach a tunnel. Paths are decoded, normalized and lowercased before matching, and /.well-known/ is never blocked. The admin host and Connector API are not affected. Blocked counts are since the Outlet started.',
    add: 'Add rule',
    enabled: 'Enabled',
    name: 'Name',
    appliesTo: 'Applies to',
    conditions: 'Conditions',
    blocked: 'Blocked',
    allAddresses: 'All public addresses',
    noRules: 'No block rules',
    addTitle: 'Add rule',
    editTitle: 'Edit rule',
    ruleName: 'Rule name',
    selectedAddresses: 'Selected addresses',
    address: 'Address',
    addAddress: 'Add address',
    removeAddress: 'Remove address',
    hostPlaceholder: 'app.example.com or *.example.com',
    conditionsHint: 'Blocked when any condition matches',
    operator: 'Operator',
    value: 'Value',
    addCondition: 'Add condition',
    removeCondition: 'Remove condition',
    regexNote: 'RE2 syntax, case-insensitive',
    operators: { equals: 'equals', starts_with: 'starts with', ends_with: 'ends with', contains: 'contains', matches: 'matches regex' } as Record<BlockOperator, string>,
    placeholders: { equals: '/phpmyadmin', starts_with: '/cgi-bin/', ends_with: '.php', contains: '/wp-', matches: '\\.(php|asp)$' } as Record<BlockOperator, string>,
    presets: { 'hidden-files': 'Hidden files', scanners: 'Vulnerability scanners', 'unused-paths': 'Paths this site does not use' } as Record<string, string>,
    delete: 'Delete',
    deleteTitle: 'Delete this rule?',
    deleted: 'Deleted',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the rule: addresses are hostnames or *.domain, equals and starts with need a leading /, and regular expressions must use RE2 syntax.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load block rules: ${code}`,
  }, {
    title: '차단 규칙',
    tip: '경로가 조건과 일치하는 공개 주소 요청은 터널로 전달되기 전에 403으로 차단됩니다. 경로는 디코딩 및 정규화 후 소문자로 변환하여 비교하며, /.well-known/ 경로는 차단하지 않습니다. 관리 호스트 및 커넥터 API에는 적용되지 않습니다. 차단 수는 Outlet 시작 이후 기준입니다.',
    add: '규칙 추가',
    enabled: '활성화',
    name: '이름',
    appliesTo: '적용 대상',
    conditions: '조건',
    blocked: '차단 수',
    allAddresses: '모든 공개 주소',
    noRules: '차단 규칙이 없습니다',
    addTitle: '규칙 추가',
    editTitle: '규칙 편집',
    ruleName: '규칙 이름',
    selectedAddresses: '지정 주소',
    address: '주소',
    addAddress: '주소 추가',
    removeAddress: '주소 삭제',
    hostPlaceholder: 'app.example.com 또는 *.example.com',
    conditionsHint: '조건 중 하나 이상 일치 시 차단',
    operator: '연산자',
    value: '값',
    addCondition: '조건 추가',
    removeCondition: '조건 삭제',
    regexNote: 'RE2 구문, 대소문자 구분 없음',
    operators: { equals: '같음', starts_with: '다음으로 시작', ends_with: '다음으로 끝남', contains: '포함', matches: '정규식과 일치' },
    placeholders: { equals: '/phpmyadmin', starts_with: '/cgi-bin/', ends_with: '.php', contains: '/wp-', matches: '\\.(php|asp)$' },
    presets: { 'hidden-files': '숨김 파일', scanners: '취약점 스캐너', 'unused-paths': '미사용 경로' },
    delete: '삭제',
    deleteTitle: '이 규칙을 삭제하시겠습니까?',
    deleted: '삭제되었습니다',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '규칙 입력값이 올바르지 않습니다. 주소는 도메인 이름 또는 *.도메인 형식이어야 하며, 같음 및 다음으로 시작 조건은 /로 시작해야 하고, 정규식은 RE2 구문만 지원합니다.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `차단 규칙을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const maxRules = 50;
  const maxConditions = 50;
  const operators: BlockOperator[] = ['equals', 'starts_with', 'ends_with', 'contains', 'matches'];
  // 기본 규칙의 이름은 영어로 저장되어 있어, 이름을 바꾸지 않은 기본 규칙만 화면 언어로 보여줍니다.
  const presetNames: Record<string, string> = { 'hidden-files': 'Hidden files', scanners: 'Vulnerability scanners', 'unused-paths': 'Paths this site does not use' };
  const displayName = (rule: BlockRule): string => (presetNames[rule.id] === rule.name ? t().presets[rule.id] ?? rule.name : rule.name);
  const operatorLabel = (operator: BlockOperator): string => t().operators[operator] ?? operator;
  const describe = (code: string): string => (code === 'invalid_block_rules' ? t().invalid : t().failed(code));

  // 대화상자의 조건 줄은 지워도 입력 칸이 섞이지 않도록 고유한 key를 가집니다.
  type ConditionRow = BlockCondition & { key: number };
  let nextKey = 0;
  const newRow = (condition: BlockCondition = { operator: 'starts_with', value: '' }): ConditionRow => ({ ...condition, key: nextKey++ });
  type HostRow = { key: number; value: string };
  const newHost = (value = ''): HostRow => ({ key: nextKey++, value });
  const maxHosts = 200;

  let rules = $state<BlockRule[] | null>(null);
  let hits = $state<Record<string, number>>({});
  let addresses = $state<string[]>([]);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  let dialogOpen = $state(false);
  let confirmOpen = $state(false);
  let deleteName = $state('');
  let editingId = $state('');
  // 규칙이 이미 가진 호스트는 공개 주소 목록에 없어도 선택지에 남깁니다.
  let form = $state<{ name: string; scope: 'all' | 'selected'; hosts: HostRow[]; conditions: ConditionRow[] }>({ name: '', scope: 'all', hosts: [], conditions: [] });
  let formError = $state('');

  const editing = $derived(editingId ? rules?.find((rule) => rule.id === editingId) : undefined);
  const hasRegex = $derived(form.conditions.some((condition) => condition.operator === 'matches'));

  const adopt = (view: BlockRulesView) => {
    rules = view.rules;
    hits = view.hits ?? {};
    addresses = view.addresses ?? [];
  };

  async function put(next: BlockRule[]): Promise<void> {
    adopt(await api<BlockRulesView>('/api/v1/security/block-rules', { method: 'PUT', body: JSON.stringify({ rules: next }) }));
  }

  onMount(() => {
    api<BlockRulesView>('/api/v1/security/block-rules').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function toggle(rule: BlockRule, enabled: boolean): Promise<void> {
    if (!rules || busy) return;
    busy = true;
    error = '';
    try {
      await put($state.snapshot(rules).map((entry) => (entry.id === rule.id ? { ...entry, enabled } : entry)));
    } catch (cause) {
      rule.enabled = !enabled;
      error = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  function openRule(rule?: BlockRule): void {
    editingId = rule?.id ?? '';
    form = {
      name: rule ? displayName(rule) : '',
      scope: rule?.hosts.length ? 'selected' : 'all',
      hosts: rule ? rule.hosts.map((host) => newHost(host)) : [],
      conditions: rule ? rule.conditions.map((condition) => newRow(condition)) : [newRow()],
    };
    formError = '';
    dialogOpen = true;
  }

  // Choosing "selected addresses" starts with one empty row, so a rule
  // never targets an empty list.
  function chooseScope(scope: string): void {
    form.scope = scope === 'selected' ? 'selected' : 'all';
    if (form.scope === 'selected' && !form.hosts.length) void addHost();
  }

  async function addHost(): Promise<void> {
    const row = newHost();
    form.hosts.push(row);
    await tick();
    document.getElementById(`block-rule-host-${row.key}`)?.focus();
  }

  async function addCondition(): Promise<void> {
    const row = newRow();
    form.conditions.push(row);
    await tick();
    document.getElementById(`block-rule-value-${row.key}`)?.focus();
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!rules || busy) return;
    const current = editing;
    const typed = form.name.trim();
    const next: BlockRule = {
      id: current?.id ?? '',
      // 기본 규칙의 이름을 그대로 두면 저장된 영어 이름을 유지합니다.
      name: current && typed === displayName(current) ? current.name : typed,
      enabled: current?.enabled ?? true,
      hosts: form.scope === 'selected' ? form.hosts.map((row) => row.value.trim().toLowerCase()).filter(Boolean) : [],
      conditions: form.conditions.map((condition) => ({ operator: condition.operator, value: condition.value.trim() })),
    };
    const list = $state.snapshot(rules);
    busy = true;
    formError = '';
    try {
      await put(current ? list.map((entry) => (entry.id === current.id ? next : entry)) : [...list, next]);
      dialogOpen = false;
      toast.success(t().saved);
    } catch (cause) {
      formError = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  async function remove(): Promise<void> {
    if (!rules || !editing || busy) return;
    const id = editing.id;
    busy = true;
    formError = '';
    try {
      await put($state.snapshot(rules).filter((entry) => entry.id !== id));
      confirmOpen = false;
      dialogOpen = false;
      toast.success(t().deleted);
    } catch (cause) {
      confirmOpen = false;
      formError = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
  }
</script>

<Card.Root size="sm" role="region" aria-labelledby="block-rules-title">
  <Card.Header>
    <Card.Title id="block-rules-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="block-rules-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if rules}
      <Card.Action>
        <Button variant="outline" size="sm" disabled={rules.length >= maxRules} onclick={() => openRule()}><Add12 aria-hidden="true" />{t().add}</Button>
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !rules}
      <Skeleton class="h-48 w-full" />
    {:else}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      {#if rules.length}
        <div class="overflow-x-auto border">
          <Table.Root>
            <Table.Header class="bg-muted">
              <Table.Row class="hover:bg-transparent">
                <Table.Head class="w-12 pl-4"><span class="sr-only">{t().enabled}</span></Table.Head>
                <Table.Head>{t().name}</Table.Head>
                <Table.Head>{t().appliesTo}</Table.Head>
                <Table.Head>{t().conditions}</Table.Head>
                <Table.Head class="pr-4 text-right">{t().blocked}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each rules as rule (rule.id)}
                <Table.Row class="relative cursor-pointer">
                  <Table.Cell class="pl-4">
                    <Switch size="sm" class="z-10" bind:checked={rule.enabled} disabled={busy} aria-label={displayName(rule)} onCheckedChange={(checked) => toggle(rule, checked)} />
                  </Table.Cell>
                  <Table.Cell>
                    <button type="button" class="block max-w-56 truncate text-left text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50" onclick={() => openRule(rule)}>{displayName(rule)}</button>
                  </Table.Cell>
                  <Table.Cell>
                    {#if rule.hosts.length}
                      <span class="flex items-center gap-1.5">
                        <span class="max-w-48 truncate font-mono">{rule.hosts[0]}</span>
                        {#if rule.hosts.length > 1}<span class="text-muted-foreground tabular-nums">+{(rule.hosts.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      </span>
                    {:else}
                      {t().allAddresses}
                    {/if}
                  </Table.Cell>
                  <Table.Cell>
                    {#if rule.conditions.length}
                      <span class="flex items-center gap-1.5">
                        <span class="text-muted-foreground">{operatorLabel(rule.conditions[0].operator)}</span>
                        <span class="max-w-48 truncate font-mono">{rule.conditions[0].value}</span>
                        {#if rule.conditions.length > 1}<span class="text-muted-foreground tabular-nums">+{(rule.conditions.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      </span>
                    {/if}
                  </Table.Cell>
                  <Table.Cell class="pr-4 text-right tabular-nums">{(hits[rule.id] ?? 0).toLocaleString(intlLocale())}</Table.Cell>
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
    {/if}
  </Card.Content>
</Card.Root>

<Dialog.Root bind:open={() => dialogOpen, (open) => { if (!busy) dialogOpen = open; }}>
  <Dialog.Content class="max-h-[calc(100dvh-2rem)] overflow-y-auto sm:max-w-xl">
    <form class="grid gap-4" onsubmit={submit}>
      <Dialog.Header>
        <Dialog.Title>{editingId ? t().editTitle : t().addTitle}</Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field>
          <Field.Label for="block-rule-name">{t().ruleName}</Field.Label>
          <Input id="block-rule-name" bind:value={form.name} maxlength={64} pattern=".*\S.*" autocomplete="off" required />
        </Field.Field>
        <Field.Set>
          <Field.Legend variant="label">{t().appliesTo}</Field.Legend>
          <RadioGroup.Root value={form.scope} onValueChange={chooseScope}>
            <Field.Field orientation="horizontal">
              <RadioGroup.Item value="all" id="block-rule-scope-all" />
              <Field.Label for="block-rule-scope-all" class="font-normal">{t().allAddresses}</Field.Label>
            </Field.Field>
            <Field.Field orientation="horizontal">
              <RadioGroup.Item value="selected" id="block-rule-scope-selected" />
              <Field.Label for="block-rule-scope-selected" class="font-normal">{t().selectedAddresses}</Field.Label>
            </Field.Field>
          </RadioGroup.Root>
          {#if form.scope === 'selected'}
            <div class="grid gap-2">
              {#each form.hosts as row, index (row.key)}
                <div class="flex items-center gap-2">
                  <Input
                    id={`block-rule-host-${row.key}`}
                    class="min-w-0 flex-1 font-mono"
                    list="block-rule-addresses"
                    bind:value={row.value}
                    placeholder={t().hostPlaceholder}
                    aria-label={`${t().address} ${index + 1}`}
                    autocomplete="off"
                    autocapitalize="none"
                    spellcheck={false}
                    pattern=".*\S.*"
                    required
                  />
                  <IconButton label={t().removeAddress} icon={Dismiss} disabled={form.hosts.length <= 1} onclick={() => form.hosts.splice(index, 1)} />
                </div>
              {/each}
            </div>
            <!-- 등록된 공개 주소를 자동 완성 후보로 보여 줍니다. 직접 입력과 와일드카드도 받습니다. -->
            <datalist id="block-rule-addresses">
              {#each addresses as address (address)}<option value={address}></option>{/each}
            </datalist>
            <div>
              <Button variant="outline" size="sm" disabled={form.hosts.length >= maxHosts} onclick={addHost}><Add12 aria-hidden="true" />{t().addAddress}</Button>
            </div>
          {/if}
        </Field.Set>
        <Field.Set>
          <Field.Legend variant="label">{t().conditions}</Field.Legend>
          <Field.Description>{t().conditionsHint}</Field.Description>
          <div class="grid gap-2">
            {#each form.conditions as condition, index (condition.key)}
              <div class="flex flex-wrap items-center gap-2">
                <NativeSelect.Root class="w-44" aria-label={`${t().operator} ${index + 1}`} bind:value={condition.operator}>
                  {#each operators as operator (operator)}
                    <NativeSelect.Option value={operator}>{operatorLabel(operator)}</NativeSelect.Option>
                  {/each}
                </NativeSelect.Root>
                <Input
                  id={`block-rule-value-${condition.key}`}
                  class="min-w-40 flex-1 font-mono"
                  bind:value={condition.value}
                  placeholder={t().placeholders[condition.operator]}
                  aria-label={`${t().value} ${index + 1}`}
                  autocomplete="off"
                  autocapitalize="none"
                  spellcheck={false}
                  pattern=".*\S.*"
                  required
                />
                <IconButton label={t().removeCondition} icon={Dismiss} disabled={form.conditions.length <= 1} onclick={() => form.conditions.splice(index, 1)} />
              </div>
            {/each}
          </div>
          {#if hasRegex}<Field.Description>{t().regexNote}</Field.Description>{/if}
          <div>
            <Button variant="outline" size="sm" disabled={form.conditions.length >= maxConditions} onclick={addCondition}><Add12 aria-hidden="true" />{t().addCondition}</Button>
          </div>
        </Field.Set>
      </Field.Group>
      {#if formError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{formError}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        {#if editingId}
          <Button variant="destructive" disabled={busy} onclick={() => { deleteName = editing ? displayName(editing) : ''; confirmOpen = true; }}>{t().delete}</Button>
        {/if}
        <div class="flex flex-col-reverse gap-2 sm:ml-auto sm:flex-row">
          <Button variant="outline" disabled={busy} onclick={() => (dialogOpen = false)}>{t().cancel}</Button>
          <Button type="submit" disabled={busy}>{#if busy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
        </div>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={() => confirmOpen, (open) => { if (!busy) confirmOpen = open; }}>
  <AlertDialog.Content>
    <AlertDialog.Header>
      <AlertDialog.Title>{t().deleteTitle}</AlertDialog.Title>
      <AlertDialog.Description>{deleteName}</AlertDialog.Description>
    </AlertDialog.Header>
    <AlertDialog.Footer>
      <AlertDialog.Cancel disabled={busy}>{t().cancel}</AlertDialog.Cancel>
      <Button variant="destructive" disabled={busy} onclick={remove}>{#if busy}<Spinner aria-hidden="true" />{/if}{t().delete}</Button>
    </AlertDialog.Footer>
  </AlertDialog.Content>
</AlertDialog.Root>
