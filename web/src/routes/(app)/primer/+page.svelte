<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account, type Settings } from '#lib/api.js';
	import type { Provider } from '#lib/types.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import PrimerPolicyFields from '#lib/components/primer-policy-fields.svelte';
	import PingModelSelect from '#lib/components/ping-model-select.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { Draft } from '#lib/draft.svelte.js';
	import { providerName } from '#lib/format.js';
	import { defaultModels } from '#lib/primer.js';
	import { saveSettings } from '#lib/settings.js';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<Settings>(untrack(() => data.settings));
	$effect.pre(() => draft.sync(data.settings));
	const form = $derived(draft.value);

	// Which accounts have the primer on, edited alongside the settings and saved with them.
	const enabledOf = (accounts: Account[]) =>
		Object.fromEntries(accounts.map((a) => [a.id, a.primer.enabled])) as Record<number, boolean>;
	const enabled = new Draft(untrack(() => enabledOf(data.accounts)));
	$effect.pre(() => enabled.sync(enabledOf(data.accounts)));

	const providers: Provider[] = ['claude', 'codex'];
	const groups = $derived(
		providers
			.map((p) => ({ provider: p, accounts: data.accounts.filter((a) => a.provider === p) }))
			.filter((g) => g.accounts.length)
	);

	function setGroup(accounts: Account[], on: boolean) {
		for (const a of accounts) enabled.value[a.id] = on;
	}

	let saving = $state(false);

	async function save() {
		saving = true;
		try {
			if (draft.dirty && !(await saveSettings(draft, false))) return;
			const changed = data.accounts.filter((a) => enabled.value[a.id] !== a.primer.enabled);
			await Promise.all(
				changed.map((a) =>
					admin.updateAccount(a.id, { primer: { ...a.primer, enabled: enabled.value[a.id] } })
				)
			);
			enabled.commit();
			toast.success('Saved');
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			await invalidateAll();
			saving = false;
		}
	}

	function reset() {
		draft.reset();
		enabled.reset();
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Session primer</h1>
		<p class="text-sm text-muted-foreground">
			Starts sessions ahead of time so limits reset when you want them to.
		</p>
	</div>

	<form
		class="grid items-start gap-6 xl:grid-cols-2"
		onsubmit={(e) => {
			e.preventDefault();
			save();
		}}
	>
		<Card.Root>
			<Card.Header>
				<Card.Title>Shared schedule</Card.Title>
				<Card.Description>
					Pick when limits should reset. Each session is started 5 hours earlier with a tiny
					request.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<PrimerPolicyFields
					bind:policy={form.pool.primer.schedule}
					serverTimezone={data.serverTimezone}
					idPrefix="primer"
				/>
			</Card.Content>
		</Card.Root>

		<div class="grid gap-6">
			<Card.Root>
				<Card.Header>
					<Card.Title>Accounts</Card.Title>
				</Card.Header>
				<Card.Content>
					{#if groups.length}
						<div class="space-y-4">
							{#each groups as g (g.provider)}
								{@const on = g.accounts.filter((a) => enabled.value[a.id]).length}
								<div>
									<label class="flex items-center gap-2 py-1.5 text-sm font-medium">
										<Checkbox
											bind:checked={() => on === g.accounts.length, (v) => setGroup(g.accounts, v)}
											indeterminate={on > 0 && on < g.accounts.length}
										/>
										<ProviderLogo provider={g.provider} />
										{providerName[g.provider]}
										<span class="text-xs font-normal text-muted-foreground tabular-nums">
											{on}/{g.accounts.length}
										</span>
									</label>
									<ul class="ml-6">
										{#each g.accounts as a (a.id)}
											<li>
												<label class="flex items-center gap-2 py-1.5 text-sm">
													<Checkbox bind:checked={enabled.value[a.id]} />
													<PrivateText class="truncate">{a.label}</PrivateText>
													{#if a.primer.schedule}
														<Badge variant="outline">Own schedule</Badge>
													{/if}
													{#if a.disabled}
														<span class="text-xs text-muted-foreground">Disabled</span>
													{/if}
												</label>
											</li>
										{/each}
									</ul>
								</div>
							{/each}
						</div>
					{:else}
						<p class="text-sm text-muted-foreground">
							No accounts yet. <Button variant="link" class="h-auto p-0" href="/accounts"
								>Add one</Button
							>
						</p>
					{/if}
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header>
					<Card.Title>Ping</Card.Title>
				</Card.Header>
				<Card.Content>
					<Field.Group>
						{#each providers as p (p)}
							<Field.Field>
								<Field.Label for="primer-model-{p}" class="flex items-center gap-2">
									<ProviderLogo provider={p} />
									{providerName[p]} model
								</Field.Label>
								<PingModelSelect
									id="primer-model-{p}"
									class="sm:w-80"
									provider={p}
									catalog={data.models}
									bind:value={
										() => form.pool.primer.models[p] || defaultModels[p],
										(v) => (form.pool.primer.models[p] = v)
									}
								/>
							</Field.Field>
						{/each}
						<Field.Field>
							<Field.Label for="primer-prompt">Prompt</Field.Label>
							<Input
								id="primer-prompt"
								class="sm:w-80"
								maxlength={200}
								bind:value={form.pool.primer.prompt}
							/>
						</Field.Field>
					</Field.Group>
				</Card.Content>
			</Card.Root>
		</div>
	</form>

	<UnsavedBar dirty={draft.dirty || enabled.dirty} {saving} onSave={save} onReset={reset} />
</div>
