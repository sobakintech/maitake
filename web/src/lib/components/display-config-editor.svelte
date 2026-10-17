<script lang="ts">
	import type { Snippet } from 'svelte';
	import {
		admin,
		errorMessage,
		type Account,
		type ProfileConfig,
		type QuotaView
	} from '#lib/api.js';
	import { providerName } from '#lib/format.js';
	import QuotaViewComponent from '#lib/components/quota-view.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Checkbox } from '#lib/components/ui/checkbox/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';

	// Edits a quota page view (what a key's holder gets to see) next to a live preview.
	let {
		config = $bindable(),
		accounts,
		top
	}: { config: ProfileConfig; accounts: Account[]; top?: Snippet } = $props();

	let preview = $state<QuotaView | null>(null);
	let previewError = $state('');

	// Re-render the preview shortly after each change.
	$effect(() => {
		const snapshot = JSON.stringify(config);
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

{#snippet percent(id: string, label: string, value: number, set: (v: number) => void)}
	<Field.Field>
		<Field.Label for={id} class="font-normal">{label}</Field.Label>
		<div class="flex items-center gap-2">
			<Input
				{id}
				type="number"
				min={0}
				max={100}
				class="w-24"
				{value}
				oninput={(e) => set(Number(e.currentTarget.value))}
			/>
			<span class="text-sm text-muted-foreground">% left</span>
		</div>
	</Field.Field>
{/snippet}

{#snippet check(id: string, label: string, checked: boolean, set: (v: boolean) => void)}
	<div class="flex items-center gap-2.5">
		<Checkbox {id} {checked} onCheckedChange={(v) => set(v === true)} />
		<Label for={id} class="font-normal">{label}</Label>
	</div>
{/snippet}

<div class="grid items-start gap-6 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)]">
	<div class="space-y-6">
		{@render top?.()}
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
										checked={config.providers.includes(p)}
										onCheckedChange={(v) =>
											(config.providers = toggle(config.providers, p, v === true))}
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
								config.windows.session,
								(v) => (config.windows.session = v)
							)}
							{@render check(
								'w-weekly',
								'Weekly',
								config.windows.weekly,
								(v) => (config.windows.weekly = v)
							)}
							{@render check(
								'w-model',
								'Per-model weekly',
								config.windows.modelWeekly,
								(v) => (config.windows.modelWeekly = v)
							)}
							{@render check(
								'w-other',
								'Other limits',
								config.windows.other,
								(v) => (config.windows.other = v)
							)}
							{@render check(
								'w-extra',
								'Extra usage / credits',
								config.extra,
								(v) => (config.extra = v)
							)}
						</div>
					</Field.Set>
					<Field.Set>
						<Field.Legend variant="label">Grouping</Field.Legend>
						{@render radios(
							'mode',
							config.mode,
							(v) => (config.mode = v as ProfileConfig['mode']),
							[
								{ value: 'pooled', label: 'One combined entry per provider' },
								{ value: 'accounts', label: 'Each account separately' },
								{ value: 'both', label: 'Both' }
							]
						)}
					</Field.Set>
					{#if config.mode !== 'accounts'}
						<Field.Set>
							<Field.Legend variant="label">Combined value</Field.Legend>
							{@render radios(
								'agg',
								config.poolAggregate,
								(v) => (config.poolAggregate = v as ProfileConfig['poolAggregate']),
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
					{#if config.mode !== 'pooled'}
						<Field.Set>
							<Field.Legend variant="label">Account names</Field.Legend>
							{@render radios(
								'naming',
								config.accountNaming,
								(v) => (config.accountNaming = v as ProfileConfig['accountNaming']),
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
								{@render check('plan', 'Plan', config.showPlan, (v) => (config.showPlan = v))}
								{@render check(
									'status',
									'Status',
									config.showStatus,
									(v) => (config.showStatus = v)
								)}
								{@render check(
									'amounts',
									'Credit amounts in dollars',
									config.showAmounts,
									(v) => (config.showAmounts = v)
								)}
							</div>
						</Field.Set>
					{/if}
					{#if accounts.length}
						<Field.Set>
							<Field.Legend variant="label">Hide accounts</Field.Legend>
							<Field.Description
								>Hidden accounts are left out entirely, including combined values.</Field.Description
							>
							<div class="grid gap-2.5 sm:grid-cols-2">
								{#each accounts as a (a.id)}
									{@render check(
										`hide-${a.id}`,
										`${a.label} (${providerName[a.provider]})`,
										config.hiddenAccounts.includes(a.id),
										(v) => (config.hiddenAccounts = toggle(config.hiddenAccounts, a.id, v))
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
							config.numberFormat,
							(v) => (config.numberFormat = v as ProfileConfig['numberFormat']),
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
						{#if config.numberFormat === 'remaining' || config.numberFormat === 'used'}
							{@render check(
								'flip',
								'Let viewers click a number to switch between left and used',
								config.allowFlip,
								(v) => (config.allowFlip = v)
							)}
						{/if}
					</Field.Set>
					<Field.Set>
						<Field.Legend variant="label">Levels</Field.Legend>
						<Field.Description
							>How much has to be left before a limit counts as low and turns yellow, or reads as
							only OK.</Field.Description
						>
						<div class="grid gap-3 sm:grid-cols-2">
							{@render percent('lvl-low', 'Low at or below', config.levels.low, (v) => {
								config.levels.low = v;
							})}
							{@render percent('lvl-ok', 'OK at or below', config.levels.ok, (v) => {
								config.levels.ok = v;
							})}
						</div>
					</Field.Set>
					<Field.Set>
						<Field.Legend variant="label">Reset times</Field.Legend>
						{@render radios(
							'reset',
							config.resetFormat,
							(v) => (config.resetFormat = v as ProfileConfig['resetFormat']),
							[
								{
									value: 'both',
									label: 'Both',
									hint: '"Resets in 2h 15m · Oct 12, 3:00 PM"'
								},
								{ value: 'relative', label: 'Countdown', hint: '"Resets in 2h 15m"' },
								{ value: 'exact', label: 'Date and time' },
								{ value: 'hidden', label: 'Hidden' }
							]
						)}
					</Field.Set>
					<Field.Set>
						<Field.Legend variant="label">Pace</Field.Legend>
						<Field.Description
							>Projects each limit to its reset at the rate it's being used, and marks where even
							use would be on the bar.</Field.Description
						>
						{@render radios(
							'pace',
							config.pace,
							(v) => (config.pace = v as ProfileConfig['pace']),
							[
								{
									value: 'warnings',
									label: 'Only when running low',
									hint: '"~4% spare", "Limit in 1h 20m"'
								},
								{ value: 'always', label: 'Always', hint: 'Also "~45% left at reset"' },
								{ value: 'hidden', label: 'Hidden' }
							]
						)}
					</Field.Set>
					{@render check(
						'updated',
						'Show when limits were last updated',
						config.showUpdatedAt,
						(v) => (config.showUpdatedAt = v)
					)}
				</Field.Group>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header><Card.Title>Layout</Card.Title></Card.Header>
			<Card.Content>
				<Field.Group>
					<Field.Set>
						<Field.Legend variant="label">Arrangement</Field.Legend>
						{@render radios(
							'layout',
							config.layout,
							(v) => (config.layout = v as ProfileConfig['layout']),
							[
								{ value: 'cards', label: 'Cards', hint: 'Full bars, with banked resets inline.' },
								{
									value: 'rows',
									label: 'Rows',
									hint: 'One compact line per account; banked resets open on click.'
								}
							]
						)}
						{@render check(
							'layout-switch',
							'Let viewers switch, remembered in their browser',
							config.allowLayoutSwitch,
							(v) => (config.allowLayoutSwitch = v)
						)}
					</Field.Set>
					<Field.Set>
						<Field.Legend variant="label">Page width</Field.Legend>
						{@render radios(
							'width',
							config.width,
							(v) => (config.width = v as ProfileConfig['width']),
							[
								{ value: 'narrow', label: 'Narrow' },
								{ value: 'wide', label: 'Wide', hint: 'Room for three accounts side by side.' },
								{ value: 'full', label: 'Full window' }
							]
						)}
					</Field.Set>
					{#if config.mode !== 'pooled'}
						<Field.Set>
							<Field.Legend variant="label">Account order</Field.Legend>
							{@render radios(
								'sort',
								config.accountSort,
								(v) => (config.accountSort = v as ProfileConfig['accountSort']),
								[
									{ value: 'default', label: 'As added' },
									{ value: 'name', label: 'By name' },
									{
										value: 'most_left',
										label: 'Most left first',
										hint: 'Going by the lowest of the limits shown.'
									},
									{ value: 'least_left', label: 'Least left first' }
								]
							)}
						</Field.Set>
					{/if}
					<Field.Set>
						<Field.Legend variant="label">Reset feeds</Field.Legend>
						{@render radios(
							'feeds-pos',
							config.feedsPosition,
							(v) => (config.feedsPosition = v as ProfileConfig['feedsPosition']),
							[
								{ value: 'bottom', label: 'Below the accounts' },
								{ value: 'top', label: 'Above the accounts' }
							]
						)}
					</Field.Set>
					{#if config.mode !== 'accounts'}
						{@render check(
							'acct-count',
							'Show how many accounts are in the pool',
							config.showAccountCount,
							(v) => (config.showAccountCount = v)
						)}
					{/if}
				</Field.Group>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header><Card.Title>Resets</Card.Title></Card.Header>
			<Card.Content>
				<Field.Group>
					<Field.Set>
						<Field.Legend variant="label">Banked resets</Field.Legend>
						{@render radios(
							'banked',
							config.banked,
							(v) => (config.banked = v as ProfileConfig['banked']),
							[
								{ value: 'hidden', label: 'Hidden' },
								{ value: 'count', label: 'Count only' },
								{ value: 'count_expiry', label: 'Count and next expiry' },
								{
									value: 'full',
									label: 'Every reset',
									hint: 'Each with its expiry and grant date; in a provider total, also which account holds it.'
								}
							]
						)}
					</Field.Set>
					{#if config.banked !== 'hidden'}
						<Field.Set>
							<Field.Legend variant="label">Banked resets per</Field.Legend>
							{@render radios(
								'bmode',
								config.bankedMode,
								(v) => (config.bankedMode = v as ProfileConfig['bankedMode']),
								[
									{ value: 'pooled', label: 'Provider total' },
									{ value: 'accounts', label: 'Account' }
								]
							)}
						</Field.Set>
					{/if}
					<Field.Set>
						<Field.Legend variant="label">claude-resets.com feed</Field.Legend>
						<div class="grid gap-2.5 sm:grid-cols-2">
							{@render check(
								'clr-latest',
								'Most recent reset',
								config.claudeResets.latest,
								(v) => (config.claudeResets.latest = v)
							)}
							{@render check(
								'clr-stats',
								'Reset statistics',
								config.claudeResets.stats,
								(v) => (config.claudeResets.stats = v)
							)}
						</div>
					</Field.Set>
					<Field.Set>
						<Field.Legend variant="label">codex-resets.com feed</Field.Legend>
						<div class="grid gap-2.5 sm:grid-cols-2">
							{@render check(
								'cr-sched',
								'Upcoming announced reset',
								config.codexResets.scheduled,
								(v) => (config.codexResets.scheduled = v)
							)}
							{@render check(
								'cr-latest',
								'Most recent reset',
								config.codexResets.latest,
								(v) => (config.codexResets.latest = v)
							)}
							{@render check(
								'cr-forecast',
								'AI forecast',
								config.codexResets.forecast,
								(v) => (config.codexResets.forecast = v)
							)}
							{@render check(
								'cr-stats',
								'Reset statistics',
								config.codexResets.stats,
								(v) => (config.codexResets.stats = v)
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
