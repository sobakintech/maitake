<script lang="ts">
	import { admin, errorMessage, type UsageRange, type UsageResponse } from '#lib/api.js';
	import { compact, duration, integer } from '#lib/format.js';
	import { fillSeries, type Metric } from '#lib/series.js';
	import BreakdownTable from './breakdown-table.svelte';
	import ColumnChart from './column-chart.svelte';
	import StatTile from './stat-tile.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Skeleton } from '#lib/components/ui/skeleton/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import ArrowUpRightIcon from '@lucide/svelte/icons/arrow-up-right';

	let { keyId, userId }: { keyId?: number; userId?: number } = $props();

	let range = $state<UsageRange>('7d');
	let metric = $state<Metric>('requests');
	let usage = $state<UsageResponse | null>(null);
	let error = $state('');

	$effect(() => {
		const params = { range, keyId, userId };
		let stale = false;
		admin
			.usage(params)
			.then((u) => {
				if (stale) return;
				usage = u;
				error = '';
			})
			.catch((e) => {
				if (!stale) error = errorMessage(e);
			});
		return () => (stale = true);
	});

	const fullHref = $derived(
		`/usage?range=${range}` + (keyId ? `&keyId=${keyId}` : '') + (userId ? `&userId=${userId}` : '')
	);
	const t = $derived(usage?.totals);
	const series = $derived(usage ? fillSeries(usage, metric) : []);
</script>

<Card.Root>
	<Card.Header>
		<Card.Title>Usage</Card.Title>
		<Card.Action class="flex flex-wrap items-center gap-2">
			<ToggleGroup.Root
				type="single"
				variant="outline"
				size="sm"
				value={range}
				onValueChange={(v) => v && (range = v as UsageRange)}
			>
				{#each ['24h', '7d', '30d', '90d'] as r (r)}
					<ToggleGroup.Item value={r} class="px-3">{r}</ToggleGroup.Item>
				{/each}
			</ToggleGroup.Root>
			<Button variant="ghost" size="sm" href={fullHref}>Full usage <ArrowUpRightIcon /></Button>
		</Card.Action>
	</Card.Header>
	<Card.Content class="space-y-6">
		{#if error}
			<p class="text-sm text-destructive">{error}</p>
		{:else if !t || !usage}
			<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
				{#each [0, 1, 2, 3] as i (i)}<Skeleton class="h-24 rounded-xl" />{/each}
			</div>
			<Skeleton class="h-44" />
		{:else}
			<div class="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
				<StatTile
					label="Requests"
					value={integer(t.requests)}
					detail="{integer(t.errors)} failed"
				/>
				<StatTile
					label="Input tokens"
					value={compact(t.inputTokens)}
					detail="{compact(t.cacheReadTokens)} read from cache, {compact(
						t.cacheWriteTokens
					)} written"
				/>
				<StatTile
					label="Output tokens"
					value={compact(t.outputTokens)}
					detail="{compact(t.reasoningTokens)} of them reasoning"
				/>
				<StatTile
					label="Time to first token"
					value={t.avgTtftMs ? duration(t.avgTtftMs) : '—'}
					detail="Average"
				/>
			</div>
			{#if t.requests > 0}
				<div class="space-y-2">
					<div class="flex items-center justify-between gap-2">
						<p class="text-sm font-medium">
							{metric === 'requests' ? 'Requests' : 'Tokens'} per {usage.seriesBy}
						</p>
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
					</div>
					<ColumnChart
						data={series}
						label={metric === 'requests' ? 'Requests' : 'Tokens'}
						format={(v) => compact(v)}
						height={180}
					/>
				</div>
				<BreakdownTable rows={usage.byModel} label="Model" />
			{:else}
				<p class="py-2 text-center text-sm text-muted-foreground">No requests in this period.</p>
			{/if}
		{/if}
	</Card.Content>
</Card.Root>
