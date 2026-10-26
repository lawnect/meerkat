import type { AccessPoliciesView, AccessPolicy, AccessProviderView, AddressMode, AlertRules, AlertSettings, AuditEvent, BlockCondition, BlockOperator, BlockRule, BlockRulesView, ConnectionLimits, RateLimitSettings, Connector, DomainMode, Outlet, Route, Service, SsoSettings, Target, UsageBucket, UsageSeries, UsageUnit } from './types';

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

function seed() {
  const day = 86400000;
  const connectors: Connector[] = [{ id: seedId('connector-home'), name: 'home-server', description: '집 서버 · whoami 테스트 앱', status: 'connected', created_at: now(), last_seen: now() }];
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
    connectors.push({ id: connectorId, name, display_name: SEED_LABELS[name] ?? '', description: SEED_NOTES[name] ?? '', status, created_at: created, last_seen: seen });
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
const alerts: AlertSettings = {
  telegram: { configured: true, enabled: true, chat_id: '-1001234567890', token_hint: 'x9Qa', verified_at: hoursAgo(50), source: 'database', environment: false },
  rules: {
    tunnel_down: { enabled: true, after_minutes: 5 },
    route_error: { enabled: true },
    dns_failure: { enabled: true },
    error_spike: { enabled: true, threshold_percent: 5, min_requests: 50 },
    login: { enabled: true },
    rate_limit: { enabled: true, min_blocked: 500 },
  },
  events: [
    { id: 7, kind: 'login', subject: 'dev@example.test', message: 'Admin login: dev@example.test from 203.0.113.7 (Chrome on macOS)', opened_at: hoursAgo(1), resolved_at: hoursAgo(1), sent: true },
    { id: 6, kind: 'tunnel_down', subject: 'office-printer', message: 'Tunnel office-printer disconnected (5 min)', opened_at: hoursAgo(26), resolved_at: null, sent: true },
    { id: 5, kind: 'tunnel_down', subject: 'legacy-erp', message: 'Tunnel legacy-erp disconnected (5 min)', opened_at: hoursAgo(30), resolved_at: null, sent: true },
    { id: 4, kind: 'error_spike', subject: 'example.test', message: '5xx rate 7.4% (41 of 552 requests) this hour', opened_at: hoursAgo(5), resolved_at: hoursAgo(4), sent: true },
    { id: 3, kind: 'dns_failure', subject: 'dev.demo.test', message: 'dev.demo.test does not resolve', opened_at: hoursAgo(20), resolved_at: hoursAgo(19), sent: true },
    { id: 2, kind: 'route_error', subject: 'erp.example.com', message: 'Certificate for erp.example.com could not be issued', opened_at: hoursAgo(40), resolved_at: null, sent: false, error: 'telegram_unreachable' },
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
  { id: 'hidden-files', name: 'Hidden files', enabled: true, hosts: [], conditions: [{ operator: 'starts_with', value: '/.' }] },
  {
    id: 'scanners',
    name: 'Vulnerability scanners',
    enabled: false,
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
let blockHits: Record<string, number> = { 'hidden-files': 1284 };
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
  { id: 'vault', name: 'Vault members', enabled: true, hosts: ['vault.example.test'], all_paths: true, conditions: [], allowed_users: ['@example.test'], allowed_groups: [], allow_admins: true },
];
const accessPoliciesResponse = (): AccessPoliciesView => structuredClone({ policies: accessPolicies, addresses: publicAddresses() });

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
// 서버처럼 값을 정리해 돌려주고, 올바르지 않으면 null입니다.
function normalizeCondition(raw: unknown): BlockCondition | null {
  const entry = (raw ?? {}) as Partial<BlockCondition>;
  if (typeof entry.operator !== 'string' || !BLOCK_OPERATORS.includes(entry.operator) || typeof entry.value !== 'string') return null;
  const operator = entry.operator as BlockOperator;
  let value = entry.value.trim();
  if (operator !== 'matches') {
    // 요청 경로처럼 조각 끝의 점과 공백을 지웁니다. 조각이 끝나는 곳이 확실한 / 앞과, 같음·다음으로 끝남 값의 끝에서만 지웁니다.
    const whole = operator === 'equals' || operator === 'ends_with';
    const parts = value.toLowerCase().replaceAll('\\', '/').split('/');
    const trimmed = parts.map((part, index) => (index < parts.length - 1 || whole ? part.replace(/[. ]+$/, '') : part)).join('/');
    value = trimmed || value.toLowerCase();
  }
  if (operator === 'equals') value = value.replace(/\/{2,}/g, '/').replace(/(.)\/$/, '$1');
  if (!value || new TextEncoder().encode(value).length > 512 || /[\u0000-\u001f\u007f]/.test(value)) return null;
  if ((operator === 'equals' || operator === 'starts_with') && !value.startsWith('/')) return null;
  if (operator === 'matches' && !validRegex(value)) return null;
  return { operator, value };
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
  const boundary = (index: number): Date => {
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

function usageSeries(params: URLSearchParams): UsageSeries {
  const unit = params.get('unit') as UsageUnit;
  const rawFrom = params.get('from') || '';
  const rawTo = params.get('to') || '';
  if (!USAGE_UNITS.includes(unit) || !RFC3339.test(rawFrom) || !RFC3339.test(rawTo)) fail('invalid_usage_range');
  const from = new Date(rawFrom);
  const to = new Date(rawTo);
  if (!(from.getTime() < to.getTime())) fail('invalid_usage_range');
  const zone = params.get('tz');
  if (zone) {
    // The mock buckets in the browser's zone but still rejects unknown names.
    try { new Intl.DateTimeFormat('en', { timeZone: zone }); } catch { fail('invalid_usage_range'); }
  }
  const boundaries = usageBoundaries(unit, from, to);
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

export async function mockApi(url: string, options: RequestInit = {}): Promise<unknown> {
  const method = options.method || 'GET';
  const path = new URL(url, 'http://localhost').pathname;
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent);
  const input = bodyOf(options.body);

  // 미리보기 서버는 로컬 관리 포트처럼 계정 복구 링크도 보여 줍니다.
  if (path === '/api/v1/setup/status' && method === 'GET') return { setup_complete: true, version: '0.1.0', sign_in: { password: true, sso: true, sso_label: 'Google Workspace', recovery: true } };
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
    const rules = input as unknown as AlertRules;
    const minutes = rules.tunnel_down?.after_minutes;
    const percent = rules.error_spike?.threshold_percent;
    const minimum = rules.error_spike?.min_requests;
    if (!(rules.rate_limit?.min_blocked >= 1) || !(minutes >= 1 && minutes <= 1440) || !(percent >= 0.1 && percent <= 100) || !(minimum >= 1 && minimum <= 1000000)) fail('invalid_alert_rules');
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
      return { id: rule.id, name, enabled: rule.enabled, hosts, conditions };
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
      if (!name || name.length > 64 || /[\u0000-\u001f\u007f]/.test(name) || typeof policy.id !== 'string' || typeof policy.enabled !== 'boolean' || typeof policy.all_paths !== 'boolean' || typeof policy.allow_admins !== 'boolean') fail('invalid_access_policies');
      if (!Array.isArray(policy.hosts) || policy.hosts.length < 1 || policy.hosts.length > 100) fail('invalid_access_policies');
      const hosts = [...new Set(policy.hosts.map((host) => (typeof host === 'string' ? host.trim().toLowerCase() : '')))];
      if (hosts.some((host) => !validBlockHost(host))) fail('invalid_access_policies');
      // 모든 경로를 보호할 때는 조건을 버리고, 선택한 경로는 조건이 1개 이상 있어야 합니다.
      let conditions: BlockCondition[] = [];
      if (!policy.all_paths) {
        if (!Array.isArray(policy.conditions) || policy.conditions.length < 1 || policy.conditions.length > 50) fail('invalid_access_policies');
        const seen = new Set<string>();
        // 요청 경로는 디코딩하고 ;매개변수를 뺀 형태로 비교하므로, 그런 값은 거부합니다.
        conditions = policy.conditions.map((raw) => normalizeCondition(raw) ?? fail('invalid_access_policies')).map((entry) => (entry.operator !== 'matches' && /%[0-9a-f]{2}|;/i.test(entry.value) ? fail('invalid_access_policies') : entry)).filter((entry) => {
          const key = `${entry.operator}\0${entry.value}`;
          if (seen.has(key)) return false;
          seen.add(key);
          return true;
        });
      }
      const allowedUsers = entries(policy.allowed_users, true, (entry) => /^[^\s@]+@[^\s@]+$/.test(entry) || /^@[^\s@]+$/.test(entry));
      const allowedGroups = entries(policy.allowed_groups, false, (entry) => !/[\u0000-\u001f\u007f]/.test(entry));
      // 아무도 허용하지 않는 정책은 주소를 모두에게 닫아 버립니다.
      if (allowedUsers.length + allowedGroups.length === 0 && !policy.allow_admins) fail('invalid_access_policies');
      return { id: policy.id, name, enabled: policy.enabled, hosts, all_paths: policy.all_paths, conditions, allowed_users: allowedUsers, allowed_groups: allowedGroups, allow_admins: policy.allow_admins };
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
