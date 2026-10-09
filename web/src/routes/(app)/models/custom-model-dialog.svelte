<script lang="ts">
	import type { CustomModel, Provider } from '#lib/types.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';

	let {
		open = $bindable(false),
		existing,
		onAdd
	}: { open: boolean; existing: string[]; onAdd: (m: CustomModel) => void } = $props();

	let id = $state('');
	let provider = $state<Provider>('claude');
	let name = $state('');
	let context = $state(200000);

	$effect.pre(() => {
		if (open) {
			id = '';
			name = '';
			provider = 'claude';
			context = 200000;
		}
	});

	const duplicate = $derived(existing.includes(id.trim().toLowerCase()));

	function add(e: SubmitEvent) {
		e.preventDefault();
		onAdd({ id: id.trim(), provider, name: name.trim(), contextLength: Number(context) || 0 });
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<form onsubmit={add} class="space-y-6">
			<Dialog.Header>
				<Dialog.Title>Add a model</Dialog.Title>
				<Dialog.Description>
					For a model the provider offers that isn't in the built-in list yet.
				</Dialog.Description>
			</Dialog.Header>
			<Field.Group>
				<Field.Field data-invalid={duplicate}>
					<Field.Label for="cm-id">Model ID</Field.Label>
					<Input
						id="cm-id"
						required
						class="font-mono"
						placeholder="claude-opus-6"
						bind:value={id}
					/>
					<Field.Description>Exactly as the provider names it.</Field.Description>
					{#if duplicate}<Field.Error>That model is already in the catalog.</Field.Error>{/if}
				</Field.Field>
				<Field.Set>
					<Field.Legend variant="label">Served by</Field.Legend>
					<RadioGroup.Root bind:value={provider} class="flex gap-6">
						<div class="flex items-center gap-2">
							<RadioGroup.Item value="claude" id="cm-claude" />
							<Label for="cm-claude" class="gap-1.5 font-normal"
								><ProviderLogo provider="claude" /> Claude</Label
							>
						</div>
						<div class="flex items-center gap-2">
							<RadioGroup.Item value="codex" id="cm-codex" />
							<Label for="cm-codex" class="gap-1.5 font-normal"
								><ProviderLogo provider="codex" /> Codex</Label
							>
						</div>
					</RadioGroup.Root>
				</Field.Set>
				<Field.Field>
					<Field.Label for="cm-name">Display name</Field.Label>
					<Input id="cm-name" placeholder="Generated from the ID" bind:value={name} />
				</Field.Field>
				<Field.Field>
					<Field.Label for="cm-ctx">Context length (tokens)</Field.Label>
					<Input id="cm-ctx" type="number" min={0} class="w-40" bind:value={context} />
				</Field.Field>
			</Field.Group>
			<Dialog.Footer>
				<Button type="submit" disabled={!id.trim() || duplicate}>Add</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
