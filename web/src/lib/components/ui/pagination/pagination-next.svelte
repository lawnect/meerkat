<script lang="ts">
	import { uiText } from "../../../../uiText.svelte";
	import { ChevronRight } from '../../../../icons';
	import { Pagination as PaginationPrimitive } from "bits-ui";
	import { buttonVariants } from "$lib/components/ui/button/index.js";
	import { cn } from "$lib/utils.js";

	let {
		ref = $bindable(null),
		class: className,
		...restProps
	}: PaginationPrimitive.NextButtonProps = $props();
</script>

<PaginationPrimitive.NextButton
	bind:ref
	aria-label={uiText().nextPage}
	class={cn(buttonVariants({ variant: "ghost", size: "default" }), "pr-1.5!", className)}
	{...restProps}
>
	<span class="hidden sm:block">{uiText().next}</span>
	<ChevronRight data-icon="inline-end" class="cn-rtl-flip" />
</PaginationPrimitive.NextButton>
