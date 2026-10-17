<script lang="ts">
  import type { Snippet } from 'svelte';
  import type { HTMLButtonAttributes } from 'svelte/elements';

  type Variant = 'primary' | 'secondary' | 'danger' | 'text' | 'plain';
  type Size = 'sm' | 'md';
  type Props = Omit<HTMLButtonAttributes, 'children' | 'class'> & {
    children: Snippet;
    class?: string;
    variant?: Variant;
    size?: Size;
  };

  let { children, variant = 'primary', size = 'md', class: className = '', type = 'button', ...rest }: Props = $props();
</script>

<button {...rest} {type} class={`ui-button ui-button--${variant} ui-button--${size} ${className}`}>
  {@render children()}
</button>
