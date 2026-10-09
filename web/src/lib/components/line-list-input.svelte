<script lang="ts">
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import { untrack } from 'svelte';

	// Edits a list of strings as one item per line.
	let {
		value = $bindable(),
		id,
		rows = 3,
		placeholder = 'one per line'
	}: { value: string[]; id?: string; rows?: number; placeholder?: string } = $props();

	let text = $state(untrack(() => value.join('\n')));
	$effect.pre(() => {
		const joined = value.join('\n');
		untrack(() => {
			if (normalize(text).join('\n') !== joined) text = joined;
		});
	});

	function normalize(t: string) {
		return t
			.split('\n')
			.map((w) => w.trim())
			.filter(Boolean);
	}
</script>

<Textarea
	{id}
	{rows}
	{placeholder}
	class="font-mono text-xs"
	bind:value={text}
	oninput={() => (value = normalize(text))}
/>
