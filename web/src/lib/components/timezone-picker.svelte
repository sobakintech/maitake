<script lang="ts" module>
	type Zone = { value: string; label: string; offset: string; search: string };

	let zones: Zone[] | null = null;

	function offsetOf(tz: string): string {
		try {
			const part = new Intl.DateTimeFormat('en-US', { timeZone: tz, timeZoneName: 'longOffset' })
				.formatToParts(new Date())
				.find((p) => p.type === 'timeZoneName')?.value;
			return part === 'GMT' ? 'UTC' : (part?.replace('GMT', 'UTC') ?? '');
		} catch {
			return '';
		}
	}

	function allZones(): Zone[] {
		if (zones) return zones;
		const names = Intl.supportedValuesOf('timeZone');
		if (!names.includes('UTC')) names.unshift('UTC');
		zones = names.map((tz) => {
			const offset = offsetOf(tz);
			const label = tz.replace(/_/g, ' ');
			return { value: tz, label, offset, search: `${label} ${tz} ${offset}`.toLowerCase() };
		});
		return zones;
	}
</script>

<script lang="ts">
	import { Combobox } from 'bits-ui';
	import CheckIcon from '@lucide/svelte/icons/check';
	import ChevronsUpDownIcon from '@lucide/svelte/icons/chevrons-up-down';
	import { cn } from '#lib/utils.js';

	// A searchable list of IANA time zones. "" is the default option, labelled by defaultLabel.
	let {
		value = $bindable(''),
		defaultLabel,
		id,
		class: className
	}: { value: string; defaultLabel: string; id?: string; class?: string } = $props();

	const DEFAULT = '__default';
	const browser = Intl.DateTimeFormat().resolvedOptions().timeZone;

	let search = $state('');
	let open = $state(false);
	const list = $derived(open ? allZones() : []);
	const q = $derived(search.trim().toLowerCase());
	const filtered = $derived.by(() => {
		const hits = q ? list.filter((z) => z.search.includes(q)) : list;
		// Keep the browser's own zone at the top when nothing is typed.
		if (q) return hits;
		const own = hits.find((z) => z.value === browser);
		return own ? [own, ...hits.filter((z) => z !== own)] : hits;
	});
	const display = $derived(value ? value.replace(/_/g, ' ') : defaultLabel);
</script>

<Combobox.Root
	type="single"
	value={value || DEFAULT}
	onValueChange={(v) => (value = v === DEFAULT ? '' : v)}
	bind:open
	onOpenChange={(o) => {
		if (!o) search = '';
	}}
	inputValue={display}
>
	<div class={cn('relative w-full', className)}>
		<Combobox.Input
			{id}
			oninput={(e) => (search = e.currentTarget.value)}
			onfocus={(e) => e.currentTarget.select()}
			onclick={() => (open = true)}
			placeholder="Search time zones"
			class="h-8 w-full min-w-0 rounded-lg border border-input bg-transparent py-1 pr-8 pl-2.5 text-sm transition-colors outline-none placeholder:text-muted-foreground focus-visible:border-ring focus-visible:ring-3 focus-visible:ring-ring/50 dark:bg-input/30"
		/>
		<Combobox.Trigger
			class="absolute inset-y-0 right-0 flex w-8 items-center justify-center text-muted-foreground"
			aria-label="Show time zones"
		>
			<ChevronsUpDownIcon class="size-4" />
		</Combobox.Trigger>
	</div>
	<Combobox.Portal>
		<Combobox.Content
			sideOffset={4}
			class="z-50 max-h-72 w-(--bits-combobox-anchor-width) min-w-64 overflow-y-auto rounded-lg bg-popover p-1 text-popover-foreground shadow-md ring-1 ring-foreground/10"
		>
			<Combobox.Viewport>
				{#if !q}
					{@render item(DEFAULT, defaultLabel, '')}
				{/if}
				{#each filtered as z (z.value)}
					{@render item(
						z.value,
						z.label,
						z.value === browser ? `${z.offset} · this browser` : z.offset
					)}
				{:else}
					<p class="px-2 py-1.5 text-sm text-muted-foreground">No time zone matches.</p>
				{/each}
			</Combobox.Viewport>
		</Combobox.Content>
	</Combobox.Portal>
</Combobox.Root>

{#snippet item(v: string, label: string, hint: string)}
	<Combobox.Item
		value={v}
		{label}
		class="relative flex w-full cursor-pointer items-center gap-2 rounded-md py-1 pr-8 pl-2 text-sm outline-hidden select-none data-highlighted:bg-accent data-highlighted:text-accent-foreground"
	>
		{#snippet children({ selected })}
			<span class="flex-1 truncate">{label}</span>
			{#if hint}<span class="shrink-0 text-xs text-muted-foreground">{hint}</span>{/if}
			{#if selected}<CheckIcon class="absolute right-2 size-4" />{/if}
		{/snippet}
	</Combobox.Item>
{/snippet}
