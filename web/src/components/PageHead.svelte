<script lang="ts">
  import type { Snippet } from 'svelte';
  import * as Breadcrumb from '$lib/components/ui/breadcrumb/index.js';

  type Crumb = { label: string; href?: string; onclick?: (event: MouseEvent) => void };
  let { title, crumbs = [], action }: { title: string; crumbs?: Crumb[]; action?: Snippet } = $props();
</script>

<header class="mb-4 grid gap-1">
  {#if crumbs.length}
    <Breadcrumb.Root>
      <Breadcrumb.List>
        {#each crumbs as crumb, index (index)}
          {#if index}<Breadcrumb.Separator />{/if}
          <Breadcrumb.Item>
            {#if crumb.href}<Breadcrumb.Link href={crumb.href} onclick={crumb.onclick}>{crumb.label}</Breadcrumb.Link>{:else}<Breadcrumb.Page>{crumb.label}</Breadcrumb.Page>{/if}
          </Breadcrumb.Item>
        {/each}
      </Breadcrumb.List>
    </Breadcrumb.Root>
  {/if}
  <div class="flex flex-wrap items-center justify-between gap-3">
    <h1 class="min-w-0 truncate text-xl font-semibold">{title}</h1>
    {#if action}<div class="flex flex-wrap items-center gap-2">{@render action()}</div>{/if}
  </div>
</header>
