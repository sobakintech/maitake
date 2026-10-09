<script lang="ts">
	import type { Snippet } from 'svelte';
	import { privacy } from '#lib/privacy.svelte.js';

	let { children, class: className = '' }: { children: Snippet; class?: string } = $props();
</script>

<span
	class={[
		'transition-[filter] duration-150',
		!privacy.revealed && 'blur-[5px] select-none hover:blur-none hover:select-auto',
		className
	]}>{@render children()}</span
>
