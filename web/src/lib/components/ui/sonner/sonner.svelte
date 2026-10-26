<script lang="ts">
	import { CheckmarkCircle, ErrorCircle, Info, SpinnerIos, Warning } from '../../../../icons';
	import { theme } from "../../../../theme.svelte";
	import { Toaster as Sonner, type ToasterProps as SonnerProps } from "svelte-sonner";

	let { ...restProps }: SonnerProps = $props();
</script>

<Sonner
	theme={theme.current}
	class="toaster group"
	style="--normal-bg: var(--color-popover); --normal-text: var(--color-popover-foreground); --normal-border: var(--color-border);"
	toastOptions={{
		classes: {
			toast: "cn-toast",
		},
	}}
	{...restProps}
>
	{#snippet loadingIcon()}
		<SpinnerIos class="size-4 animate-spin" />
	{/snippet}
	{#snippet successIcon()}
		<CheckmarkCircle class="size-4" />
	{/snippet}
	{#snippet errorIcon()}
		<ErrorCircle class="size-4" />
	{/snippet}
	{#snippet infoIcon()}
		<Info class="size-4" />
	{/snippet}
	{#snippet warningIcon()}
		<Warning class="size-4" />
	{/snippet}
</Sonner>
