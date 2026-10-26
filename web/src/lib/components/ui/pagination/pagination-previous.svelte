<script lang="ts">
	import { uiText } from "../../../../uiText.svelte";
	import { ChevronLeft } from '../../../../icons';
	import { Pagination as PaginationPrimitive } from "bits-ui";
	import { buttonVariants } from "$lib/components/ui/button/index.js";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: PaginationPrimitive.PrevButtonProps = $props();
</script>

<PaginationPrimitive.PrevButton
	bind:ref
	aria-label={uiText().previousPage}
	class={cn(
		buttonVariants({ variant: "ghost", size: "default" }),
		"pl-1.5!",
		className
	)}
	{...restProps}
>
	<ChevronLeft data-icon="inline-start" class="cn-rtl-flip" />
	<span class="hidden sm:block">{uiText().previous}</span>
</PaginationPrimitive.PrevButton>
