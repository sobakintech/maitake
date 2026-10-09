<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { api, errorMessage, type ApiKey, type DisplayProfile, type OwnedKey } from '#lib/api.js';
	import { dateTime, relativeTime } from '#lib/format.js';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import KeyRoundIcon from '@lucide/svelte/icons/key-round';
	import CopyKeyButton from './copy-key-button.svelte';
	import SettingsIcon from '@lucide/svelte/icons/settings-2';
	import Trash2Icon from '@lucide/svelte/icons/trash-2';
	import { toast } from 'svelte-sonner';

	let {
		keys,
		profiles,
		showOwner = false,
		empty = 'No keys yet.'
	}: {
		keys: (ApiKey | OwnedKey)[];
		profiles: DisplayProfile[];
		showOwner?: boolean;
		empty?: string;
	} = $props();

	let deleting = $state<ApiKey | null>(null);

	async function run(action: () => Promise<unknown>, success?: string) {
		try {
			await action();
			await invalidateAll();
			if (success) toast.success(success);
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}

	function expiryLabel(key: ApiKey) {
		if (!key.expiresAt) return 'Never';
		return new Date(key.expiresAt) < new Date() ? 'Expired' : relativeTime(key.expiresAt);
	}

	function details(key: ApiKey) {
		return [
			key.allowedModels.length ? key.allowedModels.join(', ') : '',
			key.modelRules?.rules?.length
				? `${key.modelRules.rules.length} mapping rule${key.modelRules.rules.length === 1 ? '' : 's'}`
				: '',
			key.rpmLimit ? `${key.rpmLimit}/min` : '',
			key.displayConfig
				? 'own quota view'
				: key.displayProfileId
					? `profile: ${profiles.find((p) => p.id === key.displayProfileId)?.name ?? '?'}`
					: ''
		]
			.filter(Boolean)
			.join(' · ');
	}

	function owner(key: ApiKey | OwnedKey) {
		return 'ownerType' in key ? key : null;
	}
</script>

{#if keys.length === 0}
	<div class="flex flex-col items-center gap-2 py-8 text-center text-sm text-muted-foreground">
		<KeyRoundIcon class="size-5" />
		{empty}
	</div>
{:else}
	<Table.Root>
		<Table.Header>
			<Table.Row>
				<Table.Head>Name</Table.Head>
				{#if showOwner}<Table.Head>Owner</Table.Head>{/if}
				<Table.Head>Key</Table.Head>
				<Table.Head>Created</Table.Head>
				<Table.Head>Last used</Table.Head>
				<Table.Head>Expires</Table.Head>
				<Table.Head>Enabled</Table.Head>
				<Table.Head class="w-0"></Table.Head>
			</Table.Row>
		</Table.Header>
		<Table.Body>
			{#each keys as key (key.id)}
				{@const o = owner(key)}
				{@const extra = details(key)}
				<Table.Row>
					<Table.Cell>
						<a href="/keys/{key.id}" class="font-medium hover:underline">{key.name || '—'}</a>
						{#if extra}<p class="text-xs text-muted-foreground">{extra}</p>{/if}
					</Table.Cell>
					{#if showOwner}
						<Table.Cell>
							{#if o?.ownerType === 'user' && key.userId}
								<a href="/users/{key.userId}" class="hover:underline">{o.ownerName}</a>
							{:else if o}
								<span class="flex items-center gap-1.5">
									{o.ownerName}
									<Badge variant="secondary">Admin</Badge>
								</span>
							{/if}
						</Table.Cell>
					{/if}
					<Table.Cell class="whitespace-nowrap">
						<span class="font-mono text-xs text-muted-foreground">{key.prefix}…</span>
						<CopyKeyButton id={key.id} copyable={key.copyable} />
					</Table.Cell>
					<Table.Cell class="text-muted-foreground" title={dateTime(key.createdAt)}>
						{relativeTime(key.createdAt)}
					</Table.Cell>
					<Table.Cell class="text-muted-foreground">{relativeTime(key.lastUsedAt)}</Table.Cell>
					<Table.Cell
						class="text-muted-foreground"
						title={key.expiresAt ? dateTime(key.expiresAt) : undefined}
					>
						{expiryLabel(key)}
					</Table.Cell>
					<Table.Cell>
						<Switch
							checked={!key.disabled}
							aria-label="Enabled"
							onCheckedChange={(v) => run(() => api.updateKey(key.id, { disabled: !v }))}
						/>
					</Table.Cell>
					<Table.Cell class="whitespace-nowrap">
						<Button variant="ghost" size="icon-sm" aria-label="Key settings" href="/keys/{key.id}">
							<SettingsIcon />
						</Button>
						<Button
							variant="ghost"
							size="icon-sm"
							aria-label="Delete key"
							onclick={() => (deleting = key)}
						>
							<Trash2Icon />
						</Button>
					</Table.Cell>
				</Table.Row>
			{/each}
		</Table.Body>
	</Table.Root>
{/if}

<AlertDialog.Root open={deleting !== null} onOpenChange={(v) => !v && (deleting = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete key {deleting?.name || deleting?.prefix}?</AlertDialog.Title>
			<AlertDialog.Description
				>Clients using this key will stop working immediately.</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				variant="destructive"
				onclick={() => {
					const key = deleting;
					deleting = null;
					if (key) run(() => api.deleteKey(key.id), 'Key deleted');
				}}
			>
				Delete
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
