<script lang="ts">
	import type { EntryView, QuotaView, WindowView, BankedView } from '#lib/types.js';
	import { countdown, money, relativeTime, shortDateTime } from '#lib/format.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import QuotaMeter from './quota-meter.svelte';
	import ProviderLogo from './provider-logo.svelte';
	import TimerResetIcon from '@lucide/svelte/icons/timer-reset';
	import CalendarClockIcon from '@lucide/svelte/icons/calendar-clock';
	import RadarIcon from '@lucide/svelte/icons/radar';
	import HistoryIcon from '@lucide/svelte/icons/history';

	let { view }: { view: QuotaView } = $props();

	// Numbers can be flipped between left and used; bar and level formats show no numbers.
	const flippable = $derived(view.numberFormat === 'remaining' || view.numberFormat === 'used');
	const modeFallback = $derived(view.numberFormat === 'used' ? 'used' : 'left');

	function meter(w: WindowView, entry: EntryView) {
		let remaining: number | null = null;
		let values: { left: string; used: string } | undefined;
		if (w.value === undefined) return { remaining, values };
		const tenth = (v: number) => Math.round(v * 10) / 10;
		if (w.unit === 'accounts') {
			const n = entry.accountCount || 1;
			remaining = (w.value / n) * 100;
			values = {
				left: `${w.value} of ${n} accounts left`,
				used: `${tenth(n - w.value)} of ${n} accounts used`
			};
			return { remaining, values };
		}
		remaining = w.unit === 'percent_used' ? 100 - w.value : w.value;
		if (w.amount) {
			const a = w.amount;
			const limit = money(a.limit, a.currency);
			values = {
				left: `${money(Math.max(0, a.limit - a.used), a.currency)} of ${limit} left`,
				used: `${money(a.used, a.currency)} of ${limit} used`
			};
		} else if (flippable) {
			values = { left: `${tenth(remaining)}% left`, used: `${tenth(100 - remaining)}% used` };
		}
		return { remaining, values };
	}

	const statusText: Record<string, string> = {
		ok: 'Available',
		cooldown: 'Cooling down',
		error: 'Error',
		reauth: 'Needs login',
		exhausted: 'Limit reached'
	};

	function expiryText(iso: string) {
		if (view.resetFormat === 'exact') return shortDateTime(iso);
		if (view.resetFormat === 'both') return `${shortDateTime(iso)} · in ${countdown(iso)}`;
		return `in ${countdown(iso)}`;
	}
</script>

{#snippet banked(b: BankedView)}
	<div class="flex items-start gap-2 rounded-lg bg-muted/50 px-3 py-2 text-sm">
		<TimerResetIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
		<div>
			<p>
				<span class="font-medium">{b.available}</span>
				banked reset{b.available === 1 ? '' : 's'} available
			</p>
			{#if b.expiries?.length}
				<p class="text-xs text-muted-foreground">
					Next expires {expiryText(b.expiries[0])}{#if b.expiries.length > 1}
						· {b.expiries.length} total{/if}
				</p>
			{/if}
		</div>
	</div>
{/snippet}

{#snippet entry(e: EntryView)}
	<div class="space-y-3">
		{#if e.name || e.plan || e.status}
			<div class="flex flex-wrap items-center gap-2">
				{#if e.name}<span class="text-sm font-medium">{e.name}</span>{/if}
				{#if e.plan}<Badge variant="outline">{e.plan}</Badge>{/if}
				{#if e.status && e.status !== 'ok'}<Badge variant="secondary"
						>{statusText[e.status] ?? e.status}</Badge
					>{/if}
			</div>
		{/if}
		{#each e.windows as w (w.id)}
			{@const m = meter(w, e)}
			<QuotaMeter
				label={w.label}
				remaining={m.remaining}
				level={w.level}
				values={m.values}
				{modeFallback}
				resetsAt={w.resetsAt}
				resetFormat={view.resetFormat}
			/>
		{:else}
			<p class="text-sm text-muted-foreground">No limit data yet.</p>
		{/each}
		{#if e.extra}
			<p class="text-xs text-muted-foreground">
				Extra usage {e.extra.unlimited
					? 'unlimited'
					: e.extra.enabled
						? 'enabled'
						: 'off'}{#if e.extra.usedPercent !== undefined}
					· {e.extra.usedPercent}% used{/if}
			</p>
		{/if}
		{#if e.banked}{@render banked(e.banked)}{/if}
		{#if view.showUpdatedAt && e.updatedAt}
			<p class="text-xs text-muted-foreground">Updated {relativeTime(e.updatedAt)}</p>
		{/if}
	</div>
{/snippet}

<div class="space-y-6">
	{#if view.notice}
		<div class="rounded-lg border bg-muted/40 px-4 py-3 text-sm whitespace-pre-line">
			{view.notice}
		</div>
	{/if}

	{#each view.providers as p (p.id)}
		<Card.Root>
			<Card.Header>
				<Card.Title class="flex items-center gap-2"
					><ProviderLogo provider={p.id} class="size-5" /> {p.name}</Card.Title
				>
				{#if p.pool?.accountCount}
					<Card.Description>
						{p.pool.accountCount} account{p.pool.accountCount === 1 ? '' : 's'} in the pool
					</Card.Description>
				{/if}
			</Card.Header>
			<Card.Content class="space-y-6">
				{#if p.pool}{@render entry({
						...p.pool,
						name: p.accounts?.length ? 'All accounts' : undefined
					})}{/if}
				{#if p.banked}{@render banked(p.banked)}{/if}
				{#if p.accounts?.length}
					<div class="grid gap-6 border-t pt-6 sm:grid-cols-2">
						{#each p.accounts as a, i (i)}
							{@render entry(a)}
						{/each}
					</div>
				{/if}
			</Card.Content>
		</Card.Root>
	{:else}
		<p class="text-sm text-muted-foreground">Nothing to show.</p>
	{/each}

	{#if view.codexResets}
		{@const r = view.codexResets}
		<Card.Root>
			<Card.Header>
				<Card.Title>Codex resets</Card.Title>
				<Card.Description>
					Global limit resets announced by OpenAI, via
					<a
						href="https://codex-resets.com"
						target="_blank"
						rel="noreferrer"
						class="underline underline-offset-4">codex-resets.com</a
					>
				</Card.Description>
			</Card.Header>
			<Card.Content class="space-y-4 text-sm">
				{#if r.scheduled}
					<div class="flex gap-3">
						<CalendarClockIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
						<div>
							<p class="font-medium">
								Upcoming {r.scheduled.reset_type} reset{#if r.scheduled.scheduled_for}
									· {shortDateTime(r.scheduled.scheduled_for)} ({countdown(
										r.scheduled.scheduled_for
									)}){/if}
							</p>
							<p class="text-muted-foreground">{r.scheduled.text}</p>
						</div>
					</div>
				{/if}
				{#if r.forecast}
					<div class="flex gap-3">
						<RadarIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
						<div>
							<p class="font-medium">
								Forecast: {r.forecast.level} chance{#if r.forecast.reset_chance_percent !== null}
									({r.forecast.reset_chance_percent}%){/if} · {r.forecast.forecast_window}
							</p>
							<p class="text-muted-foreground">{r.forecast.text}</p>
							<p class="text-xs text-muted-foreground">
								An AI-made guess, not an OpenAI announcement.
							</p>
						</div>
					</div>
				{/if}
				{#if r.latest}
					<div class="flex gap-3">
						<HistoryIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
						<div>
							<p class="font-medium">
								Last {r.latest.reset_type} reset {relativeTime(r.latest.announced_at)}
							</p>
							<p class="text-muted-foreground">{r.latest.text}</p>
						</div>
					</div>
				{/if}
				{#if r.stats}
					<p class="text-muted-foreground">
						{r.stats.total} resets so far{#if r.stats.avg_interval_days !== null}
							· about every {r.stats.avg_interval_days.toFixed(1)} days{/if}{#if r.stats.days_since_last !== null}
							· last one {r.stats.days_since_last.toFixed(1)} days ago{/if}
					</p>
				{/if}
			</Card.Content>
		</Card.Root>
	{/if}
</div>
