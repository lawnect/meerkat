<script lang="ts">
  import { Add, ArrowReset20, ArrowRight, ArrowSync, ArrowTrending12, Box, Checkmark, Code, Copy, Cube20, Delete, Globe12, Grid, Home12, Info, Key, NumberSymbol, PlugConnected20, Search, Search20, Storage, Tag, TextBulletListLtr, Warning } from '../icons';
  import { onMount } from 'svelte';
  import { type FilterFn, createColumnHelper, createTable, renderComponent, renderSnippet } from '@tanstack/svelte-table';
  import DataTable from '../components/data-table/DataTable.svelte';
  import DataTablePagination from '../components/data-table/DataTablePagination.svelte';
  import SortHeader from '../components/data-table/SortHeader.svelte';
  import { features } from '../components/data-table/features';
  import * as Alert from '$lib/components/ui/alert/index.js';
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Button } from '$lib/components/ui/button/index.js';
  import * as ButtonGroup from '$lib/components/ui/button-group/index.js';
  import * as Card from '$lib/components/ui/card/index.js';
  import * as Dialog from '$lib/components/ui/dialog/index.js';
  import * as Empty from '$lib/components/ui/empty/index.js';
  import * as Field from '$lib/components/ui/field/index.js';
  import * as InputGroup from '$lib/components/ui/input-group/index.js';
  import * as Item from '$lib/components/ui/item/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { Textarea } from '$lib/components/ui/textarea/index.js';
  import * as Tabs from '$lib/components/ui/tabs/index.js';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { api, connectorLabel, errorMessage, mockApiEnabled, statusLabel } from '../api';
  import { connectorState, forgetEnrollment, rememberEnrollment, storedEnrollment } from '../connectorState.svelte';
  import { intlLocale, translations } from '../i18n.svelte';
  import { connectionPollMs } from '../polling';
  import { urlTableState } from '../components/data-table/urlTable.svelte';
  import { oneOf, param, setParams } from '../urlState.svelte';
  import { copyToClipboard } from '../toast.svelte';
  import ConfirmNameDialog from '../components/ConfirmNameDialog.svelte';
  import IconButton from '../components/IconButton.svelte';
  import InfoTooltip from '../components/InfoTooltip.svelte';
  import PageHead from '../components/PageHead.svelte';
  import RouteGraph from '../components/RouteGraph.svelte';
  import ServiceBlock from '../components/ServiceBlock.svelte';
  import ShareChart from '../components/ShareChart.svelte';
  import StatCard from '../components/StatCard.svelte';
  import Status from '../components/Status.svelte';
  import TruncateText from '../components/TruncateText.svelte';
  import TunnelPreview from '../components/TunnelPreview.svelte';
  import UsageChart from '../components/UsageChart.svelte';
  import VersionBadge from '../components/VersionBadge.svelte';
  import { connectorDetailPath, pageHref } from '../navigation';
  import type { Connector, ConnectorUsage, DashboardData, EnrollmentToken } from '../types';

  // 배열로 정의한 문구는 홀수 번째 항목이 코드 조각입니다. 툴팁에서는 이어 붙여 보여줍니다.
  const t = translations({
    tunnels: 'Tunnels',
    newTunnel: 'New tunnel',
    searchFilterAria: 'Search and filter tunnels',
    search: 'Search',
    searchPlaceholder: 'Search by tunnel name',
    connectionStatus: 'Connection status',
    filterAll: 'All statuses',
    filterConnected: 'Connected',
    filterPending: 'Pending',
    filterProblem: 'Problem',
    domainFilter: 'Domain',
    allDomains: 'All domains',
    noDomainAddresses: 'No public address',
    reset: 'Reset filters',
    tunnelListAria: 'Tunnel list',
    viewAria: 'View mode',
    viewCards: 'Card view',
    viewTable: 'Table view',
    colName: 'Name',
    colServices: 'Services',
    colLastSeen: 'Last seen',
    colCreated: 'Created',
    serviceCount: (n: number) => `${n} ${n === 1 ? 'service' : 'services'}`,
    noServices: 'None',
    servicesCount: (n: number) => `${n} ${n === 1 ? 'service' : 'services'}`,
    createdOn: (date: string) => `Created ${date}`,
    detailsAria: (name: string) => `View details for tunnel ${name}`,
    noMatchesTitle: 'No matching tunnels',
    clearFilters: 'Clear filters',
    noTunnelsTitle: 'No tunnels yet',
    noTunnels: 'Create a tunnel and install it on your server to publish your apps.',

    detailTabsAria: 'Tunnel sections',
    tabOverview: 'Overview',
    tabUsage: 'Usage',
    tabRoutes: 'Routes',
    infoTitle: 'Details',
    services: 'Services',
    publicAddresses: 'Public addresses',
    publicAddress: 'Public address',
    lastSeenAt: (time: string) => `Last seen ${time}`,
    noRecord: 'No record',

    usage: 'Usage',
    usageAria: (period: string) => `Usage, ${period}`,
    last24h: 'Last 24 hours',
    last7d: 'Last 7 days',
    last30d: 'Last 30 days',
    usageUpdated: (delayed: boolean, time: string) => `${delayed ? 'Collection delayed · ' : ''}Last updated ${time}`,
    usageWaiting: 'Waiting for data',
    period: 'Period',
    hours24: '24 hours',
    days7: '7 days',
    days30: '30 days',
    service: 'Service',
    allServices: 'All services',
    usageLoadFailed: 'Couldn\'t load usage.',
    usageLoading: 'Loading usage…',
    requests: 'Requests',
    count: (n: number) => n.toLocaleString(intlLocale()),
    usageByService: 'By service',
    usageByServiceAria: (total: number) => `Requests by service, ${total.toLocaleString(intlLocale())} in total`,

    installAria: (name: string) => `${name} tunnel install guide`,
    installTitle: 'Install and connect the tunnel',
    installHelp: 'Run the tunnel on your server and check its connection. Register app addresses as services of this tunnel.',
    stepsAria: 'Setup progress',
    stepCreate: 'Create tunnel',
    stepInstall: 'Install',
    stepConnect: 'Connect',
    stepService: 'Add service',
    checkConnection: 'Check connection',
    checking: 'Checking…',
    polling: (seconds: number) => `Checking the connection every ${seconds} s`,
    statusDraft: 'Install and start the tunnel on your server to connect it.',
    statusConnecting: 'The tunnel is getting ready to connect.',
    statusConnected: 'The tunnel is connected.',
    statusStale: 'The tunnel isn\'t responding. Check the installed container and its network.',
    checkPending: 'The tunnel connection hasn\'t been confirmed yet.',
    checkFailed: 'Couldn\'t check the connection. Try again in a moment.',
    regenerate: 'Generate install instructions again',
    noRunCode: 'No run code available. Replace the token to get a new one.',
    resetAndInstall: 'Reset connection and generate install command',
    installCodeNote: 'The install code contains the tunnel run token. Use the same code when restarting; resetting the connection invalidates the old code.',
    installMethodsAria: 'Install method',
    tabDocker: 'Docker command',
    terminalIntro: 'Run the command below on the server that will host the tunnel. No persistent volume is needed.',
    resetNote: 'Stop and remove the existing tunnel container, then run the new command.',
    runCommandNoun: 'run command',
    tunnelServiceNoun: 'tunnel service config',
    composeFileNoun: 'Compose file',
    copyLabel: (noun: string) => `Copy ${noun}`,
    composeChoiceAria: 'How to use the Compose file',
    composeExisting: 'Add to existing file',
    composeNew: 'Create a new file',
    existingIntro: ['Add the tunnel service under ', 'services', ' in your existing Compose file. You can set the app network when you register a service.'],
    existingNote: ['If a ', 'connector', ' service already exists, update it instead of adding another. Keep the ', 'command', ' line that contains the run token.'],
    newIntro: ['Leave your existing Compose file alone and save the following as a dedicated ', 'meerkat-tunnel.yaml', ' file for the tunnel.'],
    newNote: ['Keep the ', 'command', ' line that contains the run token. Recreating the container reconnects with the same token.'],
    secretNote: 'Keep the install code, which contains the run token, secret. Issuing a new token invalidates the previous one.',
    mockNote: 'No real tunnel connects in the preview.',
    mockConnect: 'Switch to connected',

    servicesAria: 'Services',
    servicesHelp: 'The internal address of an app this tunnel forwards requests to. One tunnel can connect several services, and they can be added before the tunnel connects.',
    addService: 'Add service',
    noServicesYet: 'No services yet',

    name: 'Name',
    runName: 'Run name',
    runNameHelp: 'Used by the install command and container name; can\'t be changed',
    editName: 'Edit name',
    nameSaveFailed: (code: string) => `Couldn't save the name: ${code}`,
    invalidDisplayName: 'Use 64 characters or fewer.',
    created: 'Created',
    descriptionLabel: 'Description',
    descriptionOptional: 'Description (optional)',
    descriptionPlaceholder: 'What runs here, who owns it…',
    addDescription: 'Add description',
    editDescription: 'Edit description',
    descriptionSaveFailed: (code: string) => `Couldn't save the description: ${code}`,
    save: 'Save',
    saving: 'Saving…',
    tokenNote: 'Replace it if it was exposed or you lost the install code. The previous token is invalidated immediately.',
    replaceToken: 'Replace token',
    replacing: 'Replacing…',
    deleteTunnel: 'Delete tunnel',
    deleteNote: 'Its services and public addresses are deleted too.',

    requestPaths: 'Request paths',
    addServiceFromOverview: 'Add a service',
    missingTitle: 'Tunnel not found',
    missing: 'It may have been deleted, or the address is wrong.',
    backToTunnels: 'Back to tunnels',

    addServiceTitle: 'Add service',
    serviceDialogDesc: 'Enter the internal address of the app to forward requests to.',
    serviceName: 'Service name',
    appHost: 'App host',
    internalPort: 'Internal port',
    examplePlaceholder: 'e.g. app',
    examplePort: 'e.g. 80',
    exampleTunnel: 'e.g. app-tunnel',
    targetHelp: ['If the app is in the same Compose file, enter its name under ', 'services:', ' (for example ', 'app', ') as the app host and the port the app listens on in its container (for example ', '80', ') as the internal port. The tunnel and the app must share a Docker network.'],
    cancel: 'Cancel',
    registerService: 'Register service',
    registering: 'Registering…',
    newTunnelTitle: 'Create a tunnel',
    createDesc: 'Enter a name for the new tunnel.',
    tunnelName: 'Tunnel name',
    nameHelp: 'Letters, numbers, hyphens and underscores. It\'s also used in the container name.',
    createTunnel: 'Create tunnel',
    creating: 'Creating…',

    invalidTunnel: 'Check the tunnel name.',
    nameTaken: 'That tunnel name is already in use.',
    tunnelNotFound: 'Tunnel not found.',
    serviceNameTaken: 'That service name is already in use.',
    serviceTargetTaken: 'This tunnel already has a service with the same app host and port. Add a public address to the existing service instead.',
    invalidService: 'Check the service name and app address.',
    createFailed: (reason: string) => `Couldn't create the tunnel: ${reason}`,
    deleteFailed: (reason: string) => `Couldn't delete the tunnel: ${reason}`,
    serviceFailed: (reason: string) => `Couldn't register the service: ${reason}`,
    tokenFailed: (reason: string) => `Couldn't prepare the install instructions: ${reason}`,

    resetTitle: 'Replace run token',
    resetWarning: 'Every tunnel instance using the current token will disconnect. Apply the new install code to each instance to reconnect.',
    resetSubmit: 'Replace run token',
    deleteTitle: 'Delete tunnel',
    deleteWarning: 'This can\'t be undone, and running tunnel connections will drop. Your actual services aren\'t deleted.',
  }, {
    tunnels: '터널',
    newTunnel: '새 터널',
    searchFilterAria: '터널 검색 및 필터',
    search: '검색',
    searchPlaceholder: '터널 이름 검색',
    connectionStatus: '연결 상태',
    filterAll: '전체 상태',
    filterConnected: '연결됨',
    filterPending: '연결 대기',
    filterProblem: '연결 문제',
    domainFilter: '도메인',
    allDomains: '모든 도메인',
    noDomainAddresses: '공개 주소 없음',
    reset: '필터 초기화',
    tunnelListAria: '터널 목록',
    viewAria: '보기 방식',
    viewCards: '카드 보기',
    viewTable: '표 보기',
    colName: '이름',
    colServices: '서비스',
    colLastSeen: '최근 응답',
    colCreated: '생성일',
    serviceCount: (n: number) => `${n}개`,
    noServices: '미등록',
    servicesCount: (n: number) => `서비스 ${n}개`,
    createdOn: (date: string) => `${date} 생성`,
    detailsAria: (name: string) => `${name} 터널 상세 보기`,
    noMatchesTitle: '조건에 맞는 터널이 없습니다',
    clearFilters: '필터 초기화',
    noTunnelsTitle: '아직 터널이 없습니다',
    noTunnels: '터널을 만들어 서버에 설치하면 앱을 공개할 수 있습니다.',

    detailTabsAria: '터널 상세 항목',
    tabOverview: '개요',
    tabUsage: '사용량',
    tabRoutes: '경로',
    infoTitle: '기본 정보',
    services: '서비스',
    publicAddresses: '공개 주소',
    publicAddress: '공개 주소',
    lastSeenAt: (time: string) => `최근 응답 ${time}`,
    noRecord: '기록 없음',

    usage: '사용량',
    usageAria: (period: string) => `${period} 사용량`,
    last24h: '최근 24시간',
    last7d: '최근 7일',
    last30d: '최근 30일',
    usageUpdated: (delayed: boolean, time: string) => `${delayed ? '수집 지연 · ' : ''}마지막 집계 ${time}`,
    usageWaiting: '집계 대기',
    period: '기간',
    hours24: '24시간',
    days7: '7일',
    days30: '30일',
    service: '서비스',
    allServices: '전체 서비스',
    usageLoadFailed: '사용량을 불러오지 못했습니다.',
    usageLoading: '사용량을 불러오는 중입니다.',
    requests: '처리 건수',
    count: (n: number) => `${n.toLocaleString(intlLocale())}건`,
    usageByService: '서비스별',
    usageByServiceAria: (total: number) => `서비스별 처리 건수, 총 ${total.toLocaleString(intlLocale())}건`,

    installAria: (name: string) => `${name} 터널 설치 안내`,
    installTitle: '터널 설치 및 연결',
    installHelp: '서버에서 터널을 실행하고 연결 상태를 확인합니다. 앱 주소는 이 터널의 서비스로 등록합니다.',
    stepsAria: '설정 진행 단계',
    stepCreate: '터널 만들기',
    stepInstall: '설치',
    stepConnect: '연결',
    stepService: '서비스 추가',
    checkConnection: '연결 확인',
    checking: '확인 중…',
    polling: (seconds: number) => `${seconds}초마다 연결을 확인하는 중`,
    statusDraft: '서버에 터널을 설치하고 실행하면 연결됩니다.',
    statusConnecting: '터널이 연결을 준비 중입니다.',
    statusConnected: '터널이 연결되었습니다.',
    statusStale: '터널의 응답이 없습니다. 설치한 컨테이너와 네트워크를 확인하세요.',
    checkPending: '아직 터널 연결이 확인되지 않았습니다.',
    checkFailed: '연결 상태를 확인하지 못했습니다. 잠시 후 다시 시도하세요.',
    regenerate: '설치 안내 다시 만들기',
    noRunCode: '실행 코드가 없습니다. 토큰을 교체해 새 코드를 받으세요.',
    resetAndInstall: '연결 초기화 후 설치 명령 만들기',
    installCodeNote: '설치 코드에 터널 실행 토큰이 들어 있습니다. 재시작할 때도 같은 코드를 사용하며, 연결을 초기화하면 기존 코드는 무효가 됩니다.',
    installMethodsAria: '터널 설치 방법',
    tabDocker: 'Docker 명령',
    terminalIntro: '터널을 실행할 서버에서 아래 명령을 실행하세요. 별도의 영구 볼륨은 필요하지 않습니다.',
    resetNote: '기존 터널 컨테이너를 중지·삭제한 뒤 새 명령을 실행하세요.',
    runCommandNoun: '실행 명령',
    tunnelServiceNoun: '터널 서비스 설정',
    composeFileNoun: 'Compose 파일',
    copyLabel: (noun: string) => `${noun} 복사`,
    composeChoiceAria: 'Compose 파일 사용 방법',
    composeExisting: '기존 파일에 추가',
    composeNew: '새 파일로 만들기',
    existingIntro: ['기존 Compose 파일의 ', 'services', '에 터널 서비스를 추가합니다. 앱 네트워크는 서비스를 등록할 때 설정할 수 있습니다.'],
    existingNote: ['이미 ', 'connector', ' 서비스가 있다면 새로 추가하지 말고 해당 설정을 갱신하세요. 실행 토큰이 포함된 ', 'command', ' 줄은 유지해야 합니다.'],
    newIntro: ['기존 Compose 파일을 수정하지 않고, 아래 내용을 터널 전용 ', 'meerkat-tunnel.yaml', ' 파일로 저장하세요.'],
    newNote: ['실행 토큰이 포함된 ', 'command', ' 줄을 유지하세요. 컨테이너를 새로 만들어도 같은 토큰으로 다시 연결합니다.'],
    secretNote: '실행 토큰이 포함된 설치 코드는 비밀로 보관하세요. 새 토큰을 발급하면 이전 토큰은 무효가 됩니다.',
    mockNote: '미리보기에서는 실제 터널이 연결되지 않습니다.',
    mockConnect: '연결 상태로 전환',

    servicesAria: '서비스',
    servicesHelp: '이 터널이 요청을 전달할 앱의 내부 주소입니다. 한 터널에 여러 서비스를 연결할 수 있으며, 터널 연결 전에 미리 등록할 수 있습니다.',
    addService: '서비스 추가',
    noServicesYet: '등록된 서비스가 없습니다',

    name: '이름',
    runName: '실행 이름',
    runNameHelp: '설치 명령과 컨테이너 이름에 쓰여 바꿀 수 없습니다',
    editName: '이름 수정',
    nameSaveFailed: (code: string) => `이름을 저장하지 못했습니다: ${code}`,
    invalidDisplayName: '64자 이하로 입력하세요.',
    created: '생성일',
    descriptionLabel: '설명',
    descriptionOptional: '설명 (선택)',
    descriptionPlaceholder: '용도, 담당자 등',
    addDescription: '설명 추가',
    editDescription: '설명 수정',
    descriptionSaveFailed: (code: string) => `설명을 저장하지 못했습니다: ${code}`,
    save: '저장',
    saving: '저장 중…',
    tokenNote: '노출되거나 설치 코드를 잃어버렸다면 교체하세요. 이전 토큰은 즉시 무효가 됩니다.',
    replaceToken: '토큰 교체',
    replacing: '교체 중…',
    deleteTunnel: '터널 삭제',
    deleteNote: '서비스와 공개 주소도 함께 삭제됩니다.',

    requestPaths: '요청 경로',
    addServiceFromOverview: '서비스 추가',
    missingTitle: '터널을 찾을 수 없습니다',
    missing: '삭제되었거나 주소가 잘못되었습니다.',
    backToTunnels: '터널 목록으로',

    addServiceTitle: '서비스 추가',
    serviceDialogDesc: '요청을 전달할 앱의 내부 주소를 입력하세요.',
    serviceName: '서비스 이름',
    appHost: '앱 호스트',
    internalPort: '내부 포트',
    examplePlaceholder: '예: app',
    examplePort: '예: 80',
    exampleTunnel: '예: app-tunnel',
    targetHelp: ['같은 Compose 파일이면 앱 호스트에 ', 'services:', ' 아래 이름(예: ', 'app', '), 내부 포트에 앱이 컨테이너에서 듣는 포트(예: ', '80', ')를 입력하세요. 터널과 앱은 같은 Docker 네트워크에 있어야 합니다.'],
    cancel: '취소',
    registerService: '서비스 등록',
    registering: '등록 중…',
    newTunnelTitle: '새 터널 만들기',
    createDesc: '새 터널의 이름을 입력하세요.',
    tunnelName: '터널 이름',
    nameHelp: '영문자, 숫자, 하이픈, 밑줄을 쓸 수 있습니다. 컨테이너 이름에도 쓰입니다.',
    createTunnel: '터널 만들기',
    creating: '만드는 중…',

    invalidTunnel: '터널 이름을 확인하세요.',
    nameTaken: '이미 사용 중인 터널 이름입니다.',
    tunnelNotFound: '터널을 찾을 수 없습니다.',
    serviceNameTaken: '이미 사용 중인 서비스 이름입니다.',
    serviceTargetTaken: '같은 터널에 동일한 앱 호스트와 포트가 이미 등록되어 있습니다. 기존 서비스에 공개 주소를 추가하세요.',
    invalidService: '서비스 이름과 앱 주소를 확인하세요.',
    createFailed: (reason: string) => `터널을 만들지 못했습니다: ${reason}`,
    deleteFailed: (reason: string) => `터널을 삭제하지 못했습니다: ${reason}`,
    serviceFailed: (reason: string) => `서비스를 등록하지 못했습니다: ${reason}`,
    tokenFailed: (reason: string) => `설치 안내를 준비하지 못했습니다: ${reason}`,

    resetTitle: '실행 토큰 교체',
    resetWarning: '기존 토큰을 사용하는 모든 터널 인스턴스의 연결이 끊깁니다. 새 설치 코드를 각 인스턴스에 적용해야 다시 연결됩니다.',
    resetSubmit: '실행 토큰 교체',
    deleteTitle: '터널 삭제',
    deleteWarning: '삭제 후에는 되돌릴 수 없으며 실행 중인 터널 연결이 끊깁니다. 실제 서비스 자체는 삭제되지 않습니다.',
  });

  type ViewMode = 'cards' | 'table';
  type DetailTab = 'overview' | 'usage' | 'routes';
  type ConnectionFilter = 'all' | 'connected' | 'pending' | 'problem';
  type UsagePeriod = 24 | 168 | 720;
  const viewStorageKey = 'meerkat:tunnel-view';
  const usagePeriods: UsagePeriod[] = [24, 168, 720];

  function loadViewMode(): ViewMode {
    try {
      return localStorage.getItem(viewStorageKey) === 'table' ? 'table' : 'cards';
    } catch {
      return 'cards';
    }
  }

  let { data, onChanged, onCheckConnection, detailId, onOpenDetail, onBack }: { data: DashboardData; onChanged: () => Promise<void>; onCheckConnection: () => Promise<void>; detailId: string | null; onOpenDetail: (id: string, search?: string) => void; onBack: () => void } = $props();
  let name = $state('');
  let newDescription = $state('');
  let nameOpen = $state(false);
  let nameInput = $state('');
  let nameBusy = $state(false);
  let nameError = $state('');
  let descriptionOpen = $state(false);
  let descriptionInput = $state('');
  let descriptionBusy = $state(false);
  let descriptionError = $state('');
  let serviceName = $state('');
  let serviceHost = $state('');
  let servicePort = $state('');
  let serviceBusy = $state(false);
  // 화면 상태(탭·기간·서비스·보기 방식)는 주소 query가 단일 출처입니다: ?tab=routes&period=7d&service=…&view=table
  const detailTab = $derived(oneOf<DetailTab>('tab', ['overview', 'routes', 'usage'], 'overview'));
  const setDetailTab = (next: DetailTab): void => setParams({ tab: next }, { tab: 'overview' }, { push: true });
  let usage = $state<ConnectorUsage | null>(null);
  let usageConnectorId = '';
  const periodParams = { 24: '24h', 168: '7d', 720: '30d' } as const;
  const usagePeriod = $derived<UsagePeriod>(({ '24h': 24, '7d': 168, '30d': 720 } as const)[oneOf('period', ['24h', '7d', '30d'], '24h')]);
  const setUsagePeriod = (next: UsagePeriod): void => setParams({ period: periodParams[next] }, { period: '24h' });
  // 모르는 서비스 id는 전체 서비스로 되돌립니다.
  const usageServiceId = $derived(usage?.services.some((service) => service.service_id === param('service')) ? (param('service') ?? '') : '');
  const setUsageServiceId = (next: string): void => setParams({ service: next });
  let usageError = $state('');
  let serviceError = $state('');
  let createError = $state('');
  let tokenError = $state('');
  let tokenBusy = $state(false);
  let checkingConnection = $state(false);
  let connectionCheck = $state<'' | 'pending' | 'failed'>('');
  let now = $state(Date.now());
  let installMethod = $state<'terminal' | 'compose'>('terminal');
  let composeFileMode = $state<'existing' | 'new'>('existing');
  let createBusy = $state(false);
  let deleteBusy = $state(false);
  let deleteError = $state('');
  let deleteTarget = $state<Connector | null>(null);
  let deleteOpen = $state(false);
  let resetTokenTarget = $state<Connector | null>(null);
  let resetTokenOpen = $state(false);
  const viewMode = $derived<ViewMode>(oneOf('view', ['cards', 'table'], loadViewMode()));
  let createOpen = $state(false);
  let serviceOpen = $state(false);
  const composeUpCommand = 'docker compose -f meerkat-tunnel.yaml up -d';

  const selected = $derived(data.connectors.find((connector) => connector.id === detailId));
  const selectedRecords = $derived(data.records.filter((record) => record.connector.id === detailId));
  const selectedPaths = $derived(selectedRecords.flatMap((record) => record.routes.map((route) => ({ record, route }))));
  // 같은 터널의 여러 서비스가 공유하는 주소는 한 번만 셉니다.
  const selectedAddressCount = $derived(new Set(selectedPaths.map((path) => path.route.hostname)).size);
  const usageRequests = $derived(usage?.hours.reduce((total, hour) => total + hour.requests, 0) ?? 0);
  const usageShares = $derived((usage?.services ?? []).filter((service) => service.requests > 0).map((service) => ({ label: service.name, value: service.requests })));
  const usagePeriodLabel = $derived(usagePeriod === 24 ? t().last24h : usagePeriod === 168 ? t().last7d : t().last30d);
  const usageUpdated = $derived(usage?.updated_at
    ? t().usageUpdated(now - Date.parse(usage.updated_at) > 60000, new Date(usage.updated_at).toLocaleTimeString(intlLocale(), { hour: 'numeric', minute: '2-digit' }))
    : t().usageWaiting);
  // 터널 목록은 shadcn Data Table 문서처럼 TanStack Table 하나가 필터·정렬·페이지를 맡고, 카드 보기와 표 보기가 같은 행을 씁니다.
  type TunnelRow = { connector: Connector; services: number; hostnames: string[]; domains: string[] };
  const columnHelper = createColumnHelper<typeof features, TunnelRow>();
  const statusOrder: Record<string, number> = { connected: 0, connecting: 1, unknown: 2, draft: 3, stale: 4, disconnected: 5, error: 6 };

  // 검색어는 공백으로 나눈 낱말이 모두 이름에 들어 있어야 맞는 것으로 봅니다.
  const nameFilter: FilterFn<typeof features, TunnelRow> = (row, columnId, value: string) => {
    const name = `${row.getValue(columnId)} ${row.original.connector.name} ${row.original.connector.description ?? ''} ${row.original.hostnames.join(' ')}`.toLocaleLowerCase();
    return String(value).trim().toLocaleLowerCase().split(/\s+/).filter(Boolean).every((term) => name.includes(term));
  };
  const domainFilter: FilterFn<typeof features, TunnelRow> = (row, _columnId, value: string) => (value === 'none' ? row.original.hostnames.length === 0 : row.original.domains.includes(value));
  const statusFilter: FilterFn<typeof features, TunnelRow> = (row, _columnId, value: ConnectionFilter) => {
    const status = row.original.connector.status;
    if (value === 'connected') return status === 'connected';
    if (value === 'pending') return ['draft', 'connecting', 'unknown'].includes(status);
    if (value === 'problem') return ['stale', 'disconnected', 'error'].includes(status);
    return true;
  };

  type SortColumn = { getIsSorted: () => false | 'asc' | 'desc'; getToggleSortingHandler: () => ((event: unknown) => void) | undefined };
  const sortHeader = (column: SortColumn, label: string) => renderComponent(SortHeader, { label, sorted: column.getIsSorted(), onclick: column.getToggleSortingHandler() });

  const columns = columnHelper.columns([
    columnHelper.accessor((row) => connectorLabel(row.connector), {
      id: 'name',
      header: ({ column }) => sortHeader(column, t().colName),
      cell: ({ row }) => renderSnippet(nameCell, row.original),
      sortFn: 'text',
      filterFn: nameFilter,
    }),
    // 도메인은 보이지 않는 열로 두고 필터에만 씁니다. 공개 주소가 여러 도메인에 걸친 터널은 각 도메인에서 모두 보입니다.
    columnHelper.accessor((row) => row.domains.join(' '), {
      id: 'domain',
      header: () => t().domainFilter,
      cell: () => '',
      enableSorting: false,
      filterFn: domainFilter,
    }),
    columnHelper.accessor((row) => row.connector.description ?? '', {
      id: 'description',
      header: () => t().descriptionLabel,
      cell: ({ row }) => renderSnippet(descriptionCell, row.original),
      enableSorting: false,
    }),
    // 첫 공개 주소와 나머지 개수. 넓은 화면(lg 이상)에서만 보이고 정렬하지 않습니다. 검색은 모든 공개 주소를 대상으로 합니다.
    columnHelper.display({
      id: 'address',
      header: () => t().publicAddress,
      cell: ({ row }) => renderSnippet(addressCell, row.original),
      enableSorting: false,
      meta: { class: 'hidden lg:table-cell' },
    }),
    columnHelper.accessor((row) => statusOrder[row.connector.status] ?? 9, {
      id: 'status',
      header: ({ column }) => sortHeader(column, t().connectionStatus),
      cell: ({ row }) => renderSnippet(statusCell, row.original),
      sortFn: 'basic',
      filterFn: statusFilter,
    }),
    columnHelper.accessor('services', {
      header: ({ column }) => sortHeader(column, t().colServices),
      cell: ({ row }) => renderSnippet(servicesCell, row.original),
      sortFn: 'basic',
    }),
    columnHelper.accessor((row) => (row.connector.last_seen ? Date.parse(row.connector.last_seen) : 0), {
      id: 'lastSeen',
      header: ({ column }) => sortHeader(column, t().colLastSeen),
      cell: ({ row }) => renderSnippet(lastSeenCell, row.original),
      sortFn: 'basic',
    }),
    columnHelper.accessor((row) => Date.parse(row.connector.created_at), {
      id: 'created',
      header: ({ column }) => sortHeader(column, t().colCreated),
      cell: ({ row }) => renderSnippet(createdCell, row.original),
      sortFn: 'basic',
    }),
  ]);

  // 터널별 서비스·공개 주소를 한 번에 계산합니다.
  const tunnelFigures = $derived.by(() => {
    const figures = new Map<string, { services: number; addresses: number; hostnames: string[] }>();
    for (const record of data.records) {
      const figure = figures.get(record.connector.id) ?? { services: 0, addresses: 0, hostnames: [] };
      figure.services += 1;
      figure.hostnames.push(...record.routes.map((route) => route.hostname).filter((hostname) => !figure.hostnames.includes(hostname)));
      figure.addresses = figure.hostnames.length;
      figures.set(record.connector.id, figure);
    }
    return figures;
  });
  const pageSize = 12;
  const registeredDomains = $derived(data.outlet.domains ?? (data.outlet.domain ? [data.outlet.domain] : []));
  const tunnelRows = $derived<TunnelRow[]>(data.connectors.map((connector) => ({
    connector,
    services: tunnelFigures.get(connector.id)?.services ?? 0,
    hostnames: tunnelFigures.get(connector.id)?.hostnames ?? [],
    domains: registeredDomains.filter((domain) => (tunnelFigures.get(connector.id)?.hostnames ?? []).some((hostname) => hostname.endsWith(`.${domain}`))),
  })));
  // 검색·상태·도메인 필터, 정렬, 페이지는 주소(?q=&status=&domain=&sort=&page=)가 단일 출처입니다.
  // 문제가 있는 터널(오류·끊김·응답 없음)이 첫 페이지 맨 위에 오도록 상태 내림차순, 같은 상태는 이름순으로 시작합니다.
  const urlTable = urlTableState({
    filters: [
      { param: 'q', column: 'name' },
      { param: 'status', column: 'status', allowed: ['connected', 'pending', 'problem'] },
      { param: 'domain', column: 'domain', allowed: () => ['none', ...registeredDomains] },
    ],
    sortable: ['name', 'status', 'services', 'lastSeen', 'created'],
    defaultSort: [{ id: 'status', desc: true }, { id: 'name', desc: false }],
    pageSize,
  });
  const table = createTable({
    features,
    get data() { return tunnelRows; },
    columns,
    getRowId: (row) => row.connector.id,
    sortDescFirst: false,
    initialState: { columnVisibility: { domain: false } },
    ...urlTable.handlers,
    get state() { return urlTable.state; },
  });

  const searchValue = $derived((table.getColumn('name')?.getFilterValue() as string | undefined) ?? '');
  const statusValue = $derived((table.getColumn('status')?.getFilterValue() as ConnectionFilter | undefined) ?? 'all');
  const domainValue = $derived((table.getColumn('domain')?.getFilterValue() as string | undefined) ?? 'all');
  const filtersActive = $derived(Boolean(searchValue.trim()) || statusValue !== 'all' || domainValue !== 'all');
  const filteredCount = $derived(table.getFilteredRowModel().rows.length);

  // 설치 안내의 진행 단계: 만들기 → 설치 → 연결 → 서비스 추가
  // 서비스와 공개 주소는 연결 전에도 미리 등록할 수 있어, 단계마다 따로 완료를 판단합니다.
  const installStepDone = $derived(!selected ? [] : [true, selected.status !== 'draft', selected.status === 'connected', selectedRecords.length > 0]);
  const installStepCurrent = $derived(installStepDone.indexOf(false));
  const installSteps = $derived([t().stepCreate, t().stepInstall, t().stepConnect, t().stepService]);

  // 설치 안내 카드 아래 버튼은 필요한 것만 오른쪽 아래에 모아 보여줍니다.
  const showRegenerate = $derived(!connectorState.enrollment && selected?.status === 'draft');
  const showReset = $derived(tokenError.includes('already_enrolled') && selected?.status === 'draft');
  const showMockConnect = $derived(mockApiEnabled && Boolean(connectorState.enrollment));
  // 연결되기 전에는 App이 connectionPollMs마다 상태를 다시 확인합니다.
  const waiting = $derived(Boolean(selected && selected.status !== 'connected'));

  onMount(() => {
    const timer = window.setInterval(() => { now = Date.now(); }, 1000);
    return () => window.clearInterval(timer);
  });

  $effect(() => {
    const id = detailId;
    const hours = usagePeriod;
    const serviceId = usageServiceId;
    if (usageConnectorId !== id) { usage = null; usageConnectorId = id || ''; }
    usageError = '';
    if (!id) return;
    let active = true;
    const refresh = async () => {
      try {
        const next = await api<ConnectorUsage>(`/api/v1/connectors/${encodeURIComponent(id)}/usage?hours=${hours}&service_id=${encodeURIComponent(serviceId)}`);
        if (active) { usage = next; usageError = ''; }
      } catch {
        if (active) usageError = t().usageLoadFailed;
      }
    };
    void refresh();
    const timer = window.setInterval(() => void refresh(), 15000);
    return () => { active = false; window.clearInterval(timer); };
  });


  function formatDateTime(value: string): string {
    return new Date(value).toLocaleString(intlLocale(), { dateStyle: 'medium', timeStyle: 'short' });
  }

  // 카드에는 짧은 상대 시간을, 툴팁에는 정확한 시각을 보여줍니다.
  function relativeTime(value: string): string {
    const seconds = Math.min(0, Math.round((Date.parse(value) - now) / 1000));
    const formatter = new Intl.RelativeTimeFormat(intlLocale(), { numeric: 'auto', style: 'short' });
    const elapsed = Math.abs(seconds);
    if (elapsed < 45) return formatter.format(0, 'second');
    if (elapsed < 3600) return formatter.format(-Math.round(elapsed / 60), 'minute');
    if (elapsed < 86400) return formatter.format(-Math.round(elapsed / 3600), 'hour');
    if (elapsed < 86400 * 30) return formatter.format(-Math.round(elapsed / 86400), 'day');
    return new Date(value).toLocaleDateString(intlLocale(), { dateStyle: 'medium' });
  }

  function statusHint(status: string): string {
    if (status === 'draft') return t().statusDraft;
    if (status === 'connecting') return t().statusConnecting;
    if (status === 'connected') return t().statusConnected;
    return t().statusStale;
  }

  function periodLabel(hours: UsagePeriod): string {
    return hours === 24 ? t().hours24 : hours === 168 ? t().days7 : t().days30;
  }

  function setViewMode(mode: ViewMode): void {
    try {
      localStorage.setItem(viewStorageKey, mode);
    } catch {
      // 저장소를 쓸 수 없어도 주소에는 남습니다.
    }
    setParams({ view: mode });
  }

  function clearFilters(): void {
    table.setColumnFilters([]);
  }

  function tunnelError(cause: unknown): string {
    const code = errorMessage(cause);
    const labels: Record<string, string> = {
      invalid_connector: t().invalidTunnel,
      connector_name_taken: t().nameTaken,
      connector_not_found: t().tunnelNotFound,
    };
    return labels[code] || code;
  }

  $effect(() => {
    const id = detailId || '';
    if (connectorState.selectedId === id) return;
    connectorState.selectedId = id;
    const saved = id ? storedEnrollment(id) : null;
    connectorState.enrollment = saved?.enrollment || null;
    connectorState.enrollmentWasReset = saved?.reset || false;
    tokenError = '';
    connectionCheck = '';
    deleteError = '';
    serviceOpen = false;
    clearServiceForm();
  });

  $effect(() => {
    const current = selected;
    if (!current) return;
    if (connectorState.enrollment) return;
    const saved = storedEnrollment(current.id);
    if (saved) {
      connectorState.enrollment = saved.enrollment;
      connectorState.enrollmentWasReset = saved.reset;
      return;
    }
  });

  function openDetailFromLink(event: MouseEvent, id: string): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onOpenDetail(id);
  }

  function backFromLink(event: MouseEvent): void {
    if (event.defaultPrevented || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    event.preventDefault();
    onBack();
  }

  function openCreateDialog(): void {
    createError = '';
    createOpen = true;
  }

  // 작업 중에는 대화상자를 닫지 못하게 합니다.
  function setCreateOpen(next: boolean): void {
    if (!next && createBusy) return;
    createOpen = next;
    if (!next) { name = ''; createError = ''; }
  }

  function clearServiceForm(): void {
    serviceName = '';
    serviceHost = '';
    servicePort = '';
    serviceError = '';
  }

  function openServiceDialog(): void {
    if (!selected) return;
    clearServiceForm();
    serviceOpen = true;
  }

  function setServiceOpen(next: boolean): void {
    if (!next && serviceBusy) return;
    serviceOpen = next;
    if (!next) clearServiceForm();
  }

  function openDeleteDialog(): void {
    if (!selected) return;
    deleteTarget = selected;
    deleteError = '';
    deleteOpen = true;
  }

  function openResetTokenDialog(): void {
    if (!selected || tokenBusy) return;
    resetTokenTarget = selected;
    tokenError = '';
    resetTokenOpen = true;
  }

  async function resetToken(): Promise<void> {
    if (!resetTokenTarget || tokenBusy) return;
    if (await issueToken(true, resetTokenTarget.id)) resetTokenOpen = false;
  }

  function openNameDialog(): void {
    nameInput = selected?.display_name ?? '';
    nameError = '';
    nameOpen = true;
  }

  async function saveName(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!selected || nameBusy) return;
    nameBusy = true;
    nameError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}`, { method: 'PATCH', body: JSON.stringify({ display_name: nameInput.trim() }) });
      await onChanged();
      nameOpen = false;
    } catch (cause) {
      const code = errorMessage(cause);
      nameError = t().nameSaveFailed(code === 'invalid_display_name' ? t().invalidDisplayName : code);
    } finally {
      nameBusy = false;
    }
  }

  function openDescriptionDialog(): void {
    descriptionInput = selected?.description ?? '';
    descriptionError = '';
    descriptionOpen = true;
  }

  async function saveDescription(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!selected || descriptionBusy) return;
    descriptionBusy = true;
    descriptionError = '';
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}`, { method: 'PATCH', body: JSON.stringify({ description: descriptionInput.trim() }) });
      await onChanged();
      descriptionOpen = false;
    } catch (cause) {
      descriptionError = t().descriptionSaveFailed(errorMessage(cause));
    } finally {
      descriptionBusy = false;
    }
  }

  async function createConnector(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    createError = '';
    createBusy = true;
    try {
      const connector = await api<Connector>('/api/v1/connectors', { method: 'POST', body: JSON.stringify({ name, description: newDescription.trim() }) });
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      name = '';
      newDescription = '';
      await onChanged();
      clearFilters();
      createOpen = false;
      onOpenDetail(connector.id);
      void issueToken(false, connector.id);
    } catch (cause) {
      createError = t().createFailed(tunnelError(cause));
    } finally {
      createBusy = false;
    }
  }

  async function deleteConnector(): Promise<void> {
    if (!deleteTarget || deleteBusy) return;
    const connector = deleteTarget;
    deleteError = '';
    deleteBusy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(connector.id)}`, { method: 'DELETE' });
      connectorState.selectedId = '';
      connectorState.enrollment = null;
      connectorState.enrollmentWasReset = false;
      forgetEnrollment(connector.id);
      await onChanged();
      deleteOpen = false;
      onBack();
    } catch (cause) {
      deleteError = t().deleteFailed(tunnelError(cause));
    } finally {
      deleteBusy = false;
    }
  }

  async function createService(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (!selected || serviceBusy) return;
    serviceError = '';
    serviceBusy = true;
    try {
      await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}/services`, {
        method: 'POST',
        body: JSON.stringify({ name: serviceName, protocol: 'http', target: { host: serviceHost.trim(), port: Number(servicePort) } }),
      });
      await onChanged();
      serviceOpen = false;
      clearServiceForm();
    } catch (cause) {
      const code = errorMessage(cause);
      const message: Record<string, string> = {
        connector_not_found: t().tunnelNotFound,
        service_name_taken: t().serviceNameTaken,
        service_target_taken: t().serviceTargetTaken,
        invalid_service: t().invalidService,
      };
      serviceError = t().serviceFailed(message[code] || code);
    } finally {
      serviceBusy = false;
    }
  }

  async function issueToken(reset = false, id = selected?.id): Promise<boolean> {
    if (!id || tokenBusy) return false;
    tokenError = '';
    tokenBusy = true;
    try {
      const enrollment = await api<EnrollmentToken>(`/api/v1/connectors/${encodeURIComponent(id)}/enrollment-tokens${reset ? '?reset=true' : ''}`, { method: 'POST' });
      const wasReset = reset || (connectorState.selectedId === id && connectorState.enrollmentWasReset);
      rememberEnrollment(id, enrollment, wasReset);
      if (connectorState.selectedId === id) {
        connectorState.enrollment = enrollment;
        connectorState.enrollmentWasReset = wasReset;
        now = Date.now();
        installMethod = 'terminal';
      }
      await onChanged();
      return true;
    } catch (cause) {
      if (connectorState.selectedId === id) tokenError = t().tokenFailed(tunnelError(cause));
      return false;
    } finally {
      tokenBusy = false;
    }
  }

  async function checkConnection(): Promise<void> {
    if (checkingConnection) return;
    checkingConnection = true;
    connectionCheck = '';
    try {
      await onCheckConnection();
      connectionCheck = selected?.status === 'connected' ? '' : 'pending';
    } catch {
      connectionCheck = 'failed';
    } finally {
      checkingConnection = false;
    }
  }

  async function mockConnect(): Promise<void> {
    if (!mockApiEnabled || !selected) return;
    await api(`/api/v1/connectors/${encodeURIComponent(selected.id)}/mock-connect`, { method: 'POST' });
    await onChanged();
  }

  function invitation(): string {
    if (!selected || !connectorState.enrollment || !data.outlet.admin_hostname) return '';
    const payload = JSON.stringify({ v: 1, outlet: `https://${data.outlet.admin_hostname}`, name: selected.name, token: connectorState.enrollment.token });
    return btoa(payload).replaceAll('+', '-').replaceAll('/', '_').replaceAll('=', '');
  }

  function installCommand(): string {
    if (!selected) return '';
    return [
      `docker run -d --restart unless-stopped --name meerkat-${selected.name}`,
      '--read-only --cap-drop ALL --security-opt no-new-privileges',
      '--tmpfs /tmp --tmpfs /var/lib/meerkat:uid=65532,gid=65532,mode=0700',
      `ghcr.io/lawnect/meerkat:latest connect ${invitation()}`,
    ].join(' ' + String.fromCharCode(92) + '\n');
  }

  // Compose 서비스 이름. 한 파일에 터널을 여럿 붙여도 겹치지 않도록 터널 ID에서 고정된 8글자를 만듭니다.
  function composeServiceName(): string {
    let hash = 2166136261;
    for (const char of selected?.id ?? '') hash = Math.imul(hash ^ char.charCodeAt(0), 16777619);
    let suffix = '';
    for (let index = 0; index < 8; index += 1) {
      let mixed = (hash + Math.imul(index + 1, 0x9e3779b9)) | 0;
      mixed = Math.imul(mixed ^ (mixed >>> 16), 0x85ebca6b);
      mixed = Math.imul(mixed ^ (mixed >>> 13), 0xc2b2ae35);
      suffix += 'abcdefghijklmnopqrstuvwxyz0123456789'[((mixed ^ (mixed >>> 16)) >>> 0) % 36];
    }
    return `connector-${suffix}`;
  }

  function composeFile(): string {
    if (!selected) return '';
    const lines = [
      'services:',
      `  ${composeServiceName()}:`,
      '    image: ghcr.io/lawnect/meerkat:latest',
      `    container_name: meerkat-${selected.name}`,
      '    restart: unless-stopped',
      '    init: true',
      '    read_only: true',
      '    cap_drop: [ALL]',
      '    security_opt: [no-new-privileges:true]',
      `    command: ["connect", ${JSON.stringify(invitation())}]`,
      '    tmpfs:',
      '      - /tmp',
      '      - /var/lib/meerkat:uid=65532,gid=65532,mode=0700',
    ];
    return lines.join('\n');
  }

  function existingComposeService(): string {
    if (!selected) return '';
    const lines = [
      `  ${composeServiceName()}:`,
      '    image: ghcr.io/lawnect/meerkat:latest',
      `    container_name: meerkat-${selected.name}`,
      '    restart: unless-stopped',
      '    init: true',
      '    read_only: true',
      '    cap_drop: [ALL]',
      '    security_opt: [no-new-privileges:true]',
      `    command: ["connect", ${JSON.stringify(invitation())}]`,
      '    tmpfs:',
      '      - /tmp',
      '      - /var/lib/meerkat:uid=65532,gid=65532,mode=0700',
    ];
    return lines.join('\n');
  }

  function openServiceRoutes(serviceId: string): void {
    setDetailTab('routes');
    requestAnimationFrame(() => {
      const item = document.getElementById(`service-routes-${serviceId}`);
      item?.focus({ preventScroll: true });
      item?.scrollIntoView({ behavior: 'smooth', block: 'center' });
    });
  }

  // 현재 설치 방법에 맞는 짧은 안내를 툴팁으로 보여줍니다.
  const installIntro = $derived(
    installMethod === 'terminal'
      ? `${t().terminalIntro} ${t().installCodeNote}`
      : composeFileMode === 'existing'
        ? `${t().existingIntro.join('')} ${t().existingNote.join('')}`
        : `${t().newIntro.join('')} ${t().newNote.join('')}`,
  );
</script>

{#snippet nameCell(row: TunnelRow)}
  <span class="flex min-w-0 items-center gap-2">
    <TunnelPreview id={row.connector.id} name={connectorLabel(row.connector)} hostnames={row.hostnames} {onOpenDetail}>
      {#snippet trigger(props)}
        <a {...props} class="block min-w-0 max-w-56 truncate text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50" href={connectorDetailPath(row.connector.id)} onclick={(event) => openDetailFromLink(event, row.connector.id)}>{connectorLabel(row.connector)}</a>
      {/snippet}
    </TunnelPreview>
    <VersionBadge version={row.connector.version} />
  </span>
{/snippet}

{#snippet addressCell(row: TunnelRow)}
  {#if row.hostnames.length}
    <span class="flex min-w-0 items-center gap-2">
      <TruncateText text={row.hostnames[0]} class="max-w-64 font-mono" />
      {#if row.hostnames.length > 1}<Badge variant="secondary" class="cursor-default select-none">+{row.hostnames.length - 1}</Badge>{/if}
    </span>
  {:else}
    <span class="text-muted-foreground">—</span>
  {/if}
{/snippet}

{#snippet descriptionCell(row: TunnelRow)}
  {#if row.connector.description}<TruncateText text={row.connector.description} class="max-w-64 text-muted-foreground" />{/if}
{/snippet}

{#snippet statusCell(row: TunnelRow)}
  <Status label={statusLabel(row.connector.status)} value={row.connector.status} />
{/snippet}

{#snippet servicesCell(row: TunnelRow)}
  <span class={row.services ? '' : 'text-muted-foreground'}>{row.services ? t().serviceCount(row.services) : t().noServices}</span>
{/snippet}

{#snippet lastSeenCell(row: TunnelRow)}
  {#if row.connector.last_seen}{@render timeText(row.connector.last_seen)}{:else}<span class="text-muted-foreground">—</span>{/if}
{/snippet}

{#snippet createdCell(row: TunnelRow)}
  {new Date(row.connector.created_at).toLocaleDateString(intlLocale(), { dateStyle: 'medium' })}
{/snippet}

{#snippet codeBlock(value: string, noun: string)}
  <div class="relative min-w-0">
    <pre class="border bg-muted/50 py-3 pr-12 pl-3 font-mono text-xs break-all whitespace-pre-wrap"><code>{value}</code></pre>
    <div class="absolute top-1.5 right-1.5"><IconButton label={t().copyLabel(noun)} icon={Copy} side="left" onclick={() => copyToClipboard(value)} /></div>
  </div>
{/snippet}

{#snippet timeText(value: string)}
  <Tooltip.Root>
    <Tooltip.Trigger>
      {#snippet child({ props })}
        <span {...props} class="relative">{relativeTime(value)}</span>
      {/snippet}
    </Tooltip.Trigger>
    <Tooltip.Content>{formatDateTime(value)}</Tooltip.Content>
  </Tooltip.Root>
{/snippet}

{#snippet viewItem(value: ViewMode, label: string, Icon: typeof Grid)}
  <Tooltip.Root>
    <Tooltip.Trigger>
      {#snippet child({ props })}
        <ToggleGroup.Item {...props} {value} aria-label={label}><Icon aria-hidden="true" /></ToggleGroup.Item>
      {/snippet}
    </Tooltip.Trigger>
    <Tooltip.Content>{label}</Tooltip.Content>
  </Tooltip.Root>
{/snippet}

{#snippet newTunnelAction()}
  <Button onclick={openCreateDialog}><Add aria-hidden="true" />{t().newTunnel}</Button>
{/snippet}

{#snippet detailBadge()}
  <VersionBadge version={selected?.version} tip />
{/snippet}

{#snippet detailActions()}
  {#if selected && selected.status !== 'connected'}
    <Button variant="outline" disabled={checkingConnection} onclick={checkConnection}>
      {#if checkingConnection}<Spinner aria-hidden="true" />{t().checking}{:else}<ArrowSync aria-hidden="true" />{t().checkConnection}{/if}
    </Button>
  {/if}
{/snippet}

<div class="min-w-0">
  {#if detailId}
    {#if selected}
      <PageHead title={connectorLabel(selected)} badge={detailBadge} crumbs={[{ label: t().tunnels, href: pageHref('connectors'), onclick: backFromLink }, { label: connectorLabel(selected) }]} action={detailActions} />
      {#if connectionCheck}
        <Alert.Root class="mb-4" variant={connectionCheck === 'failed' ? 'destructive' : 'default'} role="status">
          {#if connectionCheck === 'pending'}<Info aria-hidden="true" />{:else}<Warning aria-hidden="true" />{/if}
          <Alert.Description>{connectionCheck === 'pending' ? t().checkPending : t().checkFailed}</Alert.Description>
        </Alert.Root>
      {/if}
      <Tabs.Root class="min-w-0 gap-4" bind:value={() => detailTab, (next) => { setDetailTab(next as DetailTab); }}>
        <Tabs.List aria-label={t().detailTabsAria}>
          <Tabs.Trigger value="overview"><Home12 class="size-3" aria-hidden="true" />{t().tabOverview}</Tabs.Trigger>
          <Tabs.Trigger value="routes"><Globe12 class="size-3" aria-hidden="true" />{t().tabRoutes}</Tabs.Trigger>
          <Tabs.Trigger value="usage"><ArrowTrending12 class="size-3" aria-hidden="true" />{t().tabUsage}</Tabs.Trigger>
        </Tabs.List>

        <Tabs.Content value="overview" class="grid min-w-0 gap-4">
          {#if selected.status !== 'connected' || connectorState.enrollment}
            <Card.Root size="sm" role="region" aria-label={t().installAria(selected.name)}>
              <Card.Header>
                <Card.Title class="flex items-center gap-1">{t().installTitle}<InfoTooltip id={`connector-connection-help-${selected.id}`} label={t().installTitle} description={`${t().installHelp} ${installIntro} ${t().secretNote}`} /></Card.Title>
              </Card.Header>
              <Card.Content class="grid min-w-0 gap-4">
                <ol class="flex flex-wrap items-center gap-x-3 gap-y-2" aria-label={t().stepsAria}>
                  {#each installSteps as label, index (index)}
                    {@const done = installStepDone[index]}
                    {@const current = index === installStepCurrent}
                    <li class="flex items-center gap-2" aria-current={current ? 'step' : undefined}>
                      <span class={['flex size-6 shrink-0 items-center justify-center text-xs font-medium tabular-nums', done ? 'bg-foreground text-background' : current ? 'bg-muted text-foreground ring-1 ring-foreground/40' : 'bg-muted text-muted-foreground']} aria-hidden="true">{#if done}<Checkmark />{:else}{index + 1}{/if}</span>
                      <span class={done || current ? 'font-medium' : 'text-muted-foreground'}>{label}</span>
                      {#if index < installSteps.length - 1}<span class="ml-1 hidden h-px w-6 bg-border sm:block" aria-hidden="true"></span>{/if}
                    </li>
                  {/each}
                </ol>
                {#if !connectorState.enrollment && selected.status !== 'draft'}
                  <Alert.Root role="status"><Info aria-hidden="true" /><Alert.Description>{t().noRunCode}</Alert.Description></Alert.Root>
                {/if}
                {#if tokenError && !resetTokenOpen}
                  <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{tokenError}</Alert.Description></Alert.Root>
                {/if}
                {#if connectorState.enrollment}
                  <ToggleGroup.Root type="single" variant="outline" size="sm" aria-label={t().installMethodsAria} bind:value={() => installMethod, (next) => { if (next) installMethod = next as typeof installMethod; }}>
                    <ToggleGroup.Item value="terminal"><Box aria-hidden="true" />{t().tabDocker}</ToggleGroup.Item>
                    <ToggleGroup.Item value="compose"><Code aria-hidden="true" />Docker Compose</ToggleGroup.Item>
                  </ToggleGroup.Root>
                  {#if installMethod === 'terminal'}
                    <div class="grid min-w-0 gap-4">
                      {#if connectorState.enrollmentWasReset}
                        <Alert.Root role="status"><Info aria-hidden="true" /><Alert.Description>{t().resetNote}</Alert.Description></Alert.Root>
                      {/if}
                      {@render codeBlock(installCommand(), t().runCommandNoun)}
                    </div>
                  {:else}
                    <div class="grid min-w-0 gap-4">
                      <ToggleGroup.Root type="single" variant="outline" size="sm" aria-label={t().composeChoiceAria} bind:value={() => composeFileMode, (next) => { if (next) composeFileMode = next as typeof composeFileMode; }}>
                        <ToggleGroup.Item value="existing">{t().composeExisting}</ToggleGroup.Item>
                        <ToggleGroup.Item value="new">{t().composeNew}</ToggleGroup.Item>
                      </ToggleGroup.Root>
                      {#if composeFileMode === 'existing'}
                        {@render codeBlock(existingComposeService(), t().tunnelServiceNoun)}
                      {:else}
                        <p class="flex items-center gap-2 font-mono font-medium"><Code aria-hidden="true" />meerkat-tunnel.yaml</p>
                        {@render codeBlock(composeFile(), t().composeFileNoun)}
                        {@render codeBlock(composeUpCommand, t().runCommandNoun)}
                      {/if}
                    </div>
                  {/if}
                {/if}
              </Card.Content>
              {#if waiting || showRegenerate || showReset || showMockConnect}
                <Card.Footer class="flex-wrap justify-between gap-2">
                  <span class="flex items-center gap-2 text-muted-foreground" role="status" aria-live="polite">
                    {#if waiting}<Spinner aria-hidden="true" />{t().polling(connectionPollMs / 1000)}{/if}
                  </span>
                  <div class="ms-auto flex flex-wrap justify-end gap-2">
                  {#if showReset}
                    <Button variant="outline" onclick={openResetTokenDialog}>{t().resetAndInstall}</Button>
                  {/if}
                  {#if showRegenerate}
                    <Button variant="outline" disabled={tokenBusy} onclick={() => issueToken()}>
                      {#if tokenBusy}<Spinner aria-hidden="true" />{:else}<ArrowSync aria-hidden="true" />{/if}{t().regenerate}
                    </Button>
                  {/if}
                  {#if showMockConnect}
                    <Tooltip.Root>
                      <Tooltip.Trigger>
                        {#snippet child({ props })}
                          <Button {...props} variant="outline" onclick={mockConnect}>{t().mockConnect}</Button>
                        {/snippet}
                      </Tooltip.Trigger>
                      <Tooltip.Content>{t().mockNote}</Tooltip.Content>
                    </Tooltip.Root>
                  {/if}
                  </div>
                </Card.Footer>
              {/if}
            </Card.Root>
          {/if}

          <div class="grid grid-cols-2 gap-4 sm:grid-cols-3">
            <StatCard class="col-span-2 sm:col-span-1" label={t().connectionStatus} hint={statusHint(selected.status)} hintId={`connector-status-help-${selected.id}`} sub={selected.last_seen ? t().lastSeenAt(relativeTime(selected.last_seen)) : t().noRecord}>
              <Status label={statusLabel(selected.status)} value={selected.status} />
            </StatCard>
            <StatCard label={t().services} value={selectedRecords.length.toLocaleString(intlLocale())} />
            <StatCard label={t().publicAddresses} value={selectedAddressCount.toLocaleString(intlLocale())} />
          </div>

          <Card.Root size="sm" role="region" aria-label={t().servicesAria}>
            <Card.Header>
              <Card.Title class="flex items-center gap-1">{t().services}<InfoTooltip id={`connector-service-help-${selected.id}`} label={t().services} description={t().servicesHelp} /></Card.Title>
              <Card.Action><Button variant="outline" onclick={openServiceDialog}><Add aria-hidden="true" />{t().addService}</Button></Card.Action>
            </Card.Header>
            <Card.Content>
              {#if selectedRecords.length}
                <Item.Group class="gap-2">
                  {#each selectedRecords as record (record.service.id)}
                    <ServiceBlock {record} outlet={data.outlet} {onChanged} view="service" onViewRoutes={() => openServiceRoutes(record.service.id)} />
                  {/each}
                </Item.Group>
              {:else}
                <Empty.Root class="border border-dashed">
                  <Empty.Header>
                    <Empty.Media variant="icon"><Cube20 class="size-5" aria-hidden="true" /></Empty.Media>
                    <Empty.Title>{t().noServicesYet}</Empty.Title>
                  </Empty.Header>
                </Empty.Root>
              {/if}
            </Card.Content>
          </Card.Root>
          <Card.Root size="sm" role="region" aria-label={t().infoTitle}>
            <Card.Header>
              <Card.Title>{t().infoTitle}</Card.Title>
              <Card.Action class="flex flex-wrap justify-end gap-2">
                <Tooltip.Root>
                  <Tooltip.Trigger>
                    {#snippet child({ props })}
                      <Button {...props} variant="outline" disabled={tokenBusy} onclick={openResetTokenDialog}><Key aria-hidden="true" />{tokenBusy ? t().replacing : t().replaceToken}</Button>
                    {/snippet}
                  </Tooltip.Trigger>
                  <Tooltip.Content class="max-w-xs">{t().tokenNote}</Tooltip.Content>
                </Tooltip.Root>
                <Tooltip.Root>
                  <Tooltip.Trigger>
                    {#snippet child({ props })}
                      <Button {...props} variant="destructive" onclick={openDeleteDialog}><Delete aria-hidden="true" />{t().deleteTunnel}</Button>
                    {/snippet}
                  </Tooltip.Trigger>
                  <Tooltip.Content class="max-w-xs">{t().deleteNote}</Tooltip.Content>
                </Tooltip.Root>
              </Card.Action>
            </Card.Header>
            <Card.Content>
              <dl class="grid grid-cols-[auto_minmax(0,1fr)] items-center gap-x-6 gap-y-2">
                <dt class="text-muted-foreground">{t().name}</dt>
                <dd class="min-w-0">
                  <Tooltip.Root>
                    <Tooltip.Trigger>
                      {#snippet child({ props })}
                        <button {...props} type="button" class="max-w-full cursor-pointer text-left wrap-anywhere text-link outline-none focus-visible:ring-3 focus-visible:ring-ring/50" onclick={openNameDialog}>{connectorLabel(selected)}</button>
                      {/snippet}
                    </Tooltip.Trigger>
                    <Tooltip.Content>{t().editName}</Tooltip.Content>
                  </Tooltip.Root>
                </dd>
                <dt class="text-muted-foreground">{t().descriptionLabel}</dt>
                <dd class="min-w-0">
                  <Tooltip.Root>
                    <Tooltip.Trigger>
                      {#snippet child({ props })}
                        <button {...props} type="button" class={['max-w-full cursor-pointer text-left whitespace-pre-line wrap-anywhere text-link outline-none focus-visible:ring-3 focus-visible:ring-ring/50', !selected.description && 'text-muted-foreground']} onclick={openDescriptionDialog}>{selected.description || t().addDescription}</button>
                      {/snippet}
                    </Tooltip.Trigger>
                    <Tooltip.Content>{t().editDescription}</Tooltip.Content>
                  </Tooltip.Root>
                </dd>
                <dt class="flex items-center gap-0.5 text-muted-foreground">{t().runName}<InfoTooltip id={`connector-run-name-help-${selected.id}`} label={t().runName} description={t().runNameHelp} /></dt>
                <dd class="min-w-0 font-mono"><TruncateText text={selected.name} /></dd>
                <dt class="text-muted-foreground">{t().created}</dt>
                <dd>{formatDateTime(selected.created_at)}</dd>
              </dl>
            </Card.Content>
          </Card.Root>
        </Tabs.Content>

        <Tabs.Content value="routes" class="grid min-w-0 gap-4">
          {#if selectedRecords.length}
            <Card.Root size="sm" role="region" aria-label={t().requestPaths}>
              <Card.Header>
                <Card.Title>{t().requestPaths}</Card.Title>
              </Card.Header>
              <Card.Content class="min-w-0">
                <RouteGraph tunnelName={connectorLabel(selected)} records={selectedRecords} />
              </Card.Content>
            </Card.Root>
            {#each selectedRecords as record (record.service.id)}
              <div class="min-w-0 outline-none focus-visible:ring-3 focus-visible:ring-ring/50" id={`service-routes-${record.service.id}`} tabindex="-1"><ServiceBlock {record} records={data.records} outlet={data.outlet} {onChanged} view="routes" /></div>
            {/each}
          {:else}
            <Empty.Root class="border border-dashed bg-card">
              <Empty.Header>
                <Empty.Media variant="icon"><Cube20 class="size-5" aria-hidden="true" /></Empty.Media>
                <Empty.Title>{t().noServicesYet}</Empty.Title>
              </Empty.Header>
              <Empty.Content><Button variant="outline" onclick={() => setDetailTab('overview')}>{t().addServiceFromOverview}<ArrowRight aria-hidden="true" /></Button></Empty.Content>
            </Empty.Root>
          {/if}
        </Tabs.Content>

        <Tabs.Content value="usage" class="grid min-w-0 gap-4">
          <Card.Root size="sm" role="region" aria-label={t().usageAria(usagePeriodLabel)}>
            <Card.Header class="@max-md/card-header:grid-cols-1 @max-md/card-header:gap-3">
              <Card.Title>{t().usage}</Card.Title>
              <Card.Description>{usageUpdated}</Card.Description>
              <Card.Action class="@max-md/card-header:col-start-1 @max-md/card-header:row-span-1 @max-md/card-header:row-start-3 @max-md/card-header:justify-self-start">
                <ButtonGroup.Root>
                  <NativeSelect.Root size="sm" aria-label={t().period} bind:value={() => String(usagePeriod), (next) => { if (next) setUsagePeriod(Number(next) as UsagePeriod); }}>
                    {#each usagePeriods as hours (hours)}
                      <NativeSelect.Option value={String(hours)}>{periodLabel(hours)}</NativeSelect.Option>
                    {/each}
                  </NativeSelect.Root>
                  <NativeSelect.Root size="sm" aria-label={t().service} bind:value={() => usageServiceId || 'all', (next) => { setUsageServiceId(next === 'all' ? '' : next); }}>
                    <NativeSelect.Option value="all">{t().allServices}</NativeSelect.Option>
                    {#each usage?.services || [] as service (service.service_id)}
                      <NativeSelect.Option value={service.service_id}>{service.name}</NativeSelect.Option>
                    {/each}
                  </NativeSelect.Root>
                </ButtonGroup.Root>
              </Card.Action>
            </Card.Header>
            {#if usageError}
              <Card.Content>
                <Alert.Root variant="destructive" role="status"><Warning aria-hidden="true" /><Alert.Description>{usageError}</Alert.Description></Alert.Root>
              </Card.Content>
            {:else if usage}
              <UsageChart hours={usage.hours} periodHours={usagePeriod} />
            {:else}
              <Card.Content class="flex justify-center py-8"><Spinner aria-label={t().usageLoading} /></Card.Content>
            {/if}
          </Card.Root>

          {#if usage && !usageServiceId && usageShares.length > 1}
            <Card.Root size="sm">
              <Card.Header>
                <Card.Title>{t().usageByService}</Card.Title>
              </Card.Header>
              <Card.Content>
                <ShareChart items={usageShares} valueLabel={t().count} centerValue={t().count(usageRequests)} centerLabel={t().requests} ariaLabel={t().usageByServiceAria(usageRequests)} />
              </Card.Content>
            </Card.Root>
          {/if}
        </Tabs.Content>
      </Tabs.Root>
    {:else}
      <PageHead title={t().missingTitle} crumbs={[{ label: t().tunnels, href: pageHref('connectors'), onclick: backFromLink }]} />
      <Empty.Root class="border border-dashed bg-card">
        <Empty.Header>
          <Empty.Media variant="icon"><Search20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().missingTitle}</Empty.Title>
          <Empty.Description>{t().missing}</Empty.Description>
        </Empty.Header>
        <Empty.Content><Button variant="outline" href={pageHref('connectors')} onclick={backFromLink}>{t().backToTunnels}</Button></Empty.Content>
      </Empty.Root>
    {/if}
  {:else}
    <PageHead title={t().tunnels} action={data.connectors.length ? newTunnelAction : undefined} />
    {#if data.connectors.length}
      <div class="grid gap-4">
        <div class="flex flex-wrap items-center gap-2" role="search" aria-label={t().searchFilterAria}>
          <InputGroup.Root class="min-w-48 grow sm:max-w-sm sm:grow-0">
            <InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
            <InputGroup.Input id="connector-search" type="search" value={searchValue} oninput={(event) => table.getColumn('name')?.setFilterValue(event.currentTarget.value || undefined)} placeholder={t().searchPlaceholder} aria-label={t().search} autocomplete="off" />
          </InputGroup.Root>
          <NativeSelect.Root id="connection-filter" class="w-full sm:w-44" aria-label={t().connectionStatus} bind:value={() => statusValue, (next) => { table.getColumn('status')?.setFilterValue(next === 'all' ? undefined : next); }}>
            <NativeSelect.Option value="all">{t().filterAll}</NativeSelect.Option>
            <NativeSelect.Option value="connected">{t().filterConnected}</NativeSelect.Option>
            <NativeSelect.Option value="pending">{t().filterPending}</NativeSelect.Option>
            <NativeSelect.Option value="problem">{t().filterProblem}</NativeSelect.Option>
          </NativeSelect.Root>
          <NativeSelect.Root class="w-full sm:w-44" aria-label={t().domainFilter} bind:value={() => domainValue, (next) => { table.getColumn('domain')?.setFilterValue(next === 'all' ? undefined : next); }}>
            <NativeSelect.Option value="all">{t().allDomains}</NativeSelect.Option>
            {#each registeredDomains as domain (domain)}
              <NativeSelect.Option value={domain}>{domain}</NativeSelect.Option>
            {/each}
            <NativeSelect.Option value="none">{t().noDomainAddresses}</NativeSelect.Option>
          </NativeSelect.Root>
          {#if filtersActive && filteredCount}
            <Tooltip.Root>
              <Tooltip.Trigger>
                {#snippet child({ props })}
                  <Button {...props} variant="outline" size="icon" aria-label={t().reset} onclick={clearFilters}><ArrowReset20 class="size-5" aria-hidden="true" /></Button>
                {/snippet}
              </Tooltip.Trigger>
              <Tooltip.Content>{t().reset}</Tooltip.Content>
            </Tooltip.Root>
          {/if}
          <ToggleGroup.Root class="ml-auto" type="single" variant="outline" aria-label={t().viewAria} bind:value={() => viewMode, (next) => { if (next) setViewMode(next as ViewMode); }}>
            {@render viewItem('cards', t().viewCards, Grid)}
            {@render viewItem('table', t().viewTable, TextBulletListLtr)}
          </ToggleGroup.Root>
        </div>

        {#if filteredCount}
          <DataTablePagination {table} placement="top" />
          {#if viewMode === 'cards'}
            <ul class="grid gap-4 sm:grid-cols-2 lg:grid-cols-3" aria-label={t().tunnelListAria}>
              {#each table.getRowModel().rows as row (row.id)}
                {@const connector = row.original.connector}
                {@const hostnames = row.original.hostnames}
                <li class="flex">
                  <a class="group/link flex w-full outline-none focus-visible:ring-3 focus-visible:ring-ring/50" href={connectorDetailPath(connector.id)} aria-label={t().detailsAria(connectorLabel(connector))} onclick={(event) => openDetailFromLink(event, connector.id)}>
                    <Card.Root size="sm" class="w-full transition-colors group-hover/link:bg-muted/30 group-hover/link:border-foreground/60">
                      <Card.Header>
                        <Card.Title class="flex min-w-0 items-center gap-2">
                          <TunnelPreview id={connector.id} name={connectorLabel(connector)} {hostnames} {onOpenDetail} nested>
                            {#snippet trigger(props)}<span {...props} class="block min-w-0 truncate">{connectorLabel(connector)}</span>{/snippet}
                          </TunnelPreview>
                          <VersionBadge version={connector.version} />
                        </Card.Title>
                        {#if connector.description}<Card.Description class="min-w-0"><TruncateText text={connector.description} /></Card.Description>{/if}
                        <Card.Action><Status label={statusLabel(connector.status)} value={connector.status} /></Card.Action>
                      </Card.Header>
                      <Card.Content class="grid flex-1 content-start gap-1">
                        {#if hostnames.length}
                          {#each hostnames.slice(0, 2) as hostname (hostname)}
                            <TruncateText text={hostname} />
                          {/each}
                          {#if hostnames.length > 2}<span class="text-muted-foreground tabular-nums">+{hostnames.length - 2}</span>{/if}
                        {:else}
                          <span class="text-muted-foreground">{t().servicesCount(row.original.services)}</span>
                        {/if}
                      </Card.Content>
                      <Card.Footer class="flex-wrap justify-between gap-x-4 gap-y-1 text-xs text-muted-foreground">
                        <span>{connector.last_seen ? t().lastSeenAt(relativeTime(connector.last_seen)) : t().noRecord}</span>
                        <span>{t().createdOn(new Date(connector.created_at).toLocaleDateString(intlLocale(), { dateStyle: 'medium' }))}</span>
                      </Card.Footer>
                    </Card.Root>
                  </a>
                </li>
              {/each}
            </ul>
          {:else}
            <DataTable {table} ariaLabel={t().tunnelListAria} />
          {/if}
          <DataTablePagination {table} />
        {:else}
          <Empty.Root class="border border-dashed bg-card">
            <Empty.Header>
              <Empty.Media variant="icon"><Search20 class="size-5" aria-hidden="true" /></Empty.Media>
              <Empty.Title>{t().noMatchesTitle}</Empty.Title>
            </Empty.Header>
            <Empty.Content><Button variant="outline" onclick={clearFilters}>{t().clearFilters}</Button></Empty.Content>
          </Empty.Root>
        {/if}
      </div>
    {:else}
      <Empty.Root class="border border-dashed bg-card">
        <Empty.Header>
          <Empty.Media variant="icon"><PlugConnected20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().noTunnelsTitle}</Empty.Title>
          <Empty.Description>{t().noTunnels}</Empty.Description>
        </Empty.Header>
        <Empty.Content><Button onclick={openCreateDialog}><Add aria-hidden="true" />{t().newTunnel}</Button></Empty.Content>
      </Empty.Root>
    {/if}
  {/if}
</div>

<Dialog.Root bind:open={() => serviceOpen, setServiceOpen}>
  <Dialog.Content class="sm:max-w-lg">
    <form class="grid gap-4" onsubmit={createService}>
      <Dialog.Header>
        <Dialog.Title>{t().addServiceTitle}</Dialog.Title>
        <Dialog.Description class="sr-only">{t().serviceDialogDesc}</Dialog.Description>
      </Dialog.Header>
      <Field.Group>
        <Field.Field>
          <Field.Label for="connector-service-name">{t().serviceName}</Field.Label>
          <InputGroup.Root>
            <InputGroup.Addon><Tag aria-hidden="true" /></InputGroup.Addon>
            <InputGroup.Input id="connector-service-name" bind:value={serviceName} placeholder={t().examplePlaceholder} required />
          </InputGroup.Root>
        </Field.Field>
        <div class="grid gap-5 sm:grid-cols-[minmax(0,1fr)_8rem]">
          <Field.Field>
            <div class="flex items-center gap-1"><Field.Label for="connector-service-host">{t().appHost}</Field.Label><InfoTooltip id="connector-service-target-help" label={t().appHost} description={t().targetHelp.join('')} /></div>
            <InputGroup.Root>
              <InputGroup.Addon><Storage aria-hidden="true" /></InputGroup.Addon>
              <InputGroup.Input id="connector-service-host" bind:value={serviceHost} placeholder={t().examplePlaceholder} aria-describedby="connector-service-target-help" required />
            </InputGroup.Root>
          </Field.Field>
          <Field.Field>
            <div class="flex h-5 items-center"><Field.Label for="connector-service-port">{t().internalPort}</Field.Label></div>
            <InputGroup.Root>
              <InputGroup.Addon><NumberSymbol aria-hidden="true" /></InputGroup.Addon>
              <InputGroup.Input id="connector-service-port" type="number" min="1" max="65535" bind:value={servicePort} placeholder={t().examplePort} aria-describedby="connector-service-target-help" required />
            </InputGroup.Root>
          </Field.Field>
        </div>
      </Field.Group>
      {#if serviceError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{serviceError}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        <Button variant="outline" disabled={serviceBusy} onclick={() => setServiceOpen(false)}>{t().cancel}</Button>
        <Button type="submit" disabled={serviceBusy}>{#if serviceBusy}<Spinner aria-hidden="true" />{t().registering}{:else}{t().registerService}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={() => createOpen, setCreateOpen}>
  <Dialog.Content class="sm:max-w-md">
    <form class="grid gap-4" onsubmit={createConnector}>
      <Dialog.Header>
        <Dialog.Title>{t().newTunnelTitle}</Dialog.Title>
        <Dialog.Description class="sr-only">{t().createDesc}</Dialog.Description>
      </Dialog.Header>
      <Field.Group>
        <Field.Field>
          <div class="flex items-center gap-1"><Field.Label for="connector-name">{t().tunnelName}</Field.Label><InfoTooltip id="connector-name-help" label={t().tunnelName} description={t().nameHelp} /></div>
          <InputGroup.Root>
            <InputGroup.Addon><Storage aria-hidden="true" /></InputGroup.Addon>
            <InputGroup.Input id="connector-name" bind:value={name} placeholder={t().exampleTunnel} aria-describedby="connector-name-help" required />
          </InputGroup.Root>
        </Field.Field>
        <Field.Field>
          <Field.Label for="connector-description">{t().descriptionOptional}</Field.Label>
          <Textarea id="connector-description" bind:value={newDescription} placeholder={t().descriptionPlaceholder} maxlength={500} rows={3} />
        </Field.Field>
      </Field.Group>
      {#if createError}<Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description>{createError}</Alert.Description></Alert.Root>{/if}
      <Dialog.Footer>
        <Button variant="outline" disabled={createBusy} onclick={() => setCreateOpen(false)}>{t().cancel}</Button>
        <Button type="submit" disabled={createBusy}>{#if createBusy}<Spinner aria-hidden="true" />{t().creating}{:else}{t().createTunnel}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={nameOpen}>
  <Dialog.Content class="sm:max-w-md">
    <form class="grid gap-4" onsubmit={saveName}>
      <Dialog.Header>
        <Dialog.Title>{t().editName}</Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field data-invalid={nameError ? 'true' : undefined}>
          <Field.Label for="connector-display-name">{t().name}</Field.Label>
          <Input id="connector-display-name" bind:value={nameInput} placeholder={selected?.name} maxlength={64} autocomplete="off" aria-invalid={nameError ? 'true' : undefined} />
          {#if nameError}<Field.Error>{nameError}</Field.Error>{/if}
        </Field.Field>
      </Field.Group>
      <Dialog.Footer>
        <Button variant="outline" disabled={nameBusy} onclick={() => (nameOpen = false)}>{t().cancel}</Button>
        <Button type="submit" disabled={nameBusy}>{#if nameBusy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={descriptionOpen}>
  <Dialog.Content class="sm:max-w-md">
    <form class="grid gap-4" onsubmit={saveDescription}>
      <Dialog.Header>
        <Dialog.Title>{t().editDescription}</Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field data-invalid={descriptionError ? 'true' : undefined}>
          <Field.Label for="connector-description-edit">{t().descriptionLabel}</Field.Label>
          <Textarea id="connector-description-edit" bind:value={descriptionInput} placeholder={t().descriptionPlaceholder} maxlength={500} rows={4} />
          {#if descriptionError}<Field.Error>{descriptionError}</Field.Error>{/if}
        </Field.Field>
      </Field.Group>
      <Dialog.Footer>
        <Button variant="outline" disabled={descriptionBusy} onclick={() => (descriptionOpen = false)}>{t().cancel}</Button>
        <Button type="submit" disabled={descriptionBusy}>{#if descriptionBusy}<Spinner aria-hidden="true" />{t().saving}{:else}{t().save}{/if}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<ConfirmNameDialog id="reset-tunnel-token" open={resetTokenOpen} title={t().resetTitle} warning={t().resetWarning} targetName={resetTokenTarget ? connectorLabel(resetTokenTarget) : ''} submitLabel={t().resetSubmit} busy={tokenBusy} error={tokenError} onConfirm={resetToken} onClose={() => { resetTokenOpen = false; resetTokenTarget = null; tokenError = ''; }} />

<ConfirmNameDialog id="delete-tunnel" open={deleteOpen} title={t().deleteTitle} warning={t().deleteWarning} targetName={deleteTarget ? connectorLabel(deleteTarget) : ''} submitLabel={t().deleteTunnel} busy={deleteBusy} error={deleteError} onConfirm={deleteConnector} onClose={() => { deleteOpen = false; deleteTarget = null; deleteError = ''; }} />
