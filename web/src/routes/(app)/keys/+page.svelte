<script lang="ts">
	import KeysTable from '#lib/components/keys-table.svelte';
	import NewKeyDialog from '#lib/components/new-key-dialog.svelte';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import PlugIcon from '@lucide/svelte/icons/plug';
	import ConnectSnippets from '#lib/components/connect-snippets.svelte';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import SearchIcon from '@lucide/svelte/icons/search';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let newKeyOpen = $state(false);
	let connectOpen = $state(false);
	let owner = $state('all');
	let search = $state('');

	const ownerLabel = $derived(
		owner === 'all'
			? 'All owners'
			: owner === 'admin'
				? 'Admin keys'
				: (data.users.find((u) => String(u.id) === owner)?.name ?? 'All owners')
	);

	const visible = $derived(
		data.keys.filter((k) => {
			if (owner === 'admin' && k.ownerType !== 'admin') return false;
			if (owner !== 'all' && owner !== 'admin' && String(k.userId) !== owner) return false;
			const q = search.trim().toLowerCase();
			return (
				!q ||
				k.name.toLowerCase().includes(q) ||
				k.prefix.toLowerCase().includes(q) ||
				k.ownerName.toLowerCase().includes(q)
			);
		})
	);
</script>

<div class="mx-auto max-w-6xl space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">API keys</h1>
			<p class="text-sm text-muted-foreground">
				Every key, whether it belongs to a user or to you.
			</p>
		</div>
		<div class="flex gap-2">
			<Button variant="outline" onclick={() => (connectOpen = true)}>
				<PlugIcon /> How to connect
			</Button>
			<Button onclick={() => (newKeyOpen = true)}><PlusIcon /> New key</Button>
		</div>
	</div>

	<Card.Root>
		<Card.Header class="flex flex-wrap items-center gap-3">
			<div class="relative">
				<SearchIcon
					class="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
				/>
				<Input placeholder="Search keys" class="w-56 pl-8" bind:value={search} />
			</div>
			<Select.Root type="single" bind:value={owner}>
				<Select.Trigger class="w-44">{ownerLabel}</Select.Trigger>
				<Select.Content>
					<Select.Item value="all" label="All owners">All owners</Select.Item>
					<Select.Item value="admin" label="Admin keys">Admin keys</Select.Item>
					{#each data.users as u (u.id)}
						<Select.Item value={String(u.id)} label={u.name}>{u.name}</Select.Item>
					{/each}
				</Select.Content>
			</Select.Root>
		</Card.Header>
		<Card.Content>
			<KeysTable
				keys={visible}
				profiles={data.profiles}
				usage={data.keyUsage}
				showOwner
				empty={data.keys.length ? 'No keys match.' : 'No keys yet.'}
			/>
		</Card.Content>
	</Card.Root>
</div>

<NewKeyDialog users={data.users} adminName={data.admin.username} bind:open={newKeyOpen} />

<Dialog.Root bind:open={connectOpen}>
	<Dialog.Content class="sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title>How to connect</Dialog.Title>
			<Dialog.Description>
				Point a client at this server and give it an API key. Each person should use their own key.
			</Dialog.Description>
		</Dialog.Header>
		<ConnectSnippets />
	</Dialog.Content>
</Dialog.Root>
