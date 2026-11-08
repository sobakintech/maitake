<script lang="ts">
	import {
		admin,
		errorMessage,
		type AliasSettings,
		type AliasStatus,
		type RoutingSettings
	} from '#lib/api.js';
	import { relativeTime } from '#lib/format.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import RouteTester from '#lib/components/route-tester.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import ArrowRightIcon from '@lucide/svelte/icons/arrow-right';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { toast } from 'svelte-sonner';

	let { aliases = $bindable(), routing }: { aliases: AliasSettings; routing: RoutingSettings } =
		$props();

	const sources = [
		{
			value: 'live',
			label: 'What each account can use',
			hint: 'Asks every account for its models. A new model works as soon as an account lists it, and requests only go to accounts that list it.'
		},
		{
			value: 'catalog',
			label: 'The Models tab',
			hint: 'Only built-in and custom models. New ones arrive with a maitake update or when you add them.'
		}
	];
	const efforts = [
		{ value: '', label: 'Effort: request' },
		{ value: 'low', label: 'Effort: low' },
		{ value: 'medium', label: 'Effort: medium' },
		{ value: 'high', label: 'Effort: high' },
		{ value: 'xhigh', label: 'Effort: xhigh' },
		{ value: 'max', label: 'Effort: max' }
	];

	let statuses = $state<AliasStatus[]>([]);
	let checkedAt = $state<string | null>(null);
	let refreshing = $state(false);
	let refreshed = $state(0);
	let failed = $state('');

	const statusOf = (id: string) => statuses.find((s) => s.id === id);
	const problem = (st: AliasStatus) =>
		st.candidates.length ? st.error : 'No account can serve it';

	$effect(() => {
		const draft = $state.snapshot(routing) as RoutingSettings;
		void refreshed;
		const timer = setTimeout(async () => {
			try {
				const res = await admin.aliasStatus(draft);
				statuses = res.aliases;
				checkedAt = res.checkedAt;
				failed = '';
			} catch (e) {
				failed = errorMessage(e);
			}
		}, 250);
		return () => clearTimeout(timer);
	});

	function add() {
		aliases.lines = [
			...aliases.lines,
			{ id: crypto.randomUUID(), enabled: true, name: '', match: '', pin: '', effort: '' }
		];
	}

	async function refresh() {
		refreshing = true;
		try {
			await admin.refreshModelLists();
			refreshed++;
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			refreshing = false;
		}
	}
</script>

<Card.Root>
	<Card.Header>
		<Card.Title>Aliases</Card.Title>
		<Card.Description>
			Clients can ask for a line like <code>sol</code> or <code>opus</code> and get its newest model.
		</Card.Description>
	</Card.Header>
	<Card.Content class="space-y-6">
		<Field.Field orientation="horizontal">
			<Field.Content>
				<Field.Label for="aliases-on">Use aliases</Field.Label>
			</Field.Content>
			<Switch id="aliases-on" bind:checked={aliases.enabled} />
		</Field.Field>
		{#if aliases.enabled}
			<div class="space-y-3">
				{#if failed}
					<p class="text-sm text-destructive">{failed}</p>
				{/if}
				{#each aliases.lines as line (line.id)}
					{@const st = statusOf(line.id)}
					<div
						class="flex flex-wrap items-center gap-2 rounded-lg border p-3 {line.enabled
							? ''
							: 'opacity-60'}"
					>
						<Switch bind:checked={line.enabled} aria-label="Alias enabled" />
						<Input
							class="w-28 font-mono text-xs"
							placeholder="sol"
							aria-label="Alias name"
							bind:value={line.name}
						/>
						<Input
							class="w-40 font-mono text-xs"
							placeholder="gpt-*-sol"
							aria-label="Models this alias covers"
							bind:value={line.match}
						/>
						<ArrowRightIcon class="hidden size-4 shrink-0 text-muted-foreground sm:block" />
						{#if st?.model}
							<span class="flex items-center gap-1.5 text-sm font-medium">
								<ProviderLogo provider={st.model.provider} />
								{st.model.name}
							</span>
						{:else if st?.error}
							<span class="text-xs text-destructive">{problem(st)}</span>
						{:else}
							<span class="text-xs text-muted-foreground">—</span>
						{/if}
						<div class="ml-auto flex w-full items-center gap-2 sm:w-auto">
							<Select.Root type="single" bind:value={line.pin}>
								<Select.Trigger
									class="min-w-0 flex-1 sm:w-44 sm:flex-none"
									aria-label="Model to use"
								>
									<span class="truncate">{line.pin || 'Always newest'}</span>
								</Select.Trigger>
								<Select.Content>
									<Select.Item value="" label="Always newest">Always newest</Select.Item>
									{#if line.pin && !st?.candidates.some((c) => c.id === line.pin)}
										<Select.Item value={line.pin} label={line.pin}>{line.pin}</Select.Item>
									{/if}
									{#each st?.candidates ?? [] as c (c.id)}
										<Select.Item value={c.id} label={c.id}>{c.id}</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
							<Select.Root type="single" bind:value={line.effort}>
								<Select.Trigger
									class="min-w-0 flex-1 sm:w-40 sm:flex-none"
									aria-label="Default effort"
								>
									<span class="truncate">
										{efforts.find((e) => e.value === line.effort)?.label ?? line.effort}
									</span>
								</Select.Trigger>
								<Select.Content>
									{#each efforts as e (e.value)}
										<Select.Item value={e.value} label={e.label}>{e.label}</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
							<Button
								variant="ghost"
								size="icon-sm"
								aria-label="Delete alias"
								onclick={() => (aliases.lines = aliases.lines.filter((l) => l.id !== line.id))}
							>
								<Trash2Icon />
							</Button>
						</div>
					</div>
				{:else}
					<p class="rounded-lg border border-dashed py-6 text-center text-sm text-muted-foreground">
						No aliases yet.
					</p>
				{/each}
				<div class="flex flex-wrap items-center justify-between gap-2">
					<p class="text-xs text-muted-foreground">
						Patterns use <code>*</code>. A real model with the same name wins, and mapping rules run
						first and can point at an alias.
					</p>
					<Button size="sm" variant="outline" onclick={add}><PlusIcon /> Add alias</Button>
				</div>
			</div>
		{/if}
	</Card.Content>
</Card.Root>

{#if aliases.enabled}
	<Card.Root>
		<Card.Header>
			<Card.Title>Finding the newest model</Card.Title>
		</Card.Header>
		<Card.Content>
			<Field.Group>
				<Field.Set>
					<Field.Legend variant="label">Choose from</Field.Legend>
					<RadioGroup.Root bind:value={aliases.source} class="gap-3">
						{#each sources as s (s.value)}
							<div class="flex items-start gap-3">
								<RadioGroup.Item value={s.value} id="as-{s.value}" class="mt-0.5" />
								<Label for="as-{s.value}" class="flex flex-col items-start gap-0.5 font-normal">
									<span class="font-medium">{s.label}</span>
									<span class="text-muted-foreground">{s.hint}</span>
								</Label>
							</div>
						{/each}
					</RadioGroup.Root>
				</Field.Set>
				{#if aliases.source === 'live'}
					<Field.Field>
						<Field.Label for="as-refresh">Check accounts every (minutes)</Field.Label>
						<div class="flex flex-wrap items-center gap-2">
							<Input
								id="as-refresh"
								type="number"
								min={5}
								class="w-32"
								bind:value={aliases.refreshMinutes}
							/>
							<Button variant="outline" disabled={refreshing} onclick={refresh}>
								<RefreshCwIcon class={refreshing ? 'animate-spin' : ''} /> Check now
							</Button>
						</div>
						<Field.Description>
							Last checked {relativeTime(checkedAt)}.
						</Field.Description>
					</Field.Field>
					<Field.Field orientation="horizontal">
						<Field.Content>
							<Field.Label for="as-preview">Include hidden and preview models</Field.Label>
							<Field.Description>Ones upstream lists but doesn't offer yet.</Field.Description>
						</Field.Content>
						<Switch id="as-preview" bind:checked={aliases.preview} />
					</Field.Field>
				{/if}
				<Field.Field orientation="horizontal">
					<Field.Content>
						<Field.Label for="as-fallback"
							>Use an older model when the newest is unavailable</Field.Label
						>
						<Field.Description>
							For when every account that could serve the newest is cooling down or out of quota for
							it. Otherwise the client gets the rate limit error.
						</Field.Description>
					</Field.Content>
					<Switch id="as-fallback" bind:checked={aliases.fallback} />
				</Field.Field>
				<Field.Field orientation="horizontal">
					<Field.Content>
						<Field.Label for="as-list">Show aliases in <code>/v1/models</code></Field.Label>
						<Field.Description>Hidden aliases still work.</Field.Description>
					</Field.Content>
					<Switch id="as-list" bind:checked={aliases.list} />
				</Field.Field>
			</Field.Group>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Try it</Card.Title>
			<Card.Description>Uses the settings above, including unsaved changes.</Card.Description>
		</Card.Header>
		<Card.Content>
			<RouteTester {routing} listId="catalog-models" initial="sol" />
		</Card.Content>
	</Card.Root>
{/if}
