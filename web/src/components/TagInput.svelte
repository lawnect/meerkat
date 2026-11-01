<script lang="ts">
  import { Badge } from '$lib/components/ui/badge/index.js';
  import { Input } from '$lib/components/ui/input/index.js';
  import { cn } from '$lib/utils.js';
  import { translations } from '../i18n.svelte';
  import { Dismiss } from '../icons';

  // 여러 값을 칩으로 쌓는 입력. Enter·쉼표(또는 입력칸을 벗어날 때)에 입력한 값이 칩이 되고(값 안의 공백은 그대로),
  // 붙여넣은 글은 쉼표·공백·줄바꿈으로 나눠 한 번에 더합니다. 중복은 무시하고, 빈 입력칸에서 Backspace는 마지막 칩을 지웁니다.
  // suggestions를 주면 입력한 글을 포함하는 제안을 입력칸 아래 목록(combobox)으로 보여 줍니다. 목록은 입력할 때, ↓를 누를 때,
  // 입력칸을 클릭할 때 열리고, 고르거나 값을 넣으면 닫힙니다. 위·아래 화살표로 고르고 Enter로 넣으며, Esc로 닫습니다.
  // 제안과 대소문자만 다르게 입력한 값은 제안의 표기로 넣습니다. autoselect면 입력할 때 첫 제안을 미리 골라 두어 Enter가 그것을
  // 넣습니다(있는 값만 받는 칸). 새 값을 만드는 칸은 autoselect 없이 쓰므로 Enter가 입력한 글을 그대로 넣습니다.
  // secondary(value)가 참인 칩은 secondary 배지로, 나머지는 outline 배지로 그립니다(예: 태그와 IP를 구분).
  type Suggestion = { value: string; hint?: string };
  let { values = $bindable([]), placeholder, id, ariaLabel, lowercase = false, mono = false, disabled = false, suggestions = [], secondary, autoselect = false }: {
    values?: string[];
    placeholder?: string;
    id?: string;
    ariaLabel: string;
    lowercase?: boolean;
    mono?: boolean;
    disabled?: boolean;
    suggestions?: Suggestion[];
    secondary?: (value: string) => boolean;
    autoselect?: boolean;
  } = $props();

  const t = translations(
    { remove: (value: string) => `Remove ${value}`, suggestions: (label: string) => `${label} suggestions` },
    { remove: (value: string) => `${value} 삭제`, suggestions: (label: string) => `${label} 제안` },
  );

  const listId = $props.id();
  let draft = $state('');
  let input = $state<HTMLInputElement | null>(null);
  let open = $state(false);
  let active = $state(-1);

  const taken = (value: string) => values.some((item) => item.toLowerCase() === value.toLowerCase());
  // 같은 이름, 그 이름으로 시작하는 것, 포함하는 것 순서입니다. autoselect는 앞의 둘만 미리 고릅니다: 입력한 태그보다
  // 이름순으로 앞선 다른 태그(예: vpn을 입력했는데 corp-vpn)가 Enter로 들어가지 않게 합니다.
  const rank = (value: string, query: string): number => (value === query ? 0 : value.startsWith(query) ? 1 : 2);
  const matches = $derived.by(() => {
    const query = draft.trim().toLowerCase();
    return suggestions
      .filter((item) => !taken(item.value) && (!query || item.value.toLowerCase().includes(query)))
      .sort((a, b) => rank(a.value.toLowerCase(), query) - rank(b.value.toLowerCase(), query))
      .slice(0, 8);
  });
  const preselect = (): number => {
    const query = draft.trim().toLowerCase();
    return autoselect && query && matches.length && rank(matches[0].value.toLowerCase(), query) < 2 ? 0 : -1;
  };
  const expanded = $derived(open && matches.length > 0);
  const optionId = (index: number) => `${listId}-option-${index}`;

  function spelled(value: string): string {
    return suggestions.find((item) => item.value.toLowerCase() === value.toLowerCase())?.value ?? value;
  }

  function add(parts: string[]): void {
    const next = [...values];
    for (const part of parts) {
      const value = spelled(lowercase ? part.trim().toLowerCase() : part.trim());
      if (value && !next.some((item) => item.toLowerCase() === value.toLowerCase())) next.push(value);
    }
    if (next.length !== values.length) values = next;
  }

  function commit(): void {
    add(draft.split(','));
    draft = '';
    active = -1;
    open = false;
  }

  function pick(item: Suggestion): void {
    add([item.value]);
    draft = '';
    active = -1;
    open = false;
    input?.focus();
  }

  function remove(value: string): void {
    values = values.filter((item) => item !== value);
    input?.focus();
  }

  function onkeydown(event: KeyboardEvent): void {
    if (event.isComposing) return;
    if (event.key === 'ArrowDown' && suggestions.length) {
      event.preventDefault();
      open = true;
      active = matches.length ? (active + 1) % matches.length : -1;
    } else if (event.key === 'ArrowUp' && expanded) {
      event.preventDefault();
      active = active <= 0 ? matches.length - 1 : active - 1;
    } else if (event.key === 'Escape' && expanded) {
      // 대화상자가 함께 닫히지 않도록 목록만 닫습니다.
      event.preventDefault();
      event.stopPropagation();
      open = false;
      active = -1;
    } else if (event.key === 'Enter' || event.key === ',') {
      // 폼이 제출되지 않도록 Enter도 여기서 막습니다.
      event.preventDefault();
      if (event.key === 'Enter' && expanded && active >= 0) pick(matches[active]);
      else commit();
    } else if (event.key === 'Backspace' && draft === '' && values.length) {
      values = values.slice(0, -1);
    }
  }

  // 제안이 있는 칸의 값(태그)은 공백을 담을 수 있으므로 쉼표와 줄바꿈으로만 나눕니다.
  function onpaste(event: ClipboardEvent): void {
    const text = event.clipboardData?.getData('text') ?? '';
    const separator = suggestions.length ? /[,\n]+/ : /[\s,]+/;
    if (!separator.test(text.trim())) return;
    event.preventDefault();
    add(text.split(separator));
  }

  // autoselect 칸에서 입력하다 벗어나면, 제안과 같은 이름이거나 어떤 제안에도 맞지 않는 값(IP 등)만 넣고,
  // 제안의 일부만 입력한 값은 칩으로 만들지 않고 입력칸에 남겨 둡니다.
  function onblur(): void {
    const query = draft.trim().toLowerCase();
    open = false;
    active = -1;
    if (autoselect && query && matches.length && !matches.some((item) => item.value.toLowerCase() === query)) return;
    commit();
  }
</script>

<div class="grid gap-2">
  <div class="relative">
    <Input
      {id}
      {placeholder}
      {disabled}
      bind:ref={input}
      bind:value={draft}
      aria-label={ariaLabel}
      autocomplete="off"
      autocapitalize="off"
      spellcheck={false}
      class={cn(mono && 'font-mono')}
      role={suggestions.length ? 'combobox' : undefined}
      aria-autocomplete={suggestions.length ? 'list' : undefined}
      aria-expanded={suggestions.length ? expanded : undefined}
      aria-controls={suggestions.length ? listId : undefined}
      aria-activedescendant={expanded && active >= 0 ? optionId(active) : undefined}
      {onkeydown}
      {onpaste}
      oninput={() => { open = draft.trim() !== ''; active = preselect(); }}
      onclick={() => { open = true; }}
      {onblur}
    />
    {#if expanded}
      <ul id={listId} role="listbox" aria-label={t().suggestions(ariaLabel)} class="absolute inset-x-0 top-full z-50 mt-1 max-h-60 overflow-y-auto bg-popover p-1 text-popover-foreground shadow-md ring-1 ring-foreground/10">
        {#each matches as item, index (item.value)}
          <!-- 입력칸이 포커스를 잃지 않도록 mousedown에서 막고, 클릭으로 고릅니다. 키보드는 입력칸(aria-activedescendant)이 다룹니다. -->
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <li
            id={optionId(index)}
            role="option"
            aria-selected={index === active}
            class={cn('flex cursor-default items-center justify-between gap-2 px-2 py-2 text-xs select-none', index === active && 'bg-accent text-accent-foreground')}
            onmousedown={(event) => event.preventDefault()}
            onclick={() => pick(item)}
            onmouseenter={() => { active = index; }}
          >
            <span class="truncate">{item.value}</span>
            {#if item.hint}<span class="shrink-0 text-muted-foreground">{item.hint}</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
  </div>
  {#if values.length}
    <ul class="flex flex-wrap gap-1.5" aria-label={ariaLabel}>
      {#each values as value (value)}
        {@const tag = secondary?.(value) ?? false}
        <li class="flex max-w-full">
          <Badge variant={tag ? 'secondary' : 'outline'} class={cn('max-w-full gap-0.5', !disabled && 'pr-0.5', mono && !tag && 'font-mono')}>
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
