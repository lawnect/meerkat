<script lang="ts" generics="T = never">
	import { Tooltip as TooltipPrimitive } from "bits-ui";

	let { ref = $bindable(null), ...restProps }: TooltipPrimitive.TriggerProps<T> = $props();
</script>

<!-- child 스니펫으로 다른 요소(Button 등)에 입히면 그 요소의 data-slot을 덮어쓰지 않도록 비워 둡니다. -->
<TooltipPrimitive.Trigger bind:ref {...(restProps.child ? {} : { "data-slot": "tooltip-trigger" })} {...restProps} />
