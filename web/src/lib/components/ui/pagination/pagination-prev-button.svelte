<script lang="ts">
	import { uiText } from "../../../../uiText.svelte";
	import { ChevronLeft } from '../../../../icons';
	import { Pagination as PaginationPrimitive } from "bits-ui";
	import { cn } from "$lib/utils.js";
	import { buttonVariants } from "../button/index.js";

	let {
		ref = $bindable(null),
		class: className,
		children,
		...restProps
	}: PaginationPrimitive.PrevButtonProps = $props();
</script>

{#snippet Fallback()}
	<ChevronLeft class={cn("cn-rtl-flip size-4", className)} />
	<span>{uiText().previous}</span>
{/snippet}

<PaginationPrimitive.PrevButton
	bind:ref
	aria-label={uiText().previousPage}
	class={cn(buttonVariants({ variant: "ghost" }), "pl-1.5!", className)}
	{...restProps}
>
	{#if children}
		{@render children?.()}
	{:else}
		{@render Fallback()}
	{/if}
</PaginationPrimitive.PrevButton>
