<script lang="ts">
	import { SpinnerIos } from '../../../../icons';
	import { uiText } from '../../../../uiText.svelte';
	import { cn } from "$lib/utils.js";
	import type { SVGAttributes } from "svelte/elements";

	let { class: className, role = "status", "aria-label": ariaLabel, ...restProps }: SVGAttributes<SVGSVGElement> = $props();
</script>

<SpinnerIos {role} aria-label={ariaLabel ?? uiText().loading} class={cn("size-4 animate-spin", className)} {...restProps} />
