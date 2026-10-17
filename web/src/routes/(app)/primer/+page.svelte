<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { admin, errorMessage, type Account, type Settings } from '#lib/api.js';
	import type { PrimerPolicy, Provider } from '#lib/types.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import PrimerDialog from '#lib/components/primer-dialog.svelte';
	import PrimerPolicyFields from '#lib/components/primer-policy-fields.svelte';
	import PingModelSelect from '#lib/components/ping-model-select.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { Draft } from '#lib/draft.svelte.js';
	import { providerName } from '#lib/format.js';
	import { clone, defaultModels } from '#lib/primer.js';
	import { saveSettings } from '#lib/settings.js';
	import Settings2Icon from '@lucide/svelte/icons/settings-2';
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

	// ?schedule=claude opens that provider's tab, e.g. from an account's primer dialog.
	let tab = $state('shared');
	$effect(() => {
		const s = page.url.searchParams.get('schedule');
		untrack(() => (tab = providers.includes(s as Provider) ? (s as Provider) : 'shared'));
	});

	// A provider's own schedule is kept while it's switched off and on, until saved.
	const stashed: Partial<Record<Provider, PrimerPolicy>> = {};
	function setProviderOwn(p: Provider, on: boolean) {
		const all = (form.pool.primer.providers ??= {});
		if (on) all[p] = stashed[p] ?? clone(form.pool.primer.schedule);
		else {
			stashed[p] = clone(all[p]);
			delete all[p];
		}
	}

	let dialogOpen = $state(false);
	let dialogAccount = $state<Account | null>(null);
	function openAccount(a: Account) {
		dialogAccount = a;
		dialogOpen = true;
	}

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
				<Card.Title>Schedules</Card.Title>
				<Card.Description>
					Pick when limits should reset. Each session is started 5 hours earlier with a tiny
					request. A provider, or a single account, can have its own.
				</Card.Description>
			</Card.Header>
			<Card.Content>
				<Tabs.Root bind:value={tab}>
					<Tabs.List>
						<Tabs.Trigger value="shared">Shared</Tabs.Trigger>
						{#each providers as p (p)}
							<Tabs.Trigger value={p}>
								<ProviderLogo provider={p} />
								{providerName[p]}
								{#if form.pool.primer.providers?.[p]}
									<span class="size-1.5 rounded-full bg-primary" aria-label="Own schedule"></span>
								{/if}
							</Tabs.Trigger>
						{/each}
					</Tabs.List>
					<Tabs.Content value="shared" class="pt-4">
						<PrimerPolicyFields
							bind:policy={form.pool.primer.schedule}
							serverTimezone={data.serverTimezone}
							idPrefix="primer"
						/>
					</Tabs.Content>
					{#each providers as p (p)}
						{@const own = form.pool.primer.providers?.[p]}
						<Tabs.Content value={p} class="space-y-4 pt-4">
							<Field.Field orientation="horizontal">
								<Field.Content>
									<Field.Label for="primer-own-{p}">Own schedule for {providerName[p]}</Field.Label>
									<Field.Description>
										Off, {providerName[p]} accounts follow the shared schedule. Accounts with their own
										schedule keep it either way.
									</Field.Description>
								</Field.Content>
								<Switch
									id="primer-own-{p}"
									checked={!!own}
									onCheckedChange={(on) => setProviderOwn(p, on)}
								/>
							</Field.Field>
							{#if own}
								<PrimerPolicyFields
									bind:policy={() => own, (v) => ((form.pool.primer.providers ??= {})[p] = v)}
									serverTimezone={data.serverTimezone}
									idPrefix="primer-{p}"
								/>
							{/if}
						</Tabs.Content>
					{/each}
				</Tabs.Root>
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
										{#if data.settings.pool.primer.providers?.[g.provider]}
											<Badge variant="outline">{providerName[g.provider]} schedule</Badge>
										{/if}
									</label>
									<ul class="ml-6">
										{#each g.accounts as a (a.id)}
											<li class="flex items-center gap-2">
												<label class="flex min-w-0 flex-1 items-center gap-2 py-1.5 text-sm">
													<Checkbox bind:checked={enabled.value[a.id]} />
													<PrivateText class="truncate">{a.label}</PrivateText>
													{#if a.primer.schedule}
														<Badge variant="outline">Own schedule</Badge>
													{/if}
													{#if a.disabled}
														<span class="text-xs text-muted-foreground">Disabled</span>
													{/if}
												</label>
												<Button
													size="icon-xs"
													variant="ghost"
													class="text-muted-foreground"
													aria-label="Session primer settings for {a.label}"
													onclick={() => openAccount(a)}
												>
													<Settings2Icon />
												</Button>
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

<PrimerDialog
	account={dialogAccount}
	bind:open={dialogOpen}
	shared={data.settings.pool.primer}
	catalog={data.models}
	serverTimezone={data.serverTimezone}
/>
