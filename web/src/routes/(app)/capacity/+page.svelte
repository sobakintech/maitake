<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import type { CapacityAccount, CapacityProvider, Provider } from '#lib/api.js';
	import { countdown, shortDateTime } from '#lib/format.js';
	import CapacityChart from '#lib/components/capacity-chart.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Toggle } from '#lib/components/ui/toggle/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import ChartLineIcon from '@lucide/svelte/icons/chart-line';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import UsersIcon from '@lucide/svelte/icons/users';
	import { onMount } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	onMount(() => {
		const t = setInterval(() => invalidateAll(), 60_000);
		return () => clearInterval(t);
	});

	let showAccounts = $state(false);

	const cap = $derived(data.capacity);
	const poolColor: Record<Provider, string> = {
		claude: 'var(--series-2)',
		codex: 'var(--series-1)'
	};
	const usageOptions = [
		{ value: 'recent', label: 'Same as recently' },
		{ value: 'average', label: 'Window average' }
	];

	function setParam(name: string, value: string | null) {
		const url = new URL(page.url.href);
		if (value) url.searchParams.set(name, value);
		else url.searchParams.delete(name);
		if (name === 'window') url.searchParams.delete('offset');
		goto(url, { reset: false, replace: true });
	}

	const rangeLabel = $derived.by(() => {
		const f = new Date(cap.from);
		const t = new Date(new Date(cap.to).getTime() - 1);
		const opts: Intl.DateTimeFormatOptions = { month: 'short', day: 'numeric' };
		return `${f.toLocaleDateString(undefined, opts)} to ${t.toLocaleDateString(undefined, opts)}`;
	});

	function name(a: CapacityAccount) {
		return a.alias || a.label;
	}

	// Colors follow the account, in id order, so filtering never repaints a line.
	function accountColors(p: CapacityProvider) {
		const ids = p.accounts.map((a) => a.id).sort((a, b) => a - b);
		return Object.fromEntries(
			ids.map((id, i) => [id, i < 6 ? `var(--series-${i + 1})` : 'var(--viz-muted)'])
		) as Record<number, string>;
	}

	function weightsNote(p: CapacityProvider) {
		const plans = p.accounts
			.filter((a) => a.remaining !== null && a.weight !== 1)
			.map((a) => `${a.plan} = ${a.weight}×`);
		return [...new Set(plans)].join(', ');
	}

	function pct(v: number) {
		return `${v < 10 && v > 0 ? v.toFixed(1) : Math.round(v)}%`;
	}

	function day(iso: string) {
		return new Date(iso).toLocaleDateString(undefined, { month: 'short', day: 'numeric' });
	}

	function burnLabel(p: CapacityProvider) {
		return {
			recent: `last ${data.burn}`,
			average: 'window average',
			mixed: `last ${data.burn} or window average`
		}[p.stats.burnSource];
	}
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Capacity</h1>
		<p class="text-sm text-muted-foreground">
			How much of each pool is left, and where it's heading.
		</p>
	</div>

	<div class="flex flex-wrap items-center gap-3">
		<ToggleGroup.Root
			type="single"
			variant="outline"
			value={data.window}
			onValueChange={(v) => v && setParam('window', v === 'weekly' ? null : v)}
		>
			<ToggleGroup.Item value="weekly" class="px-3">Weekly</ToggleGroup.Item>
			<ToggleGroup.Item value="session" class="px-3">Session</ToggleGroup.Item>
		</ToggleGroup.Root>
		<div class="flex items-center gap-2">
			<span class="text-sm text-muted-foreground">Future usage</span>
			<Select.Root
				type="single"
				value={data.usage}
				onValueChange={(v) => setParam('usage', v === 'recent' ? null : v)}
			>
				<Select.Trigger class="w-44"
					>{usageOptions.find((o) => o.value === data.usage)?.label}</Select.Trigger
				>
				<Select.Content>
					{#each usageOptions as o (o.value)}
						<Select.Item value={o.value} label={o.label}>{o.label}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
		{#if data.usage === 'recent'}
			<div class="flex items-center gap-2">
				<span class="text-sm text-muted-foreground">Burn rate over</span>
				<ToggleGroup.Root
					type="single"
					variant="outline"
					value={data.burn}
					onValueChange={(v) => v && setParam('burn', v === '3h' ? null : v)}
				>
					{#each ['1h', '3h', '6h'] as b (b)}
						<ToggleGroup.Item value={b} class="px-3">{b}</ToggleGroup.Item>
					{/each}
				</ToggleGroup.Root>
			</div>
		{/if}
		<Toggle variant="outline" bind:pressed={showAccounts} aria-label="Show each account">
			<UsersIcon /> Accounts
		</Toggle>
		<div class="ml-auto flex items-center gap-1">
			<span class="mr-2 text-sm text-muted-foreground tabular-nums">{rangeLabel}</span>
			<Button
				variant="ghost"
				size="icon"
				aria-label="Earlier"
				onclick={() => setParam('offset', String(data.offset - 1))}><ChevronLeftIcon /></Button
			>
			<Button
				variant="outline"
				disabled={data.offset === 0}
				onclick={() => setParam('offset', null)}>Today</Button
			>
			<Button
				variant="ghost"
				size="icon"
				aria-label="Later"
				onclick={() => setParam('offset', String(data.offset + 1))}><ChevronRightIcon /></Button
			>
		</div>
	</div>

	{#if cap.providers.length === 0}
		<Empty.Root class="border">
			<Empty.Header>
				<Empty.Media variant="icon"><ChartLineIcon /></Empty.Media>
				<Empty.Title>No accounts yet</Empty.Title>
				<Empty.Description>Add an account and its capacity shows up here.</Empty.Description>
			</Empty.Header>
		</Empty.Root>
	{/if}

	{#each cap.providers as p (p.id)}
		{@const colors = accountColors(p)}
		{@const counted = p.accounts.filter((a) => a.remaining !== null)}
		{@const note = weightsNote(p)}
		{@const nameOf = (id: number) => {
			const a = p.accounts.find((a) => a.id === id);
			return a ? name(a) : `#${id}`;
		}}
		<Card.Root>
			<Card.Header>
				<Card.Title class="flex items-center gap-2">
					<ProviderLogo provider={p.id} />
					{p.name}
					<span class="text-sm font-normal text-muted-foreground"
						>{p.accounts.length} account{p.accounts.length === 1 ? '' : 's'}</span
					>
				</Card.Title>
			</Card.Header>
			<Card.Content class="space-y-4">
				{#if p.remaining === null}
					<p class="text-sm text-muted-foreground">
						No account reports a {data.window === 'session' ? 'session' : 'weekly'} window yet.
					</p>
				{:else}
					<div class="flex flex-wrap items-end justify-between gap-x-8 gap-y-4">
						<div>
							<p class="text-4xl font-semibold tracking-tight tabular-nums">{pct(p.remaining)}</p>
							<p class="mt-1 text-sm text-muted-foreground">
								of the pool left{counted.length !== p.accounts.length
									? ` · ${p.accounts.length - counted.length} not counted (needs sign-in or no such window)`
									: ''}{note ? ` · ${note}` : ''}
							</p>
						</div>
						<dl class="grid grid-cols-2 gap-x-8 gap-y-3 text-sm sm:grid-cols-4">
							<div>
								<dt class="text-muted-foreground">Pool empty</dt>
								<dd class="mt-0.5 font-medium">
									{#if p.remaining <= 0}
										Empty now
									{:else if p.stats.poolEmptyAt}
										{shortDateTime(p.stats.poolEmptyAt)}
									{:else}
										Not by {day(p.stats.horizon)}
									{/if}
								</dd>
							</div>
							<div>
								<dt class="text-muted-foreground">First empty</dt>
								<dd class="mt-0.5 font-medium">
									{#if p.stats.firstEmpty}
										<PrivateText>{nameOf(p.stats.firstEmpty.accountId)}</PrivateText>
										<span class="text-muted-foreground">in {countdown(p.stats.firstEmpty.at)}</span>
									{:else if p.stats.emptyNow > 0}
										{p.stats.emptyNow} empty now
									{:else}
										None by {day(p.stats.horizon)}
									{/if}
								</dd>
							</div>
							<div>
								<dt class="text-muted-foreground">Next reset</dt>
								<dd class="mt-0.5 font-medium">
									{#if p.stats.nextReset}
										+{pct(p.stats.nextReset.gain)}
										<span class="text-muted-foreground">{shortDateTime(p.stats.nextReset.t)}</span>
									{:else}
										—
									{/if}
								</dd>
							</div>
							<div>
								<dt class="text-muted-foreground">
									Burn, {burnLabel(p)}
								</dt>
								<dd class="mt-0.5 font-medium tabular-nums">
									{p.stats.burnRate.toFixed(p.stats.burnRate < 10 ? 1 : 0)}%/h
								</dd>
							</div>
						</dl>
					</div>

					<CapacityChart
						data={p}
						from={cap.from}
						to={cap.to}
						now={cap.now}
						window={data.window}
						color={poolColor[p.id]}
						accounts={showAccounts
							? counted.map((a) => ({ id: a.id, name: name(a), color: colors[a.id] }))
							: null}
					/>

					{#if !showAccounts}
						<div class="flex flex-wrap items-center gap-x-5 gap-y-1 text-xs text-muted-foreground">
							<span class="flex items-center gap-1.5">
								<svg width="18" height="6" aria-hidden="true"
									><line
										x1="1"
										x2="17"
										y1="3"
										y2="3"
										stroke={poolColor[p.id]}
										stroke-width="2"
										stroke-linecap="round"
									/></svg
								>
								Recorded
							</span>
							<span class="flex items-center gap-1.5">
								<svg width="18" height="6" aria-hidden="true"
									><line
										x1="1"
										x2="17"
										y1="3"
										y2="3"
										stroke={poolColor[p.id]}
										stroke-width="2"
										stroke-dasharray="5 4"
									/></svg
								>
								Projected
							</span>
							{#if p.recorded.length < 3}
								<span>History fills in as maitake records quota readings.</span>
							{/if}
						</div>
					{:else}
						<Table.Root>
							<Table.Header>
								<Table.Row>
									<Table.Head>Account</Table.Head>
									<Table.Head>Plan</Table.Head>
									<Table.Head class="text-right">Left</Table.Head>
									<Table.Head class="text-right">Burn</Table.Head>
									<Table.Head>Resets</Table.Head>
									<Table.Head>Runs out</Table.Head>
								</Table.Row>
							</Table.Header>
							<Table.Body>
								{#each p.accounts as a (a.id)}
									<Table.Row>
										<Table.Cell>
											<span class="flex items-center gap-2">
												<span
													class="size-2 shrink-0 rounded-full"
													style="background: {a.remaining === null ? 'transparent' : colors[a.id]}"
												></span>
												<PrivateText class="truncate">{name(a)}</PrivateText>
												{#if a.status !== 'ok'}<Badge variant="outline">{a.status}</Badge>{/if}
											</span>
										</Table.Cell>
										<Table.Cell>{a.plan || '—'}{a.weight !== 1 ? ` · ${a.weight}×` : ''}</Table.Cell
										>
										<Table.Cell class="text-right tabular-nums"
											>{a.remaining === null ? '—' : pct(a.remaining)}</Table.Cell
										>
										<Table.Cell class="text-right tabular-nums"
											>{a.remaining === null ? '—' : `${a.rate.toFixed(1)}%/h`}</Table.Cell
										>
										<Table.Cell>{a.resetsAt ? shortDateTime(a.resetsAt) : '—'}</Table.Cell>
										<Table.Cell
											>{a.remaining === null
												? '—'
												: a.remaining <= 0
													? 'Empty now'
													: a.emptyAt
														? shortDateTime(a.emptyAt)
														: `Not by ${day(p.stats.horizon)}`}</Table.Cell
										>
									</Table.Row>
								{/each}
							</Table.Body>
						</Table.Root>
					{/if}
				{/if}
			</Card.Content>
		</Card.Root>
	{/each}
</div>
