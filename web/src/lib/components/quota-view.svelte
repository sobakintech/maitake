<script lang="ts">
	import type {
		EntryView,
		QuotaLayout,
		QuotaView,
		WindowView,
		BankedView,
		ResetsView
	} from '#lib/types.js';
	import { countdown, money, relativeTime, shortDateTime } from '#lib/format.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import QuotaMeter from './quota-meter.svelte';
	import QuotaBanked from './quota-banked.svelte';
	import ProviderLogo from './provider-logo.svelte';
	import TimerResetIcon from '@lucide/svelte/icons/timer-reset';
	import CalendarClockIcon from '@lucide/svelte/icons/calendar-clock';
	import RadarIcon from '@lucide/svelte/icons/radar';
	import HistoryIcon from '@lucide/svelte/icons/history';

	let { view, layout = view.layout }: { view: QuotaView; layout?: QuotaLayout } = $props();

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
		if (view.resetFormat === 'both') return `in ${countdown(iso)} · ${shortDateTime(iso)}`;
		return `in ${countdown(iso)}`;
	}

	function extraText(x: NonNullable<EntryView['extra']>) {
		const state = x.unlimited ? 'unlimited' : x.enabled ? 'enabled' : 'off';
		const used = x.usedPercent !== undefined ? ` · ${x.usedPercent}% used` : '';
		return `Extra usage ${state}${used}`;
	}

	// In rows, banked resets open in a dialog when there's more than the count to see.
	let bankedOpen = $state<{ title: string; banked: BankedView } | null>(null);

	const feeds = $derived(
		[
			view.claudeResets && {
				r: view.claudeResets,
				name: 'Claude',
				vendor: 'Anthropic',
				site: 'claude-resets.com'
			},
			view.codexResets && {
				r: view.codexResets,
				name: 'Codex',
				vendor: 'OpenAI',
				site: 'codex-resets.com'
			}
		].filter((f) => !!f)
	);
</script>

{#snippet header(e: EntryView)}
	{#if e.name || e.plan || (e.status && e.status !== 'ok')}
		<div class="flex flex-wrap items-center gap-2">
			{#if e.name}<span class="text-sm font-medium">{e.name}</span>{/if}
			{#if e.plan}<Badge variant="outline">{e.plan}</Badge>{/if}
			{#if e.status && e.status !== 'ok'}<Badge variant="secondary"
					>{statusText[e.status] ?? e.status}</Badge
				>{/if}
		</div>
	{/if}
{/snippet}

{#snippet meters(e: EntryView, compact: boolean)}
	{#each e.windows as w (w.id)}
		{@const m = meter(w, e)}
		<QuotaMeter
			label={w.label}
			remaining={m.remaining}
			level={w.level}
			values={m.values}
			{modeFallback}
			flip={view.allowFlip}
			resetsAt={w.resetsAt}
			resetFormat={view.resetFormat}
			pace={w.pace}
			{compact}
		/>
	{:else}
		<p class="text-sm text-muted-foreground">No limit data yet.</p>
	{/each}
{/snippet}

{#snippet bankedLine(b: BankedView, title: string)}
	{@const next = b.expiries?.[0]}
	{@const text = `${b.available} banked reset${b.available === 1 ? '' : 's'}`}
	{#if b.expiries?.length || b.credits?.length}
		<button
			type="button"
			class="flex max-w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
			onclick={() => (bankedOpen = { title, banked: b })}
		>
			<TimerResetIcon class="size-3.5 shrink-0" />
			<span class="truncate underline-offset-2 hover:underline">
				{text}{next ? ` · expires in ${countdown(next)}` : ''}
			</span>
		</button>
	{:else}
		<p class="flex items-center gap-1.5 text-xs text-muted-foreground">
			<TimerResetIcon class="size-3.5 shrink-0" />
			{text}
		</p>
	{/if}
{/snippet}

{#snippet card(e: EntryView)}
	<div class="space-y-3">
		{@render header(e)}
		{@render meters(e, false)}
		{#if e.extra}<p class="text-xs text-muted-foreground">{extraText(e.extra)}</p>{/if}
		{#if e.banked}<QuotaBanked banked={e.banked} resetFormat={view.resetFormat} />{/if}
		{#if view.showUpdatedAt && e.updatedAt}
			<p class="text-xs text-muted-foreground">Updated {relativeTime(e.updatedAt)}</p>
		{/if}
	</div>
{/snippet}

{#snippet row(e: EntryView, providerName: string)}
	{@const side = !!(
		e.name ||
		e.plan ||
		(e.status && e.status !== 'ok') ||
		e.banked ||
		e.extra ||
		(view.showUpdatedAt && e.updatedAt)
	)}
	<div class="flex flex-wrap items-center gap-x-6 gap-y-2 py-3 first:pt-0 last:pb-0">
		{#if side}
			<div class="w-full min-w-0 space-y-0.5 @lg:w-44 @lg:shrink-0">
				{@render header(e)}
				{#if e.banked}{@render bankedLine(
						e.banked,
						e.name ? `${providerName} · ${e.name}` : providerName
					)}{/if}
				{#if e.extra}<p class="text-xs text-muted-foreground">{extraText(e.extra)}</p>{/if}
				{#if view.showUpdatedAt && e.updatedAt}
					<p class="text-xs text-muted-foreground">Updated {relativeTime(e.updatedAt)}</p>
				{/if}
			</div>
		{/if}
		<div class="flex min-w-0 flex-1 flex-wrap gap-x-5 gap-y-2">
			{@render meters(e, true)}
		</div>
	</div>
{/snippet}

{#snippet providers()}
	{#each view.providers as p (p.id)}
		{@const pool = p.pool && { ...p.pool, name: p.accounts?.length ? 'All accounts' : undefined }}
		<Card.Root>
			<Card.Header>
				<Card.Title class="flex items-center gap-2"
					><ProviderLogo provider={p.id} class="size-5" /> {p.name}</Card.Title
				>
				{#if view.showAccountCount && p.pool?.accountCount}
					<Card.Description>
						{p.pool.accountCount} account{p.pool.accountCount === 1 ? '' : 's'} in the pool
					</Card.Description>
				{/if}
			</Card.Header>
			{#if layout === 'rows'}
				<Card.Content class="divide-y">
					{#if pool}{@render row(pool, p.name)}{/if}
					{#if p.banked}
						<div class="py-3 first:pt-0 last:pb-0">
							{@render bankedLine(p.banked, p.name)}
						</div>
					{/if}
					{#each p.accounts ?? [] as a, i (i)}
						{@render row(a, p.name)}
					{/each}
				</Card.Content>
			{:else}
				<Card.Content class="space-y-6">
					{#if pool}{@render card(pool)}{/if}
					{#if p.banked}<QuotaBanked banked={p.banked} resetFormat={view.resetFormat} />{/if}
					{#if p.accounts?.length}
						<div
							class="grid gap-6 @xl:grid-cols-2 @5xl:grid-cols-3 {pool || p.banked
								? 'border-t pt-6'
								: ''}"
						>
							{#each p.accounts as a, i (i)}
								{@render card(a)}
							{/each}
						</div>
					{/if}
				</Card.Content>
			{/if}
		</Card.Root>
	{:else}
		<p class="text-sm text-muted-foreground">Nothing to show.</p>
	{/each}
{/snippet}

<div class="@container space-y-6">
	{#if view.notice}
		<div class="rounded-lg border bg-muted/40 px-4 py-3 text-sm whitespace-pre-line">
			{view.notice}
		</div>
	{/if}

	{#if view.feedsPosition !== 'top'}{@render providers()}{/if}
	{#each feeds as f (f.site)}
		{@render feed(f.r, f.name, f.vendor, f.site)}
	{/each}
	{#if view.feedsPosition === 'top'}{@render providers()}{/if}
</div>

<Dialog.Root open={bankedOpen !== null} onOpenChange={(v) => !v && (bankedOpen = null)}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Banked resets</Dialog.Title>
			<Dialog.Description>{bankedOpen?.title}</Dialog.Description>
		</Dialog.Header>
		{#if bankedOpen}<QuotaBanked banked={bankedOpen.banked} resetFormat={view.resetFormat} />{/if}
	</Dialog.Content>
</Dialog.Root>

{#snippet feed(r: ResetsView, name: string, vendor: string, site: string)}
	<Card.Root>
		<Card.Header>
			<Card.Title>{name} resets</Card.Title>
			<Card.Description>
				Global limit resets announced by {vendor}, via
				<a
					href="https://{site}"
					target="_blank"
					rel="noreferrer"
					class="underline underline-offset-4">{site}</a
				>
			</Card.Description>
		</Card.Header>
		<Card.Content class="space-y-4 text-sm">
			{#if r.scheduled}
				{@const at = r.scheduled.scheduled_for}
				<div class="flex gap-3">
					<CalendarClockIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
					<div>
						<p class="font-medium">
							Upcoming {r.scheduled.reset_type} reset{at
								? ` · ${shortDateTime(at)} (${countdown(at)})`
								: ''}
						</p>
						<p class="text-muted-foreground">{r.scheduled.text}</p>
					</div>
				</div>
			{/if}
			{#if r.forecast}
				{@const chance = r.forecast.reset_chance_percent}
				<div class="flex gap-3">
					<RadarIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
					<div>
						<p class="font-medium">
							Forecast: {r.forecast.level} chance{chance !== null ? ` (${chance}%)` : ''} · {r
								.forecast.forecast_window}
						</p>
						<p class="text-muted-foreground">{r.forecast.text}</p>
						<p class="text-xs text-muted-foreground">
							An AI-made guess, not an {vendor} announcement.
						</p>
					</div>
				</div>
			{/if}
			{#if r.latest}
				<div class="flex gap-3">
					<HistoryIcon class="mt-0.5 size-4 shrink-0 text-muted-foreground" />
					<div>
						<p class="font-medium">
							Last {r.latest.reset_type} reset {relativeTime(r.latest.announced_at)}{r.latest.scope
								? ` · ${r.latest.scope}`
								: ''}
						</p>
						<p class="text-muted-foreground">{r.latest.text}</p>
						{#if r.latest.usable_until && new Date(r.latest.usable_until).getTime() > Date.now()}
							<p class="text-xs text-muted-foreground">
								Usable until {expiryText(r.latest.usable_until)}
							</p>
						{/if}
					</div>
				</div>
			{/if}
			{#if r.stats}
				{@const s = r.stats}
				<p class="text-muted-foreground">
					{[
						`${s.total} resets so far`,
						s.avg_interval_days !== null && `about every ${s.avg_interval_days.toFixed(1)} days`,
						s.days_since_last !== null && `last one ${s.days_since_last.toFixed(1)} days ago`
					]
						.filter(Boolean)
						.join(' · ')}
				</p>
			{/if}
		</Card.Content>
	</Card.Root>
{/snippet}
