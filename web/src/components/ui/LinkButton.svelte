<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLAnchorAttributes } from 'svelte/elements';

  type Variant = 'primary' | 'secondary' | 'text';
  type Size = 'sm' | 'md';
  type Props = Omit<HTMLAnchorAttributes, 'children' | 'href' | 'class'> & {
    children: Snippet;
    href: string;
    class?: string;
    variant?: Variant;
    size?: Size;
  };

  let { children, href, variant = 'secondary', size = 'md', class: className = '', ...rest }: Props = $props();
</script>

<a {...rest} {href} class={`ui-button ui-button--${variant} ui-button--${size} ${className}`}>
  {@render children()}
</a>
