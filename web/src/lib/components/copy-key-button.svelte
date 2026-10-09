<script lang="ts">
	import { admin, errorMessage } from '#lib/api.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import CheckIcon from '@lucide/svelte/icons/check';
	import CopyIcon from '@lucide/svelte/icons/copy';
	import { toast } from 'svelte-sonner';

	let { id, copyable, label }: { id: number; copyable: boolean; label?: string } = $props();

	let copied = $state(false);

	async function copy() {
		if (!copyable) {
			toast.info(
				"This key wasn't stored, so it can't be copied. Regenerate it on its page to get a copyable key."
			);
			return;
		}
		try {
			const { secret } = await admin.keySecret(id);
			await navigator.clipboard.writeText(secret);
			copied = true;
			toast.success('Key copied');
			setTimeout(() => (copied = false), 1500);
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}
</script>

<Button
	variant={label ? 'outline' : 'ghost'}
	size={label ? 'sm' : 'icon-xs'}
	class={copyable ? '' : 'text-muted-foreground/50'}
	title={copyable ? 'Copy key' : "Can't be copied: no copy was stored"}
	aria-label="Copy key"
	onclick={copy}
>
	{#if copied}<CheckIcon />{:else}<CopyIcon />{/if}
	{label}
</Button>
