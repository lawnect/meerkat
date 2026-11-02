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
  import { Add12, Copy, Dismiss, ReOrder, Shield20, Warning, Warning12 } from '../icons';
  import { isIpRange, splitScope, tagSuggestions, unknownTags } from '../ipScope.svelte';
  import { accessMethods, type AccessAction, type AccessCondition, type AccessConditionField, type AccessPoliciesView, type AccessPolicy, type AccessToken, type BlockOperator, type IpTagUse } from '../types';
  import AddressSelect from './AddressSelect.svelte';
  import IconButton from './IconButton.svelte';
  import InfoTooltip from './InfoTooltip.svelte';
  import TagInput from './TagInput.svelte';

  // 접근 보호 화면의 접근 정책 카드. 주소와 조건(경로, 메서드, 접속 IP, 접근 토큰을 필드·연산자·값으로 비교하는 줄, 모두 일치해야 적용)마다
  // 허용(로그인 후 허용 대상만)·우회(로그인 없이)·차단을 정합니다.
  // 정책은 위에서부터 확인해 처음 일치하는 하나가 적용되며, 순서는 손잡이를 드래그하거나 화살표 키로 바꿉니다.
  // 스위치, 순서 변경, 대화상자의 저장·삭제는 모두 바로 서버에 저장하고, 응답으로 목록을 다시 채웁니다.
  // providerEnabled는 접근 보호 SSO가 켜져 있는지(알 수 없으면 null)로, 꺼져 있으면 정책이 적용되지 않는다고 안내합니다.
  // tokens는 접근 토큰 목록(알 수 없으면 null)으로, 토큰 조건에서 토큰을 고르고 이름을 보여 주는 데 씁니다.
  // ipTags는 IP 주소록의 태그(알 수 없으면 null)로, 접속 IP 조건에서 태그를 제안하고 없는 태그를 저장 전에 알리는 데 씁니다.
  // onsaved는 정책을 서버에 저장할 때마다(스위치, 순서 변경, 대화상자의 저장·삭제 포함) 부릅니다.
  let { providerEnabled = null, tokens = null, ipTags = null, onsaved }: { providerEnabled?: boolean | null; tokens?: AccessToken[] | null; ipTags?: IpTagUse[] | null; onsaved?: () => void } = $props();

  const t = translations({
    title: 'Access policies',
    tip: 'Policies are checked from the top; the first enabled one whose addresses and conditions match a request applies. Drag a policy to change the order; policies whose addresses overlap share a color bar on the left. Allow lets in, after a sign-in with access SSO, only who the policy allows, Bypass lets anyone in without one, and Block lets nobody in. The signed-in email reaches the app in the X-Meerkat-Access-Email header, and the access token\'s name in X-Meerkat-Access-Service.',
    providerOff: 'Access SSO is off, so no one can sign in to protected addresses.',
    damaged: 'Saved policies are invalid, so their addresses are closed.',
    damagedEmpty: 'Saved policies can\'t be read, so every public address is closed.',
    saveAgain: 'Save again',
    add: 'Add policy',
    enabled: 'Enabled',
    name: 'Name',
    addresses: 'Addresses',
    action: 'Action',
    conditions: 'Conditions',
    scope: 'Scope',
    conditionRows: 'Conditions',
    conditionsHint: 'Applies when all conditions match',
    conditionsTip: 'Each condition compares one part of a request. The values of a condition are alternatives: it holds when any of them matches, or for a "not" operator, when none does. Requests that don\'t meet the conditions go on to the policies below. Path conditions apply to requests that reach the Outlet: a single-page app changes screens in the browser without one, so protect all of its paths and bypass only the paths its clients call. An access token is one sent in the X-Meerkat-Access-Token header, valid for the address and client.',
    noConditions: 'No conditions: applies to every request to the addresses',
    allRequests: 'All requests',
    actions: { allow: 'Allow', bypass: 'Bypass', block: 'Block' } as Record<AccessAction, string>,
    actionHints: { allow: 'Sign-in, then only who you allow', bypass: 'Anyone, without signing in', block: 'Nobody' } as Record<AccessAction, string>,
    order: 'Order',
    reorder: (name: string) => `Move ${name}`,
    reorderTip: 'Drag, or press ↑ ↓',
    group: (n: number) => `Address group ${n}`,
    orderSaved: 'Order saved',
    neverApplies: 'Never applies',
    neverAppliesTip: 'An earlier policy covers this one\'s addresses and every request its conditions match.',
    unprotected: 'Other requests unprotected',
    unprotectedTip: 'No other policy applies to requests that don\'t meet this policy\'s client IP or access token conditions. Those requests reach the service without access protection.',
    booksDamaged: 'The IP address book could not be read, so tags cannot be used. Register the addresses again on the IP address book tab.',
    noPolicies: 'No access policies',
    addTitle: 'Add policy',
    editTitle: 'Edit policy',
    policyName: 'Policy name',
    address: 'Address',
    addAddress: 'Add address',
    removeAddress: 'Remove address',
    field: 'Field',
    fields: { path: 'Path', method: 'Method', client_ip: 'Client IP', token: 'Access token' } as Record<AccessConditionField, string>,
    operator: 'Operator',
    operators: {
      path: { starts_with: 'starts with', '!starts_with': 'does not start with', equals: 'equals', '!equals': 'does not equal', ends_with: 'ends with', '!ends_with': 'does not end with', contains: 'contains', '!contains': 'does not contain', matches: 'matches regex', '!matches': 'does not match regex' },
      method: { in: 'is one of', '!in': 'is not one of' },
      client_ip: { in: 'is in', '!in': 'is not in' },
      token: { any: 'is valid', '!any': 'is missing', in: 'is one of', '!in': 'is not one of' },
    } as Record<AccessConditionField, Record<string, string>>,
    value: 'Value',
    addCondition: 'Add condition',
    removeCondition: 'Remove condition',
    regexNote: 'RE2 syntax, case-insensitive',
    strictNote: 'Compared with the path exactly as the app gets it, case included',
    headNote: 'GET includes HEAD',
    openPrefix: (value: string) => `Also matches paths like ${value}x. Enter ${value}/ for paths under it only.`,
    placeholders: { equals: '/admin', starts_with: '/private/', ends_with: '.pdf', contains: '/internal', matches: '^/(admin|reports)/' } as Record<BlockOperator, string>,
    ipPlaceholder: 'Tag, IP or CIDR',
    ipShort: 'IP',
    tokenShort: 'Token',
    needsValue: 'Enter at least one value for each condition.',
    pathsTooMany: 'Enter up to 50 paths for a condition.',
    ipRequired: 'Choose at least one tag, IP or CIDR.',
    unknownTag: (tag: string) => `Not a tag in the IP address book: ${tag}`,
    ipTooMany: 'Choose up to 20 tags and 50 IPs or CIDR ranges for a condition.',
    needsToken: 'Choose at least one token.',
    noTokens: 'Create one under Access tokens first.',
    strictPaths: 'Enter bypass paths, and paths a "not" condition leaves out, as the app gets them: without \\, //, or segments that are . or .. or end in a dot or space.',
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
    delete: 'Delete',
    duplicate: 'Duplicate',
    copyName: (name: string) => `${name} (copy)`,
    fix: 'Fix automatically',
    fixTip: 'Saves, and adds a policy below the last one on these addresses that blocks the requests this one leaves and no other policy takes.',
    fixUnavailable: 'This policy\'s "not" path conditions can\'t be used in a block policy as they are. Add the block policy yourself.',
    fixName: (name: string) => `${name} (others blocked)`,
    fixed: (name: string) => `Added ${name}`,
    deleteTitle: 'Delete this policy?',
    deleted: 'Deleted',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    saved: 'Saved',
    invalid: 'Check the policy: addresses are hostnames or *.domain, equals and starts with need a leading /, regular expressions must use RE2 syntax, and an allow policy must let someone in.',
    needsWho: 'Choose who may pass: everyone, users, groups or administrators.',
    decodedPaths: 'Enter paths without %-escapes or ;parameters.',
    outdated: 'The console is out of date. Reload the page and try again.',
    failed: (code: string) => `Couldn't save: ${code}`,
    loadFailed: (code: string) => `Couldn't load access policies: ${code}`,
  }, {
    title: '접근 정책',
    tip: '정책은 위에서부터 순서대로 확인하며, 주소와 조건이 요청에 처음 일치하는 활성화된 정책 하나가 적용됩니다. 순서는 드래그하여 변경하며, 적용 주소가 겹치는 정책은 왼쪽에 같은 색 띠로 표시됩니다. 허용은 접근 보호 SSO 로그인 후 허용 대상만, 우회는 로그인 없이 누구나 접근할 수 있으며, 차단은 모두 거부합니다. 로그인한 사용자의 이메일은 X-Meerkat-Access-Email 헤더로, 접근 토큰 이름은 X-Meerkat-Access-Service 헤더로 앱에 전달됩니다.',
    providerOff: '접근 보호 SSO가 비활성화되어 보호 대상 주소에 로그인할 수 없습니다.',
    damaged: '저장된 정책이 올바르지 않아 해당 주소를 차단 중입니다.',
    damagedEmpty: '저장된 정책을 읽을 수 없어 모든 공개 주소를 차단 중입니다.',
    saveAgain: '다시 저장',
    add: '정책 추가',
    enabled: '활성화',
    name: '이름',
    addresses: '적용 주소',
    action: '동작',
    conditions: '적용 조건',
    scope: '적용 범위',
    conditionRows: '조건',
    conditionsHint: '모든 조건 일치 시 적용',
    conditionsTip: '조건마다 요청의 한 항목을 비교합니다. 한 조건에 지정한 값은 그중 하나라도 일치하면 성립하며, 부정 연산자는 모두 일치하지 않을 때 성립합니다. 조건에 맞지 않는 요청은 아래 정책이 판단합니다. 경로 조건은 Outlet에 도달한 요청에만 적용되므로, 단일 페이지 앱(SPA)은 전체 경로를 보호하고 외부 클라이언트가 호출하는 경로만 우회로 지정하십시오. 접근 토큰은 X-Meerkat-Access-Token 헤더로 보낸, 주소와 클라이언트에 유효한 토큰입니다.',
    noConditions: '조건 없음. 주소의 모든 요청에 적용',
    allRequests: '모든 요청',
    actions: { allow: '허용', bypass: '우회', block: '차단' },
    actionHints: { allow: '로그인 후 허용 대상만 접근', bypass: '로그인 없이 누구나 접근', block: '모두 접근 거부' },
    order: '순서',
    reorder: (name: string) => `${name} 순서 변경`,
    reorderTip: '드래그 또는 ↑ ↓ 키로 이동',
    group: (n: number) => `주소 그룹 ${n}`,
    orderSaved: '순서가 저장되었습니다',
    neverApplies: '적용 안 됨',
    neverAppliesTip: '앞선 정책이 이 정책의 주소와 조건에 해당하는 모든 요청에 먼저 적용됩니다.',
    unprotected: '조건 외 요청 미보호',
    unprotectedTip: '이 정책의 접속 IP·접근 토큰 조건에 맞지 않는 요청에 적용되는 다른 정책이 없습니다. 해당 요청은 접근 보호 없이 서비스로 전달됩니다.',
    booksDamaged: 'IP 주소록을 읽을 수 없어 태그를 사용할 수 없습니다. IP 주소록 탭에서 주소를 다시 등록하십시오.',
    noPolicies: '접근 정책이 없습니다',
    addTitle: '정책 추가',
    editTitle: '정책 편집',
    policyName: '정책 이름',
    address: '주소',
    addAddress: '주소 추가',
    removeAddress: '주소 삭제',
    field: '필드',
    fields: { path: '경로', method: '메서드', client_ip: '접속 IP', token: '접근 토큰' },
    operator: '연산자',
    operators: {
      path: { starts_with: '다음으로 시작', '!starts_with': '다음으로 시작하지 않음', equals: '같음', '!equals': '같지 않음', ends_with: '다음으로 끝남', '!ends_with': '다음으로 끝나지 않음', contains: '포함', '!contains': '포함하지 않음', matches: '정규식과 일치', '!matches': '정규식과 일치하지 않음' },
      method: { in: '다음 중 하나', '!in': '다음 중 하나가 아님' },
      client_ip: { in: '다음에 포함', '!in': '다음에 포함되지 않음' },
      token: { any: '유효한 토큰 있음', '!any': '유효한 토큰 없음', in: '다음 중 하나', '!in': '다음 중 하나가 아님' },
    },
    value: '값',
    addCondition: '조건 추가',
    removeCondition: '조건 삭제',
    regexNote: 'RE2 구문, 대소문자 구분 없음',
    strictNote: '앱이 받는 경로와 대소문자까지 그대로 비교',
    headNote: 'GET 선택 시 HEAD 포함',
    openPrefix: (value: string) => `${value}x 등 다른 경로도 포함됩니다. 하위 경로만 지정하려면 ${value}/로 입력하십시오.`,
    placeholders: { equals: '/admin', starts_with: '/private/', ends_with: '.pdf', contains: '/internal', matches: '^/(admin|reports)/' },
    ipPlaceholder: '태그, IP 또는 CIDR',
    ipShort: 'IP',
    tokenShort: '토큰',
    needsValue: '조건마다 값을 하나 이상 지정하십시오.',
    pathsTooMany: '조건마다 경로는 최대 50개까지 지정할 수 있습니다.',
    ipRequired: '태그, IP 또는 CIDR을 하나 이상 지정하십시오.',
    unknownTag: (tag: string) => `IP 주소록에 없는 태그입니다: ${tag}`,
    ipTooMany: '조건마다 태그는 최대 20개, IP·CIDR은 최대 50개까지 지정할 수 있습니다.',
    needsToken: '토큰을 하나 이상 선택하십시오.',
    noTokens: '접근 토큰에서 먼저 생성하십시오.',
    strictPaths: '우회 경로와 부정 조건의 제외 경로는 앱이 받는 경로 그대로 입력하십시오. \\, //, . 또는 .. 조각, 점이나 공백으로 끝나는 조각은 사용할 수 없습니다.',
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
    delete: '삭제',
    duplicate: '복제',
    copyName: (name: string) => `${name} (복사본)`,
    fix: '자동 해결',
    fixTip: '저장 후, 이 정책의 조건 외 요청 중 다른 정책이 처리하지 않는 요청을 차단하는 정책을 해당 주소의 마지막 정책 아래에 추가합니다.',
    fixUnavailable: '이 정책의 부정 경로 조건은 차단 정책에 그대로 사용할 수 없어 자동 해결할 수 없습니다. 차단 정책을 직접 추가하십시오.',
    fixName: (name: string) => `${name} (조건 외 차단)`,
    fixed: (name: string) => `${name} 정책이 추가되었습니다`,
    deleteTitle: '이 정책을 삭제하시겠습니까?',
    deleted: '삭제되었습니다',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    saved: '저장되었습니다',
    invalid: '정책 입력값이 올바르지 않습니다. 주소는 도메인 이름 또는 *.도메인 형식이어야 하며, 같음 및 다음으로 시작 조건은 /로 시작해야 하고, 정규식은 RE2 구문만 지원합니다. 허용 정책에는 허용 대상이 필요합니다.',
    needsWho: '사용자 전체, 허용 사용자·그룹, 관리자 허용 중 하나 이상을 지정하십시오.',
    decodedPaths: '경로는 % 인코딩과 ;매개변수 없이 입력하십시오.',
    outdated: '콘솔이 최신 버전이 아닙니다. 페이지를 새로고침한 후 다시 시도하십시오.',
    failed: (code: string) => `저장 중 오류가 발생했습니다: ${code}`,
    loadFailed: (code: string) => `접근 정책을 불러오는 중 오류가 발생했습니다: ${code}`,
  });

  const maxPolicies = 100;
  const maxRows = 20;
  const maxPathValues = 50;
  const maxHosts = 100;
  const maxIpTags = 20;
  const maxIpRanges = 50;
  const actions: AccessAction[] = ['allow', 'bypass', 'block'];
  const fields: AccessConditionField[] = ['path', 'method', 'client_ip', 'token'];
  // 연산자 선택지는 부정을 앞의 !로 함께 담습니다(예: '!starts_with'는 '다음으로 시작하지 않음').
  const operatorChoices: Record<AccessConditionField, string[]> = {
    path: ['starts_with', '!starts_with', 'equals', '!equals', 'ends_with', '!ends_with', 'contains', '!contains', 'matches', '!matches'],
    method: ['in', '!in'],
    client_ip: ['in', '!in'],
    token: ['any', '!any', 'in', '!in'],
  };
  const choiceOf = (condition: AccessCondition): string => `${condition.not ? '!' : ''}${condition.operator}`;
  const operatorText = (field: AccessConditionField, choice: string): string => t().operators[field]?.[choice] ?? choice;
  const describe = (code: string): string =>
    code === 'invalid_access_policies' ? t().invalid : code === 'ip_addresses_damaged' ? t().booksDamaged : code === 'access_policies_outdated' ? t().outdated : t().failed(code);

  // 대화상자의 줄은 지워도 입력 칸이 섞이지 않도록 고유한 key를 가집니다. choice는 연산자와 부정을 함께 담은 선택지입니다.
  type ConditionRow = { key: number; field: AccessConditionField; choice: string; values: string[] };
  type HostRow = { key: number; value: string };
  let nextKey = 0;
  const newRow = (condition?: AccessCondition): ConditionRow =>
    condition ? { key: nextKey++, field: condition.field, choice: choiceOf(condition), values: [...condition.values] } : { key: nextKey++, field: 'path', choice: 'starts_with', values: [] };
  const newHost = (value = ''): HostRow => ({ key: nextKey++, value });
  // 필드를 바꾸면 그 필드의 첫 연산자와 빈 값으로 시작합니다.
  function setField(row: ConditionRow, field: string): void {
    if (!fields.includes(field as AccessConditionField)) return;
    row.field = field as AccessConditionField;
    row.choice = operatorChoices[row.field][0];
    row.values = [];
  }
  const operatorOf = (row: ConditionRow): string => row.choice.replace(/^!/, '');
  const negated = (row: ConditionRow): boolean => row.choice.startsWith('!');
  // 우회의 경로, 보호 정책이 부정 조건으로 빼는 경로는 여는 쪽이라 앱이 받는 그대로 비교합니다(서버의 StrictPath).
  const strictPath = (action: AccessAction, not: boolean): boolean => (action === 'bypass') !== not;

  // 동작 열은 허용 정책의 허용 대상 중 첫 항목만 보여주고 나머지는 개수로 줄입니다. 토큰은 이름으로 보여 줍니다.
  const tokenName = (id: string): string => tokens?.find((token) => token.id === id)?.name ?? id;
  const allowedEntries = (policy: AccessPolicy): Array<{ text: string; mono: boolean }> => [
    ...(policy.allow_everyone ? [{ text: t().everyone, mono: false }] : []),
    ...policy.allowed_users.map((text) => ({ text, mono: true })),
    ...policy.allowed_groups.map((text) => ({ text, mono: true })),
    ...(policy.allow_admins ? [{ text: t().administrators, mono: false }] : []),
  ];
  // 목록의 조건 요약: 필드와 연산자, 첫 값(토큰은 이름), 나머지 개수.
  const conditionValue = (condition: AccessCondition): string => (condition.field === 'token' ? tokenName(condition.values[0] ?? '') : (condition.values[0] ?? ''));
  const monoValue = (condition: AccessCondition): boolean => condition.field === 'path' || condition.field === 'method' || (condition.field === 'client_ip' && isIpRange(condition.values[0] ?? ''));

  // 서버의 bypassCondition과 같은 검사: \\, 빈 조각, ., .., 끝나는 곳이 확실한 조각 끝의 점·공백을 받지 않습니다.
  const plainPathValue = (operator: string, raw: string): boolean => {
    const value = raw.trim();
    if (operator === 'matches') return true;
    if (value.includes('\\')) return false;
    const parts = value.split('/');
    const whole = operator === 'equals' || operator === 'ends_with';
    return parts.every((part, index) => ((index === 0 || index === parts.length - 1) && part === '') || (part !== '' && part !== '.' && part !== '..' && !((index < parts.length - 1 || whole) && /[. ]$/.test(part))));
  };

  // 서버의 shadows와 같은 판단(보수적으로, 확실할 때만): 앞선 활성화된 정책이 이 정책의 주소를 모두 덮고, 앞선 정책의 조건마다
  // 항상 성립하거나 이 정책의 어떤 조건이 성립할 때 반드시 성립하면, 이 정책은 적용될 일이 없습니다.
  const hostCovers = (pattern: string, host: string): boolean => pattern === host || (pattern.startsWith('*.') && host.endsWith(pattern.slice(1)));
  // 같은 주소, 와일드카드가 덮는 주소, 서로 겹치는 와일드카드는 같은 요청을 받을 수 있습니다.
  const hostsOverlap = (a: string, b: string): boolean =>
    hostCovers(a, b) || hostCovers(b, a) || (a.startsWith('*.') && b.startsWith('*.') && (a.endsWith(b.slice(1)) || b.endsWith(a.slice(1))));
  const covers = (outer: string[], inner: string[], match: (inner: string, outer: string) => boolean = (a, b) => a === b): boolean =>
    inner.every((value) => outer.some((other) => match(value, other)));
  const methodCovers = (method: string, other: string): boolean => method === other;
  // IP·CIDR이 다른 IP·CIDR 안에 있는지는 같은 값일 때만 확실하다고 봅니다(태그도 같은 이름일 때만).
  const always = (condition: AccessCondition, action: AccessAction): boolean =>
    condition.field === 'path' && !condition.not && !strictPath(action, false) && condition.operator === 'starts_with' && condition.values.includes('/');
  const implies = (later: AccessCondition, laterAction: AccessAction, earlier: AccessCondition, earlierAction: AccessAction): boolean => {
    if (later.field !== earlier.field || later.not !== earlier.not) return false;
    if (later.field === 'path') {
      if (strictPath(laterAction, later.not) !== strictPath(earlierAction, earlier.not)) return false;
      if (earlier.not) return later.operator === earlier.operator && covers(later.values, earlier.values);
      // 넓게 비교하는 '같음' 값은 끝에 /를 붙인 경로로도 비교하므로, '다음으로 시작'에 대해서도 그 형태로 봅니다(서버와 같음).
      const slashed = (value: string): string => (strictPath(laterAction, false) || value === '/' ? value : `${value}/`);
      return covers(earlier.values, later.values, (value, other) =>
        (later.operator === earlier.operator && value === other)
        || (earlier.operator === 'starts_with' && later.operator === 'starts_with' && value.startsWith(other))
        || (earlier.operator === 'starts_with' && later.operator === 'equals' && slashed(value).startsWith(other)));
    }
    if (later.field === 'token') {
      if (earlier.not) return later.operator === 'any' || (earlier.operator === 'in' && covers(later.values, earlier.values));
      return earlier.operator === 'any' || (later.operator === 'in' && covers(earlier.values, later.values));
    }
    // 접속 주소를 알 수 없는 요청에는 우회가 아닌 정책의 부정 접속 IP만 성립하므로, 앞선 우회가 뒤의 다른 정책을 덮지 못합니다(서버와 같음).
    // 우회의 부정 메서드도 콘솔이 아는 메서드에만 성립합니다.
    if ((later.field === 'client_ip' || later.field === 'method') && earlier.not && earlierAction === 'bypass' && laterAction !== 'bypass') return false;
    // 메서드·접속 IP: 부정이 아니면 이 정책의 값이 모두 앞선 정책의 값 안에, 부정이면 그 반대입니다.
    return earlier.not ? covers(later.values, earlier.values, methodCovers) : covers(earlier.values, later.values, methodCovers);
  };
  const neverApplies = (list: AccessPolicy[], index: number): boolean => {
    const later = list[index];
    return list.slice(0, index).some((earlier) =>
      earlier.enabled
      && later.hosts.every((host) => earlier.hosts.some((pattern) => hostCovers(pattern, host)))
      && earlier.conditions.every((condition) => always(condition, earlier.action) || later.conditions.some((own) => implies(own, later.action, condition, earlier.action))));
  };
  // 접속 IP·접근 토큰 조건 row가 성립하지 않는 요청에 condition이 반드시 성립하는지(보수적으로, 확실할 때만). 접속 주소를 알 수
  // 없는 요청에는 "포함"이 성립하지 않고, "포함되지 않음"은 우회가 아닌 정책에서만 성립합니다(서버의 holds와 같음).
  const holdsWhenFails = (condition: AccessCondition, conditionAction: AccessAction, row: AccessCondition, rowAction: AccessAction): boolean => {
    if (condition.field !== row.field || condition.not === row.not) return false;
    if (row.field === 'client_ip') {
      return row.not ? rowAction !== 'bypass' && covers(condition.values, row.values) : conditionAction !== 'bypass' && covers(row.values, condition.values);
    }
    if (row.field === 'token') {
      if (!row.not) return row.operator === 'any' || (condition.operator === 'in' && covers(row.values, condition.values));
      return condition.operator === 'any' || (row.operator === 'in' && covers(condition.values, row.values));
    }
    return false;
  };
  // 앱이 받는 그대로 비교하는 경로 값(우회, 또는 보호 정책의 부정 경로)을 넓게 비교하는 형태로 바꿉니다: 그런 값은 평범한 경로라
  // 서버의 정규화는 소문자로 바꾸고 '같음'의 끝 /를 떼는 것뿐입니다(normalizeCondition). 정규식은 그대로 대소문자 없이 비교됩니다.
  // 소문자 변환은 ASCII만 합니다: 브라우저와 Go는 일부 유니코드(İ 등)를 다르게 바꾸므로, 그런 값은 일치하지 않는 쪽으로 봅니다.
  const asciiLower = (value: string): string => value.replace(/[A-Z]/g, (letter) => letter.toLowerCase());
  const broadValue = (operator: string, value: string): string =>
    operator === 'matches' ? value : operator === 'equals' && value !== '/' ? asciiLower(value).replace(/\/+$/, '') || '/' : asciiLower(value);
  const broadened = (condition: AccessCondition): AccessCondition => ({ ...condition, values: condition.values.map((value) => broadValue(condition.operator, value)) });
  // later가 성립하는 요청에 earlier도 반드시 성립하는지(경로). 비교 방식이 다르면 앱이 받는 그대로 비교하는 쪽이 성립하는 요청은
  // 평범한 경로라, 그 값을 넓은 형태로 바꿔 같은 방식으로 비교합니다. 부정이 아니면 later가, 부정이면 earlier가 그쪽이어야 합니다.
  const pathImplies = (later: AccessCondition, laterAction: AccessAction, earlier: AccessCondition, earlierAction: AccessAction): boolean => {
    if (later.field !== 'path' || earlier.field !== 'path' || later.not !== earlier.not) return false;
    const laterStrict = strictPath(laterAction, later.not);
    const earlierStrict = strictPath(earlierAction, earlier.not);
    if (laterStrict === earlierStrict) return implies(later, laterAction, earlier, earlierAction);
    if (!later.not && laterStrict) return implies(broadened(later), earlierAction, earlier, earlierAction);
    if (later.not && earlierStrict) return implies(later, laterAction, broadened(earlier), laterAction);
    return false;
  };
  // other가 그 주소에서 policy의 경로·메서드 조건에 맞고 row에 맞지 않는 요청 모두에 적용되는지. 조건마다 항상 성립하거나, row가
  // 성립하지 않을 때 성립하거나, policy의 경로·메서드 조건이 성립할 때 반드시 성립해야 합니다.
  const takesFailures = (other: AccessPolicy, host: string, policy: AccessPolicy, row: AccessCondition): boolean =>
    other.enabled && other.hosts.some((pattern) => hostCovers(pattern, host))
    && other.conditions.every((condition) => always(condition, other.action)
      || holdsWhenFails(condition, other.action, row, policy.action)
      || policy.conditions.some((own) => (own.field === 'path' ? pathImplies(own, policy.action, condition, other.action)
        : own.field === 'method' && implies(own, policy.action, condition, other.action))));
  // 접속 IP·접근 토큰 조건이 있는 허용·우회 정책은 같은 경로라도 조건에 맞지 않는 요청을 다른 정책에 넘깁니다. 위아래 어디에도
  // 그런 요청을 모두 받는 정책이 없으면(예: 위에 "접속 IP 다음에 포함되지 않음 → 차단", 아래에 그 주소의 모든 요청을 받는 정책)
  // 그런 요청은 보호 없이 전달됩니다. 차단 정책은 일부만 막는 것이 의도일 수 있어 표시하지 않습니다.
  // 그런 요청이 보호 없이 전달되는 주소들입니다.
  const unprotectedHosts = (list: AccessPolicy[], index: number): string[] => {
    const policy = list[index];
    const rows = policy.conditions.filter((condition) => condition.field === 'client_ip' || condition.field === 'token');
    if (!policy.enabled || policy.action === 'block' || !rows.length) return [];
    return policy.hosts.filter((host) => rows.some((row) => !list.some((other, at) => at !== index && takesFailures(other, host, policy, row))));
  };
  const unprotected = (list: AccessPolicy[], index: number): boolean => unprotectedHosts(list, index).length > 0;
  // 정책 이름은 64자까지라, 덧붙인 이름이 넘치면 원래 이름을 줄입니다.
  const maxName = 64;
  const suffixed = (name: string, make: (name: string) => string): string => {
    const chars = Array.from(name);
    const extra = Array.from(make('')).length;
    return make(chars.length + extra > maxName ? chars.slice(0, Math.max(1, maxName - extra - 1)).join('') + '…' : name);
  };
  // 자동 해결: 이 정책의 경로·메서드 조건만 가진 차단 정책을, 그 주소에 걸리는 마지막 정책 아래에 둡니다. 그래서 다른 정책이
  // 처리하던 요청은 그대로 그 정책이 처리하고, 어떤 정책에도 해당하지 않아 보호 없이 전달되던 요청만 차단됩니다. 주소는 그런
  // 요청이 있던 주소만 담습니다.
  const withLeftoversBlocked = (list: AccessPolicy[], index: number): AccessPolicy[] => {
    const hosts = unprotectedHosts(list, index);
    if (!hosts.length) return list;
    const policy = list[index];
    const block: AccessPolicy = {
      id: '', name: suffixed(policy.name, t().fixName), enabled: true, action: 'block', hosts,
      conditions: policy.conditions.filter((condition) => condition.field === 'path' || condition.field === 'method').map((condition) => ({ ...condition, values: [...condition.values] })),
      allow_everyone: false, allowed_users: [], allowed_groups: [], allow_admins: false,
    };
    const at = list.reduce((last, other, i) => (i > index && other.hosts.some((host) => hosts.some((own) => hostsOverlap(host, own))) ? i : last), index);
    return [...list.slice(0, at + 1), block, ...list.slice(at + 1)];
  };
  // 차단 정책에서는 부정 경로를 앱이 받는 그대로 비교하므로, 우회의 부정 경로 값 중에는 차단 정책에 쓸 수 없는 것이 있습니다.
  const blockable = (policy: AccessPolicy): boolean =>
    policy.conditions.every((condition) => condition.field !== 'path' || !strictPath('block', condition.not) || condition.values.every((value) => plainPathValue(condition.operator, value)));

  let policies = $state<AccessPolicy[] | null>(null);
  // 적용 주소가 겹치는 정책(같은 주소, 와일드카드가 덮는 주소, 서로 겹치는 와일드카드)끼리 묶고, 겹침이 이어지면 한 묶음으로 봅니다.
  // 정책이 둘 이상인 묶음에만 목록에서 처음 나오는 순서대로 색을 붙입니다(그룹이 6개를 넘으면 색이 반복됩니다).
  const groupColors = ['bg-group-1', 'bg-group-2', 'bg-group-3', 'bg-group-4', 'bg-group-5', 'bg-group-6'];
  const groups = $derived.by(() => {
    const list = policies ?? [];
    const root = list.map((_, index) => index);
    const find = (index: number): number => (root[index] === index ? index : (root[index] = find(root[index])));
    list.forEach((policy, index) => list.slice(0, index).forEach((earlier, at) => {
      if (policy.hosts.some((host) => earlier.hosts.some((other) => hostsOverlap(host, other)))) root[find(index)] = find(at);
    }));
    const sizes = new Map<number, number>();
    list.forEach((_, index) => sizes.set(find(index), (sizes.get(find(index)) ?? 0) + 1));
    const numbers = new Map<number, number>();
    const out = new Map<string, number>();
    list.forEach((policy, index) => {
      const group = find(index);
      if ((sizes.get(group) ?? 0) < 2) return;
      if (!numbers.has(group)) numbers.set(group, numbers.size);
      out.set(policy.id, numbers.get(group)!);
    });
    return out;
  });
  let addresses = $state<string[]>([]);
  let damaged = $state(false);
  let loadError = $state('');
  let error = $state('');
  let busy = $state(false);

  let dialogOpen = $state(false);
  let confirmOpen = $state(false);
  let deleteName = $state('');
  let editingId = $state('');
  let form = $state<{ name: string; enabled: boolean; action: AccessAction; hosts: HostRow[]; rows: ConditionRow[]; everyone: boolean; users: string[]; groups: string[]; admins: boolean }>({
    name: '', enabled: true, action: 'allow', hosts: [], rows: [], everyone: false, users: [], groups: [], admins: false,
  });
  let formError = $state('');

  const editing = $derived(editingId ? policies?.find((policy) => policy.id === editingId) : undefined);
  // 복제한 정책은 원본 바로 아래에 저장합니다.
  let copyOf = $state('');
  // 저장된 목록에서 이 정책에 조건 외 요청 미보호 표시가 있으면(적용 안 됨이 아니면) 자동 해결을 보여 줍니다.
  const fixable = $derived(!!policies && !!editingId && policies.some((policy, index) => policy.id === editingId && !neverApplies(policies!, index) && unprotected(policies!, index)));
  // 여는 쪽 '다음으로 시작' 값이 /로 끝나지 않으면 /v1이 /v1x, /v10까지 엽니다. 저장은 막지 않고 알리기만 합니다.
  const openPrefix = (row: ConditionRow): string => {
    if (row.field !== 'path' || operatorOf(row) !== 'starts_with' || !strictPath(form.action, negated(row))) return '';
    return row.values.find((value) => value.trim() !== '' && !value.trim().endsWith('/'))?.trim() ?? '';
  };

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
    onsaved?.();
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
    copyOf = '';
    form = {
      name: policy?.name ?? '',
      enabled: policy?.enabled ?? true,
      action: policy?.action ?? 'allow',
      hosts: policy ? policy.hosts.map((host) => newHost(host)) : [newHost()],
      rows: policy ? policy.conditions.map((condition) => newRow(condition)) : [],
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
    form.rows.push(row);
    await tick();
    document.getElementById(`access-policy-field-${row.key}`)?.focus();
  }

  // 줄마다 서버와 같은 검사를 먼저 하고, 걸리면 그 이유를 돌려줍니다.
  function rowError(row: ConditionRow): string {
    const operator = operatorOf(row);
    if (row.field === 'token' && operator === 'any') return '';
    if (row.field === 'token') {
      const live = tokens ? row.values.filter((id) => tokens.some((token) => token.id === id)) : row.values;
      return live.length ? '' : t().needsToken;
    }
    if (!row.values.length) return row.field === 'client_ip' ? t().ipRequired : t().needsValue;
    if (row.field === 'path' && row.values.length > maxPathValues) return t().pathsTooMany;
    if (row.field === 'path') {
      // 요청 경로는 디코딩하고 ;매개변수를 뺀 형태로 비교하므로, 그런 값은 어떤 요청과도 일치하지 않습니다.
      if (operator !== 'matches' && row.values.some((value) => /%[0-9a-f]{2}|;/i.test(value))) return t().decodedPaths;
      if (strictPath(form.action, negated(row)) && row.values.some((value) => !plainPathValue(operator, value))) return t().strictPaths;
    }
    if (row.field === 'client_ip') {
      // 주소록에 없는 태그는 서버가 거부하므로, 목록을 아는 때에만 미리 알립니다.
      const scope = splitScope(row.values);
      const unknown = ipTags ? unknownTags(scope.tags, ipTags) : [];
      if (unknown.length) return t().unknownTag(unknown[0]);
      if (scope.tags.length > maxIpTags || scope.ranges.length > maxIpRanges) return t().ipTooMany;
    }
    return '';
  }

  // 대화상자의 입력으로 정책을 만듭니다. 걸리는 검사가 있으면 그 이유를 보여 주고 null을 돌려줍니다.
  function built(): AccessPolicy | null {
    const allow = form.action === 'allow';
    // 허용 대상은 허용 정책에만 있고, 사용자 전체를 고르면 사용자·그룹·관리자는 따로 둘 필요가 없습니다.
    const named = allow && !form.everyone;
    if (allow && !form.everyone && !form.users.length && !form.groups.length && !form.admins) {
      formError = t().needsWho;
      return null;
    }
    const problem = form.rows.map(rowError).find(Boolean);
    if (problem) {
      formError = problem;
      return null;
    }
    const conditions: AccessCondition[] = form.rows.map((row) => {
      const operator = operatorOf(row) as AccessCondition['operator'];
      // 지워진 토큰은 빼고 보냅니다. 메서드는 서버처럼 accessMethods 순서로 둡니다.
      const values = row.field === 'token' ? (operator === 'any' ? [] : tokens ? row.values.filter((id) => tokens.some((token) => token.id === id)) : [...row.values])
        : row.field === 'method' ? accessMethods.filter((method) => row.values.includes(method))
        : row.values.map((value) => value.trim()).filter(Boolean);
      return { field: row.field, operator, not: negated(row), values };
    });
    return {
      id: editing?.id ?? '',
      name: form.name.trim(),
      enabled: form.enabled,
      action: form.action,
      hosts: form.hosts.map((row) => row.value.trim().toLowerCase()).filter(Boolean),
      conditions,
      allow_everyone: allow && form.everyone,
      allowed_users: named ? [...form.users] : [],
      allowed_groups: named ? [...form.groups] : [],
      allow_admins: named && form.admins,
    };
  }

  // 편집 중인 정책을 바꾸거나 새 정책을 넣은 목록입니다. 새 정책은 끝에, 복제한 정책은 원본 바로 아래에 둡니다.
  function withForm(next: AccessPolicy): AccessPolicy[] {
    const list = $state.snapshot(policies) ?? [];
    if (next.id) return list.map((entry) => (entry.id === next.id ? next : entry));
    const at = copyOf ? list.findIndex((entry) => entry.id === copyOf) : -1;
    return at < 0 ? [...list, next] : [...list.slice(0, at + 1), next, ...list.slice(at + 1)];
  }

  async function saveForm(list: AccessPolicy[], done: string): Promise<void> {
    busy = true;
    formError = '';
    try {
      await put(list);
      dialogOpen = false;
      toast.success(done);
    } catch (cause) {
      formError = describe(errorMessage(cause));
    } finally {
      busy = false;
    }
  }

  async function submit(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!policies || busy) return;
    const next = built();
    if (next) await saveForm(withForm(next), t().saved);
  }

  // 대화상자의 입력을 저장하면서, 그래도 조건 외 요청이 보호 없이 전달되면 바로 아래에 그 요청을 차단하는 정책을 추가합니다.
  async function fix(): Promise<void> {
    if (!policies || busy || !editing) return;
    const next = built();
    if (!next) return;
    const list = withForm(next);
    const fixed = withLeftoversBlocked(list, list.findIndex((entry) => entry.id === next.id));
    const block = fixed.find((entry) => !entry.id && !list.includes(entry));
    if (block && !blockable(block)) {
      formError = t().fixUnavailable;
      return;
    }
    await saveForm(fixed, block ? t().fixed(block.name) : t().saved);
  }

  // 복제: 대화상자의 입력 그대로 새 정책 추가로 바꿉니다. 저장하면 원본 바로 아래에 들어갑니다.
  async function duplicate(): Promise<void> {
    if (!editing || busy) return;
    copyOf = editing.id;
    editingId = '';
    form.name = suffixed(form.name.trim() || editing.name, t().copyName);
    formError = '';
    await tick();
    const input = document.getElementById('access-policy-name') as HTMLInputElement | null;
    input?.focus();
    input?.select();
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
                    {#if groups.has(policy.id)}
                      {@const group = groups.get(policy.id)!}
                      <span class={['absolute inset-y-0 left-0 w-1', groupColors[group % groupColors.length]]} aria-hidden="true"></span>
                      <span class="sr-only">{t().group(group + 1)}</span>
                    {/if}
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
                      {:else if unprotected(policies, index)}
                        <Tooltip.Root>
                          <Tooltip.Trigger class="relative z-10 outline-none focus-visible:ring-3 focus-visible:ring-ring/50">
                            <Badge variant="destructive" class="select-none">{t().unprotected}</Badge>
                          </Tooltip.Trigger>
                          <Tooltip.Content>{t().unprotectedTip}</Tooltip.Content>
                        </Tooltip.Root>
                      {/if}
                    </span>
                  </Table.Cell>
                  <!-- 적용 조건은 주소를 위에, 조건 줄을 아래에 필드·연산자는 흐리게, 첫 값과 나머지 개수로 보여 줍니다. -->
                  <Table.Cell>
                    <div class="grid gap-0.5">
                      <span class="flex items-center gap-1.5">
                        <span class="max-w-56 truncate font-mono">{policy.hosts[0]}</span>
                        {#if policy.hosts.length > 1}<span class="text-muted-foreground tabular-nums">+{(policy.hosts.length - 1).toLocaleString(intlLocale())}</span>{/if}
                      </span>
                      <span class="flex flex-wrap items-center gap-x-3 text-muted-foreground">
                        {#each policy.conditions as condition, row (row)}
                          <span class="flex items-center gap-1.5">
                            {t().fields[condition.field]} {operatorText(condition.field, choiceOf(condition))}
                            {#if condition.values.length}
                              <span class={['max-w-40 truncate text-foreground', monoValue(condition) && 'font-mono']}>{conditionValue(condition)}</span>
                              {#if condition.values.length > 1}<span class="tabular-nums">+{(condition.values.length - 1).toLocaleString(intlLocale())}</span>{/if}
                            {/if}
                          </span>
                        {:else}
                          {t().allRequests}
                        {/each}
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
        <Field.Legend>{t().scope}</Field.Legend>
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
          <!-- 조건 작성기: 줄마다 필드·연산자·값이고, 모든 줄이 성립해야 적용됩니다. 넓은 화면에서는 한 줄, 좁으면 값이 아래로 내려갑니다. -->
          <Item.Root variant="outline" size="sm" class="flex-col items-stretch bg-muted/50">
            <Field.Set>
              <Field.Legend variant="label" class="flex items-center gap-1">{t().conditionRows}<InfoTooltip id="access-policy-conditions-tip" label={t().conditionRows} description={t().conditionsTip} /></Field.Legend>
              <Field.Description>{form.rows.length ? t().conditionsHint : t().noConditions}</Field.Description>
              {#if form.rows.length}
                <div class="grid gap-3">
                  {#each form.rows as row, index (row.key)}
                    {@const operator = operatorOf(row)}
                    {@const strict = row.field === 'path' && strictPath(form.action, negated(row))}
                    <div class="grid gap-1.5">
                      <div class="flex items-start gap-2">
                        <div class="flex min-w-0 flex-1 flex-wrap items-start gap-2">
                          <NativeSelect.Root id={`access-policy-field-${row.key}`} class="w-28" aria-label={`${t().field} ${index + 1}`} value={row.field} onchange={(event: Event) => setField(row, (event.currentTarget as HTMLSelectElement).value)}>
                            {#each fields as field (field)}
                              <NativeSelect.Option value={field}>{t().fields[field]}</NativeSelect.Option>
                            {/each}
                          </NativeSelect.Root>
                          <NativeSelect.Root class="w-48" aria-label={`${t().operator} ${index + 1}`} bind:value={row.choice}>
                            {#each operatorChoices[row.field] as choice (choice)}
                              <NativeSelect.Option value={choice}>{operatorText(row.field, choice)}</NativeSelect.Option>
                            {/each}
                          </NativeSelect.Root>
                          <div class="min-w-56 flex-1">
                            {#if row.field === 'path'}
                              <TagInput id={`access-policy-value-${row.key}`} ariaLabel={`${t().value} ${index + 1}`} bind:values={row.values} mono commas={false} placeholder={t().placeholders[operator as BlockOperator]} />
                            {:else if row.field === 'client_ip'}
                              <TagInput id={`access-policy-value-${row.key}`} ariaLabel={`${t().value} ${index + 1}`} bind:values={row.values} suggestions={tagSuggestions(ipTags)} autoselect secondary={(value) => !isIpRange(value)} mono placeholder={t().ipPlaceholder} />
                            {:else if row.field === 'method'}
                              <div class="flex min-h-8 flex-wrap items-center gap-x-4 gap-y-2" role="group" aria-label={`${t().value} ${index + 1}`}>
                                {#each accessMethods as method (method)}
                                  <Field.Field orientation="horizontal" class="w-auto">
                                    <Checkbox
                                      id={`access-policy-method-${row.key}-${method}`}
                                      checked={row.values.includes(method)}
                                      onCheckedChange={(checked) => { row.values = checked ? [...row.values, method] : row.values.filter((entry) => entry !== method); }}
                                    />
                                    <Field.Label for={`access-policy-method-${row.key}-${method}`} class="font-mono font-normal">{method}</Field.Label>
                                  </Field.Field>
                                {/each}
                              </div>
                            {:else if operator === 'in'}
                              {#if tokens?.length}
                                <div class="flex min-h-8 flex-wrap items-center gap-x-6 gap-y-2" role="group" aria-label={`${t().value} ${index + 1}`}>
                                  {#each tokens as token (token.id)}
                                    <Field.Field orientation="horizontal" class="w-auto">
                                      <Checkbox
                                        id={`access-policy-token-${row.key}-${token.id}`}
                                        checked={row.values.includes(token.id)}
                                        onCheckedChange={(checked) => { row.values = checked ? [...row.values, token.id] : row.values.filter((id) => id !== token.id); }}
                                      />
                                      <Field.Label for={`access-policy-token-${row.key}-${token.id}`} class="min-w-0 font-normal">
                                        <span class="truncate">{token.name}</span>
                                        {#if token.hosts?.length}<span class="truncate font-mono text-muted-foreground">{token.hosts[0]}{token.hosts.length > 1 ? ` +${token.hosts.length - 1}` : ''}</span>{/if}
                                      </Field.Label>
                                    </Field.Field>
                                  {/each}
                                </div>
                              {:else if tokens}
                                <p class="flex min-h-8 items-center text-xs text-muted-foreground">{t().noTokens}</p>
                              {/if}
                            {/if}
                          </div>
                        </div>
                        <IconButton label={t().removeCondition} icon={Dismiss} variant="outline" size="icon" onclick={() => form.rows.splice(index, 1)} />
                      </div>
                      {#if openPrefix(row)}
                        <Field.Description class="flex items-start gap-1"><Warning12 class="mt-0.75 size-3 shrink-0" aria-hidden="true" />{t().openPrefix(openPrefix(row))}</Field.Description>
                      {:else if strict}
                        <Field.Description>{t().strictNote}</Field.Description>
                      {:else if row.field === 'path' && operator === 'matches'}
                        <Field.Description>{t().regexNote}</Field.Description>
                      {:else if row.field === 'method'}
                        <Field.Description>{t().headNote}</Field.Description>
                      {/if}
                    </div>
                  {/each}
                </div>
              {/if}
              <div class="flex justify-end">
                <Button variant="outline" size="sm" disabled={form.rows.length >= maxRows} onclick={addCondition}><Add12 aria-hidden="true" />{t().addCondition}</Button>
              </div>
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
                {@const hint = t().actionHints[action]}
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
          <div class="flex flex-col-reverse gap-2 sm:flex-row">
            <Button variant="destructive" disabled={busy} onclick={() => { deleteName = editing?.name ?? ''; confirmOpen = true; }}>{t().delete}</Button>
            <Button variant="outline" disabled={busy || (policies?.length ?? 0) >= maxPolicies} onclick={duplicate}><Copy aria-hidden="true" />{t().duplicate}</Button>
          </div>
        {/if}
        <div class="flex flex-col-reverse gap-2 sm:ml-auto sm:flex-row">
          {#if editingId && fixable}
            <Tooltip.Root>
              <Tooltip.Trigger>
                {#snippet child({ props })}
                  <Button {...props} variant="outline" disabled={busy || (policies?.length ?? 0) >= maxPolicies} onclick={fix}><Shield20 aria-hidden="true" />{t().fix}</Button>
                {/snippet}
              </Tooltip.Trigger>
              <Tooltip.Content>{t().fixTip}</Tooltip.Content>
            </Tooltip.Root>
          {/if}
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
