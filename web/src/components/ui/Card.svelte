<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAttributes } from 'svelte/elements';

  type Variant = 'solid' | 'glass';
  type Padding = 'none' | 'sm' | 'md';
  type Element = 'div' | 'section' | 'article';
  type Props = Omit<HTMLAttributes<HTMLElement>, 'children' | 'class'> & {
    children: Snippet;
    class?: string;
    as?: Element;
    variant?: Variant;
    padding?: Padding;
  };

  let { children, as = 'div', variant = 'solid', padding = 'md', class: className = '', ...rest }: Props = $props();
</script>

<svelte:element this={as} {...rest} class={`ui-card ui-card--${variant} ui-card--${padding} ${className}`}>
  {@render children()}
</svelte:element>
