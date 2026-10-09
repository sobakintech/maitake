<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { api, errorMessage } from '#lib/api.js';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import { toast } from 'svelte-sonner';
	import NewKeyDialog from '#lib/components/new-key-dialog.svelte';
	import KeysTable from '#lib/components/keys-table.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import { Draft } from '#lib/draft.svelte.js';
	import { untrack } from 'svelte';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let saving = $state(false);
	let newKeyOpen = $state(false);
	let deleteUserOpen = $state(false);

	type Details = { name: string; note: string };
	const details = (): Details => ({ name: data.user.name, note: data.user.note });
	const draft = new Draft<Details>(untrack(details));
	let draftUserId = untrack(() => data.user.id);
	$effect.pre(() => {
		const next = details();
		// Another user discards the draft; a refetch of the same user keeps edits in progress.
		if (data.user.id !== draftUserId) {
			draftUserId = data.user.id;
			draft.base = next;
			draft.reset();
		} else {
			draft.sync(next);
		}
	});
	const form = $derived(draft.value);

	async function run(action: () => Promise<unknown>, success?: string) {
		try {
			await action();
			await invalidateAll();
			if (success) toast.success(success);
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}

	async function saveDetails() {
		saving = true;
		try {
			const { user } = await api.updateUser(data.user.id, draft.snapshot());
			draft.value = { name: user.name, note: user.note };
			draft.commit();
			toast.success('Saved');
			await invalidateAll();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			saving = false;
		}
	}

	async function deleteUser() {
		try {
			await api.deleteUser(data.user.id);
			draft.reset();
			toast.success(`Deleted ${data.user.name}`);
			await goto('/users', { invalidateAll: true });
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}
</script>

<div class="mx-auto max-w-5xl space-y-6">
	<div class="space-y-2">
		<a
			href="/users"
			class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
		>
			<ArrowLeftIcon class="size-4" /> Users
		</a>
		<div class="flex items-center gap-3">
			<h1 class="text-xl font-semibold tracking-tight">{data.user.name}</h1>
			{#if data.user.disabled}<Badge variant="secondary">Disabled</Badge>{/if}
		</div>
	</div>

	<Card.Root>
		<Card.Header>
			<Card.Title>API keys</Card.Title>
			<Card.Description>Clients send these as a Bearer token or x-api-key header.</Card.Description>
			<Card.Action>
				<Button size="sm" onclick={() => (newKeyOpen = true)}><PlusIcon /> New key</Button>
			</Card.Action>
		</Card.Header>
		<Card.Content>
			<KeysTable keys={data.keys} profiles={data.profiles} />
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Details</Card.Title>
		</Card.Header>
		<Card.Content>
			<form
				onsubmit={(e) => {
					e.preventDefault();
					saveDetails();
				}}
			>
				<Field.Group>
					<Field.Field>
						<Field.Label for="name">Name</Field.Label>
						<Input id="name" required maxlength={100} bind:value={form.name} />
					</Field.Field>
					<Field.Field>
						<Field.Label for="note">Note</Field.Label>
						<Textarea id="note" bind:value={form.note} />
					</Field.Field>
				</Field.Group>
			</form>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Access</Card.Title>
		</Card.Header>
		<Card.Content class="space-y-6">
			<Field.Field orientation="horizontal">
				<Field.Content>
					<Field.Label for="user-disabled">Disable user</Field.Label>
					<Field.Description
						>All of this user's keys stop working until re-enabled.</Field.Description
					>
				</Field.Content>
				<Switch
					id="user-disabled"
					checked={data.user.disabled}
					onCheckedChange={(v) =>
						run(
							() => api.updateUser(data.user.id, { disabled: v }),
							v ? 'User disabled' : 'User enabled'
						)}
				/>
			</Field.Field>
			<Field.Field orientation="horizontal">
				<Field.Content>
					<Field.Label>Delete user</Field.Label>
					<Field.Description>Removes the user and all of their keys.</Field.Description>
				</Field.Content>
				<Button variant="destructive" onclick={() => (deleteUserOpen = true)}>Delete</Button>
			</Field.Field>
		</Card.Content>
	</Card.Root>

	<UnsavedBar dirty={draft.dirty} {saving} onSave={saveDetails} onReset={() => draft.reset()} />
</div>

<NewKeyDialog userId={data.user.id} bind:open={newKeyOpen} />

<AlertDialog.Root bind:open={deleteUserOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete {data.user.name}?</AlertDialog.Title>
			<AlertDialog.Description>
				This deletes the user and their {data.keys.length} key(s). Clients using them will stop working.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={deleteUser}>Delete</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
