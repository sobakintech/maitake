<script lang="ts">
	import type { BankedCreditView, BankedView, QuotaView } from '#lib/types.js';
	import { countdown, relativeTime, shortDateTime } from '#lib/format.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import TimerResetIcon from '@lucide/svelte/icons/timer-reset';

	// The banked resets a quota page view allows, with every reset listed when it includes them.
	let { banked, resetFormat }: { banked: BankedView; resetFormat: QuotaView['resetFormat'] } =
		$props();

	const credits = $derived(banked.credits ?? []);
	const unlisted = $derived(
		credits.length ? Math.max(0, banked.available - credits.reduce((n, c) => n + c.count, 0)) : 0
	);
	// One reset is shown inline rather than as a one-item list.
	const single = $derived(credits.length === 1 && unlisted === 0 ? credits[0] : null);

	function expiryText(iso: string) {
		if (resetFormat === 'exact') return shortDateTime(iso);
		if (resetFormat === 'both') return `in ${countdown(iso)} · ${shortDateTime(iso)}`;
		return `in ${countdown(iso)}`;
	}

	const soon = (iso?: string) => !!iso && new Date(iso).getTime() - Date.now() < 3 * 86_400_000;
</script>

{#snippet details(c: BankedCreditView)}
	{@const rest = [
		c.requiresLimit && 'only while a limit is hit',
		c.grantedAt && `granted ${relativeTime(c.grantedAt)}`
	].filter(Boolean)}
	<p class="text-xs text-muted-foreground">
		<span class={soon(c.expiresAt) ? 'text-destructive' : ''}
			>{c.expiresAt ? `Expires ${expiryText(c.expiresAt)}` : 'No expiry'}</span
		>{rest.length ? ` · ${rest.join(' · ')}` : ''}
	</p>
{/snippet}

{#snippet name(c: BankedCreditView)}
	<span>{c.title || 'Rate-limit reset'}</span>
	{#if c.count > 1}<Badge variant="outline">×{c.count}</Badge>{/if}
	{#if c.account}<Badge variant="secondary">{c.account}</Badge>{/if}
{/snippet}

<div class="space-y-2 rounded-lg bg-muted/50 px-3 py-2 text-sm">
	<div class="flex items-start gap-2">
		<TimerResetIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
		<div class="min-w-0 space-y-0.5">
			<p>
				<span class="font-medium">{banked.available}</span>
				banked reset{banked.available === 1 ? '' : 's'}
			</p>
			{#if single}
				<div class="flex flex-wrap items-center gap-x-2 gap-y-1 leading-tight">
					{@render name(single)}
				</div>
				{@render details(single)}
			{:else if banked.expiries?.length}
				<p class="text-xs text-muted-foreground">Next expires {expiryText(banked.expiries[0])}</p>
			{/if}
		</div>
	</div>
	{#if credits.length && !single}
		<ul class="divide-y rounded-md border bg-background">
			{#each credits as c, i (i)}
				<li class="space-y-0.5 px-3 py-1.5 leading-tight">
					<div class="flex flex-wrap items-center gap-x-2 gap-y-1">{@render name(c)}</div>
					{@render details(c)}
				</li>
			{/each}
			{#if unlisted > 0}
				<li class="px-3 py-1.5 text-xs text-muted-foreground">{unlisted} more not listed</li>
			{/if}
		</ul>
	{/if}
</div>
