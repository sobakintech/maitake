<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { errorMessage, type FeedSettings, type ResetsSnapshot } from '#lib/api.js';
	import { countdown, relativeTime, shortDateTime } from '#lib/format.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import { toast } from 'svelte-sonner';

	// One reset tracker's settings and latest data. Feeds without a schedule or forecast
	// (claude-resets.com) skip those tiles.
	let {
		name,
		site,
		description,
		snapshot,
		settings = $bindable(),
		forecasts = false,
		idPrefix,
		onRefresh
	}: {
		name: string;
		site: string;
		description: string;
		snapshot: ResetsSnapshot;
		settings: FeedSettings;
		forecasts?: boolean;
		idPrefix: string;
		onRefresh: () => Promise<void>;
	} = $props();

	const feed = $derived(snapshot.status);
	const usableUntil = $derived(
		feed?.latest_reset?.usable_until &&
			new Date(feed.latest_reset.usable_until).getTime() > Date.now()
			? feed.latest_reset.usable_until
			: null
	);

	let checking = $state(false);

	// The server fetches in the background, so give it a moment before reloading.
	async function checkNow() {
		checking = true;
		try {
			await onRefresh();
			await new Promise((r) => setTimeout(r, 2500));
			await invalidateAll();
			if (snapshot.error) toast.error(snapshot.error);
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			checking = false;
		}
	}

	function capitalize(s: string) {
		return s ? s[0].toUpperCase() + s.slice(1) : s;
	}
</script>

<Card.Root>
	<Card.Header>
		<Card.Title>{name} reset feed</Card.Title>
		<Card.Description>
			{description} from
			<a href="https://{site}" target="_blank" rel="noreferrer" class="underline underline-offset-4"
				>{site}</a
			>. Profiles choose which parts to show.
		</Card.Description>
		<Card.Action>
			<Button size="sm" variant="outline" disabled={checking} onclick={checkNow}>
				<RefreshCwIcon class={checking ? 'animate-spin' : ''} /> Check now
			</Button>
		</Card.Action>
	</Card.Header>
	<Card.Content class="space-y-6">
		<Field.Group>
			<Field.Field orientation="horizontal">
				<Field.Content>
					<Field.Label for="{idPrefix}-enabled">Fetch the feed</Field.Label>
					<Field.Description>
						maitake checks it in the background; visitors' browsers never contact it.
					</Field.Description>
				</Field.Content>
				<Switch id="{idPrefix}-enabled" bind:checked={settings.enabled} />
			</Field.Field>
			<Field.Field>
				<Field.Label for="{idPrefix}-interval">Check every (minutes)</Field.Label>
				<Input
					id="{idPrefix}-interval"
					type="number"
					min={2}
					max={1440}
					class="w-32"
					disabled={!settings.enabled}
					bind:value={settings.intervalMinutes}
				/>
			</Field.Field>
		</Field.Group>

		<Separator />

		<div class="space-y-4">
			<div class="flex flex-wrap items-baseline justify-between gap-2">
				<h3 class="text-sm font-medium">Latest data</h3>
				<p class="text-xs text-muted-foreground">
					{snapshot.fetchedAt ? `Checked ${relativeTime(snapshot.fetchedAt)}` : 'Not checked yet'}
				</p>
			</div>

			{#if snapshot.error}
				<div
					class="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm"
				>
					<TriangleAlertIcon class="mt-0.5 size-4 shrink-0 text-destructive" />
					<p>
						The last check failed: {snapshot.error}{#if snapshot.status}. Showing the last data that
							loaded.{/if}
					</p>
				</div>
			{/if}

			{#if feed}
				<div class="grid gap-3 sm:grid-cols-2 {forecasts || usableUntil ? 'lg:grid-cols-4' : ''}">
					{#snippet tile(label: string, value: string, detail?: string)}
						<div class="rounded-lg border bg-muted/30 p-3">
							<p class="text-xs text-muted-foreground">{label}</p>
							<p class="mt-1 text-sm font-medium">{value}</p>
							{#if detail}<p class="mt-0.5 text-xs text-muted-foreground">{detail}</p>{/if}
						</div>
					{/snippet}
					{#if forecasts}
						{@render tile(
							'Upcoming reset',
							feed.scheduled_reset
								? `${capitalize(feed.scheduled_reset.reset_type)} reset`
								: 'None announced',
							feed.scheduled_reset
								? feed.scheduled_reset.scheduled_for
									? `${shortDateTime(feed.scheduled_reset.scheduled_for)} · in ${countdown(feed.scheduled_reset.scheduled_for)}`
									: 'Time not announced yet'
								: undefined
						)}
					{/if}
					{@render tile(
						'Last reset',
						feed.latest_reset
							? capitalize(relativeTime(feed.latest_reset.announced_at))
							: 'None yet',
						feed.latest_reset
							? `${capitalize(feed.latest_reset.reset_type)} reset${feed.latest_reset.scope ? ` · ${feed.latest_reset.scope}` : ''}`
							: undefined
					)}
					{#if usableUntil}
						{@render tile(
							'Banked reset usable until',
							shortDateTime(usableUntil),
							`in ${countdown(usableUntil)}`
						)}
					{/if}
					{#if forecasts}
						{@render tile(
							'Forecast',
							feed.active_watch
								? `${capitalize(feed.active_watch.level)}${feed.active_watch.reset_chance_percent !== null ? ` · ${feed.active_watch.reset_chance_percent}%` : ''}`
								: 'Nothing predicted',
							feed.active_watch?.forecast_window
						)}
					{/if}
					{@render tile(
						'History',
						`${feed.stats.total} resets`,
						feed.stats.avg_interval_days !== null
							? `About every ${feed.stats.avg_interval_days.toFixed(1)} days`
							: undefined
					)}
				</div>

				{#if feed.latest_reset}
					<figure class="rounded-lg border-l-2 bg-muted/30 py-2 pr-3 pl-4">
						<blockquote class="text-sm">{feed.latest_reset.text}</blockquote>
						<figcaption class="mt-1 text-xs text-muted-foreground">
							Latest announcement{feed.latest_reset.source.author
								? ` by @${feed.latest_reset.source.author}`
								: ''}
							· {shortDateTime(feed.latest_reset.announced_at)}
							{#if feed.latest_reset.source.url}
								·
								<a
									href={feed.latest_reset.source.url}
									target="_blank"
									rel="noreferrer"
									class="underline underline-offset-4">View post</a
								>
							{/if}
						</figcaption>
					</figure>
				{/if}
			{:else if !snapshot.error}
				<p class="text-sm text-muted-foreground">
					{settings.enabled ? 'No data yet. Use “Check now” to fetch it.' : 'The feed is off.'}
				</p>
			{/if}
		</div>
	</Card.Content>
</Card.Root>
