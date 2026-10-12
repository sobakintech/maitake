<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { countdown } from '#lib/format.js';
	import { buildEvents, dayKey, kinds, type EventKind, type TimelineEvent } from '#lib/timeline.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import CalendarClockIcon from '@lucide/svelte/icons/calendar-clock';
	import ChevronLeftIcon from '@lucide/svelte/icons/chevron-left';
	import ChevronRightIcon from '@lucide/svelte/icons/chevron-right';
	import GaugeIcon from '@lucide/svelte/icons/gauge';
	import TimerResetIcon from '@lucide/svelte/icons/timer-reset';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import PauseIcon from '@lucide/svelte/icons/pause';
	import CreditCardIcon from '@lucide/svelte/icons/credit-card';
	import MegaphoneIcon from '@lucide/svelte/icons/megaphone';
	import { onMount } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	// Refresh the data and the clock every minute.
	let now = $state(new Date());
	onMount(() => {
		const t = setInterval(() => {
			now = new Date();
			invalidateAll();
		}, 60_000);
		return () => clearInterval(t);
	});

	const view = $derived(page.url.searchParams.get('view') === 'month' ? 'month' : 'agenda');
	function setView(v: string) {
		if (!v) return;
		const url = new URL(page.url.href);
		url.searchParams.set('view', v);
		goto(url, { replace: true, reset: false });
	}

	let shown = $state<EventKind[]>(kinds.map((k) => k.id));
	const all = $derived(buildEvents(data.accounts, data.keys, data.codexResets, now));
	const events = $derived(all.filter((e) => shown.includes(e.kind)));

	const kindStyle: Record<EventKind, { color: string; icon: typeof GaugeIcon }> = {
		limit: { color: 'var(--series-1)', icon: GaugeIcon },
		banked: { color: 'var(--series-2)', icon: TimerResetIcon },
		subscription: { color: 'var(--series-3)', icon: CreditCardIcon },
		key: { color: 'var(--series-4)', icon: KeyRoundIcon },
		cooldown: { color: 'var(--series-5)', icon: PauseIcon },
		codex: { color: 'var(--series-6)', icon: MegaphoneIcon }
	};

	function toggleKind(k: EventKind) {
		shown = shown.includes(k) ? shown.filter((x) => x !== k) : [...shown, k];
	}

	function dayLabel(d: Date) {
		const today = new Date(now.getFullYear(), now.getMonth(), now.getDate());
		const diff = Math.round(
			(new Date(d.getFullYear(), d.getMonth(), d.getDate()).getTime() - today.getTime()) /
				86_400_000
		);
		if (diff === 0) return 'Today';
		if (diff === 1) return 'Tomorrow';
		return d.toLocaleDateString(undefined, {
			weekday: 'long',
			month: 'short',
			day: 'numeric',
			year: d.getFullYear() === now.getFullYear() ? undefined : 'numeric'
		});
	}

	function time(d: Date) {
		return d.toLocaleTimeString(undefined, { hour: 'numeric', minute: '2-digit' });
	}

	const agenda = $derived.by(() => {
		const groups: { key: string; date: Date; events: TimelineEvent[] }[] = [];
		for (const e of events) {
			const key = dayKey(e.at);
			const last = groups[groups.length - 1];
			if (last?.key === key) last.events.push(e);
			else groups.push({ key, date: e.at, events: [e] });
		}
		return groups;
	});

	// Month view.
	let cursor = $state(new Date(new Date().getFullYear(), new Date().getMonth(), 1));
	let selected = $state(dayKey(new Date()));
	const monthLabel = $derived(
		cursor.toLocaleDateString(undefined, { month: 'long', year: 'numeric' })
	);
	const weekdays = $derived(
		Array.from({ length: 7 }, (_, i) =>
			new Date(2024, 0, 1 + i).toLocaleDateString(undefined, { weekday: 'short' })
		)
	);
	const byDay = $derived.by(() => {
		const m: Record<string, TimelineEvent[]> = {};
		for (const e of events) {
			const k = dayKey(e.at);
			(m[k] ??= []).push(e);
		}
		return m;
	});
	// Monday-first grid covering the whole month.
	const cells = $derived.by(() => {
		const first = new Date(cursor.getFullYear(), cursor.getMonth(), 1);
		const offset = (first.getDay() + 6) % 7;
		const days = new Date(cursor.getFullYear(), cursor.getMonth() + 1, 0).getDate();
		const count = Math.ceil((offset + days) / 7) * 7;
		return Array.from({ length: count }, (_, i) => {
			const date = new Date(cursor.getFullYear(), cursor.getMonth(), 1 - offset + i);
			return { date, key: dayKey(date), inMonth: date.getMonth() === cursor.getMonth() };
		});
	});
	const selectedEvents = $derived(byDay[selected] ?? []);

	function shiftMonth(n: number) {
		cursor = new Date(cursor.getFullYear(), cursor.getMonth() + n, 1);
	}
</script>

{#snippet eventRow(e: TimelineEvent)}
	{@const style = kindStyle[e.kind]}
	<a
		href={e.href}
		class="flex items-start gap-3 rounded-lg px-3 py-2.5 transition-colors hover:bg-muted/60"
	>
		<span class="w-16 shrink-0 pt-0.5 text-xs text-muted-foreground tabular-nums">
			{time(e.at)}
		</span>
		<span
			class="mt-0.5 flex size-6 shrink-0 items-center justify-center rounded-full"
			style="background: color-mix(in oklch, {style.color} 18%, transparent)"
		>
			<style.icon class="size-3.5" style="color: {style.color}" />
		</span>
		<span class="min-w-0 flex-1">
			<span class="flex flex-wrap items-center gap-1.5 text-sm font-medium">
				{#if e.provider}<ProviderLogo provider={e.provider} class="size-3.5" />{/if}
				{e.title}
			</span>
			{#if e.detail}<span class="block truncate text-xs text-muted-foreground">{e.detail}</span
				>{/if}
		</span>
		<span class="shrink-0 pt-0.5 text-xs text-muted-foreground tabular-nums">
			in {countdown(e.at.toISOString())}
		</span>
	</a>
{/snippet}

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">Timeline</h1>
			<p class="text-sm text-muted-foreground">
				Everything coming up: limit resets, expiries, renewals and announced resets.
			</p>
		</div>
		<ToggleGroup.Root type="single" variant="outline" value={view} onValueChange={setView}>
			<ToggleGroup.Item value="agenda" class="px-3">Agenda</ToggleGroup.Item>
			<ToggleGroup.Item value="month" class="px-3">Month</ToggleGroup.Item>
		</ToggleGroup.Root>
	</div>

	<div class="flex flex-wrap gap-2">
		{#each kinds as k (k.id)}
			{@const style = kindStyle[k.id]}
			{@const count = all.filter((e) => e.kind === k.id).length}
			<Button
				size="sm"
				variant={shown.includes(k.id) ? 'secondary' : 'ghost'}
				class={shown.includes(k.id) ? '' : 'text-muted-foreground line-through decoration-1'}
				aria-pressed={shown.includes(k.id)}
				onclick={() => toggleKind(k.id)}
			>
				<span class="size-2 rounded-full" style="background: {style.color}"></span>
				{k.label}
				<span class="text-xs text-muted-foreground tabular-nums">{count}</span>
			</Button>
		{/each}
	</div>

	{#if view === 'agenda'}
		{#if agenda.length === 0}
			<Empty.Root class="border">
				<Empty.Header>
					<Empty.Media variant="icon"><CalendarClockIcon /></Empty.Media>
					<Empty.Title>Nothing coming up</Empty.Title>
					<Empty.Description>
						{all.length
							? 'All upcoming events are filtered out.'
							: 'No upcoming resets or expiries.'}
					</Empty.Description>
				</Empty.Header>
			</Empty.Root>
		{:else}
			<div class="space-y-6">
				{#each agenda as g (g.key)}
					<section>
						<h2 class="sticky top-0 z-10 bg-background/90 py-1.5 text-sm font-medium backdrop-blur">
							{dayLabel(g.date)}
						</h2>
						<Card.Root class="py-1">
							<div class="divide-y">
								{#each g.events as e (e.id)}{@render eventRow(e)}{/each}
							</div>
						</Card.Root>
					</section>
				{/each}
			</div>
		{/if}
	{:else}
		<Card.Root>
			<Card.Header>
				<Card.Title>{monthLabel}</Card.Title>
				<Card.Action class="flex gap-1">
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label="Previous month"
						onclick={() => shiftMonth(-1)}
					>
						<ChevronLeftIcon />
					</Button>
					<Button
						variant="outline"
						size="sm"
						onclick={() => {
							cursor = new Date(now.getFullYear(), now.getMonth(), 1);
							selected = dayKey(now);
						}}>Today</Button
					>
					<Button
						variant="ghost"
						size="icon-sm"
						aria-label="Next month"
						onclick={() => shiftMonth(1)}
					>
						<ChevronRightIcon />
					</Button>
				</Card.Action>
			</Card.Header>
			<Card.Content>
				<div class="grid grid-cols-7 gap-px overflow-hidden rounded-lg border bg-border">
					{#each weekdays as w (w)}
						<div class="bg-muted/40 px-2 py-1.5 text-xs font-medium text-muted-foreground">{w}</div>
					{/each}
					{#each cells as c (c.key)}
						{@const dayEvents = byDay[c.key] ?? []}
						{@const isToday = c.key === dayKey(now)}
						<button
							type="button"
							class={[
								'flex min-h-24 flex-col gap-1 bg-background p-1.5 text-left transition-colors hover:bg-muted/40',
								!c.inMonth && 'text-muted-foreground/60',
								selected === c.key && 'ring-2 ring-ring ring-inset'
							]}
							onclick={() => (selected = c.key)}
						>
							<span
								class={[
									'flex size-6 items-center justify-center rounded-full text-xs tabular-nums',
									isToday && 'bg-primary font-semibold text-primary-foreground'
								]}
							>
								{c.date.getDate()}
							</span>
							{#each dayEvents.slice(0, 3) as e (e.id)}
								<span
									class="flex items-center gap-1 truncate rounded-sm px-1 text-[11px] leading-5"
									style="background: color-mix(in oklch, {kindStyle[e.kind]
										.color} 16%, transparent)"
									title={e.title}
								>
									<span
										class="size-1.5 shrink-0 rounded-full"
										style="background: {kindStyle[e.kind].color}"
									></span>
									<span class="truncate">{e.title}</span>
								</span>
							{/each}
							{#if dayEvents.length > 3}
								<span class="px-1 text-[11px] text-muted-foreground"
									>+{dayEvents.length - 3} more</span
								>
							{/if}
						</button>
					{/each}
				</div>
			</Card.Content>
		</Card.Root>

		<section class="space-y-2">
			<h2 class="text-sm font-medium">
				{dayLabel(cells.find((c) => c.key === selected)?.date ?? now)}
			</h2>
			{#if selectedEvents.length}
				<Card.Root class="py-1">
					<div class="divide-y">
						{#each selectedEvents as e (e.id)}{@render eventRow(e)}{/each}
					</div>
				</Card.Root>
			{:else}
				<p class="text-sm text-muted-foreground">Nothing on this day.</p>
			{/if}
		</section>
	{/if}
</div>
