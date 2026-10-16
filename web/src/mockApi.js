const now = () => new Date().toISOString();
const id = (kind) => `mock-${kind}-${Math.random().toString(36).slice(2, 9)}`;

const state = {
  outlet: { domain: 'example.test' },
  connectors: [{ id: 'mock-connector-home', name: 'home-server', status: 'connected', created_at: now() }],
  services: [{
    id: 'mock-service-whoami', connector_id: 'mock-connector-home', name: 'whoami', protocol: 'http',
    target: { host: 'app', port: 80 }, enabled: true, observed_status: 'connected', created_at: now(), updated_at: now(),
  }],
  routes: [{
    id: 'mock-route-whoami', service_id: 'mock-service-whoami', kind: 'http', hostname: 'whoami.example.test',
    enabled: true, desired_revision: 1, applied_revision: 1, created_at: now(), updated_at: now(),
  }],
};

function fail(code) {
  throw new Error(code);
}

function bodyOf(options) {
  if (!options.body) return {};
  try { return JSON.parse(options.body); } catch { return {}; }
}

export async function mockApi(url, options = {}) {
  const method = options.method || 'GET';
  const path = new URL(url, 'http://localhost').pathname;
  const parts = path.split('/').filter(Boolean).map(decodeURIComponent);
  const input = bodyOf(options);

  if (path === '/api/v1/setup/status' && method === 'GET') return { setup_complete: true };
  if (path === '/api/v1/setup/admin' && method === 'POST') return { status: 'created' };
  if (path === '/api/v1/session' && method === 'POST') return { admin: { email: input.email || 'dev@example.test' }, expires_at: now() };
  if (path === '/api/v1/session' && method === 'DELETE') return {};
  if (path === '/api/v1/outlet' && method === 'GET') return { ...state.outlet };
  if (path === '/api/v1/outlet' && method === 'PUT') {
    state.outlet.domain = input.domain;
    return { ...state.outlet };
  }
  if (path === '/api/v1/connectors' && method === 'GET') return { connectors: state.connectors.map((connector) => ({ ...connector })) };
  if (path === '/api/v1/connectors' && method === 'POST') {
    if (!input.name?.trim()) fail('invalid_connector');
    if (state.connectors.some((connector) => connector.name === input.name.trim())) fail('connector_name_taken');
    const connector = { id: id('connector'), name: input.name.trim(), status: 'draft', created_at: now() };
    state.connectors.unshift(connector);
    return { ...connector };
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
    const connector = state.connectors.find((item) => item.id === connectorId);
    if (!connector) fail('connector_not_found');
    if (parts.length === 5 && method === 'GET') return { services: state.services.filter((service) => service.connector_id === connectorId).map((service) => ({ ...service, target: { ...service.target } })) };
    if (parts.length === 5 && method === 'POST') {
      if (state.services.some((service) => service.connector_id === connectorId)) fail('connector_service_limit');
      if (!input.name?.trim() || !input.target?.host || !input.target?.port) fail('invalid_service');
      const service = {
        id: id('service'), connector_id: connectorId, name: input.name.trim(), protocol: input.protocol || 'http',
        target: { host: input.target.host, port: Number(input.target.port) }, enabled: true,
        observed_status: 'unknown', created_at: now(), updated_at: now(),
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
    if (parts.length === 5 && method === 'GET') return { routes: state.routes.filter((route) => route.service_id === serviceId).map((route) => ({ ...route })) };
    if (parts.length === 5 && method === 'POST') {
      const hostname = input.hostname?.trim().toLowerCase();
      if (!hostname || !(hostname === state.outlet.domain || hostname.endsWith(`.${state.outlet.domain}`))) fail('invalid_route');
      if (state.routes.some((route) => route.hostname === hostname)) fail('hostname_taken');
      const route = { id: id('route'), service_id: serviceId, kind: 'http', hostname, enabled: true, desired_revision: 1, applied_revision: 0, created_at: now(), updated_at: now() };
      state.routes.push(route);
      return { ...route };
    }
  }
  fail('mock_endpoint_not_implemented');
}
