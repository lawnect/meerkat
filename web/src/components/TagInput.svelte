<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';
  import { Dismiss } from '../icons';

  // 여러 값을 칩으로 쌓는 입력. Enter·쉼표(또는 입력칸을 벗어날 때)에 입력한 값이 칩이 되고(값 안의 공백은 그대로),
  // 붙여넣은 글은 쉼표·공백·줄바꿈으로 나눠 한 번에 더합니다. 중복은 무시하고, 빈 입력칸에서 Backspace는 마지막 칩을 지웁니다.
  let { values = $bindable([]), placeholder, id, ariaLabel, lowercase = false, mono = false, disabled = false }: {
    values?: string[];
    placeholder?: string;
    id?: string;
    ariaLabel: string;
    lowercase?: boolean;
    mono?: boolean;
    disabled?: boolean;
  } = $props();

  const t = translations({ remove: (value: string) => `Remove ${value}` }, { remove: (value: string) => `${value} 삭제` });

  let draft = $state('');
  let input = $state<HTMLInputElement | null>(null);

  function add(parts: string[]): void {
    const next = [...values];
    for (const part of parts) {
      const value = lowercase ? part.trim().toLowerCase() : part.trim();
      if (value && !next.includes(value)) next.push(value);
    }
    if (next.length !== values.length) values = next;
  }

  function commit(): void {
    add(draft.split(','));
    draft = '';
  }

  function remove(value: string): void {
    values = values.filter((item) => item !== value);
    input?.focus();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.isComposing) return;
    if (event.key === 'Enter' || event.key === ',') {
      // 폼이 제출되지 않도록 Enter도 여기서 막습니다.
      event.preventDefault();
      commit();
    } else if (event.key === 'Backspace' && draft === '' && values.length) {
      values = values.slice(0, -1);
    }
  }

  function onpaste(event: ClipboardEvent): void {
    const text = event.clipboardData?.getData('text') ?? '';
    if (!/[\s,]/.test(text.trim())) return;
    event.preventDefault();
    add(text.split(/[\s,]+/));
  }
</script>

<div class="grid gap-2">
  <Input {id} {placeholder} {disabled} bind:ref={input} bind:value={draft} aria-label={ariaLabel} autocomplete="off" autocapitalize="off" spellcheck={false} class={cn(mono && 'font-mono')} {onkeydown} {onpaste} onblur={commit} />
  {#if values.length}
    <ul class="flex flex-wrap gap-1.5" aria-label={ariaLabel}>
      {#each values as value (value)}
        <li class="flex max-w-full">
          <Badge variant="outline" class={cn('max-w-full gap-0.5', !disabled && 'pr-0.5', mono && 'font-mono')}>
            <span class="truncate">{value}</span>
            {#if !disabled}<button type="button" class="inline-flex size-4 shrink-0 items-center justify-center text-muted-foreground outline-none hover:bg-muted hover:text-foreground focus-visible:bg-muted focus-visible:text-foreground focus-visible:ring-1 focus-visible:ring-ring focus-visible:ring-inset" aria-label={t().remove(value)} onclick={() => remove(value)}>
              <Dismiss aria-hidden="true" />
            </button>{/if}
          </Badge>
        </li>
      {/each}
    </ul>
  {/if}
</div>
