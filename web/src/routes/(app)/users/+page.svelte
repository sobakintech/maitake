<script lang="ts">
	import { goto } from '$app/navigation';
	import { api, errorMessage } from '#lib/api.js';
	import { relativeTime } from '#lib/format.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import { Textarea } from '#lib/components/ui/textarea/index.js';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import UsersIcon from '@lucide/svelte/icons/users';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let open = $state(false);
	let name = $state('');
	let note = $state('');
	let error = $state('');
	let busy = $state(false);

	function openCreate() {
		name = '';
		note = '';
		error = '';
		open = true;
	}

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const { user } = await api.createUser(name, note);
			open = false;
			await goto(`/users/${user.id}`);
		} catch (err) {
			error = errorMessage(err);
		} finally {
			busy = false;
		}
	}
</script>

<div class="mx-auto max-w-5xl space-y-6">
	<div class="flex items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">Users</h1>
			<p class="text-sm text-muted-foreground">
				People who can call the proxy with their own keys.
			</p>
		</div>
		{#if data.users.length > 0}
			<Button onclick={openCreate}><PlusIcon /> New user</Button>
		{/if}
	</div>

	{#if data.users.length === 0}
		<Empty.Root class="border">
			<Empty.Header>
				<Empty.Media variant="icon"><UsersIcon /></Empty.Media>
				<Empty.Title>No users yet</Empty.Title>
				<Empty.Description>Create a user, then give them an API key.</Empty.Description>
			</Empty.Header>
			<Empty.Content>
				<Button onclick={openCreate}><PlusIcon /> New user</Button>
			</Empty.Content>
		</Empty.Root>
	{:else}
		<div class="rounded-lg border">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Name</Table.Head>
						<Table.Head>Note</Table.Head>
						<Table.Head class="text-right">Keys</Table.Head>
						<Table.Head>Status</Table.Head>
						<Table.Head>Created</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each data.users as user (user.id)}
						<Table.Row class="cursor-pointer" onclick={() => goto(`/users/${user.id}`)}>
							<Table.Cell class="font-medium">
								<a href="/users/{user.id}" class="hover:underline">{user.name}</a>
							</Table.Cell>
							<Table.Cell class="max-w-64 truncate text-muted-foreground">{user.note}</Table.Cell>
							<Table.Cell class="text-right tabular-nums">{user.keyCount}</Table.Cell>
							<Table.Cell>
								{#if user.disabled}
									<Badge variant="secondary">Disabled</Badge>
								{:else}
									<Badge variant="outline">Active</Badge>
								{/if}
							</Table.Cell>
							<Table.Cell class="text-muted-foreground">{relativeTime(user.createdAt)}</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{/if}
</div>

<Dialog.Root bind:open>
	<Dialog.Content class="sm:max-w-md">
		<form onsubmit={create} class="space-y-6">
			<Dialog.Header>
				<Dialog.Title>New user</Dialog.Title>
				<Dialog.Description>You can create API keys for them next.</Dialog.Description>
			</Dialog.Header>
			<Field.Group>
				<Field.Field>
					<Field.Label for="name">Name</Field.Label>
					<Input id="name" required maxlength={100} bind:value={name} />
				</Field.Field>
				<Field.Field data-invalid={!!error}>
					<Field.Label for="note">Note</Field.Label>
					<Textarea id="note" placeholder="Optional" bind:value={note} />
					{#if error}<Field.Error>{error}</Field.Error>{/if}
				</Field.Field>
			</Field.Group>
			<Dialog.Footer>
				<Button type="submit" disabled={busy}>Create user</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
