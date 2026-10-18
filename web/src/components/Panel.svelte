<script lang="ts">
  import type { Snippet } from 'svelte';
  import Card from './ui/Card.svelte';

  let { title, description = '', action, children }: { title: string; description?: string; action?: Snippet; children: Snippet } = $props();
</script>

<Card as="section" class="panel">
  <div class="panel-head">
    <div><h2>{title}</h2>{#if description}<p>{description}</p>{/if}</div>
    {#if action}<div class="panel-action">{@render action()}</div>{/if}
  </div>
  {@render children()}
</Card>

<style>
  :global(.panel-head) { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 18px; border-bottom: 1px solid var(--line); padding-bottom: 14px; }
  :global(.panel h2) { margin: 0; font-size: 17px; letter-spacing: -.025em; }
  :global(.panel h3) { margin: 0 0 5px; font-size: 14px; }
  :global(.panel p) { margin: 0; color: var(--muted); }
  :global(.panel-head p) { font-size: 12px; }
  .panel-action { flex: none; }
</style>
