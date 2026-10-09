<script lang="ts">
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Tooltip from '#lib/components/ui/tooltip/index.js';
	import { privacy } from '#lib/privacy.svelte.js';
	import EyeIcon from '@lucide/svelte/icons/eye';
	import EyeOffIcon from '@lucide/svelte/icons/eye-off';

	const text = $derived(privacy.revealed ? 'Blur account emails' : 'Show account emails');
</script>

<Tooltip.Root>
	<Tooltip.Trigger>
		{#snippet child({ props })}
			<Button {...props} variant="ghost" size="icon-sm" aria-label={text} onclick={privacy.toggle}>
				{#if privacy.revealed}<EyeIcon />{:else}<EyeOffIcon />{/if}
			</Button>
		{/snippet}
	</Tooltip.Trigger>
	<Tooltip.Content>{text}</Tooltip.Content>
</Tooltip.Root>
