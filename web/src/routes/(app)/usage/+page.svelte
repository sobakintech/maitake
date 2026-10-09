<script lang="ts">
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import type { UsageBucket, UsageRange } from '#lib/api.js';
	import { compact, duration, integer } from '#lib/format.js';
	import { fillSeries, type Metric } from '#lib/series.js';
	import BreakdownTable from '#lib/components/breakdown-table.svelte';
	import ColumnChart from '#lib/components/column-chart.svelte';
	import StatTile from '#lib/components/stat-tile.svelte';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
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
	const keyValue = $derived(String(data.keyId ?? 0));
	const accountValue = $derived(String(data.accountId ?? 0));
	const userLabel = $derived(data.users.find((u) => u.id === data.userId)?.name ?? 'All users');
	const keyLabel = $derived(data.keyId ? (keyName(data.keyId) ?? 'Deleted key') : 'All keys');
	// With a user picked, only their keys make sense to filter by.
	const keyOptions = $derived(
		data.keys.filter((k) => !data.userId || k.userId === data.userId || k.id === data.keyId)
	);
	const accountLabel = $derived(
		data.accounts.find((a) => a.id === data.accountId)?.label ?? 'All accounts'
	);
	const names = $derived(data.usage.names);

	function keyName(id: number) {
		const k = data.keys.find((k) => k.id === id);
		return k ? `${k.name || k.prefix + '…'} (${k.ownerName})` : undefined;
	}

	// Admin keys have no user, so they show up on their own as "k<id>".
	function userHref(r: UsageBucket) {
		if (r.key.startsWith('k'))
			return names.key[r.key.slice(1)] ? `/keys/${r.key.slice(1)}` : undefined;
		return names.user[r.key] ? `/users/${r.key}` : undefined;
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
		<Select.Root type="single" value={keyValue} onValueChange={(v) => setParam('keyId', v)}>
			<Select.Trigger class="w-56"><span class="truncate">{keyLabel}</span></Select.Trigger>
			<Select.Content>
				<Select.Item value="0" label="All keys">All keys</Select.Item>
				{#each keyOptions as k (k.id)}
					{@const label = `${k.name || k.prefix + '…'} (${k.ownerName})`}
					<Select.Item value={String(k.id)} {label}>{label}</Select.Item>
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
					<BreakdownTable rows={data.usage.byUser} label="User or admin key" href={userHref} />
				</Tabs.Content>
				<Tabs.Content value="key" class="pt-4"
					><BreakdownTable
						rows={data.usage.byKey}
						label="Key"
						href={(r) => (names.key[r.key] ? `/keys/${r.key}` : undefined)}
					/></Tabs.Content
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
</div>
