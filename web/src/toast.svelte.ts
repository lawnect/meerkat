import { toast } from 'svelte-sonner';
import { translations } from './i18n.svelte';

const t = translations(
  { copied: 'Copied', copyFailed: 'Couldn\'t copy. Select the text and copy it manually.' },
  { copied: '복사했습니다', copyFailed: '복사하지 못했습니다. 직접 선택해 복사하세요.' },
);

export async function copyToClipboard(value: string): Promise<void> {
  try {
    await navigator.clipboard.writeText(value);
    toast.success(t().copied);
  } catch {
    toast.error(t().copyFailed);
  }
}
