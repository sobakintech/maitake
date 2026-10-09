<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type DisplayProfile, type Settings } from '#lib/api.js';
	import { countdown, relativeTime, shortDateTime } from '#lib/format.js';
	import { Separator } from '#lib/components/ui/separator/index.js';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import * as RadioGroup from '#lib/components/ui/radio-group/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import { Draft } from '#lib/draft.svelte.js';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import NewProfileDialog from './new-profile-dialog.svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const draft = new Draft<Settings>(untrack(() => data.settings));
	$effect.pre(() => draft.sync(data.settings));
	const form = $derived(draft.value);
	let saving = $state(false);
	let deleting = $state<DisplayProfile | null>(null);
	let newProfileOpen = $state(false);

	async function save() {
		saving = true;
		try {
			const next = draft.snapshot();
			next.codexResets.intervalMinutes = Number(next.codexResets.intervalMinutes);
			const { settings } = await admin.saveSettings(next);
			draft.value = structuredClone(settings);
			draft.commit();
			toast.success('Saved');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			saving = false;
		}
	}

	function adoptDefault(id: number) {
		draft.base = { ...draft.base, quotaPage: { ...draft.base.quotaPage, defaultProfileId: id } };
		draft.value.quotaPage.defaultProfileId = id;
	}

	// The default profile is applied immediately and kept out of the unsaved-changes draft.
	async function setDefault(id: number, message?: string) {
		try {
			await admin.saveSettings({
				...draft.base,
				quotaPage: { ...draft.base.quotaPage, defaultProfileId: id }
			});
			adoptDefault(id);
			if (message) toast.success(message);
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}

	const resets = $derived(data.codexResets);
	const feed = $derived(resets.status);

	function capitalize(s: string) {
		return s ? s[0].toUpperCase() + s.slice(1) : s;
	}
</script>

<div class="mx-auto max-w-5xl space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">Quota page</h1>
			<p class="text-sm text-muted-foreground">
				A read-only page where people check how much of the shared accounts' limits is left.
			</p>
		</div>
		<Button variant="outline" href="/quota" target="_blank"><ExternalLinkIcon /> Open page</Button>
	</div>

	<Card.Root>
		<Card.Header>
			<Card.Title>Page</Card.Title>
		</Card.Header>
		<Card.Content>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					save();
				}}
			>
				<Field.Group>
					<Field.Field orientation="horizontal">
						<Field.Content>
							<Field.Label for="qp-enabled">Enabled</Field.Label>
							<Field.Description
								>When off, the page and its API return “not found”.</Field.Description
							>
						</Field.Content>
						<Switch id="qp-enabled" bind:checked={form.quotaPage.enabled} />
					</Field.Field>
					<Field.Set>
						<Field.Legend variant="label">Who can see it</Field.Legend>
						<RadioGroup.Root bind:value={form.quotaPage.access} class="gap-3">
							<div class="flex items-start gap-3">
								<RadioGroup.Item value="key" id="acc-key" class="mt-0.5" />
								<Label for="acc-key" class="flex flex-col items-start gap-0.5 font-normal">
									<span class="font-medium">Anyone with a valid API key</span>
									<span class="text-muted-foreground"
										>Each key sees the display profile assigned to it.</span
									>
								</Label>
							</div>
							<div class="flex items-start gap-3">
								<RadioGroup.Item value="public" id="acc-public" class="mt-0.5" />
								<Label for="acc-public" class="flex flex-col items-start gap-0.5 font-normal">
									<span class="font-medium">Everyone</span>
									<span class="text-muted-foreground">
										No key needed; visitors see the default profile. A key still unlocks its own
										profile.
									</span>
								</Label>
							</div>
						</RadioGroup.Root>
					</Field.Set>
					<Field.Field>
						<Field.Label for="qp-title">Title</Field.Label>
						<Input id="qp-title" maxlength={100} bind:value={form.quotaPage.title} />
					</Field.Field>
					<Field.Field>
						<Field.Label for="qp-notice">Notice</Field.Label>
						<Textarea
							id="qp-notice"
							rows={3}
							placeholder="e.g. Weekly limits reset Monday mornings."
							bind:value={form.quotaPage.notice}
						/>
						<Field.Description
							>Shown at the top of the page. Leave empty for none.</Field.Description
						>
					</Field.Field>
				</Field.Group>
			</form>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Display profiles</Card.Title>
			<Card.Description>
				Each profile decides what's visible. Assign profiles to keys on a user's page; keys without
				one use the default.
			</Card.Description>
			<Card.Action>
				<Button size="sm" onclick={() => (newProfileOpen = true)}><PlusIcon /> New profile</Button>
			</Card.Action>
		</Card.Header>
		<Card.Content>
			{#if data.profiles.length === 0}
				<p class="py-4 text-sm text-muted-foreground">
					No profiles yet, so everyone sees the built-in default: pooled limits, percentages left,
					no account names.
				</p>
			{:else}
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Name</Table.Head>
							<Table.Head>Keys using it</Table.Head>
							<Table.Head>Updated</Table.Head>
							<Table.Head class="w-0"></Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each data.profiles as p (p.id)}
							<Table.Row>
								<Table.Cell class="font-medium">
									<a href="/quota-page/profiles/{p.id}" class="hover:underline">{p.name}</a>
									{#if p.isDefault}<Badge variant="secondary" class="ml-2">Default</Badge>{/if}
								</Table.Cell>
								<Table.Cell class="tabular-nums">{p.keyCount}</Table.Cell>
								<Table.Cell class="text-muted-foreground">{relativeTime(p.updatedAt)}</Table.Cell>
								<Table.Cell>
									<div class="flex justify-end gap-1">
										{#if !p.isDefault}
											<Button
												variant="ghost"
												size="sm"
												onclick={() => setDefault(p.id, `${p.name} is now the default`)}
												>Make default</Button
											>
										{/if}
										<Button variant="ghost" size="sm" href="/quota-page/profiles/{p.id}"
											>Edit</Button
										>
										<Button variant="ghost" size="sm" onclick={() => (deleting = p)}>Delete</Button>
									</div>
								</Table.Cell>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			{/if}
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Codex reset feed</Card.Title>
			<Card.Description>
				Announced and predicted global Codex resets from
				<a
					href="https://codex-resets.com"
					target="_blank"
					rel="noreferrer"
					class="underline underline-offset-4">codex-resets.com</a
				>. Profiles choose which parts to show.
			</Card.Description>
			<Card.Action>
				<Button
					size="sm"
					variant="outline"
					onclick={async () => {
						await admin.refreshCodexResets();
						setTimeout(() => invalidateAll(), 2500);
					}}
				>
					<RefreshCwIcon /> Check now
				</Button>
			</Card.Action>
		</Card.Header>
		<Card.Content class="space-y-6">
			<Field.Group>
				<Field.Field orientation="horizontal">
					<Field.Content>
						<Field.Label for="cr-enabled">Fetch the feed</Field.Label>
						<Field.Description>
							maitake checks it in the background; visitors' browsers never contact it.
						</Field.Description>
					</Field.Content>
					<Switch id="cr-enabled" bind:checked={form.codexResets.enabled} />
				</Field.Field>
				<Field.Field>
					<Field.Label for="cr-interval">Check every (minutes)</Field.Label>
					<Input
						id="cr-interval"
						type="number"
						min={2}
						max={1440}
						class="w-32"
						disabled={!form.codexResets.enabled}
						bind:value={form.codexResets.intervalMinutes}
					/>
				</Field.Field>
			</Field.Group>

			<Separator />

			<div class="space-y-4">
				<div class="flex flex-wrap items-baseline justify-between gap-2">
					<h3 class="text-sm font-medium">Latest data</h3>
					<p class="text-xs text-muted-foreground">
						{resets.fetchedAt ? `Checked ${relativeTime(resets.fetchedAt)}` : 'Not checked yet'}
					</p>
				</div>

				{#if resets.error}
					<div
						class="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm"
					>
						<TriangleAlertIcon class="mt-0.5 size-4 shrink-0 text-destructive" />
						<p>
							The last check failed: {resets.error}{#if resets.status}. Showing the last data that
								loaded.{/if}
						</p>
					</div>
				{/if}

				{#if feed}
					<div class="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
						{#snippet tile(label: string, value: string, detail?: string)}
							<div class="rounded-lg border bg-muted/30 p-3">
								<p class="text-xs text-muted-foreground">{label}</p>
								<p class="mt-1 text-sm font-medium">{value}</p>
								{#if detail}<p class="mt-0.5 text-xs text-muted-foreground">{detail}</p>{/if}
							</div>
						{/snippet}
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
						{@render tile(
							'Last reset',
							feed.latest_reset
								? capitalize(relativeTime(feed.latest_reset.announced_at))
								: 'None yet',
							feed.latest_reset ? `${capitalize(feed.latest_reset.reset_type)} reset` : undefined
						)}
						{@render tile(
							'Forecast',
							feed.active_watch
								? `${capitalize(feed.active_watch.level)}${feed.active_watch.reset_chance_percent !== null ? ` · ${feed.active_watch.reset_chance_percent}%` : ''}`
								: 'Nothing predicted',
							feed.active_watch?.forecast_window
						)}
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
								Latest announcement{#if feed.latest_reset.source.author}
									by @{feed.latest_reset.source.author}{/if}
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
				{:else if !resets.error}
					<p class="text-sm text-muted-foreground">
						{form.codexResets.enabled
							? 'No data yet. Use “Check now” to fetch it.'
							: 'The feed is off.'}
					</p>
				{/if}
			</div>
		</Card.Content>
	</Card.Root>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>

<NewProfileDialog bind:open={newProfileOpen} profiles={data.profiles} />

<AlertDialog.Root open={deleting !== null} onOpenChange={(v) => !v && (deleting = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete {deleting?.name}?</AlertDialog.Title>
			<AlertDialog.Description>
				{deleting?.keyCount ?? 0} key(s) use it; they'll fall back to the default profile.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				variant="destructive"
				onclick={async () => {
					const p = deleting;
					deleting = null;
					if (!p) return;
					try {
						await admin.deleteProfile(p.id);
						if (draft.base.quotaPage.defaultProfileId === p.id) adoptDefault(0);
						toast.success('Profile deleted');
						await invalidateAll();
					} catch (e) {
						toast.error(errorMessage(e));
					}
				}}
			>
				Delete
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
