<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type ProfileConfig, type QuotaView } from '#lib/api.js';
	import { providerName } from '#lib/format.js';
	import { Draft } from '#lib/draft.svelte.js';
	import QuotaViewComponent from '#lib/components/quota-view.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	type Form = { name: string; config: ProfileConfig };
	const toForm = (): Form => ({ name: data.profile.name, config: data.profile.config });

	const draft = new Draft<Form>(untrack(toForm));
	let loadedFor = untrack(() => data.profile.id);
	$effect.pre(() => {
		const next = toForm();
		// Switching to another profile discards the draft; a refresh of the same one keeps edits.
		if (data.profile.id !== loadedFor) {
			loadedFor = data.profile.id;
			draft.base = next;
			draft.reset();
		} else {
			draft.sync(next);
		}
	});
	const form = $derived(draft.value);

	let saving = $state(false);
	let preview = $state<QuotaView | null>(null);
	let previewError = $state('');

	// Re-render the preview shortly after each change.
	$effect(() => {
		const snapshot = JSON.stringify(form.config);
		const timer = setTimeout(async () => {
			try {
				preview = (await admin.previewProfile(JSON.parse(snapshot))).view;
				previewError = '';
			} catch (e) {
				previewError = errorMessage(e);
			}
		}, 250);
		return () => clearTimeout(timer);
	});

	function toggle<T>(list: T[], item: T, on: boolean): T[] {
		return on ? [...new Set([...list, item])] : list.filter((x) => x !== item);
	}

	async function save() {
		if (!form.name.trim()) {
			toast.error('The profile needs a name');
			return;
		}
		saving = true;
		try {
			const next = draft.snapshot();
			const { profile } = await admin.updateProfile(data.profile.id, next);
			draft.value = { name: profile.name, config: profile.config };
			draft.commit();
			toast.success('Profile saved');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			saving = false;
		}
	}
</script>

{#snippet radios(
	id: string,
	value: string,
	set: (v: string) => void,
	options: { value: string; label: string; hint?: string }[]
)}
	<RadioGroup.Root {value} onValueChange={set} class="gap-2.5">
		{#each options as o (o.value)}
			<div class="flex items-start gap-3">
				<RadioGroup.Item value={o.value} id="{id}-{o.value}" class="mt-0.5" />
				<Label for="{id}-{o.value}" class="flex flex-col items-start gap-0.5 font-normal">
					<span>{o.label}</span>
					{#if o.hint}<span class="text-xs text-muted-foreground">{o.hint}</span>{/if}
				</Label>
			</div>
		{/each}
	</RadioGroup.Root>
{/snippet}

{#snippet check(id: string, label: string, checked: boolean, set: (v: boolean) => void)}
	<div class="flex items-center gap-2.5">
		<Checkbox {id} {checked} onCheckedChange={(v) => set(v === true)} />
		<Label for={id} class="font-normal">{label}</Label>
	</div>
{/snippet}

<div class="mx-auto max-w-6xl space-y-6">
	<div class="space-y-2">
		<a
			href="/quota-page"
			class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
		>
			<ArrowLeftIcon class="size-4" /> Quota page
		</a>
		<h1 class="text-xl font-semibold tracking-tight">{data.profile.name}</h1>
	</div>

	<div class="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
		<div class="space-y-6">
			<Card.Root>
				<Card.Content>
					<Field.Field>
						<Field.Label for="p-name">Profile name</Field.Label>
						<Input id="p-name" maxlength={100} placeholder="e.g. Friends" bind:value={form.name} />
					</Field.Field>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header><Card.Title>What's shown</Card.Title></Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Set>
							<Field.Legend variant="label">Providers</Field.Legend>
							<div class="flex gap-6">
								{#each ['claude', 'codex'] as const as p (p)}
									<div class="flex items-center gap-2.5">
										<Checkbox
											id="prov-{p}"
											checked={form.config.providers.includes(p)}
											onCheckedChange={(v) =>
												(form.config.providers = toggle(form.config.providers, p, v === true))}
										/>
										<Label for="prov-{p}" class="gap-1.5 font-normal">
											<ProviderLogo provider={p} />
											{providerName[p]}
										</Label>
									</div>
								{/each}
							</div>
						</Field.Set>
						<Field.Set>
							<Field.Legend variant="label">Limits</Field.Legend>
							<div class="grid gap-2.5 sm:grid-cols-2">
								{@render check(
									'w-session',
									'Short window (e.g. 5-hour)',
									form.config.windows.session,
									(v) => (form.config.windows.session = v)
								)}
								{@render check(
									'w-weekly',
									'Weekly',
									form.config.windows.weekly,
									(v) => (form.config.windows.weekly = v)
								)}
								{@render check(
									'w-model',
									'Per-model weekly',
									form.config.windows.modelWeekly,
									(v) => (form.config.windows.modelWeekly = v)
								)}
								{@render check(
									'w-other',
									'Other limits',
									form.config.windows.other,
									(v) => (form.config.windows.other = v)
								)}
								{@render check(
									'w-extra',
									'Extra usage / credits',
									form.config.extra,
									(v) => (form.config.extra = v)
								)}
							</div>
						</Field.Set>
						<Field.Set>
							<Field.Legend variant="label">Layout</Field.Legend>
							{@render radios(
								'mode',
								form.config.mode,
								(v) => (form.config.mode = v as ProfileConfig['mode']),
								[
									{ value: 'pooled', label: 'One combined entry per provider' },
									{ value: 'accounts', label: 'Each account separately' },
									{ value: 'both', label: 'Both' }
								]
							)}
						</Field.Set>
						{#if form.config.mode !== 'accounts'}
							<Field.Set>
								<Field.Legend variant="label">Combined value</Field.Legend>
								{@render radios(
									'agg',
									form.config.poolAggregate,
									(v) => (form.config.poolAggregate = v as ProfileConfig['poolAggregate']),
									[
										{ value: 'average', label: 'Average left across accounts' },
										{ value: 'min', label: 'Lowest account', hint: 'The most pessimistic view.' },
										{ value: 'max', label: 'Highest account' },
										{
											value: 'total',
											label: 'Total left, in accounts',
											hint: 'e.g. "1.6 of 3 accounts left".'
										}
									]
								)}
							</Field.Set>
						{/if}
						{#if form.config.mode !== 'pooled'}
							<Field.Set>
								<Field.Legend variant="label">Account names</Field.Legend>
								{@render radios(
									'naming',
									form.config.accountNaming,
									(v) => (form.config.accountNaming = v as ProfileConfig['accountNaming']),
									[
										{ value: 'anonymous', label: 'Numbered', hint: '"Account 1", "Account 2"…' },
										{
											value: 'alias',
											label: 'Public name',
											hint: 'Set per account; numbered if empty.'
										},
										{ value: 'label', label: 'Real label', hint: 'Usually the email address.' },
										{ value: 'hidden', label: 'No names' }
									]
								)}
							</Field.Set>
							<Field.Set>
								<Field.Legend variant="label">Account details</Field.Legend>
								<div class="flex flex-wrap gap-6">
									{@render check(
										'plan',
										'Plan',
										form.config.showPlan,
										(v) => (form.config.showPlan = v)
									)}
									{@render check(
										'status',
										'Status',
										form.config.showStatus,
										(v) => (form.config.showStatus = v)
									)}
									{@render check(
										'amounts',
										'Credit amounts in dollars',
										form.config.showAmounts,
										(v) => (form.config.showAmounts = v)
									)}
								</div>
							</Field.Set>
						{/if}
						{#if data.accounts.length}
							<Field.Set>
								<Field.Legend variant="label">Hide accounts</Field.Legend>
								<Field.Description
									>Hidden accounts are left out entirely, including combined values.</Field.Description
								>
								<div class="grid gap-2.5 sm:grid-cols-2">
									{#each data.accounts as a (a.id)}
										{@render check(
											`hide-${a.id}`,
											`${a.label} (${providerName[a.provider]})`,
											form.config.hiddenAccounts.includes(a.id),
											(v) =>
												(form.config.hiddenAccounts = toggle(form.config.hiddenAccounts, a.id, v))
										)}
									{/each}
								</div>
							</Field.Set>
						{/if}
					</Field.Group>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header><Card.Title>How it's shown</Card.Title></Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Set>
							<Field.Legend variant="label">Numbers</Field.Legend>
							{@render radios(
								'num',
								form.config.numberFormat,
								(v) => (form.config.numberFormat = v as ProfileConfig['numberFormat']),
								[
									{ value: 'remaining', label: 'Percent left' },
									{ value: 'used', label: 'Percent used' },
									{ value: 'bar', label: 'Bar only', hint: 'Rounded to 10%, no number shown.' },
									{
										value: 'level',
										label: 'Rough level only',
										hint: 'Plenty / OK / Low / Used up. Exact values never leave the server.'
									}
								]
							)}
						</Field.Set>
						<Field.Set>
							<Field.Legend variant="label">Reset times</Field.Legend>
							{@render radios(
								'reset',
								form.config.resetFormat,
								(v) => (form.config.resetFormat = v as ProfileConfig['resetFormat']),
								[
									{
										value: 'both',
										label: 'Both',
										hint: '"Resets Oct 12, 3:00 PM · in 2h 15m"'
									},
									{ value: 'relative', label: 'Countdown', hint: '"Resets in 2h 15m"' },
									{ value: 'exact', label: 'Date and time' },
									{ value: 'hidden', label: 'Hidden' }
								]
							)}
						</Field.Set>
						{@render check(
							'updated',
							'Show when limits were last updated',
							form.config.showUpdatedAt,
							(v) => (form.config.showUpdatedAt = v)
						)}
					</Field.Group>
				</Card.Content>
			</Card.Root>

			<Card.Root>
				<Card.Header><Card.Title>Codex extras</Card.Title></Card.Header>
				<Card.Content>
					<Field.Group>
						<Field.Set>
							<Field.Legend variant="label">Banked resets</Field.Legend>
							{@render radios(
								'banked',
								form.config.banked,
								(v) => (form.config.banked = v as ProfileConfig['banked']),
								[
									{ value: 'hidden', label: 'Hidden' },
									{ value: 'count', label: 'Count only' },
									{ value: 'count_expiry', label: 'Count and expiry dates' }
								]
							)}
						</Field.Set>
						{#if form.config.banked !== 'hidden'}
							<Field.Set>
								<Field.Legend variant="label">Banked resets per</Field.Legend>
								{@render radios(
									'bmode',
									form.config.bankedMode,
									(v) => (form.config.bankedMode = v as ProfileConfig['bankedMode']),
									[
										{ value: 'pooled', label: 'Provider total' },
										{ value: 'accounts', label: 'Account' }
									]
								)}
							</Field.Set>
						{/if}
						<Field.Set>
							<Field.Legend variant="label">codex-resets.com feed</Field.Legend>
							<div class="grid gap-2.5 sm:grid-cols-2">
								{@render check(
									'cr-sched',
									'Upcoming announced reset',
									form.config.codexResets.scheduled,
									(v) => (form.config.codexResets.scheduled = v)
								)}
								{@render check(
									'cr-latest',
									'Most recent reset',
									form.config.codexResets.latest,
									(v) => (form.config.codexResets.latest = v)
								)}
								{@render check(
									'cr-forecast',
									'AI forecast',
									form.config.codexResets.forecast,
									(v) => (form.config.codexResets.forecast = v)
								)}
								{@render check(
									'cr-stats',
									'Reset statistics',
									form.config.codexResets.stats,
									(v) => (form.config.codexResets.stats = v)
								)}
							</div>
						</Field.Set>
					</Field.Group>
				</Card.Content>
			</Card.Root>
		</div>

		<div class="space-y-3 lg:sticky lg:top-4">
			<p class="text-sm font-medium text-muted-foreground">Preview with live data</p>
			<div class="rounded-xl border bg-muted/30 p-4">
				{#if previewError}
					<p class="text-sm text-destructive">{previewError}</p>
				{:else if preview}
					<QuotaViewComponent view={preview} />
				{:else}
					<p class="text-sm text-muted-foreground">Loading…</p>
				{/if}
			</div>
		</div>
	</div>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>
