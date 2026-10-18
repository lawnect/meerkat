import type { Connector, Route, Service, Target } from './types';

const now = (): string => new Date().toISOString();
const id = (kind: string): string => `mock-${kind}-${Math.random().toString(36).slice(2, 9)}`;

interface MockInput {
  domain?: string;
  email?: string;
  hostname?: string;
  name?: string;
  protocol?: string;
  target?: Partial<Target>;
}

const state: {
  outlet: { domain: string; domains: string[] };
  connectors: Connector[];
  services: Service[];
  routes: Route[];
} = {
  outlet: { domain: 'example.test', domains: ['example.test'] },
  connectors: [{ id: 'mock-connector-home', name: 'home-server', status: 'connected', created_at: now(), last_seen: now() }],
  services: [{
    id: 'mock-service-whoami', connector_id: 'mock-connector-home', name: 'whoami', protocol: 'http',
    target: { host: 'app', port: 80 }, enabled: true, observed_status: 'connected', created_at: now(), updated_at: now(),
  }],
  routes: [{
    id: 'mock-route-whoami', service_id: 'mock-service-whoami', kind: 'http', hostname: 'whoami.example.test',
    enabled: true, desired_revision: 1, applied_revision: 1, created_at: now(), updated_at: now(),
  }],
};

function fail(code: string): never {
  throw new Error(code);
}

function bodyOf(body: BodyInit | null | undefined): MockInput {
  if (typeof body !== 'string') return {};
  try { return JSON.parse(body) as MockInput; } catch { return {}; }
}

export async function mockApi(url: string, options: RequestInit = {}): Promise<unknown> {
  const method = options.method || 'GET';
  const path = new URL(url, 'http://localhost').pathname;
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent);
  const input = bodyOf(options.body);

  if (path === '/api/v1/setup/status' && method === 'GET') return { setup_complete: true };
  if (path === '/api/v1/setup/admin' && method === 'POST') return { status: 'created' };
  if (path === '/api/v1/session' && method === 'POST') return { admin: { email: input.email || 'dev@example.test' }, expires_at: now() };
  if (path === '/api/v1/session' && method === 'DELETE') return {};
  if (path === '/api/v1/outlet' && method === 'GET') return { ...state.outlet };
  if (path === '/api/v1/outlet' && method === 'PUT') {
    if (!input.domain) fail('invalid_domain');
    if (state.outlet.domain !== input.domain && state.routes.length) fail('domain_has_routes');
    state.outlet.domains[0] = input.domain;
    state.outlet.domain = input.domain;
    return { ...state.outlet };
  }
  if (path === '/api/v1/outlet/domains' && method === 'POST') {
    const domain = input.domain?.trim().toLowerCase();
    if (!domain || !/^[a-z0-9.-]+$/.test(domain) || !domain.includes('.')) fail('invalid_domain');
    if (state.outlet.domains.some((current) => current === domain || current.endsWith(`.${domain}`) || domain.endsWith(`.${current}`))) fail('domain_exists');
    state.outlet.domains.push(domain);
    return { ...state.outlet, domains: [...state.outlet.domains] };
  }
  if (parts.length === 5 && parts[2] === 'outlet' && parts[3] === 'domains' && method === 'DELETE') {
    const domain = parts[4];
    if (domain === state.outlet.domain) fail('primary_domain');
    if (!state.outlet.domains.includes(domain)) fail('domain_not_found');
    if (state.routes.some((route) => route.hostname.endsWith(`.${domain}`))) fail('domain_has_routes');
    state.outlet.domains = state.outlet.domains.filter((item) => item !== domain);
    return { ...state.outlet, domains: [...state.outlet.domains] };
  }
  if (path === '/api/v1/connectors' && method === 'GET') return { connectors: state.connectors.map((connector) => ({ ...connector })) };
  if (path === '/api/v1/connectors' && method === 'POST') {
    const name = input.name?.trim();
    if (!name) fail('invalid_connector');
    if (state.connectors.some((connector) => connector.name === name)) fail('connector_name_taken');
    const connector: Connector = { id: id('connector'), name, status: 'draft', created_at: now() };
    state.connectors.unshift(connector);
    return { ...connector };
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
      if (state.services.some((service) => service.connector_id === connectorId)) fail('connector_service_limit');
      const name = input.name?.trim();
      const host = input.target?.host;
      const port = input.target?.port;
      if (!name || !host || !port) fail('invalid_service');
      const service: Service = {
        id: id('service'), connector_id: connectorId, name, protocol: input.protocol || 'http',
        target: { host, port: Number(port) }, enabled: true, observed_status: 'unknown', created_at: now(), updated_at: now(),
      };
      state.services.push(service);
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
    if (!state.connectors.some((connector) => connector.id === parts[3])) fail('connector_not_found');
    if (!state.services.some((service) => service.connector_id === parts[3])) fail('connector_service_required');
    const token = `mock_enrollment_${Math.random().toString(36).slice(2, 18)}`;
    return { token, expires_at: new Date(Date.now() + 10 * 60 * 1000).toISOString() };
  }
  if (parts[0] === 'api' && parts[1] === 'v1' && parts[2] === 'services' && parts[4] === 'routes') {
    const serviceId = parts[3];
    if (!state.services.some((service) => service.id === serviceId)) fail('service_not_found');
    if (parts.length === 5 && method === 'GET') {
      return { routes: state.routes.filter((route) => route.service_id === serviceId).map((route) => ({ ...route })) };
    }
    if (parts.length === 5 && method === 'POST') {
      const hostname = input.hostname?.trim().toLowerCase();
      if (!hostname || !state.outlet.domains.some((domain) => hostname.endsWith(`.${domain}`))) fail('invalid_route');
      if (state.routes.some((route) => route.hostname === hostname)) fail('hostname_taken');
      const route: Route = { id: id('route'), service_id: serviceId, kind: 'http', hostname, enabled: true, desired_revision: 1, applied_revision: 0, created_at: now(), updated_at: now() };
      state.routes.push(route);
      return { ...route };
    }
    if (parts.length === 6 && method === 'DELETE') {
      const index = state.routes.findIndex((route) => route.id === parts[5] && route.service_id === serviceId);
      if (index < 0) fail('route_not_found');
      state.routes.splice(index, 1);
      return {};
    }
  }
  fail('mock_endpoint_not_implemented');
}
