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
  import type { AccessPoliciesView, AccessPolicy, BlockCondition, BlockOperator } from '../types';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 설정 > 보안의 접근 정책 카드. 주소(또는 그 아래 일부 경로)를 접근 보호 SSO 로그인 뒤에 두고, 들어올 수 있는 사람을 지정합니다.
  // 스위치와 대화상자의 저장·삭제는 모두 바로 서버에 저장하고, 응답으로 목록을 다시 채웁니다.
  // providerEnabled는 접근 보호 SSO가 켜져 있는지(알 수 없으면 null)로, 꺼져 있으면 정책이 적용되지 않는다고 안내합니다.
  let { providerEnabled = null }: { providerEnabled?: boolean | null } = $props();

  const t = translations({
    title: 'Access policies',
    tip: 'Requests that match any enabled policy need a sign-in with the access SSO provider before they reach a tunnel. A request must satisfy every policy that covers it. Within a policy, allowed users and allowed groups must both match when both are set, and Allow administrators also lets in anyone whose email is a Meerkat administrator\'s. The signed-in email reaches the app in the X-Meerkat-Access-Email header.',
    providerOff: 'Access SSO is off, so no one can sign in to protected addresses.',
    damaged: 'Saved policies are invalid, so their addresses are closed.',
    damagedEmpty: 'Saved policies can\'t be read, so every public address is closed.',
    saveAgain: 'Save again',
    add: 'Add policy',
    enabled: 'Enabled',
    name: 'Name',
    addresses: 'Addresses',
    paths: 'Paths',
    allowed: 'Allowed',
    noPolicies: 'No access policies',
    addTitle: 'Add policy',
    editTitle: 'Edit policy',
    policyName: 'Policy name',
    address: 'Address',
    addAddress: 'Add address',
    removeAddress: 'Remove address',
    hostPlaceholder: 'app.example.com or *.example.com',
    allPaths: 'All paths',
    selectedPaths: 'Selected paths',
    conditionsHint: 'Sign-in required when any condition matches',
    operator: 'Operator',
    value: 'Value',
    addCondition: 'Add condition',
    removeCondition: 'Remove condition',
    regexNote: 'RE2 syntax, case-insensitive',
    operators: { equals: 'equals', starts_with: 'starts with', ends_with: 'ends with', contains: 'contains', matches: 'matches regex' } as Record<BlockOperator, string>,
    placeholders: { equals: '/admin', starts_with: '/private/', ends_with: '.pdf', contains: '/internal', matches: '^/(admin|reports)/' } as Record<BlockOperator, string>,
    who: 'Who can access',
    allowedUsers: 'Allowed users',
    allowedUsersHint: 'Email addresses or @domain.',
    allowedGroups: 'Allowed groups',
    allowedGroupsHint: 'With allowed users also set, both must match.',
    allowAdmins: 'Allow administrators',
    allowAdminsTip: 'Anyone whose email, as verified by the access SSO provider, is a Meerkat administrator\'s is let in, whether or not they match the allowed users and groups.',
    administrators: 'Administrators',
    delete: 'Delete',
    deleteTitle: 'Delete this policy?',
    deleted: 'Deleted',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the policy: addresses are hostnames or *.domain, equals and starts with need a leading /, regular expressions must use RE2 syntax, and the policy must allow at least one user, group or administrators.',
    needsWho: 'Add an allowed user or group, or allow administrators.',
    decodedPaths: 'Enter paths without %-escapes or ;parameters.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load access policies: ${code}`,
  }, {
    title: '접근 정책',
    tip: '활성화된 정책 중 하나라도 일치하는 요청은 터널로 전달되기 전에 접근 보호 SSO 로그인이 필요합니다. 요청은 해당 요청에 적용되는 모든 정책을 충족해야 합니다. 한 정책에서 허용 사용자와 허용 그룹을 모두 지정한 경우 두 조건을 모두 충족해야 하며, 관리자 허용을 활성화하면 이메일이 Meerkat 관리자의 이메일인 사용자도 허용됩니다. 로그인한 사용자의 이메일은 X-Meerkat-Access-Email 헤더로 앱에 전달됩니다.',
    providerOff: '접근 보호 SSO가 비활성화되어 보호 대상 주소에 로그인할 수 없습니다.',
    damaged: '저장된 정책이 올바르지 않아 해당 주소를 차단 중입니다.',
    damagedEmpty: '저장된 정책을 읽을 수 없어 모든 공개 주소를 차단 중입니다.',
    saveAgain: '다시 저장',
    add: '정책 추가',
    enabled: '활성화',
    name: '이름',
    addresses: '적용 주소',
    paths: '경로',
    allowed: '허용 대상',
    noPolicies: '접근 정책이 없습니다',
    addTitle: '정책 추가',
    editTitle: '정책 편집',
    policyName: '정책 이름',
    address: '주소',
    addAddress: '주소 추가',
    removeAddress: '주소 삭제',
    hostPlaceholder: 'app.example.com 또는 *.example.com',
    allPaths: '전체 경로',
    selectedPaths: '선택한 경로',
    conditionsHint: '조건 중 하나 이상 일치 시 로그인 필요',
    operator: '연산자',
    value: '값',
    addCondition: '조건 추가',
    removeCondition: '조건 삭제',
    regexNote: 'RE2 구문, 대소문자 구분 없음',
    operators: { equals: '같음', starts_with: '다음으로 시작', ends_with: '다음으로 끝남', contains: '포함', matches: '정규식과 일치' },
    placeholders: { equals: '/admin', starts_with: '/private/', ends_with: '.pdf', contains: '/internal', matches: '^/(admin|reports)/' },
    who: '접근 허용 대상',
    allowedUsers: '허용 사용자',
    allowedUsersHint: '이메일 주소 또는 @도메인',
    allowedGroups: '허용 그룹',
    allowedGroupsHint: '허용 사용자와 함께 지정 시 두 조건 모두 충족 필요',
    allowAdmins: '관리자 허용',
    allowAdminsTip: '접근 보호 SSO에서 인증된 이메일이 Meerkat 관리자 이메일과 같은 사용자는 허용 사용자·그룹 조건과 무관하게 접근할 수 있습니다.',
    administrators: '관리자',
    delete: '삭제',
    deleteTitle: '이 정책을 삭제하시겠습니까?',
    deleted: '삭제되었습니다',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '정책 입력값이 올바르지 않습니다. 주소는 도메인 이름 또는 *.도메인 형식이어야 하며, 같음 및 다음으로 시작 조건은 /로 시작해야 하고, 정규식은 RE2 구문만 지원합니다. 허용 사용자, 허용 그룹, 관리자 허용 중 하나 이상이 필요합니다.',
    needsWho: '허용 사용자, 허용 그룹, 관리자 허용 중 하나 이상을 지정하십시오.',
    decodedPaths: '경로는 % 인코딩과 ;매개변수 없이 입력하십시오.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `접근 정책을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const maxPolicies = 100;
  const maxConditions = 50;
  const maxHosts = 100;
  const operators: BlockOperator[] = ['equals', 'starts_with', 'ends_with', 'contains', 'matches'];
  const operatorLabel = (operator: BlockOperator): string => t().operators[operator] ?? operator;
  const describe = (code: string): string => (code === 'invalid_access_policies' ? t().invalid : t().failed(code));

  // 대화상자의 줄은 지워도 입력 칸이 섞이지 않도록 고유한 key를 가집니다.
  type ConditionRow = BlockCondition & { key: number };
  type HostRow = { key: number; value: string };
  let nextKey = 0;
  const newRow = (condition: BlockCondition = { operator: 'starts_with', value: '' }): ConditionRow => ({ ...condition, key: nextKey++ });
  const newHost = (value = ''): HostRow => ({ key: nextKey++, value });

  // 허용 대상 열은 첫 항목만 보여주고 나머지는 개수로 줄입니다.
  const allowedEntries = (policy: AccessPolicy): Array<{ text: string; mono: boolean }> => [
    ...policy.allowed_users.map((text) => ({ text, mono: true })),
    ...policy.allowed_groups.map((text) => ({ text, mono: true })),
    ...(policy.allow_admins ? [{ text: t().administrators, mono: false }] : []),
  ];

  let policies = $state<AccessPolicy[] | null>(null);
  let addresses = $state<string[]>([]);
  let damaged = $state(false);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  let dialogOpen = $state(false);
  let confirmOpen = $state(false);
  let deleteName = $state('');
  let editingId = $state('');
  // 선택한 경로의 조건 줄은 항상 하나 이상 남겨 두어, "선택한 경로"를 고르면 바로 입력할 수 있게 합니다.
  let form = $state<{ name: string; enabled: boolean; hosts: HostRow[]; paths: 'all' | 'selected'; conditions: ConditionRow[]; users: string[]; groups: string[]; admins: boolean }>({
    name: '', enabled: true, hosts: [], paths: 'all', conditions: [], users: [], groups: [], admins: false,
  });
  let formError = $state('');

  const editing = $derived(editingId ? policies?.find((policy) => policy.id === editingId) : undefined);
  const hasRegex = $derived(form.paths === 'selected' && form.conditions.some((condition) => condition.operator === 'matches'));

  const adopt = (view: AccessPoliciesView) => {
    policies = view.policies;
    addresses = view.addresses ?? [];
    damaged = view.damaged ?? false;
  };

  // 손상된 저장값은 지금 목록을 그대로 다시 저장해 복구합니다. 올바르지 않은 정책이 남아 있으면 오류를 보여줍니다.
  async function saveAgain(): Promise<void> {
    if (!policies || busy) return;
    busy = true;
    error = '';
    try {
      await put($state.snapshot(policies));
      toast.success(t().saved);
    } catch (cause) {
      error = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  async function put(next: AccessPolicy[]): Promise<void> {
    adopt(await api<AccessPoliciesView>('/api/v1/security/access-policies', { method: 'PUT', body: JSON.stringify({ policies: next }) }));
  }

  onMount(() => {
    api<AccessPoliciesView>('/api/v1/security/access-policies').then(adopt).catch((cause) => { loadError = t().loadFailed(errorMessage(cause)); });
  });

  async function toggle(policy: AccessPolicy, enabled: boolean): Promise<void> {
    if (!policies || busy) return;
    busy = true;
    error = '';
    try {
      await put($state.snapshot(policies).map((entry) => (entry.id === policy.id ? { ...entry, enabled } : entry)));
    } catch (cause) {
      policy.enabled = !enabled;
      error = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  function openPolicy(policy?: AccessPolicy): void {
    editingId = policy?.id ?? '';
    form = {
      name: policy?.name ?? '',
      enabled: policy?.enabled ?? true,
      hosts: policy ? policy.hosts.map((host) => newHost(host)) : [newHost()],
      paths: policy && !policy.all_paths ? 'selected' : 'all',
      conditions: policy?.conditions.length ? policy.conditions.map((condition) => newRow(condition)) : [newRow()],
      users: policy ? [...policy.allowed_users] : [],
      groups: policy ? [...policy.allowed_groups] : [],
      admins: policy?.allow_admins ?? false,
    };
    formError = '';
    dialogOpen = true;
  }

  async function addHost(): Promise<void> {
    const row = newHost();
    form.hosts.push(row);
    await tick();
    document.getElementById(`access-policy-host-${row.key}`)?.focus();
  }

  async function addCondition(): Promise<void> {
    const row = newRow();
    form.conditions.push(row);
    await tick();
    document.getElementById(`access-policy-value-${row.key}`)?.focus();
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!policies || busy) return;
    if (!form.users.length && !form.groups.length && !form.admins) {
      formError = t().needsWho;
      return;
    }
    // 요청 경로는 디코딩하고 ;매개변수를 뺀 형태로 비교하므로, 그런 값은 어떤 요청과도 일치하지 않습니다.
    if (form.paths === 'selected' && form.conditions.some((condition) => condition.operator !== 'matches' && /%[0-9a-f]{2}|;/i.test(condition.value))) {
      formError = t().decodedPaths;
      return;
    }
    const current = editing;
    const next: AccessPolicy = {
      id: current?.id ?? '',
      name: form.name.trim(),
      enabled: form.enabled,
      hosts: form.hosts.map((row) => row.value.trim().toLowerCase()).filter(Boolean),
      all_paths: form.paths === 'all',
      conditions: form.paths === 'selected' ? form.conditions.map((condition) => ({ operator: condition.operator, value: condition.value.trim() })) : [],
      allowed_users: [...form.users],
      allowed_groups: [...form.groups],
      allow_admins: form.admins,
    };
    const list = $state.snapshot(policies);
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
    if (!policies || !editing || busy) return;
    const id = editing.id;
    busy = true;
    formError = '';
    try {
      await put($state.snapshot(policies).filter((entry) => entry.id !== id));
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

<Card.Root size="sm" role="region" aria-labelledby="access-policies-title">
  <Card.Header>
    <Card.Title id="access-policies-title" class="flex items-center gap-1">{t().title}<InfoTooltip id="access-policies-tip" label={t().title} description={t().tip} /></Card.Title>
    {#if policies}
      <Card.Action>
        <Button variant="outline" size="sm" disabled={policies.length >= maxPolicies} onclick={() => openPolicy()}><Add12 aria-hidden="true" />{t().add}</Button>
      </Card.Action>
    {/if}
  </Card.Header>
  <Card.Content class="grid gap-4">
    {#if loadError}
      <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{loadError}</Alert.Description></Alert.Root>
    {:else if !policies}
      <Skeleton class="h-40 w-full" />
    {:else}
      {#if damaged}
        <Alert.Root variant="destructive">
          <Warning aria-hidden="true" />
          <Alert.Description class="break-keep pr-24">{policies.length ? t().damaged : t().damagedEmpty}</Alert.Description>
          <Alert.Action><Button variant="outline" size="sm" disabled={busy} onclick={saveAgain}>{t().saveAgain}</Button></Alert.Action>
        </Alert.Root>
      {/if}
      {#if providerEnabled === false}<Alert.Root><Warning aria-hidden="true" /><Alert.Description class="break-keep">{t().providerOff}</Alert.Description></Alert.Root>{/if}
      {#if error}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{error}</Alert.Description></Alert.Root>{/if}
      {#if policies.length}
        <div class="overflow-x-auto border">
          <Table.Root>
            <Table.Header class="bg-muted">
              <Table.Row class="hover:bg-transparent">
                <Table.Head class="w-12 pl-4"><span class="sr-only">{t().enabled}</span></Table.Head>
                <Table.Head>{t().name}</Table.Head>
                <Table.Head>{t().addresses}</Table.Head>
                <Table.Head>{t().paths}</Table.Head>
                <Table.Head class="pr-4">{t().allowed}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each policies as policy (policy.id)}
                {@const entries = allowedEntries(policy)}
                <Table.Row class="relative cursor-pointer">
                  <Table.Cell class="pl-4">
                    <Switch size="sm" class="z-10" bind:checked={policy.enabled} disabled={busy} aria-label={policy.name} onCheckedChange={(checked) => toggle(policy, checked)} />
                  </Table.Cell>
                  <Table.Cell>
                    <button type="button" class="block max-w-56 truncate text-left text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50" onclick={() => openPolicy(policy)}>{policy.name}</button>
                  </Table.Cell>
                  <Table.Cell>
                    <span class="flex items-center gap-1.5">
                      <span class="max-w-48 truncate font-mono">{policy.hosts[0]}</span>
                      {#if policy.hosts.length > 1}<span class="text-muted-foreground tabular-nums">+{(policy.hosts.length - 1).toLocaleString(intlLocale())}</span>{/if}
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    {#if policy.all_paths}
                      {t().allPaths}
                    {:else if policy.conditions.length}
                      <span class="flex items-center gap-1.5">
                        <span class="text-muted-foreground">{operatorLabel(policy.conditions[0].operator)}</span>
                        <span class="max-w-48 truncate font-mono">{policy.conditions[0].value}</span>
                        {#if policy.conditions.length > 1}<span class="text-muted-foreground tabular-nums">+{(policy.conditions.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      </span>
                    {/if}
                  </Table.Cell>
                  <Table.Cell class="pr-4">
                    {#if entries.length}
                      <span class="flex items-center gap-1.5">
                        <span class={['max-w-48 truncate', entries[0].mono && 'font-mono']}>{entries[0].text}</span>
                        {#if entries.length > 1}<span class="text-muted-foreground tabular-nums">+{(entries.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      </span>
                    {/if}
                  </Table.Cell>
                </Table.Row>
              {/each}
            </Table.Body>
          </Table.Root>
        </div>
      {:else}
        <Empty.Root class="border border-dashed">
          <Empty.Header>
            <Empty.Media variant="icon"><Shield20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{t().noPolicies}</Empty.Title>
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
          <Field.Label for="access-policy-name">{t().policyName}</Field.Label>
          <Input id="access-policy-name" bind:value={form.name} maxlength={64} pattern=".*\S.*" autocomplete="off" required />
        </Field.Field>
        <Field.Field orientation="horizontal">
          <Field.Content>
            <Field.Label for="access-policy-enabled">{t().enabled}</Field.Label>
          </Field.Content>
          <Switch id="access-policy-enabled" bind:checked={form.enabled} />
        </Field.Field>
        <Field.Set>
          <Field.Legend variant="label">{t().addresses}</Field.Legend>
          <div class="grid gap-2">
            {#each form.hosts as row, index (row.key)}
              <div class="flex items-center gap-2">
                <Input
                  id={`access-policy-host-${row.key}`}
                  class="min-w-0 flex-1 font-mono"
                  list="access-policy-addresses"
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
          <datalist id="access-policy-addresses">
            {#each addresses as address (address)}<option value={address}></option>{/each}
          </datalist>
          <div>
            <Button variant="outline" size="sm" disabled={form.hosts.length >= maxHosts} onclick={addHost}><Add12 aria-hidden="true" />{t().addAddress}</Button>
          </div>
        </Field.Set>
        <Field.Set>
          <Field.Legend variant="label">{t().paths}</Field.Legend>
          <RadioGroup.Root value={form.paths} onValueChange={(value) => { form.paths = value === 'selected' ? 'selected' : 'all'; }}>
            <Field.Field orientation="horizontal">
              <RadioGroup.Item value="all" id="access-policy-paths-all" />
              <Field.Label for="access-policy-paths-all" class="font-normal">{t().allPaths}</Field.Label>
            </Field.Field>
            <Field.Field orientation="horizontal">
              <RadioGroup.Item value="selected" id="access-policy-paths-selected" />
              <Field.Label for="access-policy-paths-selected" class="font-normal">{t().selectedPaths}</Field.Label>
            </Field.Field>
          </RadioGroup.Root>
          {#if form.paths === 'selected'}
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
                    id={`access-policy-value-${condition.key}`}
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
          {/if}
        </Field.Set>
        <Field.Set>
          <Field.Legend variant="label">{t().who}</Field.Legend>
          <Field.Field>
            <Field.Label for="access-policy-users">{t().allowedUsers}</Field.Label>
            <TagInput id="access-policy-users" ariaLabel={t().allowedUsers} bind:values={form.users} lowercase mono placeholder="user@example.com, @example.com" />
            <Field.Description>{t().allowedUsersHint}</Field.Description>
          </Field.Field>
          <Field.Field>
            <Field.Label for="access-policy-groups">{t().allowedGroups}</Field.Label>
            <TagInput id="access-policy-groups" ariaLabel={t().allowedGroups} bind:values={form.groups} mono placeholder="staff" />
            <Field.Description>{t().allowedGroupsHint}</Field.Description>
          </Field.Field>
          <Field.Field orientation="horizontal">
            <Field.Content>
              <div class="flex items-center gap-1"><Field.Label for="access-policy-admins">{t().allowAdmins}</Field.Label><InfoTooltip id="access-policy-admins-tip" label={t().allowAdmins} description={t().allowAdminsTip} /></div>
            </Field.Content>
            <Switch id="access-policy-admins" bind:checked={form.admins} />
          </Field.Field>
        </Field.Set>
      </Field.Group>
      {#if formError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{formError}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        {#if editingId}
          <Button variant="destructive" disabled={busy} onclick={() => { deleteName = editing?.name ?? ''; confirmOpen = true; }}>{t().delete}</Button>
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
