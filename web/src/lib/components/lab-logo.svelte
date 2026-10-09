<script lang="ts">
	import ProviderLogo from './provider-logo.svelte';
	import BoxIcon from '@lucide/svelte/icons/box';

	let { lab, class: className = 'size-4' }: { lab: string; class?: string } = $props();

	// Until custom sources exist, each lab's models come from the matching subscription.
	const providerFor: Record<string, 'claude' | 'codex'> = { anthropic: 'claude', openai: 'codex' };
</script>

{#if providerFor[lab]}
	<ProviderLogo provider={providerFor[lab]} class={className} />
{:else}
	<BoxIcon class={className} />
{/if}
