<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account } from '#lib/api.js';
	import type { CatalogModel, PrimerPolicy, PrimerRun, PrimerSettings } from '#lib/types.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import PrimerPolicyFields from '#lib/components/primer-policy-fields.svelte';
	import PingModelSelect from '#lib/components/ping-model-select.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import { shortDateTime } from '#lib/format.js';
	import {
		clone,
		defaultModels,
		describePolicy,
		modelLabel,
		modelOptions,
		outcomeLabel,
		primerNumbers,
		triggerLabel
	} from '#lib/primer.js';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';

	// One account's session primer: on/off, shared or own schedules, and ping model.
	let {
		account,
		open = $bindable(false),
		shared,
		catalog,
		serverTimezone
	}: {
		account: Account | null;
		open: boolean;
		shared: PrimerSettings;
		catalog: CatalogModel[];
		serverTimezone: string;
	} = $props();

	let enabled = $state(false);
	let mode = $state<'shared' | 'own'>('shared');
	// The own schedule is kept while switching back and forth, and only saved in "own" mode.
	let own = $state<PrimerPolicy>({} as PrimerPolicy);
	let model = $state('');
	let runs = $state<PrimerRun[]>([]);
	let busy = $state(false);

	// Reset the form when the dialog opens, not when the page's data refreshes behind it.
	$effect(() => {
		if (!open || !account) return;
		const a = account;
		untrack(() => {
			enabled = a.primer.enabled;
			mode = a.primer.schedule ? 'own' : 'shared';
			own = clone(a.primer.schedule ?? shared.schedule);
			model = a.primer.model ?? '';
			runs = [];
		});
		admin
			.primerRuns(a.id)
			.then((r) => (runs = r.runs.slice(0, 8)))
			.catch(() => {});
	});

	const sharedLines = $derived(describePolicy(shared.schedule));
	const sharedModel = $derived.by(() => {
		if (!account) return '';
		const m = shared.models[account.provider] || defaultModels[account.provider];
		return modelLabel(m, modelOptions(account.provider, catalog));
	});
	const segment =
		'flex-1 data-[state=on]:bg-primary data-[state=on]:text-primary-foreground hover:data-[state=on]:bg-primary/90';
	const outcomeVariant: Record<PrimerRun['outcome'], 'outline' | 'secondary' | 'destructive'> = {
		started: 'outline',
		sent: 'outline',
		skipped: 'secondary',
		failed: 'destructive'
	};

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!account) return;
		busy = true;
		try {
			await admin.updateAccount(account.id, {
				primer: {
					enabled,
					model: model || undefined,
					schedule: mode === 'own' ? primerNumbers($state.snapshot(own)) : undefined
				}
			});
			toast.success('Saved');
			open = false;
			await invalidateAll();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			busy = false;
		}
	}

	function editShared() {
		open = false;
		goto('/primer');
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class="max-h-[90vh] overflow-y-auto sm:max-w-2xl">
		<form onsubmit={save} class="space-y-6">
			<Dialog.Header>
				<Dialog.Title class="flex items-center gap-2">
					{#if account}<ProviderLogo provider={account.provider} />{/if} Session primer
				</Dialog.Title>
				<Dialog.Description><PrivateText>{account?.label}</PrivateText></Dialog.Description>
			</Dialog.Header>
			{#if account}
				<Field.Group>
					<Field.Field orientation="horizontal">
						<Field.Label for="primer-on">Start sessions on a schedule</Field.Label>
						<Switch id="primer-on" bind:checked={enabled} />
					</Field.Field>

					<Field.Separator />

					<Field.Set>
						<div class="flex items-center justify-between gap-2">
							<Field.Legend class="mb-0">Schedule</Field.Legend>
							{#if mode === 'own' && sharedLines.length}
								<Button size="xs" variant="ghost" onclick={() => (own = clone(shared.schedule))}>
									Copy from shared
								</Button>
							{/if}
						</div>
						<ToggleGroup.Root
							type="single"
							variant="outline"
							class="w-full"
							value={mode}
							onValueChange={(v) => v && (mode = v as 'shared' | 'own')}
						>
							<ToggleGroup.Item value="shared" class={segment}>Shared</ToggleGroup.Item>
							<ToggleGroup.Item value="own" class={segment}>Only this account</ToggleGroup.Item>
						</ToggleGroup.Root>
						{#if mode === 'shared'}
							<div
								class="flex items-start justify-between gap-3 rounded-lg bg-muted/40 p-3 text-sm"
							>
								{#if sharedLines.length}
									<ul class="space-y-1">
										{#each sharedLines as line (line)}<li>{line}</li>{/each}
									</ul>
								{:else}
									<p class="text-muted-foreground">No times set.</p>
								{/if}
								<Button size="sm" variant="ghost" class="shrink-0" onclick={editShared}>Edit</Button
								>
							</div>
						{:else}
							<PrimerPolicyFields bind:policy={own} {serverTimezone} idPrefix="acct-primer" />
						{/if}
					</Field.Set>

					<Field.Separator />

					<Field.Field>
						<Field.Label for="primer-model">Ping model</Field.Label>
						<PingModelSelect
							id="primer-model"
							class="sm:w-80"
							provider={account.provider}
							{catalog}
							bind:value={model}
							defaultLabel="Shared ({sharedModel})"
						/>
					</Field.Field>

					{#if runs.length}
						<Field.Separator />
						<Field.Set>
							<Field.Legend>Recent runs</Field.Legend>
							<ul class="divide-y text-sm">
								{#each runs as r (r.id)}
									<li class="flex items-baseline gap-3 py-1.5">
										<span class="w-28 shrink-0 text-xs text-muted-foreground tabular-nums">
											{shortDateTime(r.time)}
										</span>
										<Badge variant={outcomeVariant[r.outcome]}>{outcomeLabel[r.outcome]}</Badge>
										<span class="min-w-0 truncate text-xs text-muted-foreground" title={r.message}>
											{triggerLabel[r.trigger]}{r.message ? ` · ${r.message}` : ''}
										</span>
									</li>
								{/each}
							</ul>
						</Field.Set>
					{/if}
				</Field.Group>
			{/if}
			<Dialog.Footer>
				<Button type="submit" disabled={busy}>Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
