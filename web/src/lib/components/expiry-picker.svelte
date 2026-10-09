<script lang="ts">
	import { dateTime } from '#lib/format.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { untrack } from 'svelte';

	// value is an ISO timestamp, or null for a key that never expires.
	let { value = $bindable(null) }: { value: string | null } = $props();

	const presets = [
		{ id: 'never', label: 'Never', days: 0 },
		{ id: '7', label: '7 days', days: 7 },
		{ id: '30', label: '30 days', days: 30 },
		{ id: '90', label: '90 days', days: 90 },
		{ id: '365', label: '1 year', days: 365 },
		{ id: 'custom', label: 'Custom', days: 0 }
	];

	// datetime-local inputs want local "YYYY-MM-DDTHH:mm".
	function toLocalInput(iso: string) {
		const d = new Date(iso);
		const pad = (n: number) => String(n).padStart(2, '0');
		return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}T${pad(d.getHours())}:${pad(d.getMinutes())}`;
	}

	const initial = untrack(() => value);
	let mode = $state(initial ? 'custom' : 'never');
	let custom = $state(initial ? toLocalInput(initial) : '');

	const minInput = toLocalInput(new Date(Date.now() + 60_000).toISOString());
	const inPast = $derived(mode === 'custom' && !!custom && new Date(custom) <= new Date());

	function pick(id: string) {
		mode = id;
		if (id === 'never') {
			value = null;
		} else if (id === 'custom') {
			if (!custom) custom = toLocalInput(new Date(Date.now() + 30 * 86_400_000).toISOString());
			value = new Date(custom).toISOString();
		} else {
			const days = presets.find((p) => p.id === id)!.days;
			value = new Date(Date.now() + days * 86_400_000).toISOString();
		}
	}

	function onCustomInput() {
		value = custom ? new Date(custom).toISOString() : null;
	}
</script>

<div class="space-y-3">
	<div class="flex flex-wrap gap-2">
		{#each presets as p (p.id)}
			<Button
				type="button"
				size="sm"
				variant={mode === p.id ? 'default' : 'outline'}
				onclick={() => pick(p.id)}
			>
				{p.label}
			</Button>
		{/each}
	</div>
	{#if mode === 'custom'}
		<Input
			type="datetime-local"
			min={minInput}
			bind:value={custom}
			oninput={onCustomInput}
			aria-invalid={inPast}
			class="w-60"
		/>
	{/if}
	<p class="text-xs {inPast ? 'text-destructive' : 'text-muted-foreground'}">
		{#if inPast}
			Pick a time in the future.
		{:else if value}
			Expires {dateTime(value)}.
		{:else}
			This key never expires.
		{/if}
	</p>
</div>
