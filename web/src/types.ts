export type PageKey = 'overview' | 'connectors' | 'services' | 'domains' | 'diagnostics';
export type AuthMode = 'loading' | 'setup' | 'login' | 'unavailable' | 'dashboard';

export interface Outlet {
  domain?: string;
  domains?: string[];
}

export interface Connector {
  id: string;
  name: string;
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

export interface EnrollmentToken {
  token: string;
  expires_at: string;
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
