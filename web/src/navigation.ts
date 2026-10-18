import type { PageKey } from './types';

export const pagePath: Record<PageKey, string> = {
  overview: '/',
  connectors: '/connectors',
  diagnostics: '/diagnostics',
  settings: '/settings',
};

export function pageFromPath(pathname: string): PageKey {
  const path = pathname.replace(/\/+$/, '') || '/';
  return (Object.keys(pagePath) as PageKey[]).find((page) => pagePath[page] === path) ?? 'overview';
}
