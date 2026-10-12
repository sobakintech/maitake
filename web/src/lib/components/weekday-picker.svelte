<script lang="ts">
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { weekdays } from '#lib/primer.js';

	// days are weekdays (0 = Sunday); empty means every day. At least one stays selected.
	let { days = $bindable(), label = 'Days' }: { days: number[]; label?: string } = $props();

	const all = ['1', '2', '3', '4', '5', '6', '0'];
	const value = $derived(days.length ? days.map(String) : all);

	function change(v: string[]) {
		if (!v.length) return;
		days = v.length === 7 ? [] : v.map(Number).sort();
	}
</script>

<ToggleGroup.Root
	type="multiple"
	variant="outline"
	size="sm"
	aria-label={label}
	{value}
	onValueChange={change}
>
	{#each all as d (d)}
		<ToggleGroup.Item
			value={d}
			class="px-2 text-xs text-muted-foreground data-[state=on]:bg-primary data-[state=on]:text-primary-foreground hover:data-[state=on]:bg-primary/90"
			aria-label={weekdays[Number(d)]}
		>
			{weekdays[Number(d)].slice(0, 2)}
		</ToggleGroup.Item>
	{/each}
</ToggleGroup.Root>
