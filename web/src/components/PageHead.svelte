<script lang="ts">
  import type { Snippet } from 'svelte';

  let { kicker, title, description, action = null }: { kicker: string; title: string; description: string; action?: Snippet | null } = $props();
</script>

<div class="page-head">
  <div>
    <p class="eyebrow">{kicker}</p>
    <h1>{title}</h1>
    <p>{description}</p>
  </div>
  {#if action}<div class="actions">{@render action()}</div>{/if}
</div>

<style>
  .page-head { display: flex; align-items: flex-start; justify-content: space-between; gap: 16px; margin-bottom: 18px; }
  .page-head .eyebrow { margin: 0 0 4px; color: var(--accent-dark); font-size: 10px; font-weight: 800; letter-spacing: .12em; text-transform: uppercase; }
  .page-head h1 { margin: 0 0 3px; font-size: 24px; line-height: 1.2; letter-spacing: -.04em; }
  .page-head p { margin: 0; color: var(--muted); }
  .actions { display: flex; flex-wrap: wrap; gap: 8px; }
  @media (max-width: 600px) {
    .page-head { flex-wrap: wrap; gap: 10px; }
    .actions { width: 100%; }
  }
</style>
