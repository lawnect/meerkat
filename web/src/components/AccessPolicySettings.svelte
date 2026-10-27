<script lang="ts">
  import { onMount, tick } from 'svelte';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import * as AlertDialog from '$lib/components/ui/alert-dialog/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import { Checkbox } from '$lib/components/ui/checkbox/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
  import { Skeleton } from '$lib/components/ui/skeleton/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Switch } from '$lib/components/ui/switch/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { toast } from 'svelte-sonner';
  import { api, errorMessage } from '../api';
  import { intlLocale, translations } from '../i18n.svelte';
  import { Add12, Dismiss, ReOrder, Shield20, Warning } from '../icons';
  import { accessMethods, type AccessAction, type AccessMethod, type AccessPoliciesView, type AccessPolicy, type AccessToken, type BlockCondition, type BlockOperator } from '../types';
  import AddressSelect from './AddressSelect.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 접근 보호 화면의 접근 정책 카드. 주소(또는 그 아래 일부 경로)마다 허용(로그인 후 허용 대상만)·우회(로그인 없이)·차단을 정합니다.
  // 정책은 위에서부터 확인해 처음 일치하는 하나가 적용되며, 순서는 손잡이를 드래그하거나 화살표 키로 바꿉니다.
  // 스위치, 순서 변경, 대화상자의 저장·삭제는 모두 바로 서버에 저장하고, 응답으로 목록을 다시 채웁니다.
  // providerEnabled는 접근 보호 SSO가 켜져 있는지(알 수 없으면 null)로, 꺼져 있으면 정책이 적용되지 않는다고 안내합니다.
  // tokens는 접근 토큰 목록(알 수 없으면 null)으로, 토큰 조건에서 토큰을 고르고 이름을 보여 주는 데 씁니다.
  let { providerEnabled = null, tokens = null }: { providerEnabled?: boolean | null; tokens?: AccessToken[] | null } = $props();

  const t = translations({
    title: 'Access policies',
    tip: 'Policies are checked from the top; the first enabled one that matches a request applies. Drag a policy to change the order. Allow lets in, after a sign-in with access SSO, only who the policy allows, Bypass lets anyone in without one, and Block lets nobody in. A policy that requires an access token matches only requests that send one. The signed-in email reaches the app in the X-Meerkat-Access-Email header, and the token\'s name in X-Meerkat-Access-Service.',
    providerOff: 'Access SSO is off, so no one can sign in to protected addresses.',
    damaged: 'Saved policies are invalid, so their addresses are closed.',
    damagedEmpty: 'Saved policies can\'t be read, so every public address is closed.',
    saveAgain: 'Save again',
    add: 'Add policy',
    enabled: 'Enabled',
    name: 'Name',
    addresses: 'Addresses',
    paths: 'Paths',
    pathsTip: 'Conditions apply to requests that reach the Outlet. A single-page app changes screens in the browser without one, so protect all of its paths and bypass only the paths its clients call.',
    action: 'Action',
    conditions: 'Conditions',
    actions: { allow: 'Allow', bypass: 'Bypass', block: 'Block' } as Record<AccessAction, string>,
    actionHints: { allow: 'Sign-in, then only who you allow', bypass: 'Anyone, without signing in', block: 'Nobody' } as Record<AccessAction, string>,
    tokenActionHints: { allow: 'The token and a sign-in of who you allow; requests without it go to the policies below', bypass: 'The token alone, without signing in; requests without it go to the policies below', block: 'Nobody with the token; requests without it go to the policies below' } as Record<AccessAction, string>,
    order: 'Order',
    reorder: (name: string) => `Move ${name}`,
    reorderTip: 'Drag, or press ↑ ↓',
    orderSaved: 'Order saved',
    neverApplies: 'Never applies',
    neverAppliesTip: 'An earlier policy covers every address, path, method and token of this one.',
    openWithoutToken: 'Open without a token',
    openWithoutTokenTip: 'No policy below covers every path and method of its addresses for requests that send no token, so those requests reach the app unprotected. Add an all-paths policy below it.',
    noPolicies: 'No access policies',
    addTitle: 'Add policy',
    editTitle: 'Edit policy',
    policyName: 'Policy name',
    address: 'Address',
    addAddress: 'Add address',
    removeAddress: 'Remove address',
    allPaths: 'All paths',
    selectedPaths: 'Selected paths',
    conditionsHint: 'Applies when any condition matches',
    operator: 'Operator',
    value: 'Value',
    addCondition: 'Add condition',
    removeCondition: 'Remove condition',
    regexNote: 'RE2 syntax, case-insensitive',
    methods: 'Methods',
    allMethods: 'All methods',
    selectedMethods: 'Selected methods',
    methodsHint: 'Applies only to requests with the selected methods; others go to the policies below. GET includes HEAD.',
    needsMethod: 'Choose at least one method.',
    token: 'Access token',
    tokenHint: 'Applies only to requests whose X-Meerkat-Access-Token header carries a token valid for the address and client; others go to the policies below.',
    noToken: 'Not required',
    anyToken: 'Any token',
    tokenShort: 'Token',
    selectedTokens: 'Selected tokens',
    needsToken: 'Choose at least one token.',
    bypassNote: 'Compared with the path exactly as the app gets it, case included',
    bypassPaths: 'Enter bypass paths as the app gets them, without \\, //, or segments that are . or .. or end in a dot or space.',
    operators: { equals: 'equals', starts_with: 'starts with', ends_with: 'ends with', contains: 'contains', matches: 'matches regex' } as Record<BlockOperator, string>,
    placeholders: { equals: '/admin', starts_with: '/private/', ends_with: '.pdf', contains: '/internal', matches: '^/(admin|reports)/' } as Record<BlockOperator, string>,
    who: 'Who can access',
    everyone: 'Everyone with access SSO',
    everyoneTip: 'Lets in anyone the access SSO provider signs in. Check whether the provider also accepts accounts outside your organization.',
    allowedUsers: 'Allowed users',
    allowedUsersHint: 'Email addresses or @domain.',
    allowedGroups: 'Allowed groups',
    allowedGroupsHint: 'With allowed users also set, both must match.',
    allowAdmins: 'Allow administrators',
    allowAdminsTip: 'Anyone whose email, as verified by the access SSO provider, is a Meerkat administrator\'s is let in, whether or not they match the allowed users and groups.',
    administrators: 'Administrators',
    noTokens: 'Create one under Access tokens first.',
    delete: 'Delete',
    deleteTitle: 'Delete this policy?',
    deleted: 'Deleted',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the policy: addresses are hostnames or *.domain, equals and starts with need a leading /, regular expressions must use RE2 syntax, and an allow policy must let someone in.',
    needsWho: 'Choose who may pass: everyone, users, groups or administrators.',
    decodedPaths: 'Enter paths without %-escapes or ;parameters.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load access policies: ${code}`,
  }, {
    title: '접근 정책',
    tip: '정책은 위에서부터 순서대로 확인하며, 요청에 처음 일치하는 활성화된 정책 하나가 적용됩니다. 순서는 드래그하여 변경합니다. 허용은 접근 보호 SSO 로그인 후 허용 대상만, 우회는 로그인 없이 누구나 접근할 수 있으며, 차단은 모두 거부합니다. 접근 토큰이 필요한 정책은 토큰을 보낸 요청에만 적용됩니다. 로그인한 사용자의 이메일은 X-Meerkat-Access-Email 헤더로, 토큰 이름은 X-Meerkat-Access-Service 헤더로 앱에 전달됩니다.',
    providerOff: '접근 보호 SSO가 비활성화되어 보호 대상 주소에 로그인할 수 없습니다.',
    damaged: '저장된 정책이 올바르지 않아 해당 주소를 차단 중입니다.',
    damagedEmpty: '저장된 정책을 읽을 수 없어 모든 공개 주소를 차단 중입니다.',
    saveAgain: '다시 저장',
    add: '정책 추가',
    enabled: '활성화',
    name: '이름',
    addresses: '적용 주소',
    paths: '경로',
    pathsTip: '경로 조건은 Outlet에 도달한 요청에만 적용됩니다. 단일 페이지 앱(SPA)의 화면 이동은 서버로 요청되지 않으므로, 앱의 전체 경로를 보호하고 외부 클라이언트가 호출하는 경로만 우회로 지정하십시오.',
    action: '동작',
    conditions: '적용 조건',
    actions: { allow: '허용', bypass: '우회', block: '차단' },
    actionHints: { allow: '로그인 후 허용 대상만 접근', bypass: '로그인 없이 누구나 접근', block: '모두 접근 거부' },
    tokenActionHints: { allow: '토큰과 허용 대상 로그인 모두 필요. 토큰 없는 요청은 아래 정책이 판단', bypass: '토큰만으로 로그인 없이 접근. 토큰 없는 요청은 아래 정책이 판단', block: '토큰을 보낸 요청 거부. 토큰 없는 요청은 아래 정책이 판단' },
    order: '순서',
    reorder: (name: string) => `${name} 순서 변경`,
    reorderTip: '드래그 또는 ↑ ↓ 키로 이동',
    orderSaved: '순서가 저장되었습니다',
    neverApplies: '적용 안 됨',
    neverAppliesTip: '앞선 정책이 이 정책의 주소, 경로, 메서드, 토큰에 먼저 적용됩니다.',
    openWithoutToken: '토큰 없는 요청 열림',
    openWithoutTokenTip: '토큰을 보내지 않은 요청에 적용할 정책이 아래에 없어, 그런 요청은 보호 없이 앱으로 전달됩니다. 아래에 전체 경로 정책을 두십시오.',
    noPolicies: '접근 정책이 없습니다',
    addTitle: '정책 추가',
    editTitle: '정책 편집',
    policyName: '정책 이름',
    address: '주소',
    addAddress: '주소 추가',
    removeAddress: '주소 삭제',
    allPaths: '전체 경로',
    selectedPaths: '선택한 경로',
    conditionsHint: '조건 중 하나 이상 일치 시 적용',
    operator: '연산자',
    value: '값',
    addCondition: '조건 추가',
    removeCondition: '조건 삭제',
    regexNote: 'RE2 구문, 대소문자 구분 없음',
    methods: '메서드',
    allMethods: '모든 메서드',
    selectedMethods: '선택한 메서드',
    methodsHint: '선택한 메서드의 요청에만 적용되며, 다른 요청은 아래 정책이 판단합니다. GET을 선택하면 HEAD도 포함됩니다.',
    needsMethod: '메서드를 하나 이상 선택하십시오.',
    token: '접근 토큰',
    tokenHint: 'X-Meerkat-Access-Token 헤더의 토큰이 주소와 클라이언트에 유효한 요청에만 적용되며, 다른 요청은 아래 정책이 판단합니다.',
    noToken: '조건 없음',
    anyToken: '모든 토큰',
    tokenShort: '토큰',
    selectedTokens: '선택한 토큰',
    needsToken: '토큰을 하나 이상 선택하십시오.',
    bypassNote: '앱이 받는 경로와 대소문자까지 그대로 비교',
    bypassPaths: '우회 조건은 앱이 받는 경로 그대로 입력하십시오. \\, //, . 또는 .. 조각, 점이나 공백으로 끝나는 조각은 사용할 수 없습니다.',
    operators: { equals: '같음', starts_with: '다음으로 시작', ends_with: '다음으로 끝남', contains: '포함', matches: '정규식과 일치' },
    placeholders: { equals: '/admin', starts_with: '/private/', ends_with: '.pdf', contains: '/internal', matches: '^/(admin|reports)/' },
    who: '접근 허용 대상',
    everyone: '접근 보호 SSO 사용자 전체',
    everyoneTip: '접근 보호 SSO 공급자가 인증한 모든 사용자를 허용합니다. 공급자가 조직 외부 계정도 허용하는지 확인하십시오.',
    allowedUsers: '허용 사용자',
    allowedUsersHint: '이메일 주소 또는 @도메인',
    allowedGroups: '허용 그룹',
    allowedGroupsHint: '허용 사용자와 함께 지정 시 두 조건 모두 충족 필요',
    allowAdmins: '관리자 허용',
    allowAdminsTip: '접근 보호 SSO에서 인증된 이메일이 Meerkat 관리자 이메일과 같은 사용자는 허용 사용자·그룹 조건과 무관하게 접근할 수 있습니다.',
    administrators: '관리자',
    noTokens: '접근 토큰에서 먼저 생성하십시오.',
    delete: '삭제',
    deleteTitle: '이 정책을 삭제하시겠습니까?',
    deleted: '삭제되었습니다',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '정책 입력값이 올바르지 않습니다. 주소는 도메인 이름 또는 *.도메인 형식이어야 하며, 같음 및 다음으로 시작 조건은 /로 시작해야 하고, 정규식은 RE2 구문만 지원합니다. 허용 정책에는 허용 대상이 필요합니다.',
    needsWho: '사용자 전체, 허용 사용자·그룹, 관리자 허용 중 하나 이상을 지정하십시오.',
    decodedPaths: '경로는 % 인코딩과 ;매개변수 없이 입력하십시오.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `접근 정책을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const maxPolicies = 100;
  const maxConditions = 50;
  const maxHosts = 100;
  const operators: BlockOperator[] = ['equals', 'starts_with', 'ends_with', 'contains', 'matches'];
  const actions: AccessAction[] = ['allow', 'bypass', 'block'];
  const operatorLabel = (operator: BlockOperator): string => t().operators[operator] ?? operator;
  const describe = (code: string): string => (code === 'invalid_access_policies' ? t().invalid : t().failed(code));

  // 대화상자의 줄은 지워도 입력 칸이 섞이지 않도록 고유한 key를 가집니다.
  type ConditionRow = BlockCondition & { key: number };
  type HostRow = { key: number; value: string };
  let nextKey = 0;
  const newRow = (condition: BlockCondition = { operator: 'starts_with', value: '' }): ConditionRow => ({ ...condition, key: nextKey++ });
  const newHost = (value = ''): HostRow => ({ key: nextKey++, value });

  // 동작 열은 허용 정책의 허용 대상 중 첫 항목만 보여주고 나머지는 개수로 줄입니다. 토큰 열은 토큰을 이름으로 보여 줍니다.
  const tokenName = (id: string): string => tokens?.find((token) => token.id === id)?.name ?? id;
  const allowedEntries = (policy: AccessPolicy): Array<{ text: string; mono: boolean }> => [
    ...(policy.allow_everyone ? [{ text: t().everyone, mono: false }] : []),
    ...policy.allowed_users.map((text) => ({ text, mono: true })),
    ...policy.allowed_groups.map((text) => ({ text, mono: true })),
    ...(policy.allow_admins ? [{ text: t().administrators, mono: false }] : []),
  ];

  // 서버의 bypassCondition과 같은 검사: \\, 빈 조각, ., .., 끝나는 곳이 확실한 조각 끝의 점·공백을 받지 않습니다.
  const plainBypassValue = (condition: BlockCondition): boolean => {
    const value = condition.value.trim();
    if (condition.operator === 'matches') return true;
    if (value.includes('\\')) return false;
    const parts = value.split('/');
    const whole = condition.operator === 'equals' || condition.operator === 'ends_with';
    return parts.every((part, index) => ((index === 0 || index === parts.length - 1) && part === '') || (part !== '' && part !== '.' && part !== '..' && !((index < parts.length - 1 || whole) && /[. ]$/.test(part))));
  };

  // 앞선 활성화된 정책이 이 정책의 주소, 경로, 메서드를 모두 덮으면, 이 정책은 적용될 일이 없습니다. 경로는 앞선 정책이 전체 경로이거나,
  // 우회가 아닌 앞선 정책의 '다음으로 시작' 조건이 이 정책의 '같음'·'다음으로 시작' 조건을 모두 덮는 경우입니다(서버처럼 소문자로, 끝에 /를 붙여 비교).
  const hostCovers = (pattern: string, host: string): boolean => pattern === host || (pattern.startsWith('*.') && host.endsWith(pattern.slice(1)));
  // 같음 조건은 서버가 요청을 비교하는 모양(소문자, 겹친 / 하나로, 끝에 / 하나)으로 바꿔 봅니다.
  const slashedPath = (value: string): string => {
    const path = value.toLowerCase().replace(/\/{2,}/g, '/').replace(/(.)\/$/, '$1');
    return path === '/' ? path : `${path}/`;
  };
  const startsWith = (policy: AccessPolicy) => policy.conditions.filter((condition) => condition.operator === 'starts_with').map((condition) => condition.value);
  const pathsCover = (earlier: AccessPolicy, later: AccessPolicy): boolean => {
    if (earlier.all_paths) return true;
    if (earlier.action === 'bypass') return false;
    const prefixes = startsWith(earlier);
    if (prefixes.includes('/')) return true;
    return !later.all_paths && later.conditions.length > 0 && later.conditions.every((condition) => {
      if (condition.operator !== 'starts_with' && condition.operator !== 'equals') return false;
      const path = condition.operator === 'equals' ? slashedPath(condition.value) : condition.value.toLowerCase();
      return prefixes.some((prefix) => path.startsWith(prefix));
    });
  };
  // 메서드는 앞선 정책이 모든 메서드이거나, 이 정책이 고른 메서드를 모두 고른 경우입니다.
  const methodsCover = (earlier: AccessPolicy, later: AccessPolicy): boolean =>
    !earlier.methods.length || (later.methods.length > 0 && later.methods.every((method) => earlier.methods.includes(method)));
  // 토큰은 앞선 정책이 토큰 조건이 없거나, 이 정책도 토큰이 필요하고 앞선 정책이 모든 토큰 또는 이 정책이 고른 토큰을 모두 받는 경우입니다.
  const tokensCover = (earlier: AccessPolicy, later: AccessPolicy): boolean =>
    !earlier.require_token || (later.require_token && (!earlier.tokens.length || (later.tokens.length > 0 && later.tokens.every((id) => earlier.tokens.includes(id)))));
  // 토큰이 필요한 정책은 토큰 없는 요청을 아래 정책에 넘기므로, 아래에 그 주소의 모든 경로와 메서드를 토큰 없이 받는 활성화된 정책이 없으면 그런 요청은 열려 있습니다.
  const openWithoutToken = (list: AccessPolicy[], index: number): boolean =>
    list[index].enabled && list[index].require_token && list[index].hosts.some((host) => !list.slice(index + 1).some((later) =>
      later.enabled && !later.require_token && !later.methods.length && (later.all_paths || startsWith(later).includes('/')) && later.hosts.some((pattern) => hostCovers(pattern, host))));
  const neverApplies = (list: AccessPolicy[], index: number): boolean =>
    list.slice(0, index).some((earlier) => earlier.enabled && methodsCover(earlier, list[index]) && tokensCover(earlier, list[index]) && pathsCover(earlier, list[index]) && list[index].hosts.every((host) => earlier.hosts.some((pattern) => hostCovers(pattern, host))));

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
  let form = $state<{ name: string; enabled: boolean; action: AccessAction; hosts: HostRow[]; paths: 'all' | 'selected'; conditions: ConditionRow[]; methods: 'all' | 'selected'; chosen: AccessMethod[]; token: 'none' | 'any' | 'selected'; tokens: string[]; everyone: boolean; users: string[]; groups: string[]; admins: boolean }>({
    name: '', enabled: true, action: 'allow', hosts: [], paths: 'all', conditions: [], methods: 'all', chosen: [], token: 'none', tokens: [], everyone: false, users: [], groups: [], admins: false,
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

  // 드래그 중인 정책과, 놓으면 그 자리로 들어갈 정책입니다. 아래로 끌면 그 정책 아래, 위로 끌면 그 위로 들어갑니다.
  let dragId = $state<string | null>(null);
  let overId = $state<string | null>(null);
  const dragIndex = $derived(dragId && policies ? policies.findIndex((policy) => policy.id === dragId) : -1);

  // 정책을 from에서 to 자리로 옮겨 바로 저장합니다. 실패하면 원래 순서로 되돌립니다.
  async function move(from: number, to: number): Promise<void> {
    if (!policies || busy || from === to || to < 0 || to >= policies.length) return;
    const previous = $state.snapshot(policies);
    const next = [...previous];
    const [moved] = next.splice(from, 1);
    next.splice(to, 0, moved);
    policies = next;
    busy = true;
    error = '';
    try {
      await put(next);
      toast.success(t().orderSaved);
    } catch (cause) {
      policies = previous;
      error = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
    await tick();
    document.getElementById(`access-policy-handle-${moved.id}`)?.focus();
  }

  function drop(targetId: string): void {
    if (!policies || !dragId) return;
    const from = policies.findIndex((policy) => policy.id === dragId);
    const to = policies.findIndex((policy) => policy.id === targetId);
    dragId = overId = null;
    void move(from, to);
  }

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
      action: policy?.action ?? 'allow',
      hosts: policy ? policy.hosts.map((host) => newHost(host)) : [newHost()],
      paths: policy && !policy.all_paths ? 'selected' : 'all',
      conditions: policy?.conditions.length ? policy.conditions.map((condition) => newRow(condition)) : [newRow()],
      methods: policy?.methods.length ? 'selected' : 'all',
      chosen: policy ? [...policy.methods] : [],
      token: !policy?.require_token ? 'none' : policy.tokens.length ? 'selected' : 'any',
      tokens: policy ? [...policy.tokens] : [],
      everyone: policy?.allow_everyone ?? false,
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
    const allow = form.action === 'allow';
    // 허용 대상은 허용 정책에만 있고, 사용자 전체를 고르면 사용자·그룹·관리자는 따로 둘 필요가 없습니다.
    const named = allow && !form.everyone;
    if (allow && !form.everyone && !form.users.length && !form.groups.length && !form.admins) {
      formError = t().needsWho;
      return;
    }
    // 요청 경로는 디코딩하고 ;매개변수를 뺀 형태로 비교하므로, 그런 값은 어떤 요청과도 일치하지 않습니다.
    if (form.paths === 'selected' && form.conditions.some((condition) => condition.operator !== 'matches' && /%[0-9a-f]{2}|;/i.test(condition.value))) {
      formError = t().decodedPaths;
      return;
    }
    // 우회는 경로를 여는 만큼 앱이 받는 그대로 비교하므로, 앱마다 다르게 읽을 수 있는 값은 받지 않습니다(서버와 같은 검사).
    if (form.action === 'bypass' && form.paths === 'selected' && form.conditions.some((condition) => !plainBypassValue(condition))) {
      formError = t().bypassPaths;
      return;
    }
    if (form.methods === 'selected' && !form.chosen.length) {
      formError = t().needsMethod;
      return;
    }
    // 지워진 토큰은 빼고 보냅니다. 남은 토큰이 없으면 고르게 합니다.
    const chosenTokens = tokens ? form.tokens.filter((id) => tokens.some((token) => token.id === id)) : [...form.tokens];
    if (form.token === 'selected' && !chosenTokens.length) {
      formError = t().needsToken;
      return;
    }
    const current = editing;
    const next: AccessPolicy = {
      id: current?.id ?? '',
      name: form.name.trim(),
      enabled: form.enabled,
      action: form.action,
      hosts: form.hosts.map((row) => row.value.trim().toLowerCase()).filter(Boolean),
      all_paths: form.paths === 'all',
      conditions: form.paths === 'selected' ? form.conditions.map((condition) => ({ operator: condition.operator, value: condition.value.trim() })) : [],
      methods: form.methods === 'selected' ? accessMethods.filter((method) => form.chosen.includes(method)) : [],
      require_token: form.token !== 'none',
      tokens: form.token === 'selected' ? chosenTokens : [],
      allow_everyone: allow && form.everyone,
      allowed_users: named ? [...form.users] : [],
      allowed_groups: named ? [...form.groups] : [],
      allow_admins: named && form.admins,
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
                <Table.Head class="w-16 pl-2"><span class="sr-only">{t().order}</span></Table.Head>
                <Table.Head class="w-12"><span class="sr-only">{t().enabled}</span></Table.Head>
                <Table.Head>{t().name}</Table.Head>
                <Table.Head>{t().conditions}</Table.Head>
                <Table.Head class="pr-4">{t().action}</Table.Head>
              </Table.Row>
            </Table.Header>
            <Table.Body>
              {#each policies as policy, index (policy.id)}
                {@const entries = allowedEntries(policy)}
                <!-- 손잡이로 끌어 다른 행에 놓으면 그 행 자리로 옮깁니다. 표 본문은 마지막 행의 테두리를 지우므로 아래 표시선은 !로 남깁니다. -->
                <Table.Row
                  class={['relative cursor-pointer', overId === policy.id && dragId !== policy.id && (dragIndex < index ? 'border-b-2! border-b-primary' : 'border-t-2 border-t-primary'), dragId === policy.id && 'opacity-50']}
                  ondragover={(event: DragEvent) => { if (dragId) { event.preventDefault(); overId = policy.id; } }}
                  ondrop={(event: DragEvent) => { event.preventDefault(); drop(policy.id); }}
                >
                  <Table.Cell class="pl-2">
                    <span class="flex items-center gap-1">
                      <IconButton
                        id={`access-policy-handle-${policy.id}`}
                        label={`${t().reorder(policy.name)} · ${t().reorderTip}`}
                        icon={ReOrder}
                        class="relative z-10 cursor-grab"
                        disabled={busy}
                        draggable="true"
                        ondragstart={(event: DragEvent) => {
                          dragId = policy.id;
                          if (event.dataTransfer) {
                            event.dataTransfer.effectAllowed = 'move';
                            event.dataTransfer.setData('text/plain', policy.id);
                            const row = (event.currentTarget as HTMLElement).closest('tr');
                            if (row) event.dataTransfer.setDragImage(row, 16, 16);
                          }
                        }}
                        ondragend={() => { dragId = overId = null; }}
                        onkeydown={(event: KeyboardEvent) => {
                          if (event.key === 'ArrowUp' || event.key === 'ArrowDown') {
                            event.preventDefault();
                            void move(index, index + (event.key === 'ArrowUp' ? -1 : 1));
                          }
                        }}
                      />
                      <span class="text-muted-foreground tabular-nums">{(index + 1).toLocaleString(intlLocale())}</span>
                    </span>
                  </Table.Cell>
                  <Table.Cell>
                    <Switch size="sm" class="relative z-10" bind:checked={policy.enabled} disabled={busy} aria-label={policy.name} onCheckedChange={(checked) => toggle(policy, checked)} />
                  </Table.Cell>
                  <Table.Cell>
                    <span class="flex items-center gap-2">
                      <button type="button" class="block max-w-56 truncate text-left text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50" onclick={() => openPolicy(policy)}>{policy.name}</button>
                      {#if neverApplies(policies, index)}
                        <Tooltip.Root>
                          <Tooltip.Trigger class="relative z-10 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
                            <Badge variant="outline" class="text-muted-foreground select-none">{t().neverApplies}</Badge>
                          </Tooltip.Trigger>
                          <Tooltip.Content>{t().neverAppliesTip}</Tooltip.Content>
                        </Tooltip.Root>
                      {:else if openWithoutToken(policies, index)}
                        <Tooltip.Root>
                          <Tooltip.Trigger class="relative z-10 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
                            <Badge variant="destructive" class="select-none">{t().openWithoutToken}</Badge>
                          </Tooltip.Trigger>
                          <Tooltip.Content>{t().openWithoutTokenTip}</Tooltip.Content>
                        </Tooltip.Root>
                      {/if}
                    </span>
                  </Table.Cell>
                  <!-- 적용 조건은 주소를 위에, 경로와 기본값이 아닌 메서드·토큰을 아래에 흐리게 보여 줍니다. -->
                  <Table.Cell>
                    <div class="grid gap-0.5">
                      <span class="flex items-center gap-1.5">
                        <span class="max-w-56 truncate font-mono">{policy.hosts[0]}</span>
                        {#if policy.hosts.length > 1}<span class="text-muted-foreground tabular-nums">+{(policy.hosts.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      </span>
                      <span class="flex flex-wrap items-center gap-x-3 text-muted-foreground">
                        <span class="flex items-center gap-1.5">
                          {#if policy.all_paths}
                            {t().allPaths}
                          {:else if policy.conditions.length}
                            {operatorLabel(policy.conditions[0].operator)}
                            <span class="max-w-40 truncate font-mono text-foreground">{policy.conditions[0].value}</span>
                            {#if policy.conditions.length > 1}<span class="tabular-nums">+{(policy.conditions.length - 1).toLocaleString(intlLocale())}</span>{/if}
                          {/if}
                        </span>
                        {#if policy.methods.length}<span class="font-mono">{policy.methods.join(', ')}</span>{/if}
                        {#if policy.require_token}
                          <span class="flex items-center gap-1.5">
                            {#if policy.tokens.length}
                              <span class="max-w-40 truncate">{t().tokenShort} {tokenName(policy.tokens[0])}</span>
                              {#if policy.tokens.length > 1}<span class="tabular-nums">+{(policy.tokens.length - 1).toLocaleString(intlLocale())}</span>{/if}
                            {:else}
                              {t().anyToken}
                            {/if}
                          </span>
                        {/if}
                      </span>
                    </div>
                  </Table.Cell>
                  <Table.Cell class="pr-4">
                    <span class="flex items-center gap-1.5">
                      <Badge variant={policy.action === 'block' ? 'destructive' : policy.action === 'bypass' ? 'secondary' : 'outline'} class="select-none">{t().actions[policy.action]}</Badge>
                      {#if policy.action === 'allow' && entries.length}
                        <span class={['max-w-40 truncate', entries[0].mono && 'font-mono']}>{entries[0].text}</span>
                        {#if entries.length > 1}<span class="text-muted-foreground tabular-nums">+{(entries.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      {/if}
                    </span>
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
      </Field.Group>
      <!-- 어떤 요청에 적용할지(주소, 경로, 메서드, 토큰)와 그 요청에 무엇을 할지(동작, 허용 대상)를 나누고, 항목마다 상자로 묶습니다. -->
      <Field.Set>
        <Field.Legend>{t().conditions}</Field.Legend>
        <Item.Group>
          <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
            <Field.Set>
              <Field.Legend variant="label">{t().addresses}</Field.Legend>
              <div class="grid gap-2">
                {#each form.hosts as row, index (row.key)}
                  <div class="flex items-center gap-2">
                    <AddressSelect id={`access-policy-host-${row.key}`} class="min-w-0 flex-1" bind:value={row.value} {addresses} taken={form.hosts.map((other) => other.value)} ariaLabel={`${t().address} ${index + 1}`} />
                    <IconButton label={t().removeAddress} icon={Dismiss} variant="outline" size="icon" disabled={form.hosts.length <= 1} onclick={() => form.hosts.splice(index, 1)} />
                  </div>
                {/each}
              </div>
              <div class="flex justify-end">
                <Button variant="outline" size="sm" disabled={form.hosts.length >= maxHosts} onclick={addHost}><Add12 aria-hidden="true" />{t().addAddress}</Button>
              </div>
            </Field.Set>
          </Item.Root>
          <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
            <Field.Set>
              <Field.Legend variant="label" class="flex items-center gap-1">{t().paths}<InfoTooltip id="access-policy-paths-tip" label={t().paths} description={t().pathsTip} /></Field.Legend>
              <RadioGroup.Root value={form.paths} onValueChange={(value) => { form.paths = value === 'selected' ? 'selected' : 'all'; }} orientation="horizontal" class="flex flex-wrap gap-x-6 gap-y-2">
                <Field.Field orientation="horizontal" class="w-auto">
                  <RadioGroup.Item value="all" id="access-policy-paths-all" />
                  <Field.Label for="access-policy-paths-all" class="font-normal">{t().allPaths}</Field.Label>
                </Field.Field>
                <Field.Field orientation="horizontal" class="w-auto">
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
                      <IconButton label={t().removeCondition} icon={Dismiss} variant="outline" size="icon" disabled={form.conditions.length <= 1} onclick={() => form.conditions.splice(index, 1)} />
                    </div>
                  {/each}
                </div>
                {#if form.action === 'bypass'}<Field.Description>{t().bypassNote}</Field.Description>{:else if hasRegex}<Field.Description>{t().regexNote}</Field.Description>{/if}
                <div class="flex justify-end">
                  <Button variant="outline" size="sm" disabled={form.conditions.length >= maxConditions} onclick={addCondition}><Add12 aria-hidden="true" />{t().addCondition}</Button>
                </div>
              {/if}
            </Field.Set>
          </Item.Root>
          <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
            <Field.Set>
              <Field.Legend variant="label" class="flex items-center gap-1">{t().methods}<InfoTooltip id="access-policy-methods-tip" label={t().methods} description={t().methodsHint} /></Field.Legend>
              <RadioGroup.Root value={form.methods} onValueChange={(value) => { form.methods = value === 'selected' ? 'selected' : 'all'; }} orientation="horizontal" class="flex flex-wrap gap-x-6 gap-y-2">
                <Field.Field orientation="horizontal" class="w-auto">
                  <RadioGroup.Item value="all" id="access-policy-methods-all" />
                  <Field.Label for="access-policy-methods-all" class="font-normal">{t().allMethods}</Field.Label>
                </Field.Field>
                <Field.Field orientation="horizontal" class="w-auto">
                  <RadioGroup.Item value="selected" id="access-policy-methods-selected" />
                  <Field.Label for="access-policy-methods-selected" class="font-normal">{t().selectedMethods}</Field.Label>
                </Field.Field>
              </RadioGroup.Root>
              {#if form.methods === 'selected'}
                <div class="flex flex-wrap gap-x-4 gap-y-2">
                  {#each accessMethods as method (method)}
                    <Field.Field orientation="horizontal" class="w-auto">
                      <Checkbox
                        id={`access-policy-method-${method}`}
                        checked={form.chosen.includes(method)}
                        onCheckedChange={(checked) => { form.chosen = checked ? [...form.chosen, method] : form.chosen.filter((entry) => entry !== method); }}
                      />
                      <Field.Label for={`access-policy-method-${method}`} class="font-mono font-normal">{method}</Field.Label>
                    </Field.Field>
                  {/each}
                </div>
              {/if}
            </Field.Set>
          </Item.Root>
          <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
            <Field.Set>
              <Field.Legend variant="label" class="flex items-center gap-1">{t().token}<InfoTooltip id="access-policy-token-tip" label={t().token} description={t().tokenHint} /></Field.Legend>
              <RadioGroup.Root value={form.token} onValueChange={(value) => { form.token = value === 'any' || value === 'selected' ? value : 'none'; }} orientation="horizontal" class="flex flex-wrap gap-x-6 gap-y-2">
                {#each [['none', t().noToken], ['any', t().anyToken], ['selected', t().selectedTokens]] as [value, label] (value)}
                  <Field.Field orientation="horizontal" class="w-auto">
                    <RadioGroup.Item value={value} id={`access-policy-token-${value}`} />
                    <Field.Label for={`access-policy-token-${value}`} class="font-normal">{label}</Field.Label>
                  </Field.Field>
                {/each}
              </RadioGroup.Root>
              {#if form.token === 'selected'}
                {#if tokens?.length}
                  <div class="flex flex-wrap gap-x-6 gap-y-2">
                    {#each tokens as token (token.id)}
                      <Field.Field orientation="horizontal" class="w-auto">
                        <Checkbox
                          id={`access-policy-token-id-${token.id}`}
                          checked={form.tokens.includes(token.id)}
                          onCheckedChange={(checked) => { form.tokens = checked ? [...form.tokens, token.id] : form.tokens.filter((id) => id !== token.id); }}
                        />
                        <Field.Label for={`access-policy-token-id-${token.id}`} class="min-w-0 font-normal">
                          <span class="truncate">{token.name}</span>
                          {#if token.hosts?.length}<span class="truncate font-mono text-muted-foreground">{token.hosts[0]}{token.hosts.length > 1 ? ` +${token.hosts.length - 1}` : ''}</span>{/if}
                        </Field.Label>
                      </Field.Field>
                    {/each}
                  </div>
                {:else if tokens}
                  <Field.Description>{t().noTokens}</Field.Description>
                {/if}
              {/if}
            </Field.Set>
          </Item.Root>
        </Item.Group>
      </Field.Set>
      <Field.Set>
        <Field.Legend id="access-policy-action-title">{t().action}</Field.Legend>
        <Item.Group>
          <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
            <!-- 선택지는 한 줄로 두고, 각 동작의 뜻은 마우스를 올리면 툴팁으로, 화면 낭독기에는 설명으로 알립니다. -->
            <RadioGroup.Root aria-labelledby="access-policy-action-title" value={form.action} onValueChange={(value) => { if (actions.includes(value as AccessAction)) form.action = value as AccessAction; }} orientation="horizontal" class="flex flex-wrap gap-x-6 gap-y-2">
              {#each actions as action (action)}
                {@const hint = form.token === 'none' ? t().actionHints[action] : t().tokenActionHints[action]}
                <Tooltip.Root>
                  <Tooltip.Trigger>
                    {#snippet child({ props })}
                      <Field.Field orientation="horizontal" class="w-auto" {...props} tabindex={-1}>
                        <RadioGroup.Item value={action} id={`access-policy-action-${action}`} aria-describedby={`access-policy-action-${action}-hint`} />
                        <Field.Label for={`access-policy-action-${action}`} class="font-normal">{t().actions[action]}</Field.Label>
                        <span id={`access-policy-action-${action}-hint`} class="sr-only">{hint}</span>
                      </Field.Field>
                    {/snippet}
                  </Tooltip.Trigger>
                  <Tooltip.Content>{hint}</Tooltip.Content>
                </Tooltip.Root>
              {/each}
            </RadioGroup.Root>
          </Item.Root>
          {#if form.action === 'allow'}
            <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
              <Field.Set>
                <Field.Legend variant="label">{t().who}</Field.Legend>
                <Field.Field orientation="horizontal">
                  <Field.Content>
                    <div class="flex items-center gap-1"><Field.Label for="access-policy-everyone">{t().everyone}</Field.Label><InfoTooltip id="access-policy-everyone-tip" label={t().everyone} description={t().everyoneTip} /></div>
                  </Field.Content>
                  <Switch id="access-policy-everyone" bind:checked={form.everyone} />
                </Field.Field>
                {#if !form.everyone}
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
                {/if}
              </Field.Set>
            </Item.Root>
          {/if}
        </Item.Group>
      </Field.Set>
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
