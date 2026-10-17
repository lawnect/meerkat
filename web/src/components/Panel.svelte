<script lang="ts">
  import type { Snippet } from 'svelte';

  let { title, description = '', children }: { title: string; description?: string; children: Snippet } = $props();
</script>

<section class="panel card bg-base-100 border-base-300">
  <div class="panel-head">
    <div><h2>{title}</h2>{#if description}<p>{description}</p>{/if}</div>
  </div>
  {@render children()}
</section>

<style>
  :global(.panel) { border: 1px solid var(--line); padding: 22px; background: var(--panel); }
  :global(.panel-head) { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 18px; border-bottom: 1px solid var(--line); padding-bottom: 14px; }
  :global(.panel h2) { margin: 0; font-size: 17px; letter-spacing: -.025em; }
  :global(.panel h3) { margin: 0 0 5px; font-size: 14px; }
  :global(.panel p) { margin: 0; color: var(--muted); }
  :global(.panel-head p) { font-size: 12px; }
</style>
