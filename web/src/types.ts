export type PageKey = 'overview' | 'connectors' | 'domains' | 'diagnostics' | 'settings';
export type AuthMode = 'loading' | 'setup' | 'login' | 'unavailable' | 'dashboard';

// 로그인 화면이 보여 줄 방식(GET /api/v1/setup/status의 sign_in). 공개 관리자 주소에서 SSO가 켜져 있을 때만 sso가 true입니다.
export interface SignInOptions {
  password: boolean;
  sso: boolean;
  sso_label: string; // 공급자 이름. 비어 있을 수 있습니다
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
}

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
  telegram: { configured: boolean; enabled: boolean; chat_id: string; token_hint: string; verified_at?: string | null };
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
  available: boolean; // 도메인(관리자 호스트)이 생기기 전에는 false(redirect_uri는 '')
}
