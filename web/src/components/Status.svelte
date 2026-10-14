<script>
  import { statusKind } from '../api.js';
  let { label, value } = $props();
</script>

<span class="status"><i class="status-dot {statusKind(value)}"></i><span>{label}</span></span>
