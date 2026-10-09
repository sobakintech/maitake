<script lang="ts">
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { cn } from '#lib/utils.js';

	// Text that shows its other form in a flyout on hover, and switches to it on click.
	let {
		text,
		alt,
		onflip,
		class: className
	}: { text: string; alt: string; onflip: () => void; class?: string } = $props();
</script>

<Tooltip.Root disableCloseOnTriggerClick delayDuration={150}>
	<Tooltip.Trigger>
		{#snippet child({ props })}
			<button
				{...props}
				type="button"
				class={cn(
					'-mx-1.5 rounded-full px-1.5 tabular-nums transition-colors hover:bg-muted focus-visible:bg-muted focus-visible:outline-none',
					className
				)}
				onclick={onflip}>{text}</button
			>
		{/snippet}
	</Tooltip.Trigger>
	<Tooltip.Content side="top">{alt}</Tooltip.Content>
</Tooltip.Root>
