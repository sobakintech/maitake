<script lang="ts">
	import type { UsageBucket } from '#lib/types.js';
	import { compact, integer } from '#lib/format.js';
	import { totalTokens } from '#lib/series.js';
	import * as Table from '#lib/components/ui/table/index.js';

	let {
		rows,
		label,
		href
	}: { rows: UsageBucket[]; label: string; href?: (row: UsageBucket) => string | undefined } =
		$props();

	const sorted = $derived([...rows].sort((a, b) => b.requests - a.requests));
	const max = $derived(Math.max(1, ...rows.map((r) => r.requests)));
</script>

{#if sorted.length === 0}
	<p class="py-6 text-center text-sm text-muted-foreground">No requests in this period.</p>
{:else}
	<Table.Root>
		<Table.Header>
			<Table.Row>
				<Table.Head>{label}</Table.Head>
				<Table.Head class="text-right">Requests</Table.Head>
				<Table.Head class="text-right">Errors</Table.Head>
				<Table.Head class="text-right">Input</Table.Head>
				<Table.Head class="text-right">Output</Table.Head>
				<Table.Head class="text-right">Cached</Table.Head>
				<Table.Head class="text-right">Total tokens</Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each sorted as r (r.key)}
				{@const link = href?.(r)}
				<Table.Row>
					<Table.Cell class="max-w-64">
						<div class="space-y-1">
							{#if link}
								<a href={link} class="truncate font-medium hover:underline">{r.name ?? r.key}</a>
							{:else}
								<span class="truncate font-medium">{r.name ?? r.key}</span>
							{/if}
							<div class="h-1 rounded-full" style="background: var(--viz-track)">
								<div
									class="h-full rounded-full"
									style="width: {(r.requests / max) * 100}%; background: var(--viz-accent)"
								></div>
							</div>
						</div>
					</Table.Cell>
					<Table.Cell class="text-right tabular-nums">{integer(r.requests)}</Table.Cell>
					<Table.Cell class="text-right text-muted-foreground tabular-nums"
						>{integer(r.errors)}</Table.Cell
					>
					<Table.Cell class="text-right tabular-nums">{compact(r.inputTokens)}</Table.Cell>
					<Table.Cell class="text-right tabular-nums">{compact(r.outputTokens)}</Table.Cell>
					<Table.Cell class="text-right text-muted-foreground tabular-nums"
						>{compact(r.cacheReadTokens)}</Table.Cell
					>
					<Table.Cell class="text-right tabular-nums">{compact(totalTokens(r))}</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
{/if}
