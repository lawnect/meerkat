<script lang="ts">
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
  import * as RadioGroup from '$lib/components/ui/radio-group/index.js';
  import { Spinner } from '$lib/components/ui/spinner/index.js';
  import * as NativeSelect from '$lib/components/ui/native-select/index.js';
  import * as Table from '$lib/components/ui/table/index.js';
  import { type FilterFn, createColumnHelper, createTable, renderComponent, renderSnippet } from '@tanstack/svelte-table';
  import DataTable from '../components/data-table/DataTable.svelte';
  import DataTablePagination from '../components/data-table/DataTablePagination.svelte';
  import SortHeader from '../components/data-table/SortHeader.svelte';
  import { features } from '../components/data-table/features';
  import * as ToggleGroup from '$lib/components/ui/toggle-group/index.js';
  import * as Tooltip from '$lib/components/ui/tooltip/index.js';
  import { Add, ArrowReset20, ArrowSync, ArrowUndo, ChevronRight, Delete, Edit, Globe, Globe20, PlugConnected, Save, Search, Search20, Warning } from '../icons';
  import { api, connectorLabel, errorMessage } from '../api';
  import ConfirmNameDialog from '../components/ConfirmNameDialog.svelte';
  import CopyText from '../components/CopyText.svelte';
  import IconButton from '../components/IconButton.svelte';
  import InfoTooltip from '../components/InfoTooltip.svelte';
  import PageHead from '../components/PageHead.svelte';
  import Status from '../components/Status.svelte';
  import { intlLocale, translations } from '../i18n.svelte';
  import { urlTableState } from '../components/data-table/urlTable.svelte';
  import { connectorDetailPath, domainDetailPath, pageHref } from '../navigation';
  import type { DomainDNSCheck, DomainMode, Outlet, ServiceRecord, TunnelCheck } from '../types';

  type TunnelCheckKey = 'current' | 'draft';

  const t = translations({
    domains: 'Domains',
    addDomain: 'Add domain',
    domainLabel: (domain: string) => `${domain} domain`,
    primaryDomain: 'Primary domain',
    additionalDomain: 'Additional domain',
    dnsSettings: 'DNS setup',
    dnsMode: 'DNS record mode',
    modes: { wildcard: 'Wildcard', hosts: 'Individual hosts' } as Record<DomainMode, string>,
    modeHints: {
      wildcard: 'One * record routes every subdomain to the Outlet.',
      hosts: 'One record each for the admin address and every public address. Use this when the root or other subdomains run on another server.',
    } as Record<DomainMode, string>,
    modeSaveFailed: (code: string) => `Couldn't change the DNS records mode: ${code}`,
    noRecordsYet: 'Records appear here when you add public addresses.',
    adminAddress: 'Admin address',
    adminPorts: (tunnelPort: string) => tunnelPort ? `HTTPS 443: admin console · TCP ${tunnelPort}: tunnel connections` : 'HTTPS 443: admin console',
    fromEnvironment: 'Set by environment',
    fromEnvironmentTip: 'Managed by MEERKAT_DOMAIN and MEERKAT_ADMIN_HOSTNAME.',
    domainFromEnvironment: 'The primary domain is managed by environment variables and can\'t be edited.',
    publicAddresses: 'Public addresses',
    publicAddress: 'Public address',
    tunnel: 'Tunnel',
    service: 'Service',
    open: 'Open',
    search: 'Search domains',
    searchPlaceholder: 'Search domains',
    allModes: 'All modes',
    reset: 'Reset filters',
    domainListAria: 'Domains',
    noMatchesTitle: 'No matching domains',
    clearFilters: 'Clear filters',
    revert: 'Revert',
    noPublicAddresses: 'No public addresses use this domain yet',
    missingDomain: 'This domain isn\'t registered',
    backToDomains: 'Back to domains',
    dnsPointBefore: 'Point ',
    dnsPointAfter: ' to the Outlet server IP.',
    dnsPortsShort: 'Ports 80 and 443 must be reachable.',
    dnsPortsLabel: 'open ports',
    dnsPortsTip: 'Port 80 is used to issue HTTPS certificates and port 443 for HTTPS access. Certificates for this domain are issued and renewed automatically. If you use Cloudflare DNS, we recommend turning the proxy off (“DNS only”).',
    dnsCloudflareBadge: 'Cloudflare: DNS only',
    checkDns: 'Check DNS',
    checking: 'Checking…',
    resolved: 'Resolved',
    dnsNoResponse: 'No DNS response',
    noHostsToCheck: 'No hostnames to check',
    dnsNote: (target: string) => `This check only verifies that DNS responds. With the Cloudflare proxy on, the IP shown may be a Cloudflare IP rather than your server IP. Verify the HTTPS certificate and real access at ${target}.`,
    registeredPublicAddress: 'the public address you registered',
    changePrimary: 'Change primary domain',
    deleteDomain: 'Delete domain',
    noDomainsTitle: 'No domains yet',
    noDomainsBody: 'Add a domain to open the admin console and publish services on its subdomains.',
    tunnelAddress: 'Tunnel address',
    customAddress: 'Custom',
    defaultAddress: 'Default',
    notConfigured: 'Not set',
    tunnelHelp: 'The address the tunnel container on your app server uses to reach the Outlet over TCP. Defaults to the admin address on the tunnel port, such as admin address:2333; it can be set separately.',
    tunnelHelpSet: (port: string) => `This host must point to the Outlet server IP as DNS only, and TCP port ${port} must be open to the outside.`,
    tunnelHelpUnset: 'Register a primary domain or set an address manually.',
    testConnection: 'Test connection',
    changeAddress: 'Change address',
    dnsNoResponseFor: (host: string) => `No DNS response: ${host}`,
    connectFailed: 'Connection failed',
    reachable: 'Reachable',
    failed: 'Failed',
    latency: (ms: number) => `${ms} ms`,
    dialRefused: 'Connection refused · port is closed',
    dialTimeout: 'Timed out · possibly blocked by a firewall',
    dialUnreachable: 'No network route',
    dialNotAllowed: 'Local address · not checked',
    partialWarning: 'If the tunnel container tries a failing address first, the connection can be slow or fail. Check the DNS records for that address. If only IPv6 addresses fail, Docker on the Outlet server may have no IPv6 route.',
    tunnelNote: (address: string, command: string) => `This only tests a TCP connection from the Outlet server to ${address}. It doesn't check whether the server that answered is this Outlet, or whether the firewall on the server running the tunnel allows it. Also run ${command} on that server to check.`,
    domainHasRoutes: 'The primary domain can\'t be changed while public addresses are registered.',
    domainExists: 'This domain is already registered or is a subdomain of an existing domain.',
    invalidDomain: 'Check the domain format.',
    domainSaveFailed: (code: string) => `Couldn't save the domain: ${code}`,
    dnsCheckFailed: (message: string) => `Couldn't check DNS: ${message}`,
    domainInUse: 'This domain can\'t be deleted because public addresses use it.',
    domainDeleteFailed: (code: string) => `Couldn't delete the domain: ${code}`,
    invalidTunnelAddress: 'Check the address format. Example: tunnel.example.com:2333 (127.0.0.1 and localhost can\'t be used)',
    tunnelAddressUnset: 'There is no tunnel address. Register a primary domain or specify an address.',
    tunnelDefaultUnavailable: 'There is no primary domain, so there is no default and the address can\'t be left blank.',
    tunnelCheckFailed: 'Couldn\'t check the connection',
    tunnelSaveFailed: 'Couldn\'t save the tunnel address',
    setPrimary: 'Set primary domain',
    addNewDomain: 'Add a domain',
    primaryDialogDescription: 'The admin console opens at a generated 8-character address under the primary domain, e.g. k7m2x9qa.example.com. The address keeps its name when you change the primary domain.',
    domain: 'Domain',
    domainPlaceholder: 'e.g. example.com',
    close: 'Close',
    retry: 'Check again',
    cancel: 'Cancel',
    save: 'Save',
    saving: 'Saving…',
    changeTunnel: 'Change tunnel address',
    tunnelInputTip: 'Format: host:port. Leave it blank to use the default, the admin address on the tunnel port. Without a port, the Outlet container\'s tunnel port (default 2333) is used; if you changed the public port on the server (MEERKAT_TUNNEL_PORT), include it.',
    tunnelSaveNote: 'The running tunnel reconnects to the new address within about 5 seconds.',
    deleteWarning: 'This can\'t be undone. You can\'t delete a domain that still has public addresses.',
  }, {
    domains: '도메인',
    addDomain: '도메인 추가',
    domainLabel: (domain: string) => `${domain} 도메인`,
    primaryDomain: '대표 도메인',
    additionalDomain: '추가 도메인',
    dnsSettings: 'DNS 설정',
    dnsMode: 'DNS 레코드 방식',
    modes: { wildcard: '와일드카드', hosts: '개별 호스트' },
    modeHints: {
      wildcard: '* 레코드 하나로 모든 하위 도메인을 Outlet으로 연결합니다.',
      hosts: '관리 주소와 공개 주소별로 레코드를 등록합니다. 루트 도메인 또는 다른 하위 도메인을 별도 서버에서 운영하는 경우 선택합니다.',
    },
    modeSaveFailed: (code: string) => `DNS 레코드 방식 변경 중 오류가 발생했습니다: ${code}`,
    noRecordsYet: '공개 주소 추가 시 레코드가 표시됩니다.',
    adminAddress: '관리 주소',
    adminPorts: (tunnelPort: string) => tunnelPort ? `HTTPS 443: 관리 콘솔 · TCP ${tunnelPort}: 터널 접속` : 'HTTPS 443: 관리 콘솔',
    fromEnvironment: '환경 변수로 구성됨',
    fromEnvironmentTip: 'MEERKAT_DOMAIN, MEERKAT_ADMIN_HOSTNAME 환경 변수로 관리',
    domainFromEnvironment: '대표 도메인은 환경 변수로 관리되어 수정할 수 없습니다.',
    publicAddresses: '공개 주소',
    publicAddress: '공개 주소',
    tunnel: '터널',
    service: '서비스',
    open: '열기',
    search: '도메인 검색',
    searchPlaceholder: '도메인 검색',
    allModes: '전체 방식',
    reset: '필터 초기화',
    domainListAria: '도메인 목록',
    noMatchesTitle: '일치하는 도메인이 없습니다',
    clearFilters: '필터 초기화',
    revert: '되돌리기',
    noPublicAddresses: '이 도메인을 사용하는 공개 주소가 없습니다',
    missingDomain: '등록되지 않은 도메인입니다',
    backToDomains: '도메인 목록으로 이동',
    dnsPointBefore: '',
    dnsPointAfter: ' 레코드를 Outlet 서버 IP로 지정하십시오.',
    dnsPortsShort: '80, 443 포트를 외부에 개방하십시오.',
    dnsPortsLabel: '포트 개방',
    dnsPortsTip: '80 포트는 HTTPS 인증서 발급, 443 포트는 HTTPS 접속에 사용됩니다. 인증서는 자동으로 발급 및 갱신됩니다. Cloudflare DNS 사용 시 프록시를 해제한 ‘DNS 전용’ 설정을 권장합니다.',
    dnsCloudflareBadge: 'Cloudflare: DNS 전용',
    checkDns: 'DNS 확인',
    checking: '확인 중…',
    resolved: '응답 정상',
    dnsNoResponse: 'DNS 응답 없음',
    noHostsToCheck: '확인할 호스트가 없습니다',
    dnsNote: (target: string) => `DNS 응답 여부만 확인합니다. Cloudflare 프록시가 활성화된 경우 서버 IP 대신 Cloudflare IP가 표시될 수 있습니다. HTTPS 인증서와 실제 접속은 ${target}에서 확인하십시오.`,
    registeredPublicAddress: '등록한 공개 주소',
    changePrimary: '대표 도메인 변경',
    deleteDomain: '도메인 삭제',
    noDomainsTitle: '등록된 도메인이 없습니다',
    noDomainsBody: '도메인을 추가하면 관리 콘솔에 접속하고 하위 도메인으로 서비스를 공개할 수 있습니다.',
    tunnelAddress: '터널 주소',
    customAddress: '직접 지정',
    defaultAddress: '기본값',
    notConfigured: '미설정',
    tunnelHelp: '앱 서버의 터널 컨테이너가 Outlet에 TCP로 접속하는 주소입니다. 기본값은 관리 주소의 터널 포트(예: 관리 주소:2333)이며, 별도로 지정할 수 있습니다.',
    tunnelHelpSet: (port: string) => `이 호스트의 DNS 레코드가 Outlet 서버 IP를 가리켜야 하며(DNS 전용), TCP ${port} 포트를 외부에 개방해야 합니다.`,
    tunnelHelpUnset: '대표 도메인을 등록하거나 주소를 직접 지정하십시오.',
    testConnection: '접속 테스트',
    changeAddress: '주소 변경',
    dnsNoResponseFor: (host: string) => `DNS 응답 없음: ${host}`,
    connectFailed: '접속 실패',
    reachable: '접속 가능',
    failed: '실패',
    latency: (ms: number) => `${ms}ms`,
    dialRefused: '연결 거부 · 포트 닫힘',
    dialTimeout: '시간 초과 · 방화벽 차단 가능성',
    dialUnreachable: '네트워크 경로 없음',
    dialNotAllowed: '로컬 주소 · 확인 제외',
    partialWarning: '터널 컨테이너가 실패한 주소를 먼저 시도하면 연결이 지연되거나 실패할 수 있습니다. 해당 DNS 레코드를 확인하십시오. IPv6 주소만 실패한 경우 Outlet 서버의 Docker에 IPv6 경로가 없을 수 있습니다.',
    tunnelNote: (address: string, command: string) => `Outlet 서버에서 ${address}(으)로의 TCP 연결만 확인합니다. 응답한 서버가 이 Outlet인지, 터널 실행 서버의 방화벽이 허용하는지는 확인하지 않습니다. 해당 서버에서 ${command} 명령으로도 확인하십시오.`,
    domainHasRoutes: '공개 주소가 등록된 상태에서는 대표 도메인을 변경할 수 없습니다.',
    domainExists: '이미 등록된 도메인이거나 기존 도메인의 하위 도메인입니다.',
    invalidDomain: '도메인 형식이 올바르지 않습니다.',
    domainSaveFailed: (code: string) => `도메인 저장 중 오류가 발생했습니다: ${code}`,
    dnsCheckFailed: (message: string) => `DNS 확인 중 오류가 발생했습니다: ${message}`,
    domainInUse: '이 도메인을 사용하는 공개 주소가 있어 삭제할 수 없습니다.',
    domainDeleteFailed: (code: string) => `도메인 삭제 중 오류가 발생했습니다: ${code}`,
    invalidTunnelAddress: '주소 형식이 올바르지 않습니다. 예: tunnel.example.com:2333 (127.0.0.1, localhost 사용 불가)',
    tunnelAddressUnset: '터널 주소가 없습니다. 대표 도메인을 등록하거나 주소를 지정하십시오.',
    tunnelDefaultUnavailable: '대표 도메인이 없어 기본값을 사용할 수 없습니다. 주소를 입력하십시오.',
    tunnelCheckFailed: '접속 확인 중 오류가 발생했습니다',
    tunnelSaveFailed: '터널 주소 저장 중 오류가 발생했습니다',
    setPrimary: '대표 도메인 설정',
    addNewDomain: '도메인 추가',
    primaryDialogDescription: '관리 콘솔은 대표 도메인 아래 무작위 8자 주소(예: k7m2x9qa.example.com)에서 제공됩니다. 대표 도메인을 변경해도 이 이름은 유지됩니다.',
    domain: '도메인',
    domainPlaceholder: '예: example.com',
    close: '닫기',
    retry: '다시 확인',
    cancel: '취소',
    save: '저장',
    saving: '저장 중…',
    changeTunnel: '터널 주소 변경',
    tunnelInputTip: '호스트:포트 형식. 비워 두면 기본값(관리 주소:터널 포트)을 사용합니다. 포트 생략 시 Outlet 컨테이너의 터널 포트(기본 2333)가 적용되므로, 서버의 공개 포트(MEERKAT_TUNNEL_PORT)를 변경한 경우 포트를 함께 입력하십시오.',
    tunnelSaveNote: '실행 중인 터널은 약 5초 이내에 새 주소로 재연결됩니다.',
    deleteWarning: '삭제한 도메인은 복원할 수 없습니다. 이 도메인을 사용하는 공개 주소가 있으면 삭제할 수 없습니다.',
  });

  let { outlet, records, onSaved, detail, onOpenDetail, onBack }: {
    outlet: Outlet;
    records: ServiceRecord[];
    onSaved: () => Promise<void>;
    detail: string | null;
    onOpenDetail: (domain: string) => void;
    onBack: () => void;
  } = $props();
  const routes = $derived(records.flatMap((record) => record.routes.map((route) => ({ ...route, connector: record.connector, service: record.service }))));
  let domainInput = $state('');
  let modeInput = $state<DomainMode>('wildcard');
  let modeBusy = $state<Record<string, boolean>>({});
  // 상세 화면에서 바꾼 DNS 방식은 저장을 누를 때까지 초안으로만 둡니다.
  let draftMode = $state<DomainMode | null>(null);
  let modeErrors = $state<Record<string, string>>({});
  let domainDialogMode = $state<'add' | 'primary'>('add');
  let dialogError = $state('');
  let dialogBusy = $state(false);
  let deleteTarget = $state('');
  let deleteOpen = $state(false);
  let deleteError = $state('');
  let deleteBusy = $state(false);
  let dnsChecks = $state<Record<string, DomainDNSCheck>>({});
  let dnsBusy = $state<Record<string, boolean>>({});
  let dnsErrors = $state<Record<string, string>>({});
  let tunnelInput = $state('');
  let tunnelDialogError = $state('');
  let tunnelDialogBusy = $state(false);
  let tunnelChecks = $state<Partial<Record<TunnelCheckKey, TunnelCheck>>>({});
  let tunnelCheckBusy = $state<Partial<Record<TunnelCheckKey, boolean>>>({});
  let tunnelCheckErrors = $state<Partial<Record<TunnelCheckKey, string>>>({});
  let domainOpen = $state(false);
  let resultOpen = $state(false);
  let tunnelOpen = $state(false);
  let resultView = $state<{ kind: 'dns'; domain: string } | { kind: 'tunnel' } | null>(null);

  // DNS 확인과 접속 테스트 결과는 대화상자로 보여줍니다.
  function openDnsCheck(domain: string): void {
    resultView = { kind: 'dns', domain };
    resultOpen = true;
    void checkDNS(domain);
  }

  function openTunnelCheck(): void {
    resultView = { kind: 'tunnel' };
    resultOpen = true;
    void checkTunnel('current');
  }
  let tunnelDialogSession = 0;

  const tunnel = $derived(outlet.tunnel);
  const draftAddress = $derived(normalizeHost(tunnelInput) || tunnel?.default_address || '');
  const tunnelFieldError = $derived(tunnelDialogError || tunnelCheckErrors.draft || '');
  const tunnelHost = $derived(tunnel?.address ? hostOf(tunnel.address) : '');
  // 환경 변수(MEERKAT_DOMAIN)로 정한 대표 도메인은 화면에서 바꿀 수 없습니다.
  const domainFromEnvironment = $derived(outlet.domain_source === 'environment');

  function dialError(code = ''): string {
    return code === 'connection_refused' ? t().dialRefused : code === 'timeout' ? t().dialTimeout : code === 'unreachable' ? t().dialUnreachable : code === 'address_not_allowed' ? t().dialNotAllowed : t().connectFailed;
  }

  function hostOf(address: string): string {
    return address.replace(/:\d+$/, '').replace(/^\[(.*)\]$/, '$1');
  }

  function portOf(address: string): string {
    return address.match(/:(\d+)$/)?.[1] ?? '';
  }

  const domains = $derived.by(() => {
    const registered = outlet.domains?.length ? outlet.domains : outlet.domain ? [outlet.domain] : [];
    return outlet.domain ? [outlet.domain, ...registered.filter((domain) => domain !== outlet.domain)] : registered;
  });

  const modeOf = (domain: string): DomainMode => outlet.modes?.[domain] ?? 'wildcard';
  const detailMode = $derived(detail ? draftMode ?? modeOf(detail) : 'wildcard');
  const modeDirty = $derived(Boolean(detail && draftMode && draftMode !== modeOf(detail)));

  // 도메인 목록도 터널 목록과 같은 Data Table(TanStack) 구성으로 검색·필터·정렬·페이지를 처리합니다.
  type DomainRow = { domain: string; primary: boolean; mode: DomainMode; addresses: number };
  const columnHelper = createColumnHelper<typeof features, DomainRow>();
  type SortColumn = { getIsSorted: () => false | 'asc' | 'desc'; getToggleSortingHandler: () => ((event: unknown) => void) | undefined };
  const sortHeader = (column: SortColumn, label: string) => renderComponent(SortHeader, { label, sorted: column.getIsSorted(), onclick: column.getToggleSortingHandler() });
  const domainFilter: FilterFn<typeof features, DomainRow> = (row, columnId, value: string) => String(row.getValue(columnId)).includes(String(value).trim().toLowerCase());
  const modeFilter: FilterFn<typeof features, DomainRow> = (row, _columnId, value: DomainMode) => row.original.mode === value;
  const columns = columnHelper.columns([
    columnHelper.accessor('domain', { header: ({ column }) => sortHeader(column, t().domain), cell: ({ row }) => renderSnippet(domainCell, row.original), sortFn: 'text', filterFn: domainFilter }),
    columnHelper.accessor('mode', { header: ({ column }) => sortHeader(column, t().dnsMode), cell: ({ row }) => renderSnippet(modeCell, row.original), sortFn: 'text', filterFn: modeFilter }),
    columnHelper.accessor('addresses', { header: ({ column }) => sortHeader(column, t().publicAddresses), cell: ({ row }) => row.original.addresses, sortFn: 'basic' }),
  ]);
  const domainRows = $derived<DomainRow[]>(domains.map((domain) => ({
    domain,
    primary: domain === outlet.domain,
    mode: modeOf(domain),
    // 여러 서비스가 함께 쓰는 주소는 한 번만 셉니다.
    addresses: new Set(routes.filter((route) => route.hostname.endsWith(`.${domain}`)).map((route) => route.hostname)).size,
  })));
  // 검색·방식 필터, 정렬, 페이지는 주소(?q=&mode=&sort=&page=)가 단일 출처입니다.
  const urlTable = urlTableState({
    filters: [{ param: 'q', column: 'domain' }, { param: 'mode', column: 'mode', allowed: ['wildcard', 'hosts'] }],
    sortable: ['domain', 'mode', 'addresses'],
    defaultSort: [],
    pageSize: 12,
  });
  const table = createTable({
    features,
    get data() { return domainRows; },
    columns,
    getRowId: (row) => row.domain,
    sortDescFirst: false,
    ...urlTable.handlers,
    get state() { return urlTable.state; },
  });
  const searchValue = $derived((table.getColumn('domain')?.getFilterValue() as string | undefined) ?? '');
  const modeValue = $derived((table.getColumn('mode')?.getFilterValue() as DomainMode | undefined) ?? 'all');
  const filtersActive = $derived(Boolean(searchValue.trim()) || modeValue !== 'all');
  const filteredCount = $derived(table.getFilteredRowModel().rows.length);

  function clearFilters(): void {
    table.setColumnFilters([]);
  }

  // 다른 도메인으로 옮기면 저장하지 않은 DNS 방식 초안은 버립니다.
  $effect(() => {
    void detail;
    draftMode = null;
  });

  // DNS 제공자에 만들 레코드입니다. 와일드카드는 * 하나면 되고, 개별 호스트는 관리 주소·공개 주소마다 만듭니다.
  // 터널 주소가 이 도메인에 속하고 위 레코드로 덮이지 않으면(루트이거나 개별 호스트 방식) 함께 보여줍니다.
  function recordsOf(domain: string, mode: DomainMode = modeOf(domain)): string[] {
    const under = (host: string) => host.endsWith(`.${domain}`);
    const hosts = mode === 'wildcard'
      ? [`*.${domain}`]
      : [
        ...(domain === outlet.domain && outlet.admin_hostname ? [outlet.admin_hostname] : []),
        ...[...new Set(routes.map((route) => route.hostname).filter(under))].sort(),
      ];
    const tunnelCovered = mode === 'wildcard' ? under(tunnelHost) : hosts.includes(tunnelHost);
    if ((tunnelHost === domain || under(tunnelHost)) && !tunnelCovered) hosts.push(tunnelHost);
    return hosts;
  }

  async function changeMode(domain: string, mode: DomainMode): Promise<void> {
    if (modeBusy[domain] || mode === modeOf(domain)) return;
    modeBusy[domain] = true;
    modeErrors[domain] = '';
    try {
      await api<Outlet>(`/api/v1/outlet/domains/${encodeURIComponent(domain)}/mode`, { method: 'PUT', body: JSON.stringify({ mode }) });
      await onSaved();
      draftMode = null;
      delete dnsChecks[domain];
    } catch (cause) {
      modeErrors[domain] = t().modeSaveFailed(errorMessage(cause));
    } finally {
      modeBusy[domain] = false;
    }
  }


  // 주소창에서 붙여 넣은 값도 받아 줍니다: 앞뒤 공백, http(s)://, 경로를 지우고 소문자로 바꿉니다.
  function normalizeHost(value: string): string {
    return value.trim().toLowerCase().replace(/^[a-z][a-z0-9+.-]*:\/\//, '').replace(/[/?#].*$/, '');
  }

  function openDomainDialog(mode: 'add' | 'primary'): void {
    domainDialogMode = mode;
    domainInput = mode === 'primary' ? outlet.domain || '' : '';
    // 대표 도메인은 개별 호스트 방식으로 시작합니다. 와일드카드는 필요할 때 상세 화면에서 바꿉니다.
    modeInput = mode === 'primary' ? (outlet.domain ? modeOf(outlet.domain) : 'hosts') : 'wildcard';
    dialogError = '';
    domainOpen = true;
  }

  async function checkDNS(domain: string): Promise<void> {
    if (dnsBusy[domain]) return;
    dnsBusy[domain] = true;
    dnsErrors[domain] = '';
    try {
      dnsChecks[domain] = await api<DomainDNSCheck>(`/api/v1/outlet/domains/${encodeURIComponent(domain)}/check-dns`, { method: 'POST' });
    } catch (cause) {
      dnsErrors[domain] = t().dnsCheckFailed(errorMessage(cause));
    } finally {
      dnsBusy[domain] = false;
    }
  }

  async function saveDomain(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (dialogBusy) return;
    const value = normalizeHost(domainInput);
    const primary = domainDialogMode === 'primary' || !outlet.domain;
    domainInput = value;
    dialogError = '';
    if (!value || /\s/.test(value)) {
      dialogError = t().invalidDomain;
      return;
    }
    dialogBusy = true;
    try {
      await api<Outlet>(primary ? '/api/v1/outlet' : '/api/v1/outlet/domains', {
        method: primary ? 'PUT' : 'POST',
        body: JSON.stringify({ domain: value, mode: modeInput }),
      });
      await onSaved();
      if (detail || domainDialogMode === 'add') onOpenDetail(value);
      domainOpen = false;
    } catch (cause) {
      const code = errorMessage(cause);
      dialogError = code === 'domain_has_routes' ? t().domainHasRoutes : code === 'domain_exists' ? t().domainExists : code === 'invalid_domain' ? t().invalidDomain : code === 'domain_from_environment' ? t().domainFromEnvironment : t().domainSaveFailed(code);
    } finally {
      dialogBusy = false;
    }
  }

  function tunnelError(prefix: string, cause: unknown): string {
    const code = errorMessage(cause);
    return code === 'invalid_tunnel_address' ? t().invalidTunnelAddress : code === 'tunnel_address_unset' ? t().tunnelAddressUnset : code === 'tunnel_default_unavailable' ? t().tunnelDefaultUnavailable : `${prefix}: ${code}`;
  }

  async function checkTunnel(key: TunnelCheckKey, address = ''): Promise<void> {
    if (tunnelCheckBusy[key]) return;
    const session = tunnelDialogSession;
    // A draft result from a closed dialog must not appear when it opens again.
    const stale = () => key === 'draft' && session !== tunnelDialogSession;
    tunnelCheckBusy[key] = true;
    tunnelCheckErrors[key] = '';
    try {
      const result = await api<TunnelCheck>('/api/v1/outlet/tunnel/check', { method: 'POST', body: JSON.stringify({ address }) });
      if (!stale()) tunnelChecks[key] = result;
    } catch (cause) {
      if (!stale()) tunnelCheckErrors[key] = tunnelError(t().tunnelCheckFailed, cause);
    } finally {
      tunnelCheckBusy[key] = false;
    }
  }

  function openTunnelDialog(): void {
    tunnelDialogSession += 1;
    tunnelInput = tunnel?.custom ? tunnel.address : '';
    tunnelDialogError = '';
    tunnelChecks.draft = undefined;
    tunnelCheckErrors.draft = '';
    tunnelOpen = true;
  }

  async function saveTunnel(event: SubmitEvent): Promise<void> {
    event.preventDefault();
    if (tunnelDialogBusy) return;
    tunnelDialogError = '';
    tunnelDialogBusy = true;
    try {
      tunnelInput = normalizeHost(tunnelInput);
      await api<Outlet>('/api/v1/outlet/tunnel', { method: 'PUT', body: JSON.stringify({ address: tunnelInput }) });
      await onSaved();
      tunnelChecks.current = undefined;
      tunnelOpen = false;
    } catch (cause) {
      tunnelDialogError = tunnelError(t().tunnelSaveFailed, cause);
    } finally {
      tunnelDialogBusy = false;
    }
  }

  function openDeleteDialog(domain: string): void {
    deleteTarget = domain;
    deleteError = '';
    deleteOpen = true;
  }

  async function removeDomain(): Promise<void> {
    if (!deleteTarget || deleteBusy) return;
    deleteError = '';
    deleteBusy = true;
    try {
      await api<Outlet>(`/api/v1/outlet/domains/${encodeURIComponent(deleteTarget)}`, { method: 'DELETE' });
      await onSaved();
      onBack();
      deleteOpen = false;
    } catch (cause) {
      const code = errorMessage(cause);
      deleteError = code === 'domain_has_routes' ? t().domainInUse : t().domainDeleteFailed(code);
    } finally {
      deleteBusy = false;
    }
  }
</script>

{#snippet host(text: string)}<CopyText value={text} class="inline-block bg-muted px-1 py-0.5 align-middle text-xs" />{/snippet}

{#snippet alertError(message: string)}
  <Alert.Root variant="destructive"><Warning aria-hidden="true" /><Alert.Description class="wrap-anywhere">{message}</Alert.Description></Alert.Root>
{/snippet}

{#snippet stepNumber(number: number)}
  <Item.Media class="size-6 bg-muted text-xs font-medium tabular-nums">{number}</Item.Media>
{/snippet}


{#snippet modeCell(row: { mode: DomainMode })}
  <Badge variant="outline" class="cursor-default select-none">{t().modes[row.mode]}</Badge>
{/snippet}

{#snippet domainCell(row: { domain: string; primary: boolean })}
  <span class="flex min-w-0 items-center gap-2">
    <a href={domainDetailPath(row.domain)} onclick={(event) => { event.preventDefault(); onOpenDetail(row.domain); }} class="truncate text-sm font-medium outline-none after:absolute after:inset-0 text-link focus-visible:ring-3 focus-visible:ring-ring/50">{row.domain}</a>
    {#if row.primary}<Badge variant="secondary" class="cursor-default select-none">{t().primaryDomain}</Badge>{/if}
  </span>
{/snippet}

{#snippet listAction()}
  <Button onclick={() => openDomainDialog(outlet.domain ? 'add' : 'primary')}><Add aria-hidden="true" />{t().addDomain}</Button>
{/snippet}

{#if detail}
  {@const primary = detail === outlet.domain}
  {@const registered = domains.includes(detail)}
  {@const records = recordsOf(detail, detailMode)}
  {@const domainRoutes = routes.filter((route) => route.hostname.endsWith(`.${detail}`)).sort((a, b) => a.hostname.localeCompare(b.hostname))}
  {#snippet detailAction()}
    {#if primary}
      {#if !domainFromEnvironment}<Button variant="outline" onclick={() => openDomainDialog('primary')}><Edit aria-hidden="true" />{t().changePrimary}</Button>{/if}
    {:else}
      <Button variant="destructive" onclick={() => openDeleteDialog(detail)}><Delete aria-hidden="true" />{t().deleteDomain}</Button>
    {/if}
    {#if modeDirty}
      <Button variant="outline" disabled={modeBusy[detail]} onclick={() => { draftMode = null; }}><ArrowUndo aria-hidden="true" />{t().revert}</Button>
      <Button disabled={modeBusy[detail]} onclick={() => { if (detail && draftMode) void changeMode(detail, draftMode); }}>{#if modeBusy[detail]}<Spinner aria-hidden="true" />{t().saving}{:else}<Save aria-hidden="true" />{t().save}{/if}</Button>
    {/if}
  {/snippet}
  <PageHead title={detail} crumbs={[{ label: t().domains, href: pageHref('domains'), onclick: (event) => { event.preventDefault(); onBack(); } }, { label: detail }]} action={registered ? detailAction : undefined} />
  {#if !registered}
    <Empty.Root class="border border-dashed bg-card">
      <Empty.Header>
        <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
        <Empty.Title>{t().missingDomain}</Empty.Title>
      </Empty.Header>
      <Empty.Content><Button variant="outline" href={pageHref('domains')} onclick={(event) => { event.preventDefault(); onBack(); }}>{t().backToDomains}</Button></Empty.Content>
    </Empty.Root>
  {:else}
    <div class="grid min-w-0 gap-4">
      <Card.Root size="sm" role="region" aria-labelledby="dns-title">
        <Card.Header>
          <Card.Title id="dns-title" class="flex flex-wrap items-center gap-2">
            {t().dnsSettings}<Badge variant={primary ? 'secondary' : 'outline'} class="cursor-default select-none">{primary ? t().primaryDomain : t().additionalDomain}</Badge>
            {#if primary && domainFromEnvironment}
              <Tooltip.Root>
                <Tooltip.Trigger>
                  {#snippet child({ props })}<Badge {...props} variant="outline" tabindex={0} class="cursor-default select-none">{t().fromEnvironment}</Badge>{/snippet}
                </Tooltip.Trigger>
                <Tooltip.Content class="max-w-xs">{t().fromEnvironmentTip}</Tooltip.Content>
              </Tooltip.Root>
            {/if}
          </Card.Title>
          {#if primary && outlet.admin_hostname}
            <Card.Description class="grid gap-1">
              <span class="flex min-w-0 items-center gap-1.5">{t().adminAddress}<CopyText value={outlet.admin_hostname} class="text-xs" /></span>
              <span>{t().adminPorts(tunnelHost === outlet.admin_hostname && tunnel?.address ? portOf(tunnel.address) : '')}</span>
            </Card.Description>
          {/if}
          <Card.Action>
            <ToggleGroup.Root type="single" variant="outline" size="sm" aria-label={t().dnsMode} disabled={modeBusy[detail]} value={detailMode} onValueChange={(next) => { if (next) draftMode = next as DomainMode; }}>
              {#each ['wildcard', 'hosts'] as const as mode (mode)}
                <Tooltip.Root>
                  <Tooltip.Trigger>
                    {#snippet child({ props })}
                      <ToggleGroup.Item {...props} value={mode}>{t().modes[mode]}</ToggleGroup.Item>
                    {/snippet}
                  </Tooltip.Trigger>
                  <Tooltip.Content class="max-w-xs">{t().modeHints[mode]}</Tooltip.Content>
                </Tooltip.Root>
              {/each}
            </ToggleGroup.Root>
          </Card.Action>
        </Card.Header>
        <Card.Content class="grid gap-3">
          {#if modeErrors[detail]}{@render alertError(modeErrors[detail])}{/if}
        <Item.Group>
          <Item.Root size="sm" class="px-0" role="listitem">
            {@render stepNumber(1)}
            <Item.Content>
              {#if records.length}
                <Item.Description class="line-clamp-none block text-foreground">{t().dnsPointBefore}{#each records as record, index (record)}{#if index}{' '}{/if}{@render host(record)}{/each}{t().dnsPointAfter}</Item.Description>
              {:else}
                <Item.Description class="line-clamp-none block">{t().noRecordsYet}</Item.Description>
              {/if}
            </Item.Content>
          </Item.Root>
          <Item.Root size="sm" class="px-0" role="listitem">
            {@render stepNumber(2)}
            <Item.Content>
              <Item.Description class="line-clamp-none block text-foreground">{t().dnsPortsShort}<Badge variant="outline" class="mx-2 cursor-default align-middle select-none">{t().dnsCloudflareBadge}</Badge><span class="inline-block align-middle"><InfoTooltip id={`dns-ports-tip-${detail}`} label={t().dnsPortsLabel} description={t().dnsPortsTip} /></span></Item.Description>
            </Item.Content>
          </Item.Root>
        </Item.Group>
        </Card.Content>
        <Card.Footer class="justify-end">
          <Tooltip.Root>
            <Tooltip.Trigger>
              {#snippet child({ props })}
                <Button {...props} variant="outline" disabled={dnsBusy[detail]} onclick={() => detail && openDnsCheck(detail)}>
                  {#if dnsBusy[detail]}<Spinner aria-hidden="true" />{:else}<ArrowSync aria-hidden="true" />{/if}
                  {dnsBusy[detail] ? t().checking : t().checkDns}
                </Button>
              {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content side="top" class="max-w-xs">{t().dnsNote(primary && outlet.admin_hostname ? `https://${outlet.admin_hostname}` : t().registeredPublicAddress)}</Tooltip.Content>
          </Tooltip.Root>
        </Card.Footer>
      </Card.Root>

      <Card.Root size="sm" role="region" aria-labelledby="domain-routes-title">
        <Card.Header>
          <Card.Title id="domain-routes-title" class="flex items-center gap-2">{t().publicAddresses}<Badge variant="secondary">{domainRoutes.length}</Badge></Card.Title>
        </Card.Header>
        <Card.Content>
          {#if domainRoutes.length}
            <div class="overflow-hidden border">
              <Table.Root>
                <Table.Header class="bg-muted">
                  <Table.Row>
                    <Table.Head>{t().publicAddress}</Table.Head>
                    <Table.Head>{t().tunnel}</Table.Head>
                    <Table.Head>{t().service}</Table.Head>
                  </Table.Row>
                </Table.Header>
                <Table.Body>
                  {#each domainRoutes as route (route.id)}
                    <Table.Row>
                      <Table.Cell>{route.hostname}</Table.Cell>
                      <Table.Cell><a class="text-link focus-visible:ring-1 focus-visible:ring-ring focus-visible:outline-none" href={connectorDetailPath(route.connector.id)}>{connectorLabel(route.connector)}</a></Table.Cell>
                      <Table.Cell class="text-muted-foreground">{route.service.name}</Table.Cell>
                    </Table.Row>
                  {/each}
                </Table.Body>
              </Table.Root>
            </div>
          {:else}
            <Empty.Root class="border border-dashed">
              <Empty.Header>
                <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
                <Empty.Title>{t().noPublicAddresses}</Empty.Title>
              </Empty.Header>
            </Empty.Root>
          {/if}
        </Card.Content>
      </Card.Root>
    </div>
  {/if}
{:else}
  <PageHead title={t().domains} action={domains.length ? listAction : undefined} />
  <div class="grid min-w-0 gap-4">
    {#if domains.length}
      <div class="flex flex-wrap items-center gap-2" role="search" aria-label={t().search}>
        <InputGroup.Root class="min-w-48 grow sm:max-w-sm sm:grow-0">
          <InputGroup.Addon><Search aria-hidden="true" /></InputGroup.Addon>
          <InputGroup.Input type="search" value={searchValue} oninput={(event) => table.getColumn('domain')?.setFilterValue(event.currentTarget.value || undefined)} placeholder={t().searchPlaceholder} aria-label={t().search} autocomplete="off" />
        </InputGroup.Root>
        <NativeSelect.Root class="w-full sm:w-44" aria-label={t().dnsMode} bind:value={() => modeValue, (next) => { table.getColumn('mode')?.setFilterValue(next === 'all' ? undefined : next); }}>
          <NativeSelect.Option value="all">{t().allModes}</NativeSelect.Option>
          <NativeSelect.Option value="wildcard">{t().modes.wildcard}</NativeSelect.Option>
          <NativeSelect.Option value="hosts">{t().modes.hosts}</NativeSelect.Option>
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
      </div>
      {#if filteredCount}
        <DataTablePagination {table} placement="top" />
        <DataTable {table} ariaLabel={t().domainListAria} />
        <DataTablePagination {table} />
      {:else}
        <Empty.Root class="border border-dashed bg-card">
          <Empty.Header>
            <Empty.Media variant="icon"><Search20 class="size-5" aria-hidden="true" /></Empty.Media>
            <Empty.Title>{t().noMatchesTitle}</Empty.Title>
          </Empty.Header>
          <Empty.Content><Button variant="outline" onclick={clearFilters}><ArrowReset20 class="size-5" aria-hidden="true" />{t().clearFilters}</Button></Empty.Content>
        </Empty.Root>
      {/if}
    {:else}
      <Empty.Root class="border border-dashed bg-card">
        <Empty.Header>
          <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
          <Empty.Title>{t().noDomainsTitle}</Empty.Title>
          <Empty.Description>{t().noDomainsBody}</Empty.Description>
        </Empty.Header>
        <Empty.Content>
          <Button onclick={() => openDomainDialog('primary')}><Add aria-hidden="true" />{t().addDomain}</Button>
        </Empty.Content>
      </Empty.Root>
    {/if}

  <Card.Root size="sm" role="region" aria-labelledby="tunnel-title">
    <Card.Header>
      <Card.Title class="flex flex-wrap items-center gap-1.5">
        <span id="tunnel-title">{t().tunnelAddress}</span>
        <InfoTooltip id="tunnel-help-tip" label={t().tunnelAddress} description={`${t().tunnelHelp} ${tunnel?.address ? t().tunnelHelpSet(portOf(tunnel.address)) : t().tunnelHelpUnset}`} />
        <Badge variant={tunnel?.custom ? 'secondary' : 'outline'} class="cursor-default select-none">{tunnel?.custom ? t().customAddress : t().defaultAddress}</Badge>
      </Card.Title>
      <Card.Action class="max-sm:col-start-1 max-sm:row-span-1 max-sm:row-start-2 max-sm:mt-2">
        <ButtonGroup.Root>
          <Tooltip.Root disabled={!tunnel?.address}>
            <Tooltip.Trigger>
              {#snippet child({ props })}
                <Button {...props} variant="outline" disabled={!tunnel?.address || tunnelCheckBusy.current} onclick={openTunnelCheck}>
                  {#if tunnelCheckBusy.current}<Spinner aria-hidden="true" />{:else}<PlugConnected class="size-4" aria-hidden="true" />{/if}
                  {tunnelCheckBusy.current ? t().checking : t().testConnection}
                </Button>
              {/snippet}
            </Tooltip.Trigger>
            <Tooltip.Content side="bottom">{tunnel?.address ? t().tunnelNote(tunnel.address, `nc -vz ${hostOf(tunnel.address)} ${portOf(tunnel.address)}`) : ''}</Tooltip.Content>
          </Tooltip.Root>
          <Button variant="outline" onclick={openTunnelDialog}><Edit aria-hidden="true" />{t().changeAddress}</Button>
        </ButtonGroup.Root>
      </Card.Action>
    </Card.Header>
    <Card.Content class="flex min-w-0">
      {#if tunnel?.address}
        <CopyText value={tunnel.address} class="text-xs" />
      {:else}
        <p class="text-muted-foreground">{t().notConfigured}</p>
      {/if}
    </Card.Content>
  </Card.Root>
  </div>
{/if}

{#snippet dnsResult(domain: string, check: DomainDNSCheck)}
  {#if check.records.length}
    <Item.Group>
      {#each check.records as record (record.hostname)}
        {@const resolved = record.addresses.length > 0}
        <Item.Root variant="outline" size="sm" role="listitem">
          <Item.Content>
            <Item.Title class="line-clamp-none text-sm wrap-anywhere">{outlet.modes?.[domain] === 'hosts' || record.hostname === domain || record.hostname === outlet.admin_hostname || record.hostname === tunnelHost ? record.hostname : `*.${domain}`}</Item.Title>
            {#if resolved}<Item.Description class="line-clamp-none font-mono text-xs wrap-anywhere">{record.addresses.join(', ')}</Item.Description>{/if}
          </Item.Content>
          <Item.Actions><Status label={resolved ? t().resolved : t().dnsNoResponse} value={resolved ? 'connected' : 'error'} /></Item.Actions>
        </Item.Root>
      {/each}
    </Item.Group>
  {:else}
    <Empty.Root class="border border-dashed">
      <Empty.Header>
        <Empty.Media variant="icon"><Globe20 class="size-5" aria-hidden="true" /></Empty.Media>
        <Empty.Title>{t().noHostsToCheck}</Empty.Title>
      </Empty.Header>
    </Empty.Root>
  {/if}
{/snippet}

{#snippet tunnelResult(check: TunnelCheck)}
  {#if check.results.length}
    <Item.Group>
      {#each check.results as result (result.address)}
        <Item.Root variant="outline" size="sm" role="listitem">
          <Item.Content>
            <Item.Title class="line-clamp-none text-sm wrap-anywhere">{result.address}</Item.Title>
            <Item.Description class="line-clamp-none text-xs">{result.reachable ? t().latency(result.latency_ms ?? 0) : dialError(result.error)}</Item.Description>
          </Item.Content>
          <Item.Actions>
            {#if !result.reachable && check.reachable}
              <Tooltip.Root>
                <Tooltip.Trigger class="inline-flex outline-none focus-visible:ring-3 focus-visible:ring-ring/50"><Status label={t().failed} value="error" /></Tooltip.Trigger>
                <Tooltip.Content side="bottom" class="max-w-xs">{t().partialWarning}</Tooltip.Content>
              </Tooltip.Root>
            {:else}
              <Status label={result.reachable ? t().reachable : t().failed} value={result.reachable ? 'connected' : 'error'} />
            {/if}
          </Item.Actions>
        </Item.Root>
      {/each}
    </Item.Group>
  {:else}
    {@render alertError(check.error === 'dns_lookup_failed' ? t().dnsNoResponseFor(hostOf(check.address)) : t().connectFailed)}
  {/if}
{/snippet}

<Dialog.Root bind:open={resultOpen} onOpenChangeComplete={(next) => { if (!next) resultView = null; }}>
  <Dialog.Content class="sm:max-w-lg">
    <Dialog.Header>
      <Dialog.Title>{resultView?.kind === 'tunnel' ? t().testConnection : t().checkDns}</Dialog.Title>
    </Dialog.Header>
    <div class="grid gap-2" aria-live="polite">
      {#if resultView?.kind === 'dns'}
        {#if dnsBusy[resultView.domain]}
          <div class="flex justify-center py-4"><Spinner aria-label={t().checking} /></div>
        {:else if dnsErrors[resultView.domain]}
          {@render alertError(dnsErrors[resultView.domain])}
        {:else if dnsChecks[resultView.domain]}
          {@render dnsResult(resultView.domain, dnsChecks[resultView.domain])}
        {/if}
      {:else if resultView?.kind === 'tunnel'}
        {#if tunnelCheckBusy.current}
          <div class="flex justify-center py-4"><Spinner aria-label={t().checking} /></div>
        {:else if tunnelCheckErrors.current}
          {@render alertError(tunnelCheckErrors.current)}
        {:else if tunnelChecks.current}
          {@render tunnelResult(tunnelChecks.current)}
        {/if}
      {/if}
    </div>
    <Dialog.Footer>
      <Button variant="outline" disabled={resultView?.kind === 'dns' ? dnsBusy[resultView.domain] : tunnelCheckBusy.current} onclick={() => { if (resultView?.kind === 'dns') void checkDNS(resultView.domain); else void checkTunnel('current'); }}><ArrowSync aria-hidden="true" />{t().retry}</Button>
      <Dialog.Close>
        {#snippet child({ props })}<Button {...props}>{t().close}</Button>{/snippet}
      </Dialog.Close>
    </Dialog.Footer>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={domainOpen} onOpenChangeComplete={(next) => { if (!next) { domainInput = ''; dialogError = ''; } }}>
  <Dialog.Content class="sm:max-w-md" escapeKeydownBehavior={dialogBusy ? 'ignore' : 'close'} interactOutsideBehavior={dialogBusy ? 'ignore' : 'close'}>
    <form class="grid gap-4" onsubmit={saveDomain}>
      <Dialog.Header>
        <Dialog.Title class="flex items-center gap-1">
          {domainDialogMode === 'primary' ? outlet.domain ? t().changePrimary : t().setPrimary : t().addNewDomain}
          {#if domainDialogMode === 'primary'}<InfoTooltip id="primary-dialog-tip" label={t().primaryDomain} description={t().primaryDialogDescription} />{/if}
        </Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field data-invalid={dialogError ? 'true' : undefined}>
          <Field.Label for="domain-input">{t().domain}</Field.Label>
          <InputGroup.Root>
            <InputGroup.Addon><Globe aria-hidden="true" /></InputGroup.Addon>
            <InputGroup.Input id="domain-input" bind:value={domainInput} oninput={() => (dialogError = '')} placeholder={t().domainPlaceholder} autocapitalize="none" spellcheck={false} aria-invalid={dialogError ? 'true' : undefined} aria-describedby={dialogError ? 'domain-input-error' : undefined} required />
          </InputGroup.Root>
          {#if dialogError}<Field.Error id="domain-input-error">{dialogError}</Field.Error>{/if}
        </Field.Field>
        <Field.Set>
          <Field.Legend variant="label">{t().dnsMode}</Field.Legend>
          <RadioGroup.Root bind:value={() => modeInput, (next) => { modeInput = next as DomainMode; }}>
            {#each ['wildcard', 'hosts'] as const as mode (mode)}
              <Field.Label for={`domain-mode-${mode}`}>
                <Field.Field orientation="horizontal">
                  <Field.Content>
                    <Field.Title>{t().modes[mode]}</Field.Title>
                    <Field.Description>{t().modeHints[mode]}</Field.Description>
                  </Field.Content>
                  <RadioGroup.Item value={mode} id={`domain-mode-${mode}`} />
                </Field.Field>
              </Field.Label>
            {/each}
          </RadioGroup.Root>
        </Field.Set>
      </Field.Group>
      <Dialog.Footer>
        <Button variant="outline" disabled={dialogBusy} onclick={() => (domainOpen = false)}>{t().cancel}</Button>
        <Button type="submit" disabled={dialogBusy}>{#if dialogBusy}<Spinner aria-hidden="true" />{/if}{dialogBusy ? t().saving : t().save}</Button>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<Dialog.Root bind:open={tunnelOpen} onOpenChangeComplete={(next) => { if (!next) { tunnelInput = ''; tunnelDialogError = ''; } }}>
  <Dialog.Content class="sm:max-w-lg" escapeKeydownBehavior={tunnelDialogBusy ? 'ignore' : 'close'} interactOutsideBehavior={tunnelDialogBusy ? 'ignore' : 'close'}>
    <form class="grid gap-4" onsubmit={saveTunnel}>
      <Dialog.Header>
        <Dialog.Title>{t().changeTunnel}</Dialog.Title>
      </Dialog.Header>
      <Field.Group>
        <Field.Field data-invalid={tunnelFieldError ? 'true' : undefined}>
          <div class="flex items-center gap-1"><Field.Label for="tunnel-input">{t().tunnelAddress}</Field.Label><InfoTooltip id="tunnel-input-tip" label={t().tunnelAddress} description={t().tunnelInputTip} /></div>
          <InputGroup.Root>
            <InputGroup.Addon><PlugConnected aria-hidden="true" /></InputGroup.Addon>
            <InputGroup.Input id="tunnel-input" bind:value={tunnelInput} oninput={() => { tunnelDialogError = ''; tunnelCheckErrors.draft = ''; tunnelChecks.draft = undefined; }} placeholder={tunnel?.default_address || 'tunnel.example.com:2333'} autocapitalize="none" spellcheck={false} aria-invalid={tunnelFieldError ? 'true' : undefined} aria-describedby={tunnelFieldError ? 'tunnel-input-error' : undefined} />
            <InputGroup.Addon align="inline-end">
              <InputGroup.Button variant="secondary" disabled={tunnelCheckBusy.draft || !draftAddress} onclick={() => checkTunnel('draft', draftAddress)}>
                {#if tunnelCheckBusy.draft}<Spinner aria-hidden="true" />{:else}<PlugConnected class="size-4" aria-hidden="true" />{/if}
                {tunnelCheckBusy.draft ? t().checking : t().testConnection}
              </InputGroup.Button>
            </InputGroup.Addon>
          </InputGroup.Root>
          {#if tunnelFieldError}<Field.Error id="tunnel-input-error">{tunnelFieldError}</Field.Error>{/if}
        </Field.Field>
        {#if tunnelChecks.draft}{@render tunnelResult(tunnelChecks.draft)}{/if}
      </Field.Group>
      <Dialog.Footer>
        <Button variant="outline" disabled={tunnelDialogBusy} onclick={() => (tunnelOpen = false)}>{t().cancel}</Button>
        <Tooltip.Root>
          <Tooltip.Trigger>
            {#snippet child({ props })}
              <Button {...props} type="submit" disabled={tunnelDialogBusy}>{#if tunnelDialogBusy}<Spinner aria-hidden="true" />{/if}{tunnelDialogBusy ? t().saving : t().save}</Button>
            {/snippet}
          </Tooltip.Trigger>
          <Tooltip.Content side="top">{t().tunnelSaveNote}</Tooltip.Content>
        </Tooltip.Root>
      </Dialog.Footer>
    </form>
  </Dialog.Content>
</Dialog.Root>

<ConfirmNameDialog id="delete-domain" open={deleteOpen} title={t().deleteDomain} warning={t().deleteWarning} targetName={deleteTarget} submitLabel={t().deleteDomain} busy={deleteBusy} error={deleteError} onConfirm={removeDomain} onClose={() => { deleteOpen = false; deleteTarget = ''; deleteError = ''; }} />
