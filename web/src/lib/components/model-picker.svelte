<script lang="ts">
	import type { CatalogModel } from '#lib/types.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import LabLogo from './lab-logo.svelte';
	import { untrack } from 'svelte';

	// value is a key's allow-list of patterns; empty means every model.
	let {
		value = $bindable(),
		catalog,
		labs
	}: { value: string[]; catalog: CatalogModel[]; labs: { id: string; name: string }[] } = $props();

	let mode = $state(untrack(() => (value.length ? 'some' : 'all')));

	const labPattern = (lab: string) => `${lab}/*`;
	const covered = $derived(
		new Set([...labs.map((l) => labPattern(l.id)), ...catalog.flatMap((m) => [m.canonical, m.id])])
	);
	const extra = $derived(value.filter((p) => !covered.has(p)).join('\n'));

	function setMode(m: string) {
		mode = m;
		if (m === 'all') value = [];
	}

	function labOn(lab: string) {
		return value.includes(labPattern(lab));
	}

	function modelOn(m: CatalogModel) {
		return labOn(m.lab) || value.includes(m.canonical) || value.includes(m.id);
	}

	function toggleLab(lab: string, on: boolean) {
		const inLab = new Set(catalog.filter((m) => m.lab === lab).flatMap((m) => [m.canonical, m.id]));
		const rest = value.filter((p) => p !== labPattern(lab) && !inLab.has(p));
		value = on ? [...rest, labPattern(lab)] : rest;
	}

	function toggleModel(m: CatalogModel, on: boolean) {
		const rest = value.filter((p) => p !== m.canonical && p !== m.id);
		value = on ? [...rest, m.canonical] : rest;
	}

	function setExtra(text: string) {
		const kept = value.filter((p) => covered.has(p));
		value = [
			...kept,
			...text
				.split(/[\n,]/)
				.map((p) => p.trim())
				.filter(Boolean)
		];
	}
</script>

<div class="space-y-4">
	<RadioGroup.Root value={mode} onValueChange={setMode} class="gap-2.5">
		<div class="flex items-center gap-3">
			<RadioGroup.Item value="all" id="mp-all" />
			<Label for="mp-all" class="font-normal">Every model, including ones added later</Label>
		</div>
		<div class="flex items-center gap-3">
			<RadioGroup.Item value="some" id="mp-some" />
			<Label for="mp-some" class="font-normal">Only the models picked below</Label>
		</div>
	</RadioGroup.Root>

	{#if mode === 'some'}
		<div class="space-y-4">
			{#each labs as lab (lab.id)}
				{@const list = catalog.filter((m) => m.lab === lab.id)}
				{#if list.length}
					<div class="rounded-lg border">
						<div class="flex items-center gap-2.5 border-b bg-muted/30 px-3 py-2">
							<Checkbox
								id="lab-{lab.id}"
								checked={labOn(lab.id)}
								onCheckedChange={(v) => toggleLab(lab.id, v === true)}
							/>
							<Label for="lab-{lab.id}" class="gap-1.5">
								<LabLogo lab={lab.id} />
								All {lab.name} models
							</Label>
							<span class="ml-auto text-xs text-muted-foreground">
								includes future ones · <code>{labPattern(lab.id)}</code>
							</span>
						</div>
						<div class="grid gap-2 p-3 sm:grid-cols-2">
							{#each list as m (m.canonical)}
								<div class="flex items-center gap-2.5">
									<Checkbox
										id="m-{m.canonical}"
										checked={modelOn(m)}
										disabled={labOn(lab.id)}
										onCheckedChange={(v) => toggleModel(m, v === true)}
									/>
									<Label for="m-{m.canonical}" class="font-normal">
										{m.name}
										<span class="font-mono text-xs text-muted-foreground">{m.id}</span>
									</Label>
								</div>
							{/each}
						</div>
					</div>
				{/if}
			{/each}

			<div class="space-y-1.5">
				<Label for="mp-extra">Extra patterns</Label>
				<Textarea
					id="mp-extra"
					rows={2}
					class="font-mono text-xs"
					placeholder="e.g. claude-*-5* or gpt-6*"
					value={extra}
					onchange={(e) => setExtra(e.currentTarget.value)}
				/>
				<p class="text-xs text-muted-foreground">
					One per line, <code>*</code> as a wildcard. Matches names with or without the lab prefix.
				</p>
			</div>
			{#if value.length === 0}
				<p class="text-xs text-destructive">
					Nothing is picked, which means every model is allowed. Pick at least one to restrict the
					key.
				</p>
			{/if}
		</div>
	{/if}
</div>
