<script lang="ts">
	import type { Account, CatalogModel, ModelOverride, Strategy } from '#lib/types.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';

	// Edits one model's override. Changes go into the page's unsaved draft via onApply.
	let {
		model,
		override,
		accounts,
		open = $bindable(false),
		onApply
	}: {
		model: CatalogModel | null;
		override: ModelOverride;
		accounts: Account[];
		open: boolean;
		onApply: (o: ModelOverride) => void;
	} = $props();

	let name = $state('');
	let effort = $state('');
	let strategy = $state<Strategy | ''>('');
	let picked = $state<number[]>([]);

	$effect.pre(() => {
		if (open) {
			name = override.name;
			effort = override.effort;
			strategy = override.strategy;
			picked = [...(override.accounts ?? [])];
		}
	});

	const efforts = ['', 'none', 'minimal', 'low', 'medium', 'high', 'xhigh', 'max'];
	const strategies: { value: Strategy | ''; label: string }[] = [
		{ value: '', label: 'Same as the provider default' },
		{ value: 'round_robin', label: 'Take turns' },
		{ value: 'fill', label: 'Fill one account first' },
		{ value: 'most_remaining', label: 'Most quota left' },
		{ value: 'random', label: 'Random' }
	];
	const providerAccounts = $derived(accounts.filter((a) => a.provider === model?.provider));

	function apply(e: SubmitEvent) {
		e.preventDefault();
		onApply({ ...override, name: name.trim(), effort, strategy, accounts: picked });
		open = false;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<form onsubmit={apply} class="space-y-6">
			<Dialog.Header>
				<Dialog.Title class="flex items-center gap-2">
					{#if model}<ProviderLogo provider={model.provider} />{/if}
					{model?.name}
				</Dialog.Title>
				<Dialog.Description class="font-mono">{model?.canonical}</Dialog.Description>
			</Dialog.Header>
			<Field.Group>
				<Field.Field>
					<Field.Label for="md-name">Display name</Field.Label>
					<Input id="md-name" placeholder={model?.name} bind:value={name} />
					<Field.Description>Shown in model lists. Leave empty for the default.</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label>Default reasoning effort</Field.Label>
					<Select.Root type="single" bind:value={effort}>
						<Select.Trigger class="w-full">{effort || 'Upstream default'}</Select.Trigger>
						<Select.Content>
							{#each efforts as e (e)}
								<Select.Item value={e} label={e || 'Upstream default'}
									>{e || 'Upstream default'}</Select.Item
								>
							{/each}
						</Select.Content>
					</Select.Root>
					<Field.Description
						>Used when a request doesn't ask for a reasoning level itself.</Field.Description
					>
				</Field.Field>
				<Field.Field>
					<Field.Label>Picking an account</Field.Label>
					<Select.Root type="single" bind:value={strategy}>
						<Select.Trigger class="w-full">
							{strategies.find((s) => s.value === strategy)?.label}
						</Select.Trigger>
						<Select.Content>
							{#each strategies as s (s.value)}
								<Select.Item value={s.value} label={s.label}>{s.label}</Select.Item>
							{/each}
						</Select.Content>
					</Select.Root>
				</Field.Field>
				<Field.Set>
					<Field.Legend variant="label">Accounts that may serve it</Field.Legend>
					{#if providerAccounts.length === 0}
						<p class="text-sm text-muted-foreground">No accounts for this provider yet.</p>
					{:else}
						<div class="space-y-2">
							{#each providerAccounts as a (a.id)}
								<div class="flex items-center gap-2.5">
									<Checkbox
										id="md-acc-{a.id}"
										checked={picked.includes(a.id)}
										onCheckedChange={(v) =>
											(picked = v === true ? [...picked, a.id] : picked.filter((x) => x !== a.id))}
									/>
									<Label for="md-acc-{a.id}" class="font-normal">{a.label}</Label>
								</div>
							{/each}
						</div>
						<Field.Description>None ticked means any account.</Field.Description>
					{/if}
				</Field.Set>
			</Field.Group>
			<Dialog.Footer>
				<Button type="submit">Apply</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
