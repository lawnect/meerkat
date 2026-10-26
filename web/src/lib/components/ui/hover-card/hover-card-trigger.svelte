<script lang="ts">
	import { LinkPreview as HoverCardPrimitive } from "bits-ui";

	let { ref = $bindable(null), ...restProps }: HoverCardPrimitive.TriggerProps = $props();
</script>

<!-- child 스니펫으로 다른 요소에 입힐 때는 그 요소의 data-slot을 덮어쓰지 않습니다. -->
<HoverCardPrimitive.Trigger bind:ref {...(restProps.child ? {} : { "data-slot": "hover-card-trigger" })} {...restProps} />
