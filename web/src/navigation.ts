import { localizePath, stripLocale } from './i18n.svelte';
import type { PageKey } from './types';

export const pagePath: Record<PageKey, string> = {
  overview: '/',
  connectors: '/connectors',
  domains: '/domains',
  diagnostics: '/diagnostics',
  settings: '/settings',
};

// 링크와 history에는 현재 언어의 접두사를 붙인 주소를 사용합니다.
export function pageHref(page: PageKey): string {
  return localizePath(pagePath[page]);
}

export function connectorDetailPath(id: string): string {
  return localizePath(`${pagePath.connectors}/${encodeURIComponent(id)}`);
}

export function connectorIdFromPath(pathname: string): string | null {
  const path = stripLocale(pathname).replace(/\/+$/, '');
  if (!path.startsWith(`${pagePath.connectors}/`)) return null;
  const encodedId = path.slice(pagePath.connectors.length + 1);
  if (!encodedId || encodedId.includes('/')) return null;
  try { return decodeURIComponent(encodedId); } catch { return null; }
}

export function domainDetailPath(domain: string): string {
  return localizePath(`${pagePath.domains}/${encodeURIComponent(domain)}`);
}

export function domainFromPath(pathname: string): string | null {
  const path = stripLocale(pathname).replace(/\/+$/, '');
  if (!path.startsWith(`${pagePath.domains}/`)) return null;
  const encoded = path.slice(pagePath.domains.length + 1);
  if (!encoded || encoded.includes('/')) return null;
  try { return decodeURIComponent(encoded); } catch { return null; }
}

export function pageFromPath(pathname: string): PageKey {
  const path = stripLocale(pathname).replace(/\/+$/, '') || '/';
  if (connectorIdFromPath(path)) return 'connectors';
  if (domainFromPath(path)) return 'domains';
  return (Object.keys(pagePath) as PageKey[]).find((page) => pagePath[page] === path) ?? 'overview';
}
