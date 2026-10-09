<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { UsageEvent, UsageRange } from '#lib/api.js';
	import { compact, duration, integer, shortDateTime } from '#lib/format.js';
	import { fillSeries, type Metric } from '#lib/series.js';
	import BreakdownTable from '#lib/components/breakdown-table.svelte';
	import ColumnChart from '#lib/components/column-chart.svelte';
	import StatTile from '#lib/components/stat-tile.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let metric = $state<Metric>('requests');
	let breakdown = $state('model');

	const t = $derived(data.usage.totals);
	const series = $derived(fillSeries(data.usage, metric));
	const ranges: { value: UsageRange; label: string }[] = [
		{ value: '1h', label: '1h' },
		{ value: '24h', label: '24h' },
		{ value: '7d', label: '7d' },
		{ value: '30d', label: '30d' },
		{ value: '90d', label: '90d' }
	];

	function setParam(name: string, value: string | undefined) {
		const url = new URL(page.url.href);
		if (value && value !== '0') url.searchParams.set(name, value);
		else url.searchParams.delete(name);
		goto(url, { reset: false, replace: true });
	}

	const userValue = $derived(String(data.userId ?? 0));
	const accountValue = $derived(String(data.accountId ?? 0));
	const userLabel = $derived(data.users.find((u) => u.id === data.userId)?.name ?? 'All users');
	const accountLabel = $derived(
		data.accounts.find((a) => a.id === data.accountId)?.label ?? 'All accounts'
	);
	const names = $derived(data.usage.names);

	// Only non-default tiers are worth a badge.
	function fastTier(e: UsageEvent) {
		const tier = e.servedTier || e.serviceTier;
		return tier && !['default', 'standard', 'auto', 'flex'].includes(tier) ? tier : '';
	}
</script>

<div class="mx-auto max-w-6xl space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Usage</h1>
		<p class="text-sm text-muted-foreground">Requests and tokens going through the proxy.</p>
	</div>

	<div class="flex flex-wrap items-center gap-3">
		<ToggleGroup.Root
			type="single"
			variant="outline"
			value={data.range}
			onValueChange={(v) => v && setParam('range', v)}
		>
			{#each ranges as r (r.value)}
				<ToggleGroup.Item value={r.value} class="px-3">{r.label}</ToggleGroup.Item>
			{/each}
		</ToggleGroup.Root>
		<Select.Root type="single" value={userValue} onValueChange={(v) => setParam('userId', v)}>
			<Select.Trigger class="w-44">{userLabel}</Select.Trigger>
			<Select.Content>
				<Select.Item value="0" label="All users">All users</Select.Item>
				{#each data.users as u (u.id)}
					<Select.Item value={String(u.id)} label={u.name}>{u.name}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<Select.Root type="single" value={accountValue} onValueChange={(v) => setParam('accountId', v)}>
			<Select.Trigger class="w-52">{accountLabel}</Select.Trigger>
			<Select.Content>
				<Select.Item value="0" label="All accounts">All accounts</Select.Item>
				{#each data.accounts as a (a.id)}
					<Select.Item value={String(a.id)} label={a.label}>{a.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
	</div>

	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<StatTile label="Requests" value={integer(t.requests)} detail="{integer(t.errors)} failed" />
		<StatTile
			label="Input tokens"
			value={compact(t.inputTokens)}
			detail="{compact(t.cacheReadTokens)} read from cache, {compact(t.cacheWriteTokens)} written"
		/>
		<StatTile
			label="Output tokens"
			value={compact(t.outputTokens)}
			detail="{compact(t.reasoningTokens)} of them reasoning"
		/>
		<StatTile
			label="Time to first token"
			value={t.avgTtftMs ? duration(t.avgTtftMs) : '—'}
			detail="Average, until the first output arrives"
		/>
	</div>

	<Card.Root>
		<Card.Header>
			<Card.Title
				>{metric === 'requests' ? 'Requests' : 'Tokens'} per {data.usage.seriesBy}</Card.Title
			>
			<Card.Action>
				<ToggleGroup.Root
					type="single"
					variant="outline"
					size="sm"
					value={metric}
					onValueChange={(v) => v && (metric = v as Metric)}
				>
					<ToggleGroup.Item value="requests" class="px-3">Requests</ToggleGroup.Item>
					<ToggleGroup.Item value="tokens" class="px-3">Tokens</ToggleGroup.Item>
				</ToggleGroup.Root>
			</Card.Action>
		</Card.Header>
		<Card.Content>
			<ColumnChart
				data={series}
				label={metric === 'requests' ? 'Requests' : 'Tokens'}
				format={(v) => compact(v)}
			/>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Breakdown</Card.Title>
		</Card.Header>
		<Card.Content>
			<Tabs.Root bind:value={breakdown}>
				<Tabs.List>
					<Tabs.Trigger value="model">Models</Tabs.Trigger>
					<Tabs.Trigger value="user">Users</Tabs.Trigger>
					<Tabs.Trigger value="key">Keys</Tabs.Trigger>
					<Tabs.Trigger value="account">Accounts</Tabs.Trigger>
					<Tabs.Trigger value="provider">Providers</Tabs.Trigger>
				</Tabs.List>
				<Tabs.Content value="model" class="pt-4"
					><BreakdownTable rows={data.usage.byModel} label="Model" /></Tabs.Content
				>
				<Tabs.Content value="user" class="pt-4">
					<BreakdownTable
						rows={data.usage.byUser}
						label="User"
						href={(r) => (names.user[r.key] ? `/users/${r.key}` : undefined)}
					/>
				</Tabs.Content>
				<Tabs.Content value="key" class="pt-4"
					><BreakdownTable rows={data.usage.byKey} label="Key" /></Tabs.Content
				>
				<Tabs.Content value="account" class="pt-4">
					<BreakdownTable rows={data.usage.byAccount} label="Account" />
				</Tabs.Content>
				<Tabs.Content value="provider" class="pt-4">
					<BreakdownTable rows={data.usage.byProvider} label="Provider" />
				</Tabs.Content>
			</Tabs.Root>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Recent requests</Card.Title>
		</Card.Header>
		<Card.Content>
			{#if data.usage.recent.length === 0}
				<p class="py-6 text-center text-sm text-muted-foreground">No requests in this period.</p>
			{:else}
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Time</Table.Head>
							<Table.Head>User</Table.Head>
							<Table.Head>Model</Table.Head>
							<Table.Head>Account</Table.Head>
							<Table.Head>Status</Table.Head>
							<Table.Head class="text-right">In</Table.Head>
							<Table.Head class="text-right">Out</Table.Head>
							<Table.Head class="text-right">First token</Table.Head>
							<Table.Head class="text-right">Duration</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each data.usage.recent as e, i (i)}
							<Table.Row>
								<Table.Cell class="whitespace-nowrap text-muted-foreground"
									>{shortDateTime(e.time)}</Table.Cell
								>
								<Table.Cell>{names.user[e.userId] ?? '—'}</Table.Cell>
								<Table.Cell class="font-mono text-xs">
									{#if e.requestedModel && e.requestedModel !== e.model}
										<span class="text-muted-foreground" title="Requested {e.requestedModel}"
											>{e.requestedModel} →</span
										>
									{/if}
									{e.model}
									{#if fastTier(e)}
										<Badge
											variant="secondary"
											class="ml-1"
											title="Asked for {e.serviceTier || 'default'}, served as {e.servedTier ||
												'not reported'}">{fastTier(e)}</Badge
										>
									{/if}
								</Table.Cell>
								<Table.Cell class="max-w-40 truncate"
									><PrivateText>{names.account[e.accountId] ?? '—'}</PrivateText></Table.Cell
								>
								<Table.Cell>
									{#if e.status >= 400}
										<Badge variant="destructive" title={e.error}>{e.status}</Badge>
									{:else}
										<Badge variant="outline">{e.status}</Badge>
									{/if}
								</Table.Cell>
								<Table.Cell class="text-right tabular-nums"
									>{compact(e.inputTokens + e.cacheReadTokens)}</Table.Cell
								>
								<Table.Cell class="text-right tabular-nums">{compact(e.outputTokens)}</Table.Cell>
								<Table.Cell class="text-right text-muted-foreground tabular-nums"
									>{e.ttftMs ? duration(e.ttftMs) : '—'}</Table.Cell
								>
								<Table.Cell class="text-right text-muted-foreground tabular-nums"
									>{duration(e.durationMs)}</Table.Cell
								>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			{/if}
		</Card.Content>
	</Card.Root>
</div>
