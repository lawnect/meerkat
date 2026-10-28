import type { AccessCounts, AccessLogEntry, AccessMetrics, AccessPoliciesView, AccessPolicy, AccessProviderView, AccessToken, AccessTokenInput, AccessTokensView, CreatedAccessToken, AddressMode, AlertRules, AlertSettings, AuditEvent, BlockCondition, BlockOperator, BlockRule, BlockRulesView, ClientBan, ClientBanSettings, ClientBansView, ConnectionLimits, RateLimitSettings, Connector, DomainMode, FailureCause, FailureLogEntry, LatencySeries, LatencyStats, LogSettings, LogView, Outlet, RequestLogEntry, RequestTopRow, RequestTopView, Route, Service, SsoSettings, Target, TunnelGuardSettings, TunnelGuardView, UsageBucket, UsageSeries, UsageUnit } from './types';
import { accessDecisions, accessMethods, accessRequesters, failureCauseGroups, failureCauses, refusedCauses, requestClasses, requestTopPeriods, requestTopSorts, serviceCauses } from './types';

const now = (): string => new Date().toISOString();
const ID_ALPHABET = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-_';
// Same shape as the control plane's IDs: "m_" and 43 URL-safe characters.
const id = (): string => 'm_' + Array.from(crypto.getRandomValues(new Uint8Array(43)), (byte) => ID_ALPHABET[byte & 63]).join('');
// Seeded rows keep their ID across reloads so detail URLs stay valid.
function seedId(name: string): string {
  let h = 0x811c9dc5;
  for (const char of name) h = Math.imul(h ^ char.charCodeAt(0), 0x01000193);
  return 'm_' + Array.from({ length: 43 }, (_, i) => ID_ALPHABET[Math.floor(hashUnit(h + i * 0x9e3779b1) * 64)]).join('');
}

interface MockInput {
  address?: string;
  password?: string;
  description?: string;
  display_name?: string;
  domain?: string;
  email?: string;
  mode?: string;
  hostname?: string;
  name?: string;
  protocol?: string;
  target?: Partial<Target>;
}

// 목록·페이지네이션을 확인할 수 있도록 터널 30여 개와 도메인 5개를 미리 만들어 둡니다.
// 첫 터널(home-server)은 화면 점검 스크립트가 쓰므로 그대로 둡니다.
const DOMAINS: Array<[string, DomainMode]> = [
  ['example.test', 'wildcard'], ['example.com', 'hosts'], ['example.net', 'wildcard'], ['example.org', 'hosts'], ['demo.test', 'wildcard'],
];
// 표시 이름을 붙여 둔 터널(나머지는 실행 이름을 그대로 보여줍니다).
const SEED_LABELS: Record<string, string> = {
  'nas-livingroom': '거실 NAS',
  'blog-vps': '개인 블로그',
  'staging-api': 'Staging API',
  'office-printer': '사무실 프린터',
};
const SEED_NOTES: Record<string, string> = {
  'nas-livingroom': '거실 NAS · 사진과 파일 백업',
  'blog-vps': '개인 블로그 VPS',
  'staging-api': '스테이징 API 서버. 배포 전 검증용',
  'office-printer': '사무실 프린터 관리 화면',
  'legacy-erp': '구 ERP. 2027년 상반기 종료 예정',
};
const SEED_TUNNELS: Array<[string, string, Array<[string, string, number, string[]]>]> = [
  ['nas-livingroom', 'connected', [['photos', 'immich', 2283, ['photos.example.test']], ['files', 'nextcloud', 80, ['files.example.test', 'drive.example.com']]]],
  ['blog-vps', 'connected', [['blog', 'ghost', 2368, ['blog.example.com']]]],
  ['staging-api', 'connected', [['api', 'api', 8080, ['api.staging.example.net']], ['admin', 'admin', 3000, []]]],
  ['grafana-box', 'connected', [['grafana', 'grafana', 3000, ['grafana.example.net']]]],
  ['homeassistant', 'connected', [['ha', 'homeassistant', 8123, ['ha.example.test']]]],
  ['office-printer', 'disconnected', [['printer', 'cups', 631, []]]],
  ['dev-laptop', 'stale', [['vite', 'host.docker.internal', 5173, ['dev.demo.test']]]],
  ['ci-runner', 'connected', []],
  ['media-server', 'connected', [['jellyfin', 'jellyfin', 8096, ['tv.example.test']]]],
  ['git-server', 'connected', [['gitea', 'gitea', 3000, ['git.example.org']]]],
  ['wiki', 'connected', [['wiki', 'outline', 3000, ['wiki.example.org']]]],
  ['minecraft', 'disconnected', []],
  ['shop-frontend', 'connected', [['web', 'web', 3000, ['shop.example.com', 'www.example.com']]]],
  ['shop-backend', 'connected', [['api', 'api', 8000, ['api.example.com']]]],
  // shop.example.com을 shop-frontend와 함께 쓰는 두 번째 대상(주소 공유 데모).
  ['shop-frontend-b', 'connected', [['web', 'web', 3000, ['shop.example.com']]]],
  ['analytics', 'connected', [['plausible', 'plausible', 8000, ['stats.example.net']]]],
  ['vaultwarden', 'connected', [['vault', 'vaultwarden', 80, ['vault.example.test']]]],
  ['uptime-kuma', 'connected', [['status', 'uptime-kuma', 3001, ['status.demo.test']]]],
  ['backup-node', 'draft', []],
  ['lab-k3s', 'connected', [['argocd', 'argocd-server', 443, ['argo.example.net']], ['dashboard', 'kubernetes-dashboard', 8443, []]]],
  ['raspberry-pi', 'disconnected', [['pihole', 'pihole', 80, ['dns.example.test']]]],
  ['mail-relay', 'connected', [['webmail', 'roundcube', 80, ['mail.example.org']]]],
  ['docs-site', 'connected', [['docs', 'docs', 4000, ['docs.example.com']]]],
  ['test-env-01', 'draft', []],
  ['test-env-02', 'connected', [['app', 'app', 8080, ['app.demo.test']]]],
  ['n8n', 'connected', [['n8n', 'n8n', 5678, ['flows.example.net']]]],
  ['paperless', 'connected', [['paperless', 'paperless', 8000, ['paper.example.test']]]],
  ['game-server', 'stale', [['panel', 'pterodactyl', 80, []]]],
  ['ollama-gpu', 'connected', [['webui', 'open-webui', 8080, ['ai.example.org']]]],
  ['iot-gateway', 'connected', [['mqtt-ui', 'mqtt-explorer', 4000, []]]],
  ['legacy-erp', 'disconnected', [['erp', 'erp', 8069, ['erp.example.com']]]],
  ['sandbox', 'draft', []],
];

// 공개 주소 공유 필드는 읽을 때 routeView가 채웁니다.
function newRoute(fields: Omit<Route, 'priority' | 'address_mode' | 'address_sticky' | 'address_backends'> & { priority?: number }): Route {
  return { priority: 0, address_mode: 'failover', address_sticky: false, address_backends: 1, ...fields };
}

// Connectors that last reported an older release; drafts never connected and have no version.
const OLD_CONNECTORS = ['raspberry-pi', 'legacy-erp', 'minecraft', 'docs-site'];

function seed() {
  const day = 86400000;
  const connectors: Connector[] = [{ id: seedId('connector-home'), name: 'home-server', description: '집 서버 · whoami 테스트 앱', status: 'connected', created_at: now(), last_seen: now(), version: '0.5.1' }];
  const services: Service[] = [{
    id: seedId('service-whoami'), connector_id: seedId('connector-home'), name: 'whoami', protocol: 'http',
    target: { host: 'app', port: 80 }, enabled: true, observed_status: 'connected', created_at: now(), updated_at: now(),
  }];
  const routes: Route[] = [newRoute({
    id: seedId('route-whoami'), service_id: seedId('service-whoami'), kind: 'http', hostname: 'whoami.example.test',
    enabled: true, desired_revision: 1, applied_revision: 1, created_at: now(), updated_at: now(),
  })];
  SEED_TUNNELS.forEach(([name, status, items], index) => {
    const created = new Date(Date.now() - (index + 2) * 3.7 * day).toISOString();
    const seen = status === 'draft' ? undefined : new Date(Date.now() - (status === 'connected' ? (index % 4) * 20000 : (index + 1) * 5 * 3600000)).toISOString();
    const connectorId = seedId(`connector-${name}`);
    connectors.push({ id: connectorId, name, display_name: SEED_LABELS[name] ?? '', description: SEED_NOTES[name] ?? '', status, created_at: created, last_seen: seen, ...(status === 'draft' ? {} : { version: OLD_CONNECTORS.includes(name) ? '0.5.0' : '0.5.1' }) });
    for (const [serviceName, host, port, hostnames] of items) {
      const serviceId = seedId(`service-${name}-${serviceName}`);
      services.push({
        id: serviceId, connector_id: connectorId, name: serviceName, protocol: 'http', target: { host, port }, enabled: true,
        observed_status: status === 'connected' ? 'connected' : 'disconnected', created_at: created, updated_at: created,
      });
      hostnames.forEach((hostname, routeIndex) => routes.push(newRoute({
        id: seedId(`route-${name}-${serviceName}-${routeIndex}`), service_id: serviceId, kind: 'http', hostname, enabled: true,
        desired_revision: 1, applied_revision: status === 'connected' ? 1 : 0, created_at: created, updated_at: created,
        priority: routes.filter((route) => route.hostname === hostname).length,
      })));
    }
  });
  return { connectors, services, routes };
}

const state: {
  outlet: { domain: string; domains: string[]; modes: Record<string, DomainMode> };
  tunnelAddress: string;
  connectors: Connector[];
  services: Service[];
  routes: Route[];
  policies: Record<string, { mode: AddressMode; sticky: boolean }>;
} = {
  outlet: { domain: DOMAINS[0][0], domains: DOMAINS.map(([domain]) => domain), modes: Object.fromEntries(DOMAINS) },
  tunnelAddress: '',
  ...seed(),
  policies: { 'shop.example.com': { mode: 'balance', sticky: true } },
};

function routeView(route: Route): Route {
  const policy = state.policies[route.hostname] ?? { mode: 'failover', sticky: false };
  return { ...route, address_mode: policy.mode, address_sticky: policy.sticky, address_backends: state.routes.filter((other) => other.hostname === route.hostname).length };
}

// 알림·2단계 인증 데모 상태. 2단계 인증을 켜면 로그인 코드는 항상 123456입니다.
const hoursAgo = (hours: number): string => new Date(Date.now() - hours * 3600000).toISOString();
const hourOf = (hours: number): string => `${hoursAgo(hours).slice(0, 13).replace('T', ' ')}:00 UTC`;
const daysAhead = (days: number): string => new Date(Date.now() + days * 86400000).toISOString().slice(0, 10);
const alerts: AlertSettings = {
  telegram: { configured: true, enabled: true, chat_id: '-1001234567890', token_hint: 'x9Qa', verified_at: hoursAgo(50), source: 'database', environment: false },
  rules: {
    tunnel_down: { enabled: true, after_minutes: 5 },
    service_down: { enabled: true, after_minutes: 5 },
    route_error: { enabled: true },
    dns_failure: { enabled: true },
    error_spike: { enabled: true, threshold_percent: 5, min_requests: 50 },
    login: { enabled: true },
    rate_limit: { enabled: true, min_blocked: 500 },
    latency: { enabled: true, increase_percent: 50, min_requests: 100 },
    outlet_restart: { enabled: true },
    disk_low: { enabled: true, min_free_percent: 10 },
    memory_low: { enabled: true, min_free_percent: 10 },
    tunnel_unstable: { enabled: true, min_reconnects: 3 },
  },
  events: [
    { id: 13, kind: 'service_down', subject: 'home-server / app', message: "Service app on home-server down (5 min): The Connector can't reach the app", opened_at: hoursAgo(0.5), resolved_at: null, sent: true },
    { id: 12, kind: 'login', subject: 'dev@example.test', message: 'Admin login: dev@example.test from 203.0.113.7 (Chrome on macOS)', opened_at: hoursAgo(1), resolved_at: hoursAgo(1), sent: true },
    { id: 11, kind: 'memory_low', subject: 'memory', message: 'Memory low: 7% free', opened_at: hoursAgo(1.5), resolved_at: null, sent: true },
    { id: 10, kind: 'tunnel_unstable', subject: 'ci-runner', message: 'Tunnel ci-runner unstable: 4 reconnects in the last hour', opened_at: hoursAgo(2), resolved_at: hoursAgo(2), sent: true },
    { id: 9, kind: 'latency', subject: 'services', message: `Services p95 420 ms (usual 180 ms) in the hour of ${hourOf(3)}`, opened_at: hoursAgo(3), resolved_at: hoursAgo(3), sent: true },
    { id: 8, kind: 'error_spike', subject: 'example.test', message: '5xx rate 7.4% (41 of 552 requests) this hour', opened_at: hoursAgo(5), resolved_at: hoursAgo(4), sent: true },
    { id: 7, kind: 'outlet_restart', subject: 'outlet', message: 'Outlet restarted unexpectedly', opened_at: hoursAgo(8), resolved_at: hoursAgo(8), sent: true },
    { id: 6, kind: 'route_error', subject: 'app.example.test', message: `Public address app.example.test failing: certificate expires ${daysAhead(7)}`, opened_at: hoursAgo(12), resolved_at: null, sent: true },
    { id: 5, kind: 'disk_low', subject: 'disk', message: 'Disk space low: 6% free', opened_at: hoursAgo(14), resolved_at: hoursAgo(12), sent: true },
    { id: 4, kind: 'dns_failure', subject: 'dev.demo.test', message: 'dev.demo.test does not resolve', opened_at: hoursAgo(20), resolved_at: hoursAgo(19), sent: true },
    { id: 3, kind: 'tunnel_down', subject: 'office-printer', message: 'Tunnel office-printer disconnected (5 min)', opened_at: hoursAgo(26), resolved_at: null, sent: true },
    { id: 2, kind: 'tunnel_down', subject: 'legacy-erp', message: 'Tunnel legacy-erp disconnected (5 min)', opened_at: hoursAgo(30), resolved_at: null, sent: true },
    { id: 1, kind: 'route_error', subject: 'erp.example.com', message: 'Certificate for erp.example.com could not be issued', opened_at: hoursAgo(40), resolved_at: null, sent: false, error: 'telegram_unreachable' },
  ],
  login_second_factor: false,
};
let loginChallenge = '';
let rateLimit: RateLimitSettings = {
  enabled: true,
  per_ip: { rps: 300, burst: 1000 },
  per_subnet: { rps: 1500, burst: 5000 },
  per_address: { rps: 0, burst: 0 },
  endpoints: [{ host: 'k7m2x9qa.example.test', path_prefix: '/api/v1/session', rps: 3, burst: 30, per: 'ip' }],
};
// SSO 데모 상태. 리디렉션 URI는 대표 도메인이 있을 때만 만들어집니다(redirectUri 참고).
let sso: SsoSettings = {
  enabled: true,
  label: 'Google Workspace',
  issuer: 'https://accounts.google.com',
  client_id: '123456789-abc.apps.googleusercontent.com',
  client_secret_set: true,
  client_secret_hint: 'x7Qa',
  scopes: ['openid', 'email', 'profile'],
  groups_claim: 'groups',
  allowed_users: ['@example.test'],
  allowed_groups: [],
  password_login: true,
  redirect_uri: 'https://k7m2x9qa.example.test/api/v1/session/sso/callback',
  post_logout_redirect_uri: 'https://k7m2x9qa.example.test/',
  available: true,
  source: 'environment',
  environment: true,
};
// 데모의 SSO는 환경 변수에서 온 것으로 둡니다. 실제 Outlet처럼 저장·초기화를 거부합니다.
let connectionLimits: ConnectionLimits = {
  max_upload_mb: 100,
  per_ip_concurrent: 512,
  total_concurrent: 4096,
  idle_timeout_seconds: 30,
  min_upload_kb_per_second: 1,
  hosts: [{ host: 'photos.example.test', max_upload_mb: 2048 }],
};
let blockRules: BlockRule[] = [
  {
    id: 'honeypot',
    name: 'Honeypot',
    enabled: true,
    ban: true,
    hosts: [],
    conditions: [
      ...['/.env', '/.git/', '/.aws/', '/.ssh/', '/.svn/', '/.hg/', '/.docker/', '/.kube/', '/.htpasswd', '/.ds_store'].map((value): BlockCondition => ({ operator: 'starts_with', value })),
      { operator: 'matches', value: '/\\.env(\\.[a-z0-9_-]+)?$' },
      { operator: 'matches', value: '/\\.git/(config|head)$' },
      { operator: 'contains', value: '/wp-config.php' },
    ],
  },
  { id: 'hidden-files', name: 'Hidden files', enabled: true, ban: false, hosts: [], conditions: [{ operator: 'starts_with', value: '/.' }] },
  {
    id: 'scanners',
    name: 'Vulnerability scanners',
    enabled: false,
    ban: false,
    hosts: [],
    conditions: [
      { operator: 'matches', value: '\\.(php|phtml|asp|aspx|jsp|cgi|sql|bak|key|log|ya?ml|zip|env)$' },
      { operator: 'starts_with', value: '/cgi-bin/' },
      { operator: 'contains', value: '/wp-' },
      { operator: 'contains', value: '/.env' },
      { operator: 'matches', value: '^/(env|pma|phpmyadmin|cpanel|kpanel|plesk-stat|plesk/login|setup|config\\.json|config\\.js|settings\\.js|secrets\\.json|js/env\\.js|js/config\\.js|api/env|api/config|actuator/heapdump|_ignition/health-check)$' },
    ],
  },
  {
    id: 'unused-paths',
    name: 'Paths this site does not use',
    enabled: false,
    ban: false,
    hosts: [],
    conditions: [
      { operator: 'starts_with', value: '/assets/' },
      { operator: 'starts_with', value: '/media/' },
      { operator: 'starts_with', value: '/static/' },
      { operator: 'starts_with', value: '/uploads/' },
      { operator: 'matches', value: '^/(admin|administrator|components|feed|files|login|register|backup\\.tar\\.gz|config\\.xml|config/production\\.json)$' },
    ],
  },
];
let blockHits: Record<string, number> = { honeypot: 212, 'hidden-files': 1284 };
// 규칙·정책을 한정할 수 있는 공개 주소: 지금 있는 경로의 호스트 이름입니다.
const publicAddresses = (): string[] => [...new Set(state.routes.map((route) => route.hostname))].sort();
const blockRulesResponse = (): BlockRulesView => structuredClone({
  rules: blockRules,
  hits: blockHits,
  addresses: publicAddresses(),
});
// 접근 보호 데모 상태. 공급자는 꺼져 있고, 정책 하나가 vault 주소 전체를 보호합니다.
let accessProvider: Omit<AccessProviderView, 'redirect_uri' | 'available'> = {
  enabled: false,
  issuer: '',
  client_id: '',
  client_secret_set: false,
  client_secret_hint: '',
  scopes: ['openid', 'email', 'profile'],
  groups_claim: 'groups',
  session_hours: 24,
};
let accessPolicies: AccessPolicy[] = [
  { id: 'vault-api', name: 'Vault API', enabled: true, action: 'bypass', hosts: ['vault.example.test'], all_paths: false, conditions: [{ operator: 'starts_with', value: '/v1/' }], methods: ['GET', 'POST'], require_token: true, tokens: ['deploybot'], allow_everyone: false, allowed_users: [], allowed_groups: [], allow_admins: false },
  { id: 'vault', name: 'Vault members', enabled: true, action: 'allow', hosts: ['vault.example.test'], all_paths: true, conditions: [], methods: [], require_token: false, tokens: [], allow_everyone: false, allowed_users: ['@example.test'], allowed_groups: [], allow_admins: true },
];
const accessPoliciesResponse = (): AccessPoliciesView => structuredClone({ policies: accessPolicies, addresses: publicAddresses() });
// 접근 토큰 데모 상태. 값은 만들 때만 돌려줍니다.
let accessTokens: Array<Omit<AccessToken, 'policies'>> = [
  { id: 'deploybot', name: 'Deploy bot', hosts: ['vault.example.test'], ranges: ['203.0.113.0/24'], created_at: '2026-10-20T03:00:00Z', expires_at: '', last_used_at: '2026-10-27T01:12:00Z' },
  { id: 'monitor', name: 'Uptime monitor', hosts: ['*.example.test'], ranges: [], created_at: '2026-10-22T09:30:00Z', expires_at: '2027-01-20T09:30:00Z', last_used_at: '' },
];
// 서버처럼 토큰을 지정했거나, 모든 토큰을 받으면서 주소가 겹치는 정책을 보여 줍니다.
const hostsMeet = (a: string, b: string): boolean => a === b || (a.startsWith('*.') && b.endsWith(a.slice(1))) || (b.startsWith('*.') && a.endsWith(b.slice(1)));
const tokenView = (token: Omit<AccessToken, 'policies'>): AccessToken => structuredClone({
  ...token,
  policies: accessPolicies
    .filter((policy) => policy.tokens.includes(token.id) || (policy.require_token && !policy.tokens.length && policy.hosts.some((host) => token.hosts.some((other) => hostsMeet(host, other)))))
    .map((policy) => ({ id: policy.id, name: policy.name })),
});
// 서버처럼 IP 대역을 CIDR로 정리합니다: 단일 주소는 /32·/128, IPv4는 네트워크 주소로 맞추고, IPv6는 URL 파서로 확인해 표준 표기로 둡니다.
function normalizeRange(value: string): string | null {
  const [address, bitsText, extra] = value.split('/');
  if (extra !== undefined || bitsText !== undefined && !/^\d{1,3}$/.test(bitsText)) return null;
  const v4 = address.match(/^(\d{1,3})\.(\d{1,3})\.(\d{1,3})\.(\d{1,3})$/);
  if (v4) {
    const octets = v4.slice(1).map(Number);
    const bits = bitsText === undefined ? 32 : Number(bitsText);
    if (octets.some((octet) => octet > 255) || bits > 32) return null;
    const number = octets.reduce((sum, octet) => sum * 256 + octet, 0);
    const masked = bits === 0 ? 0 : Math.floor(number / 2 ** (32 - bits)) * 2 ** (32 - bits);
    return `${[24, 16, 8, 0].map((shift) => Math.floor(masked / 2 ** shift) % 256).join('.')}/${bits}`;
  }
  const bits = bitsText === undefined ? 128 : Number(bitsText);
  if (!address.includes(':') || bits > 128) return null;
  try {
    return `${new URL(`http://[${address}]/`).hostname.slice(1, -1)}/${bits}`;
  } catch {
    return null;
  }
}

// 클라이언트 차단의 제외 대상과 터널 포트 보호의 허용 범위: IP 하나 또는 CIDR을 표준 표기로 정리합니다(단일 주소는 /32·/128 없이).
function normalizeAddress(value: unknown): string | null {
  if (typeof value !== 'string') return null;
  const text = value.trim().toLowerCase();
  const range = normalizeRange(text);
  return range === null ? null : text.includes('/') ? range : range.replace(/\/(32|128)$/, '');
}
// 최대 50개, 중복은 한 번만. 하나라도 올바르지 않으면 null입니다.
function normalizeAddresses(value: unknown): string[] | null {
  if (!Array.isArray(value) || value.length > 50) return null;
  const list = value.map(normalizeAddress);
  return list.every((entry): entry is string => entry !== null) ? [...new Set(list)] : null;
}

// 서버의 AccessTokenScope.Normalize: 이름, 주소(소문자, 1–100개), IP 대역(CIDR 또는 단일 주소, 0–50개)을 확인합니다.
function tokenScope(body: Partial<AccessTokenInput>): Pick<AccessToken, 'name' | 'hosts' | 'ranges'> {
  const name = typeof body.name === 'string' ? body.name.trim() : '';
  if (!name || name.length > 64 || /[\u0000-\u001f\u007f]/.test(name) || !Array.isArray(body.hosts) || body.hosts.length < 1 || body.hosts.length > 100 || !Array.isArray(body.ranges) || body.ranges.length > 50) fail('invalid_access_token');
  const hosts = [...new Set(body.hosts.map((host) => (typeof host === 'string' ? host.trim().toLowerCase() : '')))];
  if (hosts.some((host) => !validBlockHost(host))) fail('invalid_access_token');
  const ranges = [...new Set(body.ranges.map((range) => normalizeRange(typeof range === 'string' ? range.trim() : '') ?? fail('invalid_access_token')))];
  return { name, hosts, ranges };
}

// 감사 로그 데모: 최근 며칠 동안의 관리 작업을 만들어 둡니다.
const AUDIT_SAMPLES: Array<[string, string, string, string]> = [
  ['session.login', 'session', 'dev@example.test', '{"method":"password"}'],
  ['connector.create', 'connector', 'nas-livingroom', '{"name":"nas-livingroom"}'],
  ['service.create', 'service', 'photos', '{"connector":"nas-livingroom","target":"immich:2283"}'],
  ['route.create', 'route', 'photos.example.test', '{"service":"photos"}'],
  ['domain.add', 'domain', 'example.com', '{"mode":"hosts"}'],
  ['domain.mode', 'domain', 'example.org', '{"from":"wildcard","to":"hosts"}'],
  ['tunnel.update', 'outlet', 'tunnel', '{"address":"default"}'],
  ['connector.token_reset', 'connector', 'office-printer', '{}'],
  ['session.login_failed', 'session', 'admin@example.test', '{"reason":"invalid_credential"}'],
  ['telegram.update', 'alerts', 'telegram', '{"enabled":true}'],
  ['alert_rules.update', 'alerts', 'rules', '{"tunnel_down":{"after_minutes":5}}'],
  ['connector.describe', 'connector', 'legacy-erp', '{}'],
  ['route.delete', 'route', 'old.example.net', '{"service":"api"}'],
  ['session.logout', 'session', 'dev@example.test', '{}'],
];
const auditEvents: AuditEvent[] = Array.from({ length: 128 }, (_, index) => {
  const [action, resource_type, resource_id, detail] = AUDIT_SAMPLES[index % AUDIT_SAMPLES.length];
  const local = index % 9 === 4;
  return {
    id: `audit-${128 - index}`,
    created_at: new Date(Date.now() - index * 47 * 60000).toISOString(),
    actor_email: action === 'session.login_failed' ? 'admin@example.test' : 'dev@example.test',
    action, resource_type, resource_id, detail: JSON.parse(detail) as Record<string, unknown>,
    source: local ? 'local' : 'public',
    ip: local ? null : `203.0.113.${(index * 7) % 250}`,
    user_agent: local ? null : 'Chrome on macOS',
  };
});
const alertsResponse = (): AlertSettings => structuredClone(alerts);
// 도메인을 모두 지우면 관리자 호스트가 없어 리디렉션 URI도 만들 수 없습니다.
const ssoResponse = (): SsoSettings => {
  const domain = state.outlet.domain;
  return structuredClone({ ...sso, redirect_uri: domain ? `https://${adminHostname(domain)}/api/v1/session/sso/callback` : '', post_logout_redirect_uri: domain ? `https://${adminHostname(domain)}/` : '', available: Boolean(domain) });
};

// 도메인을 모두 지우면 관리자 호스트가 없어 콜백 주소도 만들 수 없습니다.
const accessProviderResponse = (): AccessProviderView => {
  const domain = state.outlet.domain;
  return structuredClone({ ...accessProvider, redirect_uri: domain ? `https://${adminHostname(domain)}/api/v1/access/callback` : '', available: Boolean(domain) });
};

function fail(code: string): never {
  throw new Error(code);
}

// 서버처럼 https이고 query·fragment가 없는 주소만 Issuer로 받습니다. 끝의 /는 지웁니다.
function issuerOf(value: unknown): string | null {
  if (typeof value !== 'string') return null;
  try {
    const url = new URL(value.trim());
    if (url.protocol !== 'https:' || url.search || url.hash || url.username || url.password) return null;
    return `${url.origin}${url.pathname}`.replace(/\/+$/, '');
  } catch {
    return null;
  }
}

// 차단 규칙과 접근 정책이 함께 쓰는 검사: 호스트 이름(`*.도메인` 포함)과 경로 조건.
const validHostname = (host: string): boolean => host.length <= 253 && host.split('.').every((label) => /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(label));
const validBlockHost = (host: string): boolean => validHostname(host.startsWith('*.') ? host.slice(2) : host);
const BLOCK_OPERATORS: readonly string[] = ['equals', 'starts_with', 'ends_with', 'contains', 'matches'];
function validRegex(value: string): boolean {
  if (/\(\?(?:=|!|<=|<!)/.test(value) || /\\[1-9]/.test(value)) return false;
  try {
    new RegExp(value, 'i');
    return true;
  } catch {
    return false;
  }
}
// 조각 끝의 점과 공백을 지웁니다. 점과 공백뿐인 조각은 그대로 둡니다.
const trimSegment = (segment: string): string => segment.replace(/[. ]+$/, '') || segment;

// 서버의 NormalizeBlockPath: 소문자로 바꾸고, \\를 /로 읽고, NUL 뒤와 조각마다 ; 뒤를 버린 다음 ., .., 빈 조각을 정리하고 조각 끝의 점과 공백을 지웁니다.
function normalizeBlockPath(value: string): string {
  const out: string[] = [];
  for (const raw of value.toLowerCase().replaceAll('\\', '/').split('\0')[0].split('/')) {
    const segment = trimSegment(raw.split(';')[0]);
    if (segment === '..') out.pop();
    else if (segment && segment !== '.') out.push(segment);
  }
  return `/${out.join('/')}`;
}

// 경로 일부를 normalizeBlockPath의 모양으로: 조각이 끝나는 곳이 확실한 / 앞과, whole이면 값의 끝에서 조각을 다듬습니다.
function conditionFragment(value: string, whole: boolean): string {
  const parts = value.toLowerCase().replaceAll('\\', '/').split('/');
  return parts.map((part, index) => (index < parts.length - 1 || whole ? trimSegment(part) : part)).join('/');
}

// 서버처럼 값을 정리해 돌려주고, 올바르지 않으면 null입니다.
function normalizeCondition(raw: unknown): BlockCondition | null {
  const entry = (raw ?? {}) as Partial<BlockCondition>;
  if (typeof entry.operator !== 'string' || !BLOCK_OPERATORS.includes(entry.operator) || typeof entry.value !== 'string') return null;
  const operator = entry.operator as BlockOperator;
  let value = entry.value.trim();
  if (!value || new TextEncoder().encode(value).length > 512 || /[\u0000-\u001f\u007f]/.test(value)) return null;
  if ((operator === 'equals' || operator === 'starts_with') && !value.startsWith('/')) return null;
  if (operator === 'matches' && !validRegex(value)) return null;
  if (operator === 'equals') value = normalizeBlockPath(value);
  else if (operator !== 'matches') value = conditionFragment(value, operator === 'ends_with');
  return { operator, value };
}

// 서버의 bypassCondition: 우회 조건은 대소문자를 그대로 두고, 앱이 한 가지로만 읽는 경로여야 합니다.
function bypassCondition(raw: unknown): BlockCondition | null {
  const entry = (raw ?? {}) as Partial<BlockCondition>;
  if (typeof entry.operator !== 'string' || !BLOCK_OPERATORS.includes(entry.operator) || typeof entry.value !== 'string') return null;
  const operator = entry.operator as BlockOperator;
  const value = entry.value.trim();
  if (!value || new TextEncoder().encode(value).length > 512 || /[\u0000-\u001f\u007f]/.test(value)) return null;
  if (operator === 'matches') return validRegex(value) ? { operator, value } : null;
  if ((operator === 'equals' || operator === 'starts_with') && !value.startsWith('/')) return null;
  if (value.includes('\\')) return null;
  const parts = value.split('/');
  const whole = operator === 'equals' || operator === 'ends_with';
  const plain = parts.every((part, index) => (index === 0 || index === parts.length - 1) && part === '' || part !== '' && part !== '.' && part !== '..' && (!(index < parts.length - 1 || whole) || !/[. ]$/.test(part)));
  return plain ? { operator, value } : null;
}

// 실제 Outlet은 대표 도메인을 처음 등록할 때 8글자 이름을 만듭니다.
const ADMIN_LABEL = 'k7m2x9qa';
const adminHostname = (domain: string): string => `${ADMIN_LABEL}.${domain}`;

function outletResponse(): Outlet {
  const defaultAddress = state.outlet.domain ? `${adminHostname(state.outlet.domain)}:2333` : '';
  return {
    ...state.outlet,
    domains: [...state.outlet.domains],
    modes: { ...state.outlet.modes },
    admin_hostname: state.outlet.domain ? adminHostname(state.outlet.domain) : undefined,
    // 미리보기는 대표 도메인을 환경 변수(MEERKAT_DOMAIN)로 정한 배포를 흉내 냅니다.
    domain_source: state.outlet.domain ? 'environment' : undefined,
    version: '0.1.0',
    tunnel: { address: state.tunnelAddress || defaultAddress, default_address: defaultAddress, custom: Boolean(state.tunnelAddress) },
  };
}

function modeOf(raw: string | undefined, fallback: DomainMode = 'wildcard'): DomainMode {
  if (raw === undefined || raw === '') return fallback;
  if (raw !== 'wildcard' && raw !== 'hosts') fail('invalid_domain_mode');
  return raw;
}

// 실제 Outlet과 같은 규칙으로 DNS 확인 대상을 고릅니다. 와일드카드는 임의의 하위 주소 하나,
// 개별 호스트는 이 도메인의 공개 주소마다 확인합니다. 관리 주소와, 이 도메인에 속한 터널 주소도 함께 봅니다.
function dnsHostnames(domain: string): string[] {
  const hosts = new Set<string>();
  if (domain === state.outlet.domain) hosts.add(adminHostname(domain));
  if (state.outlet.modes[domain] === 'hosts') {
    for (const route of state.routes) if (route.hostname.endsWith(`.${domain}`)) hosts.add(route.hostname);
  } else {
    hosts.add(`check-example.${domain}`);
  }
  const tunnelHost = outletResponse().tunnel?.address.replace(/:\d+$/, '') ?? '';
  if (tunnelHost === domain || tunnelHost.endsWith(`.${domain}`)) hosts.add(tunnelHost);
  return [...hosts];
}

function tunnelAddressOf(raw = ''): string {
  const value = raw.trim().toLowerCase();
  if (!value) return '';
  const address = /:\d+$/.test(value) ? value : `${value}:2333`;
  if (!/^[a-z0-9.-]+:\d{1,5}$/.test(address) || /^(127\.|localhost:)/.test(address)) fail('invalid_tunnel_address');
  return address;
}

function bodyOf(body: BodyInit | null | undefined): MockInput {
  if (typeof body !== 'string') return {};
  try { return JSON.parse(body) as MockInput; } catch { return {}; }
}

const HOUR_MS = 3600000;
const USAGE_UNITS: readonly UsageUnit[] = ['hour', 'day', 'month', 'year'];
const MINUTE_MS = 60000;
const MAX_USAGE_BUCKETS = 1000;
const RFC3339 = /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})$/;
// Synthetic traffic grows slowly from this hour so older years look smaller.
const TRAFFIC_EPOCH_HOUR = Math.floor(Date.UTC(2023, 0, 1) / HOUR_MS);

function hashUnit(seed: number): number {
  let x = Math.imul(seed | 0, 0x85ebca6b) ^ 0x9e3779b9;
  x = Math.imul(x ^ (x >>> 15), 0xc2b2ae35);
  x = Math.imul(x ^ (x >>> 13), 0x85ebca6b);
  return ((x ^ (x >>> 16)) >>> 0) / 4294967296;
}

// Deterministic Outlet-wide totals for one UTC hour index (hours since the epoch).
function syntheticHour(hour: number, localOffsetHours: number): Omit<UsageBucket, 'start'> {
  const localHour = hour + localOffsetHours;
  const hourOfDay = ((localHour % 24) + 24) % 24;
  const weekday = (((Math.floor(localHour / 24) + 4) % 7) + 7) % 7;
  const daily = 0.15 + 0.85 * (0.5 + 0.5 * Math.sin(((hourOfDay - 9) / 24) * 2 * Math.PI));
  const weekly = weekday === 0 || weekday === 6 ? 0.55 : 1 + 0.06 * (weekday - 3);
  const trend = 1 + Math.max(0, hour - TRAFFIC_EPOCH_HOUR) / (24 * 365 * 3);
  const requests = Math.round(180 * daily * weekly * trend * (0.8 + 0.4 * hashUnit(hour)));
  const errors = hashUnit(hour * 3 + 5) < 0.05 ? Math.max(1, Math.round(requests * (0.01 + 0.04 * hashUnit(hour + 99)))) : 0;
  return {
    requests,
    request_bytes: requests * (600 + Math.floor(hashUnit(hour + 11) * 200)),
    response_bytes: requests * (14000 + Math.floor(hashUnit(hour + 23) * 8000)),
    error_responses: errors,
  };
}

function rfc3339Local(date: Date): string {
  const pad = (value: number, size = 2): string => String(Math.abs(value)).padStart(size, '0');
  const offset = -date.getTimezoneOffset();
  const zone = offset === 0 ? 'Z' : `${offset < 0 ? '-' : '+'}${pad(Math.trunc(offset / 60))}:${pad(offset % 60)}`;
  return `${pad(date.getFullYear(), 4)}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}${zone}`;
}

// Bucket boundaries in the browser's local time: the unit-aligned starts that
// cover [from, to) plus the end of the last bucket, like the real API.
function usageBoundaries(unit: UsageUnit, from: Date, to: Date): Date[] {
  const first = new Date(from);
  if (unit === 'hour') first.setMinutes(0, 0, 0);
  if (unit === 'minute') first.setSeconds(0, 0);
  const boundary = (index: number): Date => {
    if (unit === 'minute') return new Date(first.getTime() + index * MINUTE_MS);
    if (unit === 'hour') return new Date(first.getTime() + index * HOUR_MS);
    if (unit === 'day') return new Date(from.getFullYear(), from.getMonth(), from.getDate() + index);
    if (unit === 'month') return new Date(from.getFullYear(), from.getMonth() + index, 1);
    return new Date(from.getFullYear() + index, 0, 1);
  };
  const boundaries: Date[] = [];
  do {
    if (boundaries.length > MAX_USAGE_BUCKETS) fail('invalid_usage_range');
    boundaries.push(boundary(boundaries.length));
  } while (boundaries[boundaries.length - 1].getTime() < to.getTime());
  return boundaries;
}

// Fixed per-tunnel weights (not random), so the same range always splits the same way.
const USAGE_SHARES: Array<{ name: string; requests: number; bytes: number; errors: number }> = [
  { name: 'nas-livingroom', requests: 34, bytes: 41, errors: 8 },
  { name: 'blog-vps', requests: 22, bytes: 12, errors: 6 },
  { name: 'staging-api', requests: 16, bytes: 9, errors: 44 },
  { name: 'grafana-box', requests: 12, bytes: 14, errors: 4 },
  { name: 'homeassistant', requests: 9, bytes: 6, errors: 10 },
  { name: 'shop-frontend', requests: 7, bytes: 18, errors: 28 },
];

// Splits the range totals across the seeded tunnels that still exist. The rounding remainder goes to the first tunnel,
// so every metric sums back to the series totals. A range without requests has no rows, like the real API.
function usageConnectors(totals: Omit<UsageBucket, 'start'>): UsageSeries['connectors'] {
  const shares = USAGE_SHARES.flatMap((share) => {
    const connector = state.connectors.find((item) => item.name === share.name);
    return connector ? [{ share, connector }] : [];
  });
  if (totals.requests <= 0 || !shares.length) return [];
  const split = (total: number, weight: (share: (typeof USAGE_SHARES)[number]) => number): number[] => {
    const sum = shares.reduce((acc, item) => acc + weight(item.share), 0);
    const parts = shares.map((item) => Math.floor((total * weight(item.share)) / sum));
    parts[0] += total - parts.reduce((acc, part) => acc + part, 0);
    return parts;
  };
  const requests = split(totals.requests, (share) => share.requests);
  const requestBytes = split(totals.request_bytes, (share) => share.requests);
  const responseBytes = split(totals.response_bytes, (share) => share.bytes);
  const errors = split(totals.error_responses, (share) => share.errors);
  return shares
    .map(({ connector }, index) => ({
      connector_id: connector.id,
      name: connector.name,
      requests: requests[index],
      request_bytes: requestBytes[index],
      response_bytes: responseBytes[index],
      error_responses: errors[index],
    }))
    .sort((a, b) => b.requests - a.requests);
}

// Validates unit/from/to/tz like the real API and returns the unit-aligned bucket boundaries.
// minutes는 Outlet 사용량(/api/v1/usage)만 받습니다. 접근 판정·지연 시간은 시간 단위로만 보관합니다.
function usageRange(params: URLSearchParams, minutes = false): { unit: UsageUnit; boundaries: Date[] } {
  const unit = params.get('unit') as UsageUnit;
  const rawFrom = params.get('from') || '';
  const rawTo = params.get('to') || '';
  if (!(USAGE_UNITS.includes(unit) || (minutes && unit === 'minute')) || !RFC3339.test(rawFrom) || !RFC3339.test(rawTo)) fail('invalid_usage_range');
  const from = new Date(rawFrom);
  const to = new Date(rawTo);
  if (!(from.getTime() < to.getTime())) fail('invalid_usage_range');
  const zone = params.get('tz');
  if (zone) {
    // The mock buckets in the browser's zone but still rejects unknown names.
    try { new Intl.DateTimeFormat('en', { timeZone: zone }); } catch { fail('invalid_usage_range'); }
  }
  return { unit, boundaries: usageBoundaries(unit, from, to) };
}

// 분별은 그 시간의 합계를 분마다 고르지 않게 나눈 값이고, 지금 이후는 0입니다.
function minuteSeries(boundaries: Date[]): UsageBucket[] {
  const offsetHours = Math.round(-new Date().getTimezoneOffset() / 60);
  return boundaries.slice(0, -1).map((start): UsageBucket => {
    const bucket: UsageBucket = { start: rfc3339Local(start), requests: 0, request_bytes: 0, response_bytes: 0, error_responses: 0 };
    if (start.getTime() > Date.now()) return bucket;
    const minute = Math.floor(start.getTime() / MINUTE_MS);
    const hour = syntheticHour(Math.floor(start.getTime() / HOUR_MS), offsetHours);
    const share = (0.4 + 1.2 * hashUnit(minute)) / 60;
    bucket.requests = Math.round(hour.requests * share);
    bucket.request_bytes = Math.round(hour.request_bytes * share);
    bucket.response_bytes = Math.round(hour.response_bytes * share);
    bucket.error_responses = hashUnit(minute * 7 + 3) < 0.04 ? 1 : 0;
    return bucket;
  });
}

function usageSeries(params: URLSearchParams): UsageSeries {
  const { unit, boundaries } = usageRange(params, true);
  if (unit === 'minute') {
    const buckets = minuteSeries(boundaries);
    const totals = buckets.reduce((sum, bucket) => ({ requests: sum.requests + bucket.requests, request_bytes: sum.request_bytes + bucket.request_bytes, response_bytes: sum.response_bytes + bucket.response_bytes, error_responses: sum.error_responses + bucket.error_responses }), { requests: 0, request_bytes: 0, response_bytes: 0, error_responses: 0 });
    return { unit, updated_at: now(), buckets, connectors: usageConnectors(totals) };
  }
  // Data exists for the last three years only, up to and including this hour.
  const earliest = new Date();
  earliest.setFullYear(earliest.getFullYear() - 3);
  const firstHour = Math.ceil(earliest.getTime() / HOUR_MS);
  const lastHour = Math.floor(Date.now() / HOUR_MS);
  const offsetHours = Math.round(-new Date().getTimezoneOffset() / 60);
  const buckets = boundaries.slice(0, -1).map((start, index): UsageBucket => {
    const bucket: UsageBucket = { start: rfc3339Local(start), requests: 0, request_bytes: 0, response_bytes: 0, error_responses: 0 };
    // An hourly row belongs to the bucket that contains the instant it starts.
    const last = Math.min(lastHour, Math.ceil(boundaries[index + 1].getTime() / HOUR_MS) - 1);
    for (let hour = Math.max(firstHour, Math.ceil(start.getTime() / HOUR_MS)); hour <= last; hour += 1) {
      const totals = syntheticHour(hour, offsetHours);
      bucket.requests += totals.requests;
      bucket.request_bytes += totals.request_bytes;
      bucket.response_bytes += totals.response_bytes;
      bucket.error_responses += totals.error_responses;
    }
    return bucket;
  });
  const totals = buckets.reduce(
    (sum, bucket) => ({
      requests: sum.requests + bucket.requests,
      request_bytes: sum.request_bytes + bucket.request_bytes,
      response_bytes: sum.response_bytes + bucket.response_bytes,
      error_responses: sum.error_responses + bucket.error_responses,
    }),
    { requests: 0, request_bytes: 0, response_bytes: 0, error_responses: 0 },
  );
  return { unit, updated_at: now(), buckets, connectors: usageConnectors(totals) };
}

// ---- Observe: access decision metrics and the three request logs (GET/PUT /api/v1/observe/…) ----

const ACCESS_HISTORY_DAYS = 400;
// A policy that was deleted still shows up in the metrics with the counts it had.
const REMOVED_POLICY = 'p_removed';

// Deterministic decision counts per policy for one UTC hour index, like syntheticHour: busy by day, quieter at weekends.
function accessHour(hour: number, localOffsetHours: number): Array<[string, AccessCounts]> {
  const localHour = hour + localOffsetHours;
  const hourOfDay = ((localHour % 24) + 24) % 24;
  const weekday = (((Math.floor(localHour / 24) + 4) % 7) + 7) % 7;
  const daily = 0.12 + 0.88 * (0.5 + 0.5 * Math.sin(((hourOfDay - 9) / 24) * 2 * Math.PI));
  const weekly = weekday === 0 || weekday === 6 ? 0.5 : 1;
  const rows: Array<[string, AccessCounts]> = [];
  accessPolicies.forEach((policy, order) => {
    if (!policy.enabled) return;
    const base = Math.round((daily * weekly * (policy.action === 'bypass' ? 180 : 60) * (0.75 + 0.5 * hashUnit(hour * 5 + order))) / (order + 1));
    rows.push([policy.id, policy.action === 'block'
      ? { passed: 0, challenged: 0, denied: base }
      : policy.action === 'bypass' ? { passed: base, challenged: 0, denied: 0 } : { passed: base, challenged: Math.round(base / 9), denied: Math.round(base / 40) }]);
  });
  if (hashUnit(hour * 11 + 3) < 0.4) rows.push([REMOVED_POLICY, { passed: 0, challenged: 0, denied: Math.max(1, Math.round(4 * daily)) }]);
  return rows;
}

function accessMetrics(params: URLSearchParams): AccessMetrics {
  const { unit, boundaries } = usageRange(params);
  const firstHour = Math.ceil((Date.now() - ACCESS_HISTORY_DAYS * 24 * HOUR_MS) / HOUR_MS);
  const lastHour = Math.floor(Date.now() / HOUR_MS);
  const offsetHours = Math.round(-new Date().getTimezoneOffset() / 60);
  const policies: Record<string, AccessCounts> = {};
  const buckets = boundaries.slice(0, -1).map((start, index) => {
    const bucket = { start: rfc3339Local(start), passed: 0, challenged: 0, denied: 0 };
    const last = Math.min(lastHour, Math.ceil(boundaries[index + 1].getTime() / HOUR_MS) - 1);
    for (let hour = Math.max(firstHour, Math.ceil(start.getTime() / HOUR_MS)); hour <= last; hour += 1) {
      for (const [policy, counts] of accessHour(hour, offsetHours)) {
        bucket.passed += counts.passed;
        bucket.challenged += counts.challenged;
        bucket.denied += counts.denied;
        const total = (policies[policy] ??= { passed: 0, challenged: 0, denied: 0 });
        total.passed += counts.passed;
        total.challenged += counts.challenged;
        total.denied += counts.denied;
      }
    }
    return bucket;
  });
  return { unit, buckets, policies };
}

const NO_LATENCY: LatencyStats = { count: 0, avg_ms: 0, p50_ms: 0, p95_ms: 0, p99_ms: 0 };

// Deterministic latency of one UTC hour, busier by day like accessHour. Outlet processing stays around half a millisecond, upstream answers take
// tens to a few hundred milliseconds and slow down under load, with the odd slow hour. Quiet night hours may have no timed request at all.
function latencyHour(hour: number, localOffsetHours: number): { outlet: LatencyStats; upstream: LatencyStats } {
  const localHour = hour + localOffsetHours;
  const hourOfDay = ((localHour % 24) + 24) % 24;
  const weekday = (((Math.floor(localHour / 24) + 4) % 7) + 7) % 7;
  const daily = 0.12 + 0.88 * (0.5 + 0.5 * Math.sin(((hourOfDay - 9) / 24) * 2 * Math.PI));
  const load = daily * (weekday === 0 || weekday === 6 ? 0.5 : 1);
  if (load < 0.4 && hashUnit(hour * 7 + 1) < 0.4) return { outlet: NO_LATENCY, upstream: NO_LATENCY };
  const count = Math.max(1, Math.round(load * 900 * (0.75 + 0.5 * hashUnit(hour * 7 + 2))));
  const stats = (avg: number, tail: number): LatencyStats => ({ count, avg_ms: avg, p50_ms: avg * 0.7, p95_ms: avg * tail, p99_ms: avg * tail * 1.7 });
  const slow = hashUnit(hour * 7 + 5) < 0.04 ? 2.5 : 1;
  return {
    outlet: stats(0.3 + 0.5 * (0.6 * load + 0.4 * hashUnit(hour * 7 + 3)), 3.3 + 0.45 * hashUnit(hour * 7 + 6)),
    upstream: stats((80 + 170 * (0.5 * load + 0.5 * hashUnit(hour * 7 + 4))) * slow, 3.4 + 0.4 * hashUnit(hour * 7 + 8)),
  };
}

// Several hours folded into one: counts add up and the other figures are averaged by count (the real server estimates them from merged histograms).
function mergeLatency(parts: LatencyStats[]): LatencyStats {
  const count = parts.reduce((sum, part) => sum + part.count, 0);
  if (!count) return NO_LATENCY;
  const weighted = (pick: (part: LatencyStats) => number): number => parts.reduce((sum, part) => sum + pick(part) * part.count, 0) / count;
  return { count, avg_ms: weighted((part) => part.avg_ms), p50_ms: weighted((part) => part.p50_ms), p95_ms: weighted((part) => part.p95_ms), p99_ms: weighted((part) => part.p99_ms) };
}

function latencySeries(params: URLSearchParams): LatencySeries {
  const { unit, boundaries } = usageRange(params);
  const firstHour = Math.ceil((Date.now() - ACCESS_HISTORY_DAYS * 24 * HOUR_MS) / HOUR_MS);
  const lastHour = Math.floor(Date.now() / HOUR_MS);
  const offsetHours = Math.round(-new Date().getTimezoneOffset() / 60);
  const buckets = boundaries.slice(0, -1).map((start, index) => {
    const hours: Array<{ outlet: LatencyStats; upstream: LatencyStats }> = [];
    const last = Math.min(lastHour, Math.ceil(boundaries[index + 1].getTime() / HOUR_MS) - 1);
    for (let hour = Math.max(firstHour, Math.ceil(start.getTime() / HOUR_MS)); hour <= last; hour += 1) hours.push(latencyHour(hour, offsetHours));
    return { start: rfc3339Local(start), outlet: mergeLatency(hours.map((item) => item.outlet)), upstream: mergeLatency(hours.map((item) => item.upstream)) };
  });
  return { unit, buckets, outlet: mergeLatency(buckets.map((bucket) => bucket.outlet)), upstream: mergeLatency(buckets.map((bucket) => bucket.upstream)) };
}

interface MockLog<Entry> {
  settings: LogSettings;
  available: boolean;
  dropped: number;
  suppressed: number; // routine entries over a client's budget, counted but not logged
  entries: Entry[]; // newest first
}

// Entries are made once, newest first, with gaps of one to eleven minutes.
function mockEntries<Entry>(count: number, make: (index: number, at: string, roll: (salt: number) => number) => Entry): Entry[] {
  let at = Date.now() - 20000;
  return Array.from({ length: count }, (_, index) => {
    const entry = make(index, new Date(at).toISOString(), (salt) => hashUnit(index * 31 + salt));
    at -= (60 + Math.floor(hashUnit(index * 17 + 2) * 600)) * 1000;
    return entry;
  });
}
const pick = <T>(items: readonly T[], roll: number): T => items[Math.min(items.length - 1, Math.floor(roll * items.length))];
const clientIp = (roll: number): string => `${pick(['198.51.100', '203.0.113', '192.0.2'], roll)}.${1 + Math.floor((roll * 997) % 250)}`;

const accessLog: MockLog<AccessLogEntry> = {
  settings: { enabled: true, max_mib: 256, retention_days: 30 },
  available: true,
  dropped: 0,
  suppressed: 0,
  entries: mockEntries(420, (index, at, roll): AccessLogEntry => {
    const base = { id: 5000 - index, at, host: 'vault.example.test', client: clientIp(roll(1)), email: '', token: '', routine: true };
    const kind = Math.floor(roll(2) * 9);
    if (kind === 0) return { ...base, method: 'POST', path: '/v1/secret/data/app', policy: 'vault-api', decision: 'passed', reason: 'bypass', token: 'deploybot', routine: false };
    if (kind === 1) return { ...base, method: 'GET', path: '/v1/sys/health', policy: 'vault-api', decision: 'passed', reason: 'bypass', token: 'monitor', routine: false };
    if (kind === 2) return { ...base, method: 'GET', path: '/ui/', policy: 'vault', decision: 'challenged', reason: 'no_session' };
    if (kind === 3) return { ...base, method: 'GET', path: '/ui/vault/secrets', policy: 'vault', decision: 'passed', reason: 'session', email: 'alice@example.test', routine: false };
    if (kind === 4) return { ...base, method: 'GET', path: '/ui/vault/policies/acl/' + 'team-'.repeat(1 + Math.floor(roll(3) * 12)) + 'readers', policy: 'vault', decision: 'passed', reason: 'session', email: 'bob@example.test', routine: false };
    if (kind === 5) return { ...base, method: 'GET', path: '/ui/vault/auth', policy: 'vault', decision: 'denied', reason: 'not_allowed', email: 'contractor@partner.example', routine: false };
    if (kind === 6) return { ...base, method: 'POST', path: '/v1/auth/token/create', policy: 'vault-api', decision: 'denied', reason: 'token_expired', token: 'monitor', routine: false };
    if (kind === 7) return { ...base, method: 'PUT', path: '/v1/sys/policy/admin', policy: 'vault-api', decision: 'denied', reason: 'token_invalid' };
    return { ...base, method: 'GET', path: '/old/console', policy: REMOVED_POLICY, decision: 'denied', reason: 'blocked' };
  }),
};

// Public addresses with the service behind them; the failure log uses addresses that exist in the demo.
const FAILURE_SITES: Array<[host: string, service: string, paths: string[]]> = [
  ['photos.example.test', 'photos', ['/api/assets/thumbnail/9f2c', '/api/server-info', '/api/albums/shared']],
  ['files.example.test', 'files', ['/remote.php/dav/files/alice/backup.tar', '/index.php/apps/files', '/ocs/v2.php/apps/notifications']],
  ['blog.example.com', 'blog', ['/ghost/api/admin/posts', '/rss/', '/2026/10/hello-world']],
  ['api.staging.example.net', 'api', ['/v2/orders', '/v2/orders/18273/items', '/v2/search']],
  ['shop.example.com', 'web', ['/checkout', '/cart/add', '/products/48']],
  ['vault.example.test', 'vault', ['/v1/secret/data/app', '/ui/vault/secrets']],
  ['erp.example.com', 'erp', ['/web/login', '/web/dataset/call_kw']],
  ['dns.example.test', 'pihole', ['/admin/api.php', '/admin/']],
];
// Paths no real client asks for: the honeypot rule trips on them (failure log) and they start the demo bans.
const BAN_PATHS = ['/.env', '/.git/config', '/wp-config.php', '/api/.env', '/.aws/credentials', '/app/.env.production', '/blog/wp-config.php.bak', '/.ssh/id_rsa', '/.docker/config.json', '/laravel/.env', '/admin/.git/config'];
// cause: [statuses, weight, duration in ms]
const FAILURE_SHAPES: Record<FailureCause, { statuses: number[]; weight: number; duration: (roll: number) => number }> = {
  app_error: { statuses: [500, 500, 503], weight: 30, duration: (roll) => 20 + Math.round(roll * 900) },
  unreachable: { statuses: [502], weight: 18, duration: (roll) => 2 + Math.round(roll * 40) },
  timeout: { statuses: [504], weight: 8, duration: () => 30000 },
  no_service: { statuses: [404], weight: 10, duration: (roll) => Math.round(roll * 3) },
  rate_limited: { statuses: [429], weight: 12, duration: () => 0 },
  blocked: { statuses: [403], weight: 14, duration: () => 0 },
  trapped: { statuses: [403], weight: 6, duration: () => 0 },
  banned: { statuses: [403], weight: 9, duration: () => 0 },
  too_long: { statuses: [414], weight: 2, duration: () => 0 },
  too_large: { statuses: [413], weight: 3, duration: (roll) => 100 + Math.round(roll * 2400) },
  busy: { statuses: [503, 429], weight: 3, duration: () => 1 },
};
const failureLog: MockLog<FailureLogEntry> = {
  settings: { enabled: true, max_mib: 256, retention_days: 30 },
  available: true,
  dropped: 7,
  suppressed: 2345,
  entries: mockEntries(360, (index, at, roll): FailureLogEntry => {
    const total = failureCauses.reduce((sum, cause) => sum + FAILURE_SHAPES[cause].weight, 0);
    let target = roll(4) * total;
    const cause = failureCauses.find((item) => (target -= FAILURE_SHAPES[item].weight) < 0) ?? 'app_error';
    const shape = FAILURE_SHAPES[cause];
    const [host, service, paths] = pick(FAILURE_SITES, roll(5));
    const refused = cause === 'blocked' || cause === 'trapped' || cause === 'banned' || cause === 'too_long' || cause === 'rate_limited';
    const path = cause === 'blocked' ? pick(['/wp-login.php', '/phpmyadmin/', '/xmlrpc.php', '/cgi-bin/luci'], roll(6))
      : cause === 'trapped' ? pick(BAN_PATHS, roll(6))
      : cause === 'too_long' ? `/search/${'a'.repeat(20)}…` : pick(paths, roll(6));
    return {
      id: 9000 - index, at, host, path, client: clientIp(roll(7)),
      method: cause === 'too_large' ? 'POST' : pick(['GET', 'GET', 'GET', 'POST', 'PUT'], roll(8)),
      cause, status: pick(shape.statuses, roll(9)),
      service: cause === 'no_service' || (refused && roll(10) < 0.4) ? '' : service,
      duration_ms: shape.duration(roll(11)),
      routine: (refusedCauses as readonly string[]).includes(cause),
    };
  }),
};

// The request log holds every public request, so it gets the most entries: about nine days, denser towards now so the periods of the top lists differ.
const REQUEST_AGENTS = {
  browsers: [
    'Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.4 Safari/605.1.15',
    'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/126.0.0.0 Safari/537.36',
    'Mozilla/5.0 (iPhone; CPU iPhone OS 17_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/17.5 Mobile/15E148 Safari/604.1',
    'Mozilla/5.0 (X11; Linux x86_64; rv:127.0) Gecko/20100101 Firefox/127.0',
  ],
  tools: ['curl/8.4.0', 'Go-http-client/2.0', 'Prometheus/2.51.2', 'okhttp/4.12.0'],
  bots: ['Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)', 'Mozilla/5.0 (compatible; bingbot/2.0; +http://www.bing.com/bingbot.htm)'],
  scanners: ['Nuclei - Open-source project (github.com/projectdiscovery/nuclei)', 'Mozilla/5.0 zgrab/0.x', 'python-requests/2.31.0', 'masscan/1.3', ''],
};
const SCANNER_PATHS = ['/wp-login.php', '/phpmyadmin/', '/xmlrpc.php', '/cgi-bin/luci', '/admin/config.php', '/actuator/env', '/.env', ...BAN_PATHS];
const SCANNER_CLIENTS = ['203.0.113.66', '192.0.2.113', '198.51.100.250', '203.0.113.9'];
// A handful of regular visitors, a few of them far busier than the rest.
const REGULAR_CLIENTS = Array.from({ length: 20 }, (_, index) => clientIp(hashUnit(index + 400)));
const REQUEST_COUNT = 320;
const REQUEST_SPAN_MS = 9 * 24 * HOUR_MS;

function mockRequestEntry(index: number): RequestLogEntry {
  const roll = (salt: number) => hashUnit(index * 41 + salt + 7000);
  // Newest first. The exponent packs the entries towards now.
  const at = new Date(Date.now() - 20000 - Math.pow((index + roll(1)) / REQUEST_COUNT, 2.2) * REQUEST_SPAN_MS).toISOString();
  const [host, service, paths] = pick(FAILURE_SITES, roll(2));
  const kind = roll(3);
  const regular = REGULAR_CLIENTS[Math.floor(roll(5) ** 2 * REGULAR_CLIENTS.length)];
  const entry: RequestLogEntry = {
    id: 20000 - index, at, host, method: 'GET', path: pick(paths, roll(4)), client: regular, source: '', status: 200, cause: '', service,
    duration_ms: 4 + Math.round(roll(6) ** 2 * 400), outlet_us: 0, upstream_us: 0, bytes: 800 + Math.round(roll(7) ** 3 * 400000), agent: pick(REQUEST_AGENTS.browsers, roll(8)), routine: false,
  };
  if (kind < 0.5) {
    const outcome = roll(9);
    if (outcome < 0.1) return { ...entry, method: 'POST', status: 201, bytes: 120 };
    if (outcome < 0.2) return { ...entry, status: 304, bytes: 0 };
    if (outcome < 0.25) return { ...entry, status: 302, bytes: 0, duration_ms: 3 };
    if (outcome < 0.3) return { ...entry, agent: pick(REQUEST_AGENTS.tools, roll(10)) };
    return entry;
  }
  if (kind < 0.54) return { ...entry, path: pick(['/ws', '/api/socket'], roll(10)), status: 101, duration_ms: 30000 + Math.round(roll(11) * 600000), bytes: 90 };
  if (kind < 0.6) return { ...entry, path: pick(['/favicon.ico', '/robots.txt', '/apple-touch-icon.png', '/sitemap.xml'], roll(10)), status: 404, bytes: 146, routine: true };
  if (kind < 0.78) {
    // Scanners come from a few addresses and ask for paths no site uses; the Outlet refuses most of them, some reach the app and get a 404.
    const path = pick(SCANNER_PATHS, roll(10));
    const base = { ...entry, path, client: pick(SCANNER_CLIENTS, roll(11)), agent: pick(REQUEST_AGENTS.scanners, roll(12)), duration_ms: 0, bytes: 0, routine: true };
    const outcome = roll(13);
    if (BAN_PATHS.includes(path) && outcome < 0.7) return { ...base, status: 403, cause: 'trapped', service: '' };
    if (outcome < 0.25) return { ...base, status: 403, cause: 'banned', service: '' };
    if (outcome < 0.4) return { ...base, status: 429, cause: 'rate_limited', service: '' };
    if (outcome < 0.55) return { ...base, status: 403, cause: 'blocked', service: '' };
    return { ...base, status: 404, duration_ms: 2 + Math.round(roll(14) * 30), bytes: 146 };
  }
  if (kind < 0.85) return { ...entry, agent: pick(REQUEST_AGENTS.bots, roll(10)), status: roll(11) < 0.15 ? 404 : 200, routine: roll(11) < 0.15 };
  if (kind < 0.93) {
    const cause = pick(['app_error', 'app_error', 'unreachable', 'timeout'] as const, roll(10));
    if (cause === 'app_error') return { ...entry, status: pick([500, 500, 503], roll(11)), cause, duration_ms: 20 + Math.round(roll(12) * 900), bytes: 310 };
    if (cause === 'unreachable') return { ...entry, status: 502, cause, duration_ms: 2 + Math.round(roll(12) * 40), bytes: 150 };
    return { ...entry, status: 504, cause, duration_ms: 30000, bytes: 150 };
  }
  if (kind < 0.96) return { ...entry, status: 499, duration_ms: 2000 + Math.round(roll(10) * 8000), bytes: 0, routine: true };
  const refused = pick(['too_large', 'too_long', 'busy'] as const, roll(10));
  if (refused === 'too_large') return { ...entry, method: 'POST', status: 413, cause: refused, duration_ms: 100 + Math.round(roll(11) * 2400), bytes: 0, routine: true };
  if (refused === 'too_long') return { ...entry, path: `/search/${'a'.repeat(20)}…`, status: 414, cause: refused, service: '', duration_ms: 0, bytes: 0, routine: true };
  return { ...entry, status: 503, cause: refused, service: '', duration_ms: 1, bytes: 0 };
}

// 서버의 clientKey를 흉내 냅니다: IPv4는 주소 그대로, IPv6는 /56 접두사(목업은 앞 세 묶음만 남깁니다).
const sourceOf = (client: string): string => (client.includes(':') ? `${client.split(':').slice(0, 3).join(':')}::/56` : client);

// What the Outlet timed for a request it forwarded: its own processing well under a few milliseconds, then the wait for the service's first byte
// (an upgrade is over once the 101 is back, so the connection time doesn't count). Refused requests, those with no service and those the Outlet couldn't
// reach the service for weren't forwarded and aren't timed.
function withLatency(entry: RequestLogEntry): RequestLogEntry {
  const roll = (salt: number) => hashUnit(entry.id * 13 + salt);
  const forwarded = entry.service !== '' && entry.cause !== 'unreachable' && entry.cause !== 'no_service' && !(refusedCauses as readonly string[]).includes(entry.cause);
  if (!forwarded) return { ...entry, outlet_us: 0, upstream_us: 0 };
  const outletUs = 120 + Math.round(roll(1) ** 2 * 1600);
  const upstreamMs = entry.status === 101 ? 20 + roll(2) * 180 : Math.max(0.4, entry.duration_ms - outletUs / 1000);
  return { ...entry, outlet_us: outletUs, upstream_us: Math.round(upstreamMs * 1000) };
}

const requestLog: MockLog<RequestLogEntry> = {
  settings: { enabled: true, max_mib: 512, retention_days: 30 },
  available: true,
  dropped: 0,
  suppressed: 18204,
  entries: Array.from({ length: REQUEST_COUNT }, (_, index) => mockRequestEntry(index)).map((entry) => withLatency({ ...entry, source: sourceOf(entry.client) })),
};

function logPageParam(params: URLSearchParams, name: string, fallback: number, max: number): number {
  const value = params.get(name);
  if (value === null) return fallback;
  const number = Number(value);
  if (!/^\d+$/.test(value) || number < 1 || number > max) fail('invalid_log_filter');
  return number;
}

// `q` is at most 128 bytes and matches a case-insensitive substring of the given columns.
function logSearch(params: URLSearchParams): string {
  const q = params.get('q') ?? '';
  if (new TextEncoder().encode(q).length > 128) fail('invalid_log_filter');
  return q.toLowerCase();
}

const includesText = (q: string, ...values: string[]): boolean => !q || values.some((value) => value.toLowerCase().includes(q));

function logView<Entry>(log: MockLog<Entry>, params: URLSearchParams, matches: (entry: Entry) => boolean): LogView<Entry> {
  const page = logPageParam(params, 'page', 1, Number.MAX_SAFE_INTEGER);
  const perPage = logPageParam(params, 'per_page', 50, 200);
  const matched = log.entries.filter(matches);
  // The demo pretends the file holds far more than the few hundred entries kept in memory.
  const sizeBytes = Math.round(log.entries.length * 30000 + 1.4 * 1048576);
  return structuredClone({
    settings: log.settings,
    available: log.available,
    size_bytes: sizeBytes,
    dropped: log.dropped,
    suppressed: log.suppressed,
    total: matched.length,
    entries: matched.slice((page - 1) * perPage, page * perPage),
  });
}

function saveLogSettings<Entry>(kind: 'access' | 'failures' | 'requests', log: MockLog<Entry>, input: Partial<LogSettings>): void {
  const integer = (value: unknown, min: number, max: number): value is number => typeof value === 'number' && Number.isInteger(value) && value >= min && value <= max;
  if (typeof input.enabled !== 'boolean' || !integer(input.max_mib, 16, 4096) || !integer(input.retention_days, 1, 365)) fail('invalid_log_settings');
  if (input.enabled && !log.available) fail('log_unavailable');
  log.settings = { enabled: input.enabled, max_mib: input.max_mib, retention_days: input.retention_days };
  auditEvents.unshift({
    id: `audit-${auditEvents.length + 1}`, created_at: now(), actor_email: 'dev@example.test', action: 'observe.log_settings', resource_type: 'observe', resource_id: kind,
    detail: { log: kind, ...log.settings }, source: 'public', ip: '203.0.113.7', user_agent: 'Chrome on macOS',
  });
}

// ---- Security: banned clients (GET/PUT /api/v1/security/client-bans, POST …/release) and the tunnel port guard ----

let clientBanSettings: ClientBanSettings = { minutes: 60, exempt: ['198.51.100.0/24'] };
// Forty bans, newest first: all but the last started within the past 53 minutes, so a 60-minute ban is still running. Every 9th comes from a rule that was deleted since.
let clientBans: ClientBan[] = Array.from({ length: 40 }, (_, index): ClientBan => {
  const roll = (salt: number) => hashUnit(index * 53 + salt + 900);
  // The last one is an old ban from before the duration was changed: it started yesterday and ends the day after tomorrow.
  const since = index === 39 ? Date.now() - 26 * 3600000 : Date.now() - (2 + index * 80 + Math.floor(roll(1) * 60)) * 1000;
  const v6 = index % 7 === 3;
  return {
    client: v6 ? `2001:db8:${(0xa100 + index).toString(16)}:${(0x1000 + index * 37).toString(16)}::/64` : `${['203.0.113', '192.0.2', '100.64.8'][index % 3]}.${10 + index * 5}`,
    rule: index % 9 === 5 ? 'r8f3k2m1' : 'honeypot',
    host: FAILURE_SITES[index % FAILURE_SITES.length][0],
    path: index === 7 ? `/${'deeply/nested/'.repeat(14)}.env` : BAN_PATHS[index % BAN_PATHS.length],
    since: new Date(since).toISOString(),
    until: new Date(index === 39 ? Date.now() + 46 * 3600000 : since + 60 * 60000).toISOString(),
    hits: index % 6 === 4 ? 0 : Math.floor(roll(2) ** 3 * 400),
  };
});

// Like the server: the search is a substring of client, host or path (at most 128 bytes), per_page 1–200.
function clientBansView(params: URLSearchParams): ClientBansView {
  const page = logPageParam(params, 'page', 1, Number.MAX_SAFE_INTEGER);
  const perPage = logPageParam(params, 'per_page', 20, 200);
  const q = logSearch(params);
  const matched = clientBans.filter((ban) => includesText(q, ban.client, ban.host, ban.path));
  return structuredClone({ settings: clientBanSettings, total: matched.length, bans: matched.slice((page - 1) * perPage, page * perPage) });
}

function auditSecurity(action: string, detail: Record<string, unknown>): void {
  auditEvents.unshift({
    id: `audit-${auditEvents.length + 1}`, created_at: now(), actor_email: 'dev@example.test', action, resource_type: 'security', resource_id: action.slice('security.'.length),
    detail, source: 'public', ip: '203.0.113.7', user_agent: 'Chrome on macOS',
  });
}

function clientBansApi(path: string, method: string, params: URLSearchParams, input: unknown): unknown {
  if (path === '/api/v1/security/client-bans' && method === 'GET') {
    try {
      return clientBansView(params);
    } catch {
      return fail('invalid_ban_query');
    }
  }
  if (path === '/api/v1/security/client-bans' && method === 'PUT') {
    const body = input as { minutes?: unknown; exempt?: unknown };
    const exempt = normalizeAddresses(body.exempt);
    if (!Number.isInteger(body.minutes) || (body.minutes as number) < 1 || (body.minutes as number) > 10080 || !exempt) fail('invalid_client_bans');
    clientBanSettings = { minutes: body.minutes as number, exempt };
    // Clients that are exempt now are released at once; the others keep their end time.
    const exemptRanges = exempt.map((entry) => normalizeRange(entry)!);
    const covered = (client: string): boolean => exemptRanges.some((range) => {
      const [address, bits] = range.split('/');
      return !client.includes(':') && !address.includes(':') && ipv4Number(client) >= ipv4Number(address) && ipv4Number(client) < ipv4Number(address) + 2 ** (32 - Number(bits));
    });
    clientBans = clientBans.filter((ban) => !covered(ban.client));
    auditSecurity('security.client_bans', { minutes: clientBanSettings.minutes, exempt });
    return clientBansView(new URLSearchParams());
  }
  if (path === '/api/v1/security/client-bans/release' && method === 'POST') {
    const body = input as { client?: unknown; all?: unknown };
    if (body.all === true) {
      const count = clientBans.length;
      clientBans = [];
      auditSecurity('security.client_ban_release', { all: true, count });
    } else if (typeof body.client === 'string') {
      if (!clientBans.some((ban) => ban.client === body.client)) fail('ban_not_found');
      clientBans = clientBans.filter((ban) => ban.client !== body.client);
      auditSecurity('security.client_ban_release', { client: body.client, count: 1 });
    } else {
      fail('invalid_client_bans');
    }
    return clientBansView(new URLSearchParams());
  }
  return fail('not_found');
}
const ipv4Number = (address: string): number => address.split('.').reduce((sum, octet) => sum * 256 + Number(octet), 0);

let tunnelGuard: TunnelGuardSettings = { mode: 'limited', allowed: [] };
// The addresses connectors made authenticated requests from; newest first.
const tunnelConnectors = [
  { client: '203.0.113.7', last_seen: new Date(Date.now() - 40 * 1000).toISOString() },
  { client: '198.51.100.23', last_seen: new Date(Date.now() - 4 * 60 * 1000).toISOString() },
  { client: '2001:db8:3c4d:15::/64', last_seen: new Date(Date.now() - 11 * 60 * 1000).toISOString() },
];
const tunnelGuardStats = { active: 4, accepted: 18342, refused: { rate: 211, busy: 36, idle: 1274, strict: 0 } };
function tunnelGuardView(): TunnelGuardView {
  return structuredClone({ settings: tunnelGuard, stats: tunnelGuardStats, connectors: tunnelConnectors });
}

function tunnelGuardApi(method: string, input: unknown): unknown {
  if (method === 'GET') return tunnelGuardView();
  if (method === 'PUT') {
    const body = input as { mode?: unknown; allowed?: unknown };
    const allowed = normalizeAddresses(body.allowed);
    if ((body.mode !== 'limited' && body.mode !== 'strict') || !allowed) fail('invalid_tunnel_guard');
    tunnelGuard = { mode: body.mode, allowed };
    auditSecurity('security.tunnel_guard', { mode: body.mode, allowed });
    return tunnelGuardView();
  }
  return fail('not_found');
}

const TOP_PERIOD_MS: Record<string, number> = { '1h': HOUR_MS, '24h': 24 * HOUR_MS, '7d': 7 * 24 * HOUR_MS, '30d': 30 * 24 * HOUR_MS };

// GET /api/v1/observe/request-log/top: the requests still in the log inside the period, grouped by endpoint (host + path) or by client.
function requestTop(params: URLSearchParams): RequestTopView {
  const by = params.get('by') ?? 'endpoint';
  const period = params.get('period') ?? '24h';
  const [sortKey, direction = 'desc'] = (params.get('sort') ?? 'requests.desc').split('.');
  const host = params.get('host') ?? '';
  const q = logSearch(params);
  if ((by !== 'endpoint' && by !== 'client') || !(requestTopPeriods as readonly string[]).includes(period)
    || !(requestTopSorts as readonly string[]).includes(sortKey) || (direction !== 'asc' && direction !== 'desc')) fail('invalid_log_filter');
  const page = logPageParam(params, 'page', 1, Number.MAX_SAFE_INTEGER);
  const perPage = logPageParam(params, 'per_page', 50, 200);
  const from = period === 'all' ? null : new Date(Date.now() - TOP_PERIOD_MS[period]);

  const groups = new Map<string, { row: RequestTopRow; others: Set<string> }>();
  for (const entry of requestLog.entries) {
    if ((from && Date.parse(entry.at) < from.getTime()) || (host && entry.host !== host) || !includesText(q, entry.host, entry.path, entry.client, entry.service, entry.agent)) continue;
    const endpoint = `${entry.host}\u0000${entry.path}`;
    const key = by === 'client' ? entry.source : endpoint;
    let group = groups.get(key);
    if (!group) {
      group = { row: { ...(by === 'client' ? { client: entry.source } : { host: entry.host, path: entry.path }), requests: 0, client_errors: 0, server_errors: 0, distinct: 0, last_at: entry.at }, others: new Set() };
      groups.set(key, group);
    }
    group.row.requests += 1;
    if (entry.status >= 400 && entry.status < 500) group.row.client_errors += 1;
    if (entry.status >= 500 && entry.status < 600) group.row.server_errors += 1;
    if (entry.at > group.row.last_at) group.row.last_at = entry.at;
    group.others.add(by === 'client' ? endpoint : entry.source);
  }
  // 서버처럼 같은 값은 묶은 열(주소, 경로 또는 출처)의 바이트 순서로 놓습니다.
  const compare = (x: string, y: string): number => (x < y ? -1 : x > y ? 1 : 0);
  const keys = (row: RequestTopRow): string[] => (row.client !== undefined ? [row.client] : [row.host ?? '', row.path ?? '']);
  const sign = direction === 'asc' ? 1 : -1;
  const rows = [...groups.values()].map(({ row, others }) => ({ ...row, distinct: others.size })).sort((a, b) => {
    const order = sortKey === 'last_at' ? compare(a.last_at, b.last_at) : (a[sortKey as 'requests'] - b[sortKey as 'requests']);
    return sign * order || keys(a).map((key, index) => compare(key, keys(b)[index])).find((result) => result !== 0) || 0;
  });
  return structuredClone({
    settings: requestLog.settings,
    available: requestLog.available,
    from: from ? from.toISOString() : null,
    total: rows.length,
    rows: rows.slice((page - 1) * perPage, page * perPage),
  });
}

function observe(path: string, method: string, params: URLSearchParams, input: unknown): unknown {
  if (path === '/api/v1/observe/access-metrics' && method === 'GET') return accessMetrics(params);
  if (path === '/api/v1/observe/latency' && method === 'GET') return latencySeries(params);
  if (path === '/api/v1/observe/access-log') {
    if (method === 'PUT') {
      saveLogSettings('access', accessLog, input as Partial<LogSettings>);
      return logView(accessLog, new URLSearchParams(), () => true);
    }
    if (method === 'GET') {
      const decision = params.get('decision') ?? '';
      const policy = params.get('policy') ?? '';
      const who = params.get('who') ?? '';
      const q = logSearch(params);
      if (decision && !(accessDecisions as readonly string[]).includes(decision)) fail('invalid_log_filter');
      if (who && !(accessRequesters as readonly string[]).includes(who)) fail('invalid_log_filter');
      return logView(accessLog, params, (entry) => (!decision || entry.decision === decision) && (!policy || entry.policy === policy)
        && (!who || entry.routine === (who === 'anonymous')) && includesText(q, entry.host, entry.path, entry.client, entry.email, entry.token));
    }
  }
  if (path === '/api/v1/observe/failure-log') {
    if (method === 'PUT') {
      saveLogSettings('failures', failureLog, input as Partial<LogSettings>);
      return logView(failureLog, new URLSearchParams(), () => true);
    }
    if (method === 'GET') {
      const cause = params.get('cause') ?? '';
      const host = params.get('host') ?? '';
      const q = logSearch(params);
      if (cause && !(failureCauses as readonly string[]).includes(cause) && !(failureCauseGroups as readonly string[]).includes(cause)) fail('invalid_log_filter');
      // `service` and `refused` stand for the four and the five causes of each group.
      const causes: readonly string[] = cause === 'service' ? serviceCauses : cause === 'refused' ? refusedCauses : cause ? [cause] : [];
      return logView(failureLog, params, (entry) => (!causes.length || causes.includes(entry.cause)) && (!host || entry.host === host) && includesText(q, entry.host, entry.path, entry.client, entry.service));
    }
  }
  if (path === '/api/v1/observe/request-log/top' && method === 'GET') return requestTop(params);
  if (path === '/api/v1/observe/request-log') {
    if (method === 'PUT') {
      saveLogSettings('requests', requestLog, input as Partial<LogSettings>);
      return logView(requestLog, new URLSearchParams(), () => true);
    }
    if (method === 'GET') {
      const cls = params.get('class') ?? '';
      const host = params.get('host') ?? '';
      const exactPath = params.get('path') ?? '';
      const source = params.get('source') ?? '';
      const q = logSearch(params);
      if (cls && !(requestClasses as readonly string[]).includes(cls)) fail('invalid_log_filter');
      if (new TextEncoder().encode(exactPath).length > 512) fail('invalid_log_filter');
      const hundreds = cls ? Number(cls[0]) * 100 : 0;
      return logView(requestLog, params, (entry) => (!cls || (entry.status >= hundreds && entry.status < hundreds + 100)) && (!host || entry.host === host)
        && (!exactPath || entry.path === exactPath) && (!source || entry.source === source) && includesText(q, entry.host, entry.path, entry.client, entry.service, entry.agent));
    }
  }
  return fail('not_found');
}

export async function mockApi(url: string, options: RequestInit = {}): Promise<unknown> {
  const method = options.method || 'GET';
  const path = new URL(url, 'http://localhost').pathname;
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent);
  const input = bodyOf(options.body);

  // 미리보기 서버는 로컬 관리 포트처럼 계정 복구 링크도 보여 줍니다.
  if (path === '/api/v1/setup/status' && method === 'GET') return { setup_complete: true, sign_in: { password: true, sso: true, recovery: true } };
  if (path === '/api/v1/setup/admin' && method === 'POST') return { status: 'created' };
  if (path === '/api/v1/setup/recover' && method === 'POST') {
    if ((input.password ?? '').length < 12) fail('invalid_setup_credential');
    return { status: 'recovered' };
  }
  if (path === '/api/v1/session' && method === 'POST') {
    if (alerts.login_second_factor) {
      loginChallenge = id();
      return { second_factor: 'telegram', challenge: loginChallenge };
    }
    return { admin: { email: input.email || 'dev@example.test' }, expires_at: now() };
  }
  if (path === '/api/v1/session/second-factor' && method === 'POST') {
    const body = input as { challenge?: string; code?: string };
    if (!loginChallenge || body.challenge !== loginChallenge || body.code !== '123456') fail('invalid_second_factor');
    loginChallenge = '';
    return { admin: { email: 'dev@example.test' }, expires_at: now() };
  }
  if (path === '/api/v1/audit' && method === 'GET') {
    const params = new URL(url, 'http://localhost').searchParams;
    const limit = Math.min(200, Math.max(1, Number(params.get('limit') ?? 50)));
    const offset = Math.max(0, Number(params.get('offset') ?? 0));
    const action = params.get('action') ?? '';
    const q = (params.get('q') ?? '').trim().toLowerCase();
    const matches = auditEvents.filter((event) => event.action.startsWith(action) && (!q || [event.actor_email, event.resource_id, JSON.stringify(event.detail ?? {})].some((value) => value?.toLowerCase().includes(q))));
    return { events: matches.slice(offset, offset + limit), total: matches.length };
  }
  if (path === '/api/v1/alerts' && method === 'GET') return alertsResponse();
  if (path === '/api/v1/alerts/telegram' && method === 'PUT') {
    const body = input as { bot_token?: string; chat_id?: string; enabled?: boolean };
    const token = body.bot_token?.trim() ?? '';
    if (token && !/^\d{5,}:[A-Za-z0-9_-]{30,}$/.test(token)) fail('invalid_telegram_token');
    if (!alerts.telegram.configured && !token) fail('invalid_telegram_token');
    if (!/^(-?\d{1,20}|@\w{5,})$/.test(body.chat_id?.trim() ?? '')) fail('invalid_telegram_chat');
    const changed = Boolean(token) || body.chat_id !== alerts.telegram.chat_id;
    alerts.telegram = {
      ...alerts.telegram,
      source: 'database',
      configured: true,
      enabled: Boolean(body.enabled),
      chat_id: body.chat_id!.trim(),
      token_hint: token ? token.slice(-4) : alerts.telegram.token_hint,
      verified_at: changed ? null : alerts.telegram.verified_at,
    };
    if (changed) alerts.login_second_factor = false;
    return alertsResponse();
  }
  if (path === '/api/v1/alerts/telegram/test' && method === 'POST') {
    if (!alerts.telegram.configured) fail('telegram_not_configured');
    alerts.telegram.verified_at = now();
    return alertsResponse();
  }
  if (path === '/api/v1/alerts/telegram' && method === 'DELETE') {
    alerts.telegram = { configured: false, enabled: false, chat_id: '', token_hint: '', verified_at: null, source: 'default', environment: false };
    alerts.login_second_factor = false;
    return alertsResponse();
  }
  if (path === '/api/v1/alerts/rules' && method === 'PUT') {
    // Like the Outlet, the original six rules are required; service_down and the later rules keep their stored values when omitted.
    const body = input as unknown as Partial<AlertRules>;
    const required: Array<keyof AlertRules> = ['tunnel_down', 'route_error', 'dns_failure', 'error_spike', 'login', 'rate_limit'];
    if (required.some((key) => !body[key])) fail('invalid_alert_rules');
    const rules = { ...alerts.rules, ...body };
    const within = (value: unknown, low: number, high: number, whole = true): boolean => typeof value === 'number' && value >= low && value <= high && (!whole || Number.isInteger(value));
    const valid = within(rules.tunnel_down.after_minutes, 1, 1440)
      && within(rules.service_down.after_minutes, 1, 1440)
      && within(rules.error_spike.threshold_percent, 0.1, 100, false)
      && within(rules.error_spike.min_requests, 1, 1000000)
      && within(rules.rate_limit.min_blocked, 1, 1000000)
      && within(rules.latency.increase_percent, 10, 1000)
      && within(rules.latency.min_requests, 1, 1000000)
      && within(rules.disk_low.min_free_percent, 1, 50)
      && within(rules.memory_low.min_free_percent, 1, 50)
      && within(rules.tunnel_unstable.min_reconnects, 2, 100);
    if (!valid) fail('invalid_alert_rules');
    alerts.rules = structuredClone(rules);
    return alertsResponse();
  }
  if (path === '/api/v1/security/rate-limit' && method === 'GET') return structuredClone(rateLimit);
  if (path === '/api/v1/security/rate-limit' && method === 'PUT') {
    const next = input as unknown as RateLimitSettings;
    const bucketOk = (bucket?: { rps: number; burst: number }) => Boolean(bucket) && ((bucket!.rps === 0 && bucket!.burst === 0) || (bucket!.rps >= 1 && bucket!.rps <= 10000 && bucket!.burst >= bucket!.rps && bucket!.burst <= 100000));
    const rulesOk = Array.isArray(next.endpoints) && next.endpoints.length <= 50 && next.endpoints.every((rule) => (rule.host === '*' || /^[a-z0-9.-]+$/.test(rule.host)) && rule.path_prefix.startsWith('/') && (rule.per === 'total' || rule.per === 'ip') && bucketOk(rule) && rule.rps > 0);
    if (typeof next.enabled !== 'boolean' || !bucketOk(next.per_ip) || !bucketOk(next.per_subnet) || !bucketOk(next.per_address) || !rulesOk) fail('invalid_rate_limit');
    rateLimit = structuredClone(next);
    return structuredClone(rateLimit);
  }
  if (path === '/api/v1/security/sso' && method === 'GET') return ssoResponse();
  if (path === '/api/v1/security/sso' && method === 'PUT') {
    if (sso.source === 'environment') fail('sso_from_environment');
    const body = input as unknown as Partial<Record<keyof SsoSettings | 'client_secret', unknown>>;
    const strings = (value: unknown): string[] | null => (Array.isArray(value) && value.every((item) => typeof item === 'string') ? value as string[] : null);
    const list = (value: unknown, fold: boolean): string[] | null => {
      const items = strings(value);
      if (!items) return null;
      const cleaned = items.map((item) => (fold ? item.trim().toLowerCase() : item.trim())).filter(Boolean);
      return cleaned.length <= 200 && cleaned.every((item) => item.length <= 200) ? [...new Set(cleaned)] : null;
    };
    const scopes = strings(body.scopes)?.map((scope) => scope.trim()).filter(Boolean);
    const users = list(body.allowed_users, true);
    const groups = list(body.allowed_groups, false);
    const label = typeof body.label === 'string' ? body.label.trim() : null;
    const clientId = typeof body.client_id === 'string' ? body.client_id.trim() : null;
    const claim = typeof body.groups_claim === 'string' ? body.groups_claim.trim() : null;
    const secret = typeof body.client_secret === 'string' ? body.client_secret.trim() : body.client_secret === undefined ? '' : null;
    const rawIssuer = typeof body.issuer === 'string' ? body.issuer.trim() : null;
    const issuer = rawIssuer === '' ? '' : issuerOf(rawIssuer);
    if (typeof body.enabled !== 'boolean' || typeof body.password_login !== 'boolean' || !scopes || !users || !groups || label === null || label.length > 32 || clientId === null || claim === null || secret === null || issuer === null || clientId.length > 200 || claim.length > 200 || (secret?.length ?? 0) > 1000) fail('invalid_sso');
    if (body.enabled && (!issuer || !clientId || users.length + groups.length === 0)) fail('invalid_sso');
    const secretSet = secret ? true : sso.client_secret_set;
    sso = {
      ...sso,
      enabled: body.enabled,
      label,
      issuer,
      client_id: clientId,
      client_secret_set: secretSet,
      client_secret_hint: secret ? (secret.length >= 8 ? secret.slice(-4) : '') : sso.client_secret_hint,
      scopes: ['openid', ...new Set(scopes.filter((scope) => scope !== 'openid'))],
      groups_claim: claim || 'groups',
      allowed_users: users,
      allowed_groups: groups,
      password_login: body.password_login,
      source: 'database',
    };
    return ssoResponse();
  }
  if (path === '/api/v1/security/sso' && method === 'DELETE') fail('sso_from_environment');
  if (path === '/api/v1/security/sso/test' && method === 'POST') {
    const issuer = issuerOf((input as { issuer?: unknown }).issuer);
    if (!issuer) fail('invalid_issuer');
    if (new URL(issuer).hostname.endsWith('.invalid')) fail('provider_unreachable');
    return { authorization_endpoint: `${issuer}/o/oauth2/v2/auth` };
  }
  if (path === '/api/v1/security/connection-limits' && method === 'GET') return structuredClone(connectionLimits);
  if (path === '/api/v1/security/connection-limits' && method === 'PUT') {
    const next = input as unknown as ConnectionLimits;
    const within = (value: unknown, max: number) => Number.isInteger(value) && (value as number) >= 0 && (value as number) <= max;
    const hostnameOk = (host: string) => host.length <= 253 && host.split('.').every((label) => /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$/.test(label));
    const hosts = Array.isArray(next.hosts) ? next.hosts.map((entry) => ({ host: typeof entry?.host === 'string' ? entry.host.trim().toLowerCase() : '', max_upload_mb: entry?.max_upload_mb })) : null;
    const hostsOk = hosts !== null && hosts.length <= 50 && new Set(hosts.map((entry) => entry.host)).size === hosts.length && hosts.every((entry) => hostnameOk(entry.host) && within(entry.max_upload_mb, 102400));
    const idle = next.idle_timeout_seconds;
    if (!within(next.max_upload_mb, 102400) || !within(next.per_ip_concurrent, 1000000) || !within(next.total_concurrent, 1000000) || !(idle === 0 || (within(idle, 3600) && idle >= 5)) || !within(next.min_upload_kb_per_second, 100000) || !hostsOk) fail('invalid_connection_limits');
    connectionLimits = { ...next, hosts: hosts! };
    return structuredClone(connectionLimits);
  }
  if (path === '/api/v1/security/tunnel-guard') return tunnelGuardApi(method, input);
  if (path.startsWith('/api/v1/security/client-bans')) return clientBansApi(path, method, new URL(url, 'http://localhost').searchParams, input);
  if (path === '/api/v1/security/block-rules' && method === 'GET') return blockRulesResponse();
  if (path === '/api/v1/security/block-rules' && method === 'PUT') {
    // 실제 서버처럼 rules만 받고, 값은 공백 제거·소문자 변환(정규식 제외)·정리 후 검사합니다.
    const body = input as { rules?: unknown };
    if (Object.keys(body).some((key) => key !== 'rules') || !Array.isArray(body.rules) || body.rules.length > 50) fail('invalid_block_rules');
    const condition = (raw: unknown): BlockCondition => normalizeCondition(raw) ?? fail('invalid_block_rules');
    const rules = (body.rules as unknown[]).map((raw): BlockRule => {
      const rule = (raw ?? {}) as Partial<BlockRule>;
      const name = typeof rule.name === 'string' ? rule.name.trim() : '';
      if (!name || name.length > 64 || typeof rule.enabled !== 'boolean' || typeof rule.id !== 'string') fail('invalid_block_rules');
      if (!Array.isArray(rule.hosts) || !Array.isArray(rule.conditions) || rule.conditions.length < 1 || rule.conditions.length > 50) fail('invalid_block_rules');
      const hosts = [...new Set(rule.hosts.map((host) => (typeof host === 'string' ? host.trim().toLowerCase() : '')))];
      if (hosts.some((host) => !validBlockHost(host))) fail('invalid_block_rules');
      const seen = new Set<string>();
      const conditions = rule.conditions.map(condition).filter((entry) => {
        const key = `${entry.operator}\0${entry.value}`;
        if (seen.has(key)) return false;
        seen.add(key);
        return true;
      });
      return { id: rule.id, name, enabled: rule.enabled, ban: rule.ban === true, hosts, conditions };
    });
    const ids = rules.map((rule) => rule.id).filter(Boolean);
    if (new Set(ids).size !== ids.length) fail('invalid_block_rules');
    for (const rule of rules) if (!rule.id) rule.id = `r${Math.random().toString(36).slice(2, 10)}`;
    blockHits = Object.fromEntries(Object.entries(blockHits).filter(([id]) => rules.some((rule) => rule.id === id)));
    blockRules = rules;
    return blockRulesResponse();
  }
  if (path === '/api/v1/security/access-provider' && method === 'GET') return accessProviderResponse();
  if (path === '/api/v1/security/access-provider' && method === 'PUT') {
    // 실제 서버처럼 값을 정리한 뒤 검사합니다. 시크릿은 비우거나 생략하면 저장된 값을 유지합니다.
    const body = input as unknown as Partial<Record<keyof AccessProviderView | 'client_secret', unknown>>;
    const strings = (value: unknown): string[] | null => (Array.isArray(value) && value.every((item) => typeof item === 'string') ? value as string[] : null);
    const scopes = strings(body.scopes)?.map((scope) => scope.trim()).filter(Boolean);
    const clientId = typeof body.client_id === 'string' ? body.client_id.trim() : null;
    const claim = typeof body.groups_claim === 'string' ? body.groups_claim.trim() : null;
    const secret = typeof body.client_secret === 'string' ? body.client_secret.trim() : body.client_secret === undefined ? '' : null;
    const rawIssuer = typeof body.issuer === 'string' ? body.issuer.trim() : null;
    const issuer = rawIssuer === '' ? '' : issuerOf(rawIssuer);
    // 0 또는 생략은 기본값(24시간)입니다.
    const hours = body.session_hours === undefined || body.session_hours === 0 ? 24 : body.session_hours;
    if (typeof body.enabled !== 'boolean' || !scopes || scopes.length > 200 || scopes.some((scope) => scope.length > 64) || clientId === null || clientId.length > 200 || claim === null || claim.length > 64 || secret === null || secret.length > 1024 || issuer === null || typeof hours !== 'number' || !Number.isInteger(hours) || hours < 1 || hours > 720) fail('invalid_access_provider');
    if (body.enabled && (!issuer || !clientId)) fail('invalid_access_provider');
    accessProvider = {
      enabled: body.enabled,
      issuer,
      client_id: clientId,
      client_secret_set: secret ? true : accessProvider.client_secret_set,
      client_secret_hint: secret ? (secret.length >= 8 ? secret.slice(-4) : '') : accessProvider.client_secret_hint,
      scopes: ['openid', ...new Set(scopes.filter((scope) => scope !== 'openid'))],
      groups_claim: claim || 'groups',
      session_hours: hours,
    };
    return accessProviderResponse();
  }
  if (path === '/api/v1/security/access-provider/test' && method === 'POST') {
    const issuer = issuerOf((input as { issuer?: unknown }).issuer);
    if (!issuer) fail('invalid_issuer');
    if (new URL(issuer).hostname.endsWith('.invalid')) fail('provider_unreachable');
    return { authorization_endpoint: `${issuer}/authorize` };
  }
  if (path === '/api/v1/security/access/sessions/revoke' && method === 'POST') return {};
  if (path === '/api/v1/security/access-policies' && method === 'GET') return accessPoliciesResponse();
  if (path.startsWith('/api/v1/observe/')) return observe(path, method, new URL(url, 'http://localhost').searchParams, input);
  if (path === '/api/v1/security/access-tokens' && method === 'GET') return { tokens: accessTokens.map(tokenView), addresses: publicAddresses() } satisfies AccessTokensView;
  if (path === '/api/v1/security/access-tokens' && method === 'POST') {
    const body = input as Partial<AccessTokenInput>;
    const days = body.expires_days;
    if (typeof days !== 'number' || !Number.isInteger(days) || days < 0 || days > 3650 || accessTokens.length >= 100) fail('invalid_access_token');
    const scope = tokenScope(body);
    const now = new Date();
    const token = { id: Math.random().toString(36).slice(2, 14), ...scope, created_at: now.toISOString().replace(/\.\d+Z$/, 'Z'), expires_at: days ? new Date(now.getTime() + days * 86400000).toISOString().replace(/\.\d+Z$/, 'Z') : '', last_used_at: '' };
    accessTokens = [...accessTokens, token];
    const secret = Array.from(crypto.getRandomValues(new Uint8Array(32)), (byte) => byte.toString(16).padStart(2, '0')).join('');
    return { token: tokenView(token), value: `${token.id}.${secret}` } satisfies CreatedAccessToken;
  }
  const tokenMatch = path.match(/^\/api\/v1\/security\/access-tokens\/([^/]+)$/);
  if (tokenMatch && method === 'PUT') {
    const id = decodeURIComponent(tokenMatch[1]);
    const token = accessTokens.find((entry) => entry.id === id) ?? fail('access_token_not_found');
    Object.assign(token, tokenScope(input as Partial<AccessTokenInput>));
    return tokenView(token);
  }
  if (tokenMatch && method === 'DELETE') {
    const id = decodeURIComponent(tokenMatch[1]);
    if (!accessTokens.some((token) => token.id === id)) fail('access_token_not_found');
    if (accessPolicies.some((policy) => policy.tokens.includes(id))) fail('access_token_in_use');
    accessTokens = accessTokens.filter((token) => token.id !== id);
    return undefined;
  }
  if (path === '/api/v1/security/access-policies' && method === 'PUT') {
    // 실제 서버처럼 policies만 받고, 값은 공백 제거·소문자 변환(정규식 제외)·중복 제거 후 검사합니다.
    const body = input as { policies?: unknown };
    if (Object.keys(body).some((key) => key !== 'policies') || !Array.isArray(body.policies) || body.policies.length > 100) fail('invalid_access_policies');
    const entries = (value: unknown, fold: boolean, valid: (entry: string) => boolean): string[] => {
      if (!Array.isArray(value) || value.length > 200) fail('invalid_access_policies');
      const cleaned = value.map((item) => (typeof item === 'string' ? (fold ? item.trim().toLowerCase() : item.trim()) : ''));
      if (cleaned.some((item) => !item || item.length > 200 || !valid(item))) fail('invalid_access_policies');
      return [...new Set(cleaned)];
    };
    const policies = (body.policies as unknown[]).map((raw): AccessPolicy => {
      const policy = (raw ?? {}) as Partial<AccessPolicy>;
      const name = typeof policy.name === 'string' ? policy.name.trim() : '';
      if (!name || name.length > 64 || /[\u0000-\u001f\u007f]/.test(name) || typeof policy.id !== 'string' || typeof policy.enabled !== 'boolean' || typeof policy.all_paths !== 'boolean' || typeof policy.allow_admins !== 'boolean' || typeof policy.allow_everyone !== 'boolean') fail('invalid_access_policies');
      const action = policy.action ?? 'allow';
      if (action !== 'allow' && action !== 'bypass' && action !== 'block') fail('invalid_access_policies');
      if (!Array.isArray(policy.hosts) || policy.hosts.length < 1 || policy.hosts.length > 100) fail('invalid_access_policies');
      const hosts = [...new Set(policy.hosts.map((host) => (typeof host === 'string' ? host.trim().toLowerCase() : '')))];
      if (hosts.some((host) => !validBlockHost(host))) fail('invalid_access_policies');
      // 모든 경로를 보호할 때는 조건을 버리고, 선택한 경로는 조건이 1개 이상 있어야 합니다.
      let conditions: BlockCondition[] = [];
      if (!policy.all_paths) {
        if (!Array.isArray(policy.conditions) || policy.conditions.length < 1 || policy.conditions.length > 50) fail('invalid_access_policies');
        const seen = new Set<string>();
        // 요청 경로는 디코딩하고 ;매개변수를 뺀 형태로 비교하므로, 그런 값은 거부합니다.
        conditions = policy.conditions.map((raw) => {
          const entry = (raw ?? {}) as Partial<BlockCondition>;
          if (entry.operator !== 'matches' && typeof entry.value === 'string' && /%[0-9a-f]{2}|;/i.test(entry.value)) fail('invalid_access_policies');
          return (action === 'bypass' ? bypassCondition(raw) : normalizeCondition(raw)) ?? fail('invalid_access_policies');
        }).filter((entry) => {
          const key = `${entry.operator}\0${entry.value}`;
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        });
      }
      // 서버처럼 메서드는 대문자로 바꿔 accessMethods 순서로 정리하고, 그 밖의 메서드(HEAD 포함)는 거부합니다.
      if (policy.methods !== undefined && policy.methods !== null && (!Array.isArray(policy.methods) || policy.methods.some((entry) => typeof entry !== 'string'))) fail('invalid_access_policies');
      const named = (policy.methods ?? []).map((entry) => entry.trim().toUpperCase());
      if (named.some((entry) => !(accessMethods as readonly string[]).includes(entry))) fail('invalid_access_policies');
      const methods = accessMethods.filter((entry) => named.includes(entry));
      let allowedUsers = entries(policy.allowed_users, true, (entry) => /^[^\s@]+@[^\s@]+$/.test(entry) || /^@[^\s@]+$/.test(entry));
      let allowedGroups = entries(policy.allowed_groups, false, (entry) => !/[\u0000-\u001f\u007f]/.test(entry));
      // 서버처럼 토큰 ID는 공백을 지우지 않고 형식부터 확인한 뒤, 토큰 조건이 없으면 비우고, 있으면 있는 토큰만 받습니다.
      if (policy.require_token !== undefined && typeof policy.require_token !== 'boolean') fail('invalid_access_policies');
      if (policy.tokens !== undefined && policy.tokens !== null && !Array.isArray(policy.tokens)) fail('invalid_access_policies');
      const ids = [...new Set(policy.tokens ?? [])];
      if (ids.length > 100 || ids.some((id) => typeof id !== 'string' || !/^[A-Za-z0-9_-]{1,32}$/.test(id))) fail('invalid_access_policies');
      const tokens = policy.require_token ? ids : [];
      if (tokens.some((id) => !accessTokens.some((token) => token.id === id))) fail('invalid_access_policies');
      let everyone = policy.allow_everyone;
      let admins = policy.allow_admins;
      // 서버처럼 우회·차단 정책은 허용 대상을, 전체 허용은 사용자·그룹·관리자를 비웁니다.
      if (action !== 'allow') everyone = false;
      if (action !== 'allow' || everyone) [allowedUsers, allowedGroups, admins] = [[], [], false];
      // 아무도 허용하지 않는 허용 정책은 주소를 모두에게 닫아 버립니다.
      if (action === 'allow' && !everyone && allowedUsers.length + allowedGroups.length === 0 && !admins) fail('invalid_access_policies');
      return { id: policy.id, name, enabled: policy.enabled, action, hosts, all_paths: policy.all_paths, conditions, methods, require_token: policy.require_token === true, tokens, allow_everyone: everyone, allowed_users: allowedUsers, allowed_groups: allowedGroups, allow_admins: admins };
    });
    const ids = policies.map((policy) => policy.id).filter(Boolean);
    if (new Set(ids).size !== ids.length) fail('invalid_access_policies');
    for (const policy of policies) if (!policy.id) policy.id = `p${Math.random().toString(36).slice(2, 10)}`;
    accessPolicies = policies;
    return accessPoliciesResponse();
  }
  if (path === '/api/v1/security/second-factor' && method === 'PUT') {
    const enabled = Boolean((input as { enabled?: boolean }).enabled);
    if (enabled && !alerts.telegram.verified_at) fail('telegram_not_verified');
    alerts.login_second_factor = enabled;
    return { login_second_factor: enabled };
  }
  if (path === '/api/v1/session' && method === 'DELETE') return {};
  if (path === '/api/v1/outlet' && method === 'GET') return outletResponse();
  if (path === '/api/v1/outlet/tunnel' && method === 'PUT') {
    const address = tunnelAddressOf(input.address);
    if (!address && !state.outlet.domain) fail('tunnel_default_unavailable');
    state.tunnelAddress = address;
    return outletResponse();
  }
  if (path === '/api/v1/outlet/tunnel/check' && method === 'POST') {
    const address = tunnelAddressOf(input.address) || outletResponse().tunnel?.address;
    if (!address) fail('tunnel_address_unset');
    // The bare primary domain demonstrates a root record that points elsewhere.
    const reachable = !address.startsWith(`${state.outlet.domain}:`);
    const port = address.slice(address.lastIndexOf(':') + 1);
    return {
      checked_at: now(),
      address,
      reachable,
      results: [reachable ? { address: `203.0.113.10:${port}`, reachable, latency_ms: 12 } : { address: `198.51.100.20:${port}`, reachable, error: 'timeout' }],
    };
  }
  if (parts.length === 6 && parts[2] === 'outlet' && parts[3] === 'domains' && parts[5] === 'check-dns' && method === 'POST') {
    const domain = parts[4];
    if (!state.outlet.domains.includes(domain)) fail('domain_not_found');
    return { checked_at: now(), records: dnsHostnames(domain).map((hostname) => ({ hostname, addresses: ['203.0.113.10'] })) };
  }
  if (path === '/api/v1/outlet' && method === 'PUT') {
    if (!input.domain) fail('invalid_domain');
    if (state.outlet.domain && input.domain !== state.outlet.domain) fail('domain_from_environment');
    if (state.outlet.domain !== input.domain && state.routes.length) fail('domain_has_routes');
    const mode = modeOf(input.mode, state.outlet.modes[state.outlet.domain]);
    delete state.outlet.modes[state.outlet.domain];
    state.outlet.domains[0] = input.domain;
    state.outlet.domain = input.domain;
    state.outlet.modes[input.domain] = mode;
    return outletResponse();
  }
  if (path === '/api/v1/outlet/domains' && method === 'POST') {
    const domain = input.domain?.trim().toLowerCase();
    if (!domain || !/^[a-z0-9.-]+$/.test(domain) || !domain.includes('.')) fail('invalid_domain');
    if (state.outlet.domains.some((current) => current === domain || current.endsWith(`.${domain}`) || domain.endsWith(`.${current}`))) fail('domain_exists');
    state.outlet.domains.push(domain);
    state.outlet.modes[domain] = modeOf(input.mode);
    return outletResponse();
  }
  if (parts.length === 6 && parts[2] === 'outlet' && parts[3] === 'domains' && parts[5] === 'mode' && method === 'PUT') {
    if (!state.outlet.domains.includes(parts[4])) fail('domain_not_found');
    state.outlet.modes[parts[4]] = modeOf(input.mode);
    return outletResponse();
  }
  if (parts.length === 5 && parts[2] === 'outlet' && parts[3] === 'domains' && method === 'DELETE') {
    const domain = parts[4];
    if (domain === state.outlet.domain) fail('primary_domain');
    if (!state.outlet.domains.includes(domain)) fail('domain_not_found');
    if (state.routes.some((route) => route.hostname.endsWith(`.${domain}`))) fail('domain_has_routes');
    state.outlet.domains = state.outlet.domains.filter((item) => item !== domain);
    delete state.outlet.modes[domain];
    return outletResponse();
  }
  if (path === '/api/v1/connectors' && method === 'GET') return { connectors: state.connectors.map((connector) => ({ ...connector })) };
  if (path === '/api/v1/usage' && method === 'GET') return usageSeries(new URL(url, 'http://localhost').searchParams);
  if (parts.length === 5 && parts[2] === 'connectors' && parts[4] === 'usage' && method === 'GET') {
    if (!state.connectors.some((connector) => connector.id === parts[3])) fail('connector_not_found');
    const params = new URL(url, 'http://localhost').searchParams;
    const periodHours = Number(params.get('hours') || 24);
    const serviceId = params.get('service_id') || '';
    if (![24, 168, 720].includes(periodHours)) fail('invalid_usage_period');
    const services = state.services.filter((service) => service.connector_id === parts[3]);
    const active = serviceId ? services.filter((service) => service.id === serviceId) : services;
    const hours = Array.from({ length: periodHours }, (_, index) => ({
      hour: new Date(Math.floor(Date.now() / 3600000) * 3600000 - (periodHours - 1 - index) * 3600000).toISOString(),
      requests: active.length ? Math.max(0, Math.round(12 + 10 * Math.sin(index / 3) + index / 2)) : 0,
      request_bytes: active.length ? 1200 + index * 120 : 0,
      response_bytes: active.length ? 18000 + index * 2200 : 0,
      error_responses: active.length && index === 16 ? 2 : 0,
    }));
    const share = (total: number, index: number): number => Math.floor(total / services.length) + (index < total % services.length ? 1 : 0);
    return { period_hours: periodHours, updated_at: now(), hours, services: services.map((service, index) => ({
      service_id: service.id, name: service.name,
      requests: share(hours.reduce((sum, hour) => sum + hour.requests, 0), index),
      request_bytes: share(hours.reduce((sum, hour) => sum + hour.request_bytes, 0), index),
      response_bytes: share(hours.reduce((sum, hour) => sum + hour.response_bytes, 0), index),
      error_responses: share(hours.reduce((sum, hour) => sum + hour.error_responses, 0), index),
    })) };
  }
  if (path === '/api/v1/connectors' && method === 'POST') {
    const name = input.name?.trim();
    if (!name) fail('invalid_connector');
    if (state.connectors.some((connector) => connector.name === name)) fail('connector_name_taken');
    const description = (input.description ?? '').trim();
    if (description.length > 500) fail('invalid_description');
    const connector: Connector = { id: id(), name, description, status: 'draft', created_at: now() };
    state.connectors.unshift(connector);
    return { ...connector };
  }
  if (parts.length === 4 && parts[2] === 'connectors' && method === 'PATCH') {
    const connector = state.connectors.find((item) => item.id === parts[3]);
    if (!connector) fail('connector_not_found');
    if (input.display_name !== undefined) {
      const displayName = input.display_name.trim();
      if ([...displayName].length > 64) fail('invalid_display_name');
      connector.display_name = displayName;
    }
    if (input.description !== undefined) {
      const description = input.description.trim();
      if (description.length > 500) fail('invalid_description');
      connector.description = description;
    }
    return { id: connector.id, display_name: connector.display_name ?? '', description: connector.description ?? '' };
  }
  if (parts.length === 4 && parts[2] === 'connectors' && method === 'DELETE') {
    const connectorIndex = state.connectors.findIndex((connector) => connector.id === parts[3]);
    if (connectorIndex < 0) fail('connector_not_found');
    state.connectors.splice(connectorIndex, 1);
    const serviceIds = new Set(state.services.filter((service) => service.connector_id === parts[3]).map((service) => service.id));
    state.services = state.services.filter((service) => service.connector_id !== parts[3]);
    state.routes = state.routes.filter((route) => !serviceIds.has(route.service_id));
    return {};
  }
  if (path === '/api/v1/diagnostics/trace' && method === 'GET') {
    return {
      observed_at: now(),
      outlet: { hostname: 'mock-outlet', region: 'Local development' },
      request: {
        remote_address: '127.0.0.1', host: 'localhost:5173', protocol: 'HTTP/1.1', method: 'GET',
        forwarded_for: '', forwarded_host: '', forwarded_proto: '', cloudflare_ray: '', cloudflare_colo: '', cloudflare_country: '',
      },
    };
  }
  if (parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'connectors' && parts[4] === 'services') {
    const connectorId = parts[3];
    if (!state.connectors.some((connector) => connector.id === connectorId)) fail('connector_not_found');
    if (parts.length === 5 && method === 'GET') {
      return { services: state.services.filter((service) => service.connector_id === connectorId).map((service) => ({ ...service, target: { ...service.target } })) };
    }
    if (parts.length === 5 && method === 'POST') {
      const name = input.name?.trim();
      const host = input.target?.host?.trim();
      const port = Number(input.target?.port);
      if (!name || !host || !Number.isInteger(port) || port < 1 || port > 65535) fail('invalid_service');
      if (state.services.some((item) => item.connector_id === connectorId && item.name === name)) fail('service_name_taken');
      if (state.services.some((item) => item.connector_id === connectorId && item.target.host.toLowerCase() === host.toLowerCase() && item.target.port === port)) fail('service_target_taken');
      const service: Service = {
        id: id(), connector_id: connectorId, name, protocol: input.protocol || 'http',
        target: { host, port }, enabled: true, observed_status: 'unknown', created_at: now(), updated_at: now(),
      };
      state.services.push(service);
      return { ...service, target: { ...service.target } };
    }
    if (parts.length === 6 && method === 'PATCH') {
      const service = state.services.find((item) => item.id === parts[5] && item.connector_id === connectorId);
      if (!service) fail('service_not_found');
      const host = input.target?.host?.trim();
      const port = Number(input.target?.port);
      if (!host || !Number.isInteger(port) || port < 1 || port > 65535) fail('invalid_service');
      if (state.services.some((item) => item.connector_id === connectorId && item.id !== service.id && item.target.host.toLowerCase() === host.toLowerCase() && item.target.port === port)) fail('service_target_taken');
      service.target = { host, port };
      service.observed_status = 'unknown';
      service.updated_at = now();
      return { ...service, target: { ...service.target } };
    }
    if (parts.length === 6 && method === 'DELETE') {
      const serviceId = parts[5];
      if (state.routes.some((route) => route.service_id === serviceId)) fail('service_has_routes');
      const index = state.services.findIndex((service) => service.id === serviceId && service.connector_id === connectorId);
      if (index < 0) fail('service_not_found');
      state.services.splice(index, 1);
      return {};
    }
  }
  if (parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'connectors' && parts[4] === 'enrollment-tokens' && method === 'POST') {
    const connector = state.connectors.find((item) => item.id === parts[3]);
    if (!connector) fail('connector_not_found');
    if (new URL(url, 'http://localhost').searchParams.get('reset') === 'true') {
      connector.status = 'draft';
      delete connector.last_seen;
    }
    const token = `mock_enrollment_${Math.random().toString(36).slice(2, 18)}`;
    return { token };
  }
  if (parts.length === 5 && parts[2] === 'connectors' && parts[4] === 'mock-connect' && method === 'POST') {
    const connector = state.connectors.find((item) => item.id === parts[3]);
    if (!connector) fail('connector_not_found');
    connector.status = 'connected';
    connector.last_seen = now();
    return { ...connector };
  }
  if (parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'services' && parts[4] === 'routes') {
    const serviceId = parts[3];
    if (!state.services.some((service) => service.id === serviceId)) fail('service_not_found');
    if (parts.length === 5 && method === 'GET') {
      return { routes: state.routes.filter((route) => route.service_id === serviceId).map(routeView) };
    }
    if (parts.length === 5 && method === 'POST') {
      const hostname = input.hostname?.trim().toLowerCase();
      if (!hostname || hostname === adminHostname(state.outlet.domain) || !state.outlet.domains.some((domain) => hostname.endsWith(`.${domain}`))) fail('invalid_route');
      const sharing = state.routes.filter((route) => route.hostname === hostname);
      if (sharing.some((route) => route.service_id === serviceId) || (sharing.length && !(input as { shared?: boolean }).shared)) fail('hostname_taken');
      const route = newRoute({ id: id(), service_id: serviceId, kind: 'http', hostname, enabled: true, desired_revision: 1, applied_revision: 0, created_at: now(), updated_at: now(), priority: sharing.length ? Math.max(...sharing.map((other) => other.priority)) + 1 : 0 });
      state.routes.push(route);
      return routeView(route);
    }
    if (parts.length === 6 && method === 'DELETE') {
      const index = state.routes.findIndex((route) => route.id === parts[5] && route.service_id === serviceId);
      if (index < 0) fail('route_not_found');
      const [removed] = state.routes.splice(index, 1);
      if (!state.routes.some((route) => route.hostname === removed.hostname)) delete state.policies[removed.hostname];
      return {};
    }
  }
  if (parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'addresses' && parts.length === 4 && method === 'PUT') {
    const hostname = decodeURIComponent(parts[3]);
    const sharing = state.routes.filter((route) => route.hostname === hostname);
    if (!sharing.length) fail('address_not_found');
    const body = input as { mode?: string; sticky?: boolean; order?: string[]; current?: { mode: string; sticky: boolean; order: string[] } };
    const order = body.order ?? [];
    if (body.current) {
      const saved = state.policies[hostname] ?? { mode: 'failover', sticky: false };
      const savedOrder = [...sharing].sort((a, b) => a.priority - b.priority).map((route) => route.id);
      if (saved.mode !== body.current.mode || saved.sticky !== body.current.sticky || savedOrder.join() !== body.current.order.join()) fail('address_changed');
    }
    if ((body.mode !== 'failover' && body.mode !== 'balance') || order.length !== sharing.length || new Set(order).size !== order.length || !sharing.every((route) => order.includes(route.id))) fail('invalid_address_policy');
    order.forEach((routeId, priority) => { const route = state.routes.find((item) => item.id === routeId); if (route) route.priority = priority; });
    state.policies[hostname] = { mode: body.mode as AddressMode, sticky: body.mode === 'balance' && Boolean(body.sticky) };
    return {};
  }
  fail('mock_endpoint_not_implemented');
}
