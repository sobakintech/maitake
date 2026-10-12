<script lang="ts">
	import type { CatalogModel, Provider } from '#lib/types.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { modelLabel, modelOptions, type ModelOption } from '#lib/primer.js';

	// The model a primer ping uses. With defaultLabel set, "" is an extra first option.
	let {
		value = $bindable(''),
		provider,
		catalog,
		defaultLabel,
		id,
		class: className
	}: {
		value: string;
		provider: Provider;
		catalog: CatalogModel[];
		defaultLabel?: string;
		id?: string;
		class?: string;
	} = $props();

	const DEFAULT = '__default';
	const options = $derived.by(() => {
		const list = modelOptions(provider, catalog);
		// Keep a model typed in before, or one the catalog no longer lists.
		if (value && !list.some((o) => o.value === value)) list.push({ value, label: value });
		return list;
	});
	const newest = $derived(options.filter((o) => o.newest));
	const specific = $derived(options.filter((o) => !o.newest));
	const shown = $derived(value ? modelLabel(value, options) : (defaultLabel ?? ''));
</script>

<Select.Root
	type="single"
	value={value || DEFAULT}
	onValueChange={(v) => (value = v === DEFAULT ? '' : v)}
>
	<Select.Trigger {id} class={className}>{shown}</Select.Trigger>
	<Select.Content>
		{#if defaultLabel}
			<Select.Item value={DEFAULT} label={defaultLabel}>{defaultLabel}</Select.Item>
			<Select.Separator />
		{/if}
		{#if newest.length}
			<Select.Group>
				<Select.Label>Always the newest</Select.Label>
				{#each newest as o (o.value)}{@render item(o)}{/each}
			</Select.Group>
			<Select.Separator />
		{/if}
		<Select.Group>
			{#if newest.length}<Select.Label>Specific model</Select.Label>{/if}
			{#each specific as o (o.value)}{@render item(o)}{/each}
		</Select.Group>
	</Select.Content>
</Select.Root>

{#snippet item(o: ModelOption)}
	<Select.Item value={o.value} label={o.label}>
		<span class="flex-1">{o.label}</span>
		{#if o.label !== o.value && !o.newest}
			<span class="font-mono text-xs text-muted-foreground">{o.value}</span>
		{/if}
	</Select.Item>
{/snippet}
