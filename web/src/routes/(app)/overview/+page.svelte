<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { compact, integer } from '#lib/format.js';
	import { fillSeries } from '#lib/series.js';
	import AccountQuota from '#lib/components/account-quota.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import BreakdownTable from '#lib/components/breakdown-table.svelte';
	import LineChart from '#lib/components/line-chart.svelte';
	import StatTile from '#lib/components/stat-tile.svelte';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { onMount } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const t = $derived(data.usage.totals);
	const series = $derived(fillSeries(data.usage, 'requests'));
	const enabled = $derived(data.accounts.filter((a) => !a.disabled));
	const healthy = $derived(
		enabled.filter(
			(a) =>
				a.status === 'ok' ||
				(a.status === 'cooldown' && a.cooldownUntil && new Date(a.cooldownUntil) <= new Date())
		)
	);
	const errorRate = $derived(t.requests ? (t.errors / t.requests) * 100 : 0);

	onMount(() => {
		const timer = setInterval(() => invalidateAll(), 30_000);
		return () => clearInterval(timer);
	});
</script>

<div class="space-y-6">
	<div>
		<h1 class="text-xl font-semibold tracking-tight">Overview</h1>
		<p class="text-sm text-muted-foreground">The last 24 hours.</p>
	</div>

	<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
		<StatTile label="Requests" value={integer(t.requests)} />
		<StatTile
			label="Tokens"
			value={compact(t.inputTokens + t.outputTokens + t.cacheReadTokens + t.cacheWriteTokens)}
			detail="{compact(t.inputTokens)} in · {compact(t.outputTokens)} out · {compact(
				t.cacheReadTokens
			)} cached"
		/>
		<StatTile
			label="Error rate"
			value="{errorRate.toFixed(errorRate > 0 && errorRate < 1 ? 1 : 0)}%"
			detail="{integer(t.errors)} failed request{t.errors === 1 ? '' : 's'}"
		/>
		<StatTile
			label="Accounts available"
			value="{healthy.length} of {enabled.length}"
			detail={data.accounts.length - enabled.length > 0
				? `${data.accounts.length - enabled.length} disabled`
				: undefined}
		/>
	</div>

	<Card.Root>
		<Card.Header>
			<Card.Title>Requests per hour</Card.Title>
		</Card.Header>
		<Card.Content>
			<LineChart data={series} label="Requests" format={(v) => compact(v)} />
		</Card.Content>
	</Card.Root>

	{#if data.accounts.length}
		<div class="space-y-3">
			<div class="flex items-center justify-between">
				<h2 class="text-sm font-medium">Account limits</h2>
				<Button variant="link" size="sm" href="/accounts">All accounts</Button>
			</div>
			<div class="grid gap-4 md:grid-cols-2 lg:grid-cols-3">
				{#each enabled as a (a.id)}
					<Card.Root size="sm">
						<Card.Header>
							<Card.Title class="flex items-center gap-2 text-sm">
								<ProviderLogo provider={a.provider} />
								<PrivateText class="truncate">{a.label}</PrivateText>
							</Card.Title>
						</Card.Header>
						<Card.Content>
							<AccountQuota quota={a.quota} />
						</Card.Content>
					</Card.Root>
				{/each}
			</div>
		</div>
	{/if}

	<div class="grid gap-6 lg:grid-cols-2">
		<Card.Root>
			<Card.Header><Card.Title>Top users</Card.Title></Card.Header>
			<Card.Content>
				<BreakdownTable
					rows={data.usage.byUser.slice(0, 8)}
					label="User or admin key"
					href={(r) =>
						r.key.startsWith('k')
							? `/keys/${r.key.slice(1)}`
							: Number(r.key) > 0
								? `/users/${r.key}`
								: undefined}
				/>
			</Card.Content>
		</Card.Root>
		<Card.Root>
			<Card.Header><Card.Title>Top models</Card.Title></Card.Header>
			<Card.Content>
				<BreakdownTable rows={data.usage.byModel.slice(0, 8)} label="Model" />
			</Card.Content>
		</Card.Root>
	</div>
</div>
