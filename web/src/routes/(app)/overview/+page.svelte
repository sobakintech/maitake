<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import type { Account, UsageBucket, UsageRange } from '#lib/api.js';
	import { compact, countdown, duration, integer } from '#lib/format.js';
	import { fillSeries, type Metric } from '#lib/series.js';
	import BreakdownTable from '#lib/components/breakdown-table.svelte';
	import LineChart from '#lib/components/line-chart.svelte';
	import StatTile from '#lib/components/stat-tile.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { onMount } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let metric = $state<Metric>('requests');
	let breakdown = $state('model');

	const t = $derived(data.usage.totals);
	const series = $derived(fillSeries(data.usage, metric));
	const errorRate = $derived(t.requests ? (t.errors / t.requests) * 100 : 0);
	const ranges: { value: UsageRange; label: string }[] = [
		{ value: '1h', label: '1h' },
		{ value: '24h', label: '24h' },
		{ value: '7d', label: '7d' },
		{ value: '30d', label: '30d' },
		{ value: '90d', label: '90d' }
	];

	function future(iso: string | null | undefined) {
		return !!iso && new Date(iso).getTime() > Date.now();
	}

	// When an account's limits all lift, or null if nothing is holding it back.
	function limitedUntil(a: Account): number | null {
		const ends: number[] = [];
		if (a.status === 'cooldown' && future(a.cooldownUntil))
			ends.push(new Date(a.cooldownUntil!).getTime());
		for (const w of a.quota?.windows ?? []) {
			if (!w.limited) continue;
			if (!w.resetsAt) ends.push(Infinity);
			else if (future(w.resetsAt)) ends.push(new Date(w.resetsAt).getTime());
		}
		return ends.length ? Math.max(...ends) : null;
	}

	const enabled = $derived(data.accounts.filter((a) => !a.disabled));
	const disabledCount = $derived(data.accounts.length - enabled.length);
	const healthy = $derived(
		enabled.filter(
			(a) => a.status === 'ok' || (a.status === 'cooldown' && !future(a.cooldownUntil))
		)
	);
	const limited = $derived(
		enabled.map((a) => limitedUntil(a)).filter((u): u is number => u !== null)
	);
	const nextFree = $derived(limited.length ? Math.min(...limited) : null);
	const reauth = $derived(enabled.filter((a) => a.status === 'reauth').length);
	const failing = $derived(enabled.filter((a) => a.status === 'error').length);
	const banked = $derived.by(() => {
		let total = 0;
		let holders = 0;
		let soonest: string | undefined;
		for (const a of enabled) {
			const b = a.quota?.banked;
			if (!b?.available) continue;
			total += b.available;
			holders++;
			for (const c of b.credits ?? []) {
				if (c.expiresAt && future(c.expiresAt) && (!soonest || c.expiresAt < soonest))
					soonest = c.expiresAt;
			}
		}
		return { total, holders, soonest };
	});

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

	onMount(() => {
		const timer = setInterval(() => invalidateAll(), 30_000);
		return () => clearInterval(timer);
	});
</script>

<div class="space-y-6">
	<h1 class="text-xl font-semibold tracking-tight">Overview</h1>

	<div class="space-y-3">
		<div class="flex items-center justify-between">
			<h2 class="text-sm font-medium">Accounts</h2>
			<Button variant="link" size="sm" href="/accounts">All accounts</Button>
		</div>
		<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
			<StatTile
				label="Available"
				value="{healthy.length} of {enabled.length}"
				detail={disabledCount > 0 ? `${disabledCount} disabled` : 'Ready to take requests'}
			/>
			<StatTile
				label="Limited now"
				value={integer(limited.length)}
				detail={nextFree === null
					? 'Nothing is rate-limited'
					: nextFree === Infinity
						? 'No reset time reported'
						: `Next one frees up in ${countdown(new Date(nextFree).toISOString())}`}
			/>
			<StatTile
				label="Banked resets"
				value={integer(banked.total)}
				detail={banked.total === 0
					? 'None saved up'
					: `Across ${banked.holders} account${banked.holders === 1 ? '' : 's'}` +
						(banked.soonest ? ` · next expires in ${countdown(banked.soonest)}` : '')}
			/>
			<StatTile
				label="Need attention"
				value={integer(reauth + failing)}
				detail={reauth + failing === 0
					? 'All signed in and working'
					: [reauth && `${reauth} need sign-in`, failing && `${failing} failing`]
							.filter(Boolean)
							.join(' · ')}
			/>
		</div>
	</div>

	<div class="space-y-3">
		<h2 class="text-sm font-medium">Traffic</h2>
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
			<Select.Root
				type="single"
				value={accountValue}
				onValueChange={(v) => setParam('accountId', v)}
			>
				<Select.Trigger class="w-52">{accountLabel}</Select.Trigger>
				<Select.Content>
					<Select.Item value="0" label="All accounts">All accounts</Select.Item>
					{#each data.accounts as a (a.id)}
						<Select.Item value={String(a.id)} label={a.label}>{a.label}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</div>
	</div>

	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<StatTile
			label="Requests"
			value={integer(t.requests)}
			detail="{integer(t.errors)} failed · {errorRate.toFixed(
				errorRate > 0 && errorRate < 1 ? 1 : 0
			)}% error rate"
		/>
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
			<LineChart
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
