<script>
  let { title, description = '', children } = $props();
</script>

<section class="panel">
  <div class="panel-head">
    <div><h2>{title}</h2>{#if description}<p>{description}</p>{/if}</div>
  </div>
  {@render children()}
</section>
