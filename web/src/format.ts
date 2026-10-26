import { intlLocale } from './i18n.svelte';

export function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes.toLocaleString(intlLocale())} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / 1024 / 1024).toFixed(1)} MB`;
  return `${(bytes / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
