<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type DisplayProfile, type Settings } from '#lib/api.js';
	import { relativeTime } from '#lib/format.js';
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
	import * as Select from '#lib/components/ui/select/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import ExternalLinkIcon from '@lucide/svelte/icons/external-link';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import { Draft } from '#lib/draft.svelte.js';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import NewProfileDialog from './new-profile-dialog.svelte';
	import ResetFeedCard from './reset-feed-card.svelte';
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
			next.claudeResets.intervalMinutes = Number(next.claudeResets.intervalMinutes);
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

	// A deleted profile is already cleared on the server; keep the draft in step without marking it dirty.
	function clearPublic() {
		draft.base = { ...draft.base, quotaPage: { ...draft.base.quotaPage, publicProfileId: 0 } };
		draft.value.quotaPage.publicProfileId = 0;
	}
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">Quota page</h1>
			<p class="text-sm text-muted-foreground">
				A read-only page where people check how much of the shared accounts' limits is left.
			</p>
		</div>
		<Button variant="outline" href="/quota" target="_blank"><ExternalLinkIcon /> Open page</Button>
	</div>

	<div class="grid items-start gap-6 xl:grid-cols-2">
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
										<span class="font-medium">Keys with a view</span>
										<span class="text-muted-foreground"
											>Each key sees its own view or its profile. Keys with neither see nothing.</span
										>
									</Label>
								</div>
								<div class="flex items-start gap-3">
									<RadioGroup.Item value="public" id="acc-public" class="mt-0.5" />
									<Label for="acc-public" class="flex flex-col items-start gap-0.5 font-normal">
										<span class="font-medium">Everyone</span>
										<span class="text-muted-foreground">
											Visitors without a key see the profile below. Keys still see their own view.
										</span>
									</Label>
								</div>
							</RadioGroup.Root>
						</Field.Set>
						{#if form.quotaPage.access === 'public'}
							<Field.Field>
								<Field.Label>Profile for visitors without a key</Field.Label>
								<Select.Root
									type="single"
									value={String(form.quotaPage.publicProfileId)}
									onValueChange={(v) => (form.quotaPage.publicProfileId = Number(v))}
								>
									<Select.Trigger class="w-64">
										{data.profiles.find((p) => p.id === form.quotaPage.publicProfileId)?.name ??
											'None'}
									</Select.Trigger>
									<Select.Content>
										<Select.Item value="0" label="None">None</Select.Item>
										{#each data.profiles as p (p.id)}
											<Select.Item value={String(p.id)} label={p.name}>{p.name}</Select.Item>
										{/each}
									</Select.Content>
								</Select.Root>
								<Field.Description>
									{data.profiles.length
										? 'Without one, the page still asks for a key.'
										: 'Create a profile first; until then the page still asks for a key.'}
								</Field.Description>
							</Field.Field>
						{/if}
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
				<Card.Title>Profiles</Card.Title>
				<Card.Description>
					Saved views you can give to several keys at once. A key can also have its own view
					instead.
				</Card.Description>
				<Card.Action>
					<Button size="sm" onclick={() => (newProfileOpen = true)}><PlusIcon /> New profile</Button
					>
				</Card.Action>
			</Card.Header>
			<Card.Content>
				{#if data.profiles.length === 0}
					<p class="py-4 text-sm text-muted-foreground">
						No profiles. Keys can still have their own view.
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
										{#if p.isPublic}<Badge variant="secondary" class="ml-2">Public</Badge>{/if}
									</Table.Cell>
									<Table.Cell class="tabular-nums">{p.keyCount}</Table.Cell>
									<Table.Cell class="text-muted-foreground">{relativeTime(p.updatedAt)}</Table.Cell>
									<Table.Cell>
										<div class="flex justify-end gap-1">
											<Button variant="ghost" size="sm" href="/quota-page/profiles/{p.id}"
												>Edit</Button
											>
											<Button variant="ghost" size="sm" onclick={() => (deleting = p)}
												>Delete</Button
											>
										</div>
									</Table.Cell>
								</Table.Row>
							{/each}
						</Table.Body>
					</Table.Root>
				{/if}
			</Card.Content>
		</Card.Root>

		<ResetFeedCard
			name="Claude"
			site="claude-resets.com"
			description="Global Claude resets announced by Anthropic, including banked ones,"
			snapshot={data.claudeResets}
			bind:settings={form.claudeResets}
			idPrefix="clr"
			onRefresh={admin.refreshClaudeResets}
		/>

		<ResetFeedCard
			name="Codex"
			site="codex-resets.com"
			description="Announced and predicted global Codex resets"
			snapshot={data.codexResets}
			bind:settings={form.codexResets}
			forecasts
			idPrefix="cr"
			onRefresh={admin.refreshCodexResets}
		/>
	</div>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={save} onReset={() => draft.reset()} />
</div>

<NewProfileDialog bind:open={newProfileOpen} profiles={data.profiles} />

<AlertDialog.Root open={deleting !== null} onOpenChange={(v) => !v && (deleting = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete {deleting?.name}?</AlertDialog.Title>
			<AlertDialog.Description>
				{deleting?.keyCount ?? 0} key(s) use it; they'll lose quota page access unless they have their
				own view.
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
						if (draft.base.quotaPage.publicProfileId === p.id) clearPublic();
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
