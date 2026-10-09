<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, api, errorMessage, type User } from '#lib/api.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import ExpiryPicker from './expiry-picker.svelte';
	import ConnectSnippets from './connect-snippets.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import CheckIcon from '@lucide/svelte/icons/check';
	import CopyIcon from '@lucide/svelte/icons/copy';

	// With userId the key is created for that user; with users, an owner picker is shown
	// (the signed-in admin or any user).
	let {
		userId,
		users,
		adminName,
		open = $bindable(false)
	}: { userId?: number; users?: User[]; adminName?: string; open: boolean } = $props();

	let owner = $state('me');
	const ownerLabel = $derived(
		owner === 'me'
			? `${adminName ?? 'Me'} (admin)`
			: (users?.find((u) => String(u.id) === owner)?.name ?? '…')
	);

	let name = $state('');
	let expiresAt = $state<string | null>(null);
	let error = $state('');
	let busy = $state(false);
	let secret = $state('');
	let copied = $state(false);

	$effect(() => {
		if (open) {
			name = '';
			owner = 'me';
			expiresAt = null;
			error = '';
			secret = '';
			copied = false;
		}
	});

	async function create(e: SubmitEvent) {
		e.preventDefault();
		busy = true;
		error = '';
		try {
			const res =
				userId !== undefined
					? await api.createKey(userId, name, expiresAt)
					: await admin.createKey({
							name,
							expiresAt,
							userId: owner === 'me' ? undefined : Number(owner)
						});
			secret = res.secret;
			await invalidateAll();
		} catch (err) {
			error = errorMessage(err);
		} finally {
			busy = false;
		}
	}

	async function copy() {
		await navigator.clipboard.writeText(secret);
		copied = true;
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content class={secret ? 'sm:max-w-xl' : 'sm:max-w-md'}>
		{#if secret}
			<Dialog.Header>
				<Dialog.Title>Key created</Dialog.Title>
				<Dialog.Description>
					Copy it now. It is stored hashed and can't be shown again.
				</Dialog.Description>
			</Dialog.Header>
			<div class="flex items-center gap-2">
				<Input
					readonly
					value={secret}
					class="font-mono text-xs"
					onfocus={(e) => e.currentTarget.select()}
				/>
				<Button variant="outline" size="icon" onclick={copy} aria-label="Copy key">
					{#if copied}<CheckIcon />{:else}<CopyIcon />{/if}
				</Button>
			</div>
			<div class="space-y-2 border-t pt-4">
				<p class="text-sm font-medium">Use it</p>
				<ConnectSnippets apiKey={secret} />
			</div>
			<Dialog.Footer>
				<Button onclick={() => (open = false)}>Done</Button>
			</Dialog.Footer>
		{:else}
			<form onsubmit={create} class="space-y-6">
				<Dialog.Header>
					<Dialog.Title>New API key</Dialog.Title>
					<Dialog.Description>The full key is shown once after creation.</Dialog.Description>
				</Dialog.Header>
				<Field.Group>
					{#if users}
						<Field.Field>
							<Field.Label>Owner</Field.Label>
							<Select.Root type="single" bind:value={owner}>
								<Select.Trigger class="w-full">{ownerLabel}</Select.Trigger>
								<Select.Content>
									<Select.Item value="me" label={`${adminName ?? 'Me'} (admin)`}>
										{adminName ?? 'Me'} (admin)
									</Select.Item>
									{#each users as u (u.id)}
										<Select.Item value={String(u.id)} label={u.name}>{u.name}</Select.Item>
									{/each}
								</Select.Content>
							</Select.Root>
							<Field.Description>Usage is tracked under the owner.</Field.Description>
						</Field.Field>
					{/if}
					<Field.Field>
						<Field.Label for="key-name">Name</Field.Label>
						<Input id="key-name" placeholder="e.g. laptop" maxlength={100} bind:value={name} />
					</Field.Field>
					<Field.Field data-invalid={!!error}>
						<Field.Label>Expires</Field.Label>
						<ExpiryPicker bind:value={expiresAt} />
						{#if error}<Field.Error>{error}</Field.Error>{/if}
					</Field.Field>
				</Field.Group>
				<Dialog.Footer>
					<Button type="submit" disabled={busy}>Create key</Button>
				</Dialog.Footer>
			</form>
		{/if}
	</Dialog.Content>
</Dialog.Root>
