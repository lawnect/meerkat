import type { PageKey } from './types';

export const pagePath: Record<PageKey, string> = {
  overview: '/',
  connectors: '/connectors',
  domains: '/domains',
  diagnostics: '/diagnostics',
};

export function connectorDetailPath(id: string): string {
  return `${pagePath.connectors}/${encodeURIComponent(id)}`;
}

export function connectorIdFromPath(pathname: string): string | null {
  const path = pathname.replace(/\/+$/, '');
  if (!path.startsWith(`${pagePath.connectors}/`)) return null;
  const encodedId = path.slice(pagePath.connectors.length + 1);
  if (!encodedId || encodedId.includes('/')) return null;
  try { return decodeURIComponent(encodedId); } catch { return null; }
}

export function pageFromPath(pathname: string): PageKey {
  const path = pathname.replace(/\/+$/, '') || '/';
  if (connectorIdFromPath(path)) return 'connectors';
  return (Object.keys(pagePath) as PageKey[]).find((page) => pagePath[page] === path) ?? 'overview';
}
