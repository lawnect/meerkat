<script>
  import { statusKind } from '../api.js';
  let { label, value } = $props();
</script>

<span class="status"><i class="status-dot {statusKind(value)}"></i><span>{label}</span></span>

<style>
  .status { color: var(--muted); font-size: 12px; }
  .status-dot { display: inline-block; width: 7px; height: 7px; margin-right: 6px; border-radius: 50%; background: #9a9d9a; vertical-align: 1px; }
  .status-dot.ok { background: #3a8a6b; }
  .status-dot.warn { background: #c98a27; }
  .status-dot.bad { background: #bb4d3d; }
</style>
