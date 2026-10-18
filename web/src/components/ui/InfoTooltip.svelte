<script lang="ts">
  import Button from './Button.svelte';

  let { id, label, description }: { id: string; label: string; description: string } = $props();
</script>

<span class="info-tooltip">
  <Button variant="plain" class="info-trigger" aria-label={`${label} 설명`} aria-describedby={id}>
    <svg viewBox="0 0 20 20" fill="none" stroke="currentColor" stroke-width="1.6" aria-hidden="true">
      <circle cx="10" cy="10" r="7.5" />
      <path d="M10 9v5M10 6.2h.01" stroke-linecap="round" />
    </svg>
  </Button>
  <span {id} class="info-content" role="tooltip">{description}</span>
</span>

<style>
  .info-tooltip { position: relative; display: inline-flex; align-items: center; }
  .info-tooltip :global(.info-trigger) { display: grid; place-items: center; width: 18px; height: 18px; border-radius: 50%; color: #8d8178; }
  .info-tooltip :global(.info-trigger:hover), .info-tooltip :global(.info-trigger:focus-visible) { color: var(--accent-dark); }
  .info-tooltip :global(.info-trigger svg) { width: 16px; height: 16px; }
  .info-content { position: absolute; z-index: 10; top: calc(100% + 7px); left: 0; width: max-content; max-width: min(280px, calc(100vw - 48px)); border-radius: 6px; padding: 8px 10px; background: #37312e; box-shadow: 0 8px 20px rgba(48,36,28,.16); color: #fff; font-size: 11px; font-weight: 500; line-height: 1.5; white-space: normal; pointer-events: none; visibility: hidden; }
  .info-tooltip:hover .info-content, .info-tooltip:focus-within .info-content { visibility: visible; }
</style>
