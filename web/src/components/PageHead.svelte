<script>
  let { kicker, title, description, action = null } = $props();
</script>

<div class="page-head">
  <div>
    <p class="eyebrow">{kicker}</p>
    <h1>{title}</h1>
    <p>{description}</p>
  </div>
  {#if action}<div class="actions">{@render action()}</div>{/if}
</div>
