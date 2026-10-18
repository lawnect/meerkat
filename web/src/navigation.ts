import type { PageKey } from './types';

export const pagePath: Record<PageKey, string> = {
  overview: '/',
  connectors: '/connectors',
  domains: '/domains',
  diagnostics: '/diagnostics',
};

export function pageFromPath(pathname: string): PageKey {
  const path = pathname.replace(/\/+$/, '') || '/';
  return (Object.keys(pagePath) as PageKey[]).find((page) => pagePath[page] === path) ?? 'overview';
}
