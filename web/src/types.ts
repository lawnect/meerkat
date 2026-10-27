export type PageKey = 'overview' | 'connectors' | 'domains' | 'access' | 'diagnostics' | 'settings';
export type AuthMode = 'loading' | 'setup' | 'login' | 'unavailable' | 'dashboard';

// 로그인 화면이 보여 줄 방식(GET /api/v1/setup/status의 sign_in). 공개 관리자 주소에서 SSO가 켜져 있을 때만 sso가 true입니다.
export interface SignInOptions {
  password: boolean;
  sso: boolean;
  sso_label: string; // 공급자 이름. 비어 있을 수 있습니다
  recovery?: boolean; // 로컬 관리 포트(SSH)에서만 true: admin.token으로 관리자 비밀번호를 설정할 수 있음
}

export interface OutletTunnel {
  address: string;
  default_address: string;
  custom: boolean;
}

// wildcard: *.도메인 레코드 하나로 모든 하위 주소를 Outlet으로 보냅니다.
// hosts: 관리 주소와 공개 주소마다 DNS 레코드를 따로 만듭니다(루트나 다른 하위 주소는 다른 서버에 둘 수 있습니다).
export type DomainMode = 'wildcard' | 'hosts';

export interface Outlet {
  domain?: string;
  domains?: string[];
  modes?: Record<string, DomainMode>;
  // 관리 화면과 Connector API 주소. 대표 도메인을 처음 등록할 때 정한 8글자 이름을 씁니다.
  admin_hostname?: string;
  // environment: 환경 변수(MEERKAT_DOMAIN)로 정한 대표 도메인이라 화면에서 바꿀 수 없음.
  domain_source?: 'environment' | 'database';
  // Outlet 프로그램 버전.
  version?: string;
  tunnel?: OutletTunnel;
}

export interface TunnelCheck {
  checked_at: string;
  address: string;
  reachable: boolean;
  error?: string;
  results: Array<{ address: string; reachable: boolean; latency_ms?: number; error?: string }>;
}

export interface DomainDNSCheck {
  checked_at: string;
  records: Array<{ hostname: string; addresses: string[]; error?: string }>;
}

export interface Connector {
  id: string;
  // 설치 명령과 컨테이너 이름에 쓰는 바꿀 수 없는 이름.
  name: string;
  // 화면에 보여줄 이름(최대 64자). 비어 있으면 name을 씁니다.
  display_name?: string;
  // 관리자가 적어 두는 메모. 화면에만 쓰입니다.
  description?: string;
  status: string;
  created_at: string;
  last_seen?: string;
}

export interface Target {
  host: string;
  port: number;
}

export interface Service {
  id: string;
  connector_id: string;
  name: string;
  protocol: string;
  target: Target;
  enabled: boolean;
  observed_status: string;
  created_at: string;
  updated_at: string;
}

export interface Route {
  id: string;
  service_id: string;
  kind: string;
  hostname: string;
  enabled: boolean;
  desired_revision: number;
  applied_revision: number;
  last_error?: string;
  created_at: string;
  updated_at: string;
  // 같은 공개 주소를 여러 서비스가 함께 쓸 때의 순서(장애 조치 우선순위, 작을수록 먼저)와 주소의 분산 방식.
  priority: number;
  address_mode: AddressMode;
  address_sticky: boolean;
  address_backends: number;
}

export type AddressMode = 'failover' | 'balance';

export interface ServiceRecord {
  connector: Connector;
  service: Service;
  routes: Route[];
}

export interface DashboardData {
  connectors: Connector[];
  services: Service[];
  routes: Route[];
  records: ServiceRecord[];
  outlet: Outlet;
}

export interface UsageHour {
  hour: string;
  requests: number;
  request_bytes: number;
  response_bytes: number;
  error_responses: number;
}

export interface ConnectorUsage {
  period_hours: number;
  updated_at?: string;
  hours: UsageHour[];
  services: { service_id: string; name: string; requests: number; request_bytes: number; response_bytes: number; error_responses: number }[];
}

export interface EnrollmentToken {
  token: string;
}

export interface TraceSnapshot {
  observed_at: string;
  outlet: { hostname: string; region?: string };
  request: {
    remote_address: string;
    host: string;
    protocol: string;
    method: string;
    forwarded_for?: string;
    forwarded_host?: string;
    forwarded_proto?: string;
    cloudflare_ray?: string;
    cloudflare_colo?: string;
    cloudflare_country?: string;
  };
}

export type UsageUnit = 'hour' | 'day' | 'month' | 'year';

export interface UsageBucket {
  start: string;
  requests: number;
  request_bytes: number;
  response_bytes: number;
  error_responses: number;
}

export interface UsageSeries {
  unit: UsageUnit;
  updated_at?: string;
  buckets: UsageBucket[];
  // 같은 기간의 터널별 합계. 처리 건수가 많은 순입니다.
  connectors: Array<{
    connector_id: string;
    name: string;
    requests: number;
    request_bytes: number;
    response_bytes: number;
    error_responses: number;
  }>;
}

// 텔레그램 알림과 로그인 2단계 인증 설정(GET /api/v1/alerts).
export interface AlertRules {
  tunnel_down: { enabled: boolean; after_minutes: number };
  route_error: { enabled: boolean };
  dns_failure: { enabled: boolean };
  error_spike: { enabled: boolean; threshold_percent: number; min_requests: number };
  login: { enabled: boolean };
  rate_limit: { enabled: boolean; min_blocked: number };
}

export interface AlertEvent {
  id: number;
  kind: keyof AlertRules | string;
  subject: string;
  message: string;
  opened_at: string;
  resolved_at?: string | null;
  sent: boolean;
  error?: string;
}

export interface AlertSettings {
  // source: 관리 화면에서 저장한 값(database), 환경 변수(environment), 없음(default). environment: 환경 변수에 봇이 있는지.
  telegram: { configured: boolean; enabled: boolean; chat_id: string; token_hint: string; verified_at?: string | null; source: SettingSource; environment: boolean };
  rules: AlertRules;
  events: AlertEvent[];
  login_second_factor: boolean;
}

// 감사 로그(GET /api/v1/audit). 기록은 지울 수 없습니다.
export interface AuditEvent {
  id: string;
  created_at: string;
  actor_email?: string | null;
  action: string;
  resource_type: string;
  resource_id: string;
  source: 'public' | 'local' | string;
  ip?: string | null;
  user_agent?: string | null;
  detail?: Record<string, unknown> | null;
}

export interface AuditPage {
  events: AuditEvent[];
  total: number;
}

// 요청 제한(GET/PUT /api/v1/security/rate-limit). rps와 burst가 0이면 그 제한은 꺼집니다.
export interface RateBucket {
  rps: number;
  burst: number;
}

export interface RateEndpointRule extends RateBucket {
  host: string;
  path_prefix: string;
  per: 'total' | 'ip';
}

export interface RateLimitSettings {
  enabled: boolean;
  per_ip: RateBucket;
  per_subnet: RateBucket;
  per_address: RateBucket;
  endpoints: RateEndpointRule[];
}

// 연결 제한(GET/PUT /api/v1/security/connection-limits). 0이면 그 제한은 꺼집니다.
export interface HostUpload {
  host: string;
  max_upload_mb: number;
}

export interface ConnectionLimits {
  max_upload_mb: number;
  per_ip_concurrent: number;
  total_concurrent: number;
  idle_timeout_seconds: number;
  min_upload_kb_per_second: number;
  hosts: HostUpload[];
}

// 차단 규칙(GET/PUT /api/v1/security/block-rules). 조건 중 하나라도 맞는 요청은 403으로 거부합니다.
export type BlockOperator = 'equals' | 'starts_with' | 'ends_with' | 'contains' | 'matches';

export interface BlockCondition {
  operator: BlockOperator;
  value: string; // equals·starts_with는 /로 시작, matches는 RE2 정규식(대소문자 구분 안 함), 512바이트 이하
}

export interface BlockRule {
  id: string; // 새 규칙은 '', 서버가 부여합니다
  name: string; // 1–64자
  enabled: boolean;
  hosts: string[]; // 호스트 이름. []이면 모든 공개 주소
  conditions: BlockCondition[]; // 1–50개. 하나라도 맞으면 차단합니다
}

export interface BlockRulesView {
  rules: BlockRule[];
  hits: Record<string, number>; // 규칙 id별로 Outlet 시작 이후 차단한 요청 수. 없는 id도 있습니다
  addresses: string[]; // 규칙을 한정할 수 있는 공개 주소(호스트 이름), 정렬됨
}

// OpenID Connect 로그인(GET/PUT /api/v1/security/sso). 클라이언트 시크릿은 응답에 없고, PUT에서 비우면 저장된 값을 유지합니다.
// 설정값의 출처. 관리 화면에서 저장한 값이 환경 변수보다 우선합니다.
export type SettingSource = 'database' | 'environment' | 'default';

export interface SsoSettings {
  enabled: boolean;
  label: string; // 32자 이하, 로그인 버튼에 표시
  issuer: string; // https URL, 끝에 / 없음
  client_id: string;
  client_secret_set: boolean;
  client_secret_hint: string; // 끝 4자. 짧거나 없으면 ''
  scopes: string[]; // 항상 'openid' 포함
  groups_claim: string; // 기본 'groups'
  allowed_users: string[]; // 이메일 또는 '@도메인'
  allowed_groups: string[];
  password_login: boolean; // 공개 주소에서 비밀번호 로그인 허용
  redirect_uri: string; // 공급자에 등록할 주소
  post_logout_redirect_uri: string; // 공급자에 등록할 로그아웃 후 리디렉션 주소
  available: boolean; // 도메인(관리자 호스트)이 생기기 전에는 false(redirect_uri는 '')
  source: SettingSource;
  environment: boolean; // 환경 변수에 SSO 설정이 있는지. 있으면 source도 environment이고 관리 화면에서 바꿀 수 없습니다
}

// 접근 보호용 OpenID Connect 공급자(GET/PUT /api/v1/security/access-provider). 관리자 SSO와 별개이며, 클라이언트 시크릿은 응답에 없고 PUT에서 비우거나 생략하면 저장된 값을 유지합니다.
export interface AccessProviderView {
  enabled: boolean;
  issuer: string; // https URL, 끝에 / 없음
  client_id: string;
  client_secret_set: boolean;
  client_secret_hint: string; // 끝 4자. 짧거나 없으면 ''
  scopes: string[]; // 항상 'openid' 포함
  groups_claim: string; // 기본 'groups'
  session_hours: number; // 1–720. 보호된 주소에서 로그인이 유지되는 시간
  redirect_uri: string; // 공급자에 등록할 주소(https://<관리자 호스트>/api/v1/access/callback)
  available: boolean; // 도메인(관리자 호스트)이 생기기 전에는 false(redirect_uri는 '')
}

// 접근 정책(GET/PUT /api/v1/security/access-policies). 정책은 목록 순서대로 확인하며, 요청에 처음 일치하는 활성화된 정책 하나가 적용됩니다.
export type AccessAction = 'allow' | 'bypass' | 'block'; // 로그인 후 허용 대상만 / 로그인 없이 누구나 / 모두 거부
export const accessMethods = ['GET', 'POST', 'PUT', 'PATCH', 'DELETE', 'OPTIONS'] as const; // 정책을 한정할 수 있는 메서드. GET은 HEAD 포함
export type AccessMethod = (typeof accessMethods)[number];

export interface AccessPolicy {
  id: string; // 새 정책은 '', 서버가 부여합니다
  name: string; // 1–64자
  enabled: boolean;
  action: AccessAction;
  hosts: string[]; // 호스트 이름 또는 '*.도메인', 1개 이상
  all_paths: boolean; // true이면 호스트의 모든 경로, false이면 conditions가 일치하는 경로만
  conditions: BlockCondition[]; // all_paths가 false일 때 1–50개. 하나라도 맞으면 일치합니다
  methods: AccessMethod[]; // 이 메서드의 요청에만 적용. 비어 있으면 모든 메서드, 서버가 accessMethods 순서로 정리합니다
  require_token: boolean; // true이면 그 주소와 클라이언트에 유효한 접근 토큰을 보낸 요청에만 적용
  tokens: string[]; // require_token일 때 받는 토큰 ID. 비어 있으면 모든 토큰, 있는 토큰만
  // 아래는 allow일 때만 의미가 있고, 서버는 bypass·block이면 비웁니다.
  allow_everyone: boolean; // 접근 보호 SSO로 로그인한 모든 사용자. 켜면 사용자·그룹·관리자 허용은 비웁니다
  allowed_users: string[]; // 이메일 또는 '@도메인', 소문자
  allowed_groups: string[]; // 사용자와 그룹을 모두 지정하면 둘 다 맞아야 합니다
  allow_admins: boolean; // Meerkat 관리자의 이메일도 허용. allow에는 전체·사용자·그룹·관리자 중 하나가 필요합니다
}

// 접근 토큰(GET/POST /api/v1/security/access-tokens, PUT·DELETE …/{id}). 로그인할 수 없는 클라이언트가
// X-Meerkat-Access-Token 헤더에 값을 보내면, 토큰의 이용 범위(주소, IP 대역) 안에서 그 토큰이 필요한 정책에 해당합니다.
export interface AccessToken {
  id: string;
  name: string; // 1–64자
  hosts: string[]; // 쓸 수 있는 주소: 호스트 이름 또는 '*.도메인', 1–100개
  ranges: string[]; // 쓸 수 있는 클라이언트 IP 대역(CIDR, 단일 주소는 /32·/128로 저장), 비어 있으면 모든 IP. 0–50개
  created_at: string; // RFC 3339
  expires_at: string; // RFC 3339, ''이면 만료 없음
  last_used_at: string; // RFC 3339, ''이면 사용 기록 없음
  policies: Array<{ id: string; name: string }>; // 이 토큰을 지정했거나, 모든 토큰을 받으면서 주소가 겹치는 정책
}

// 접근 정책이 판정한 요청 수(GET /api/v1/security/access-metrics?hours=24|168|720). 통과는 우회, 또는 정책이 받는 세션·토큰,
// 로그인 요구는 로그인으로 보낸 요청(브라우저가 아니면 401), 거부는 차단, 정책이 들이지 않는 세션·토큰, 공급자 꺼짐입니다.
export interface AccessCounts {
  passed: number;
  challenged: number;
  denied: number;
}

export interface AccessMetrics {
  period_hours: number;
  hours: Array<AccessCounts & { hour: string }>; // 기간의 모든 시간, 오래된 것부터 (UTC 정각, RFC 3339)
  policies: Record<string, AccessCounts>; // 정책 ID별 기간 합계. 지워진 정책도 남습니다
}

// 접근 보호 요청 기록(GET·PUT /api/v1/security/access-log). 별도 파일에 30일, 최대 50만 건·256MiB까지 둡니다.
// 거부·로그인 요구는 모두, 통과는 로그인한 사용자나 토큰으로 들어온 것만 남깁니다.
export type AccessDecision = keyof AccessCounts;
export interface AccessLogEntry {
  id: number;
  at: string; // RFC 3339
  host: string;
  method: string;
  path: string; // 쿼리 없이, 512바이트까지
  client: string; // 클라이언트 IP
  policy: string; // 정책 ID
  decision: AccessDecision;
  reason: string; // bypass·session(통과), no_session(로그인 요구), blocked·not_allowed·token_*·unavailable(거부)
  email: string;
  token: string; // 토큰 ID
}

export interface AccessLogView {
  enabled: boolean; // 기록 스위치. 처음에는 꺼져 있습니다
  available: boolean; // 기록 파일을 열었는지
  entries: AccessLogEntry[]; // 최신순
  next: number; // 다음 쪽을 받을 before 값, 0이면 끝
  dropped: number; // Outlet 시작 이후 대기열이 넘쳐 기록하지 못한 수
}

export interface AccessTokensView {
  tokens: AccessToken[]; // 만든 순서
  addresses: string[]; // 토큰을 한정할 수 있는 공개 주소(호스트 이름), 정렬됨
}

// POST 본문. PUT은 expires_days 없이 같은 모양이며 값은 바뀌지 않습니다.
export interface AccessTokenInput {
  name: string;
  hosts: string[];
  ranges: string[];
  expires_days?: number; // 0이면 만료 없음, 1–3650
}

// POST 응답. value는 이때 한 번만 보여 줍니다.
export interface CreatedAccessToken {
  token: AccessToken;
  value: string;
}

export interface AccessPoliciesView {
  policies: AccessPolicy[];
  addresses: string[]; // 정책을 한정할 수 있는 공개 주소(호스트 이름), 정렬됨
  damaged?: boolean; // 저장된 정책이 검사를 통과하지 못해, 지정한 주소(읽을 수 없으면 모든 공개 주소)를 차단 중. 다시 저장하면 해제
}
