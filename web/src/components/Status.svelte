<script lang="ts">
  import { statusKind } from '../api';
  let { label, value }: { label: string; value?: string } = $props();
</script>

<span class="connection-status"><i class="connection-status-dot {statusKind(value)}"></i><span>{label}</span></span>

<style>
  .connection-status { display: inline-flex; align-items: center; gap: 6px; color: var(--muted); font-size: 12px; white-space: nowrap; }
  .connection-status-dot { flex: none; width: 7px; height: 7px; border-radius: 50%; background: #9a9d9a; }
  .connection-status-dot.ok { background: #3a8a6b; }
  .connection-status-dot.warn { background: #c98a27; }
  .connection-status-dot.bad { background: #bb4d3d; }
</style>
