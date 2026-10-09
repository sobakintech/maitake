<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { admin, api, errorMessage, type KeyRules, type MappingRule } from '#lib/api.js';
	import { dateTime, relativeTime } from '#lib/format.js';
	import { Draft } from '#lib/draft.svelte.js';
	import ExpiryPicker from '#lib/components/expiry-picker.svelte';
	import CopyKeyButton from '#lib/components/copy-key-button.svelte';
	import ConnectSnippets from '#lib/components/connect-snippets.svelte';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import ModelPicker from '#lib/components/model-picker.svelte';
	import RouteTester from '#lib/components/route-tester.svelte';
	import RulesEditor from '#lib/components/rules-editor.svelte';
	import UnsavedBar from '#lib/components/unsaved-bar.svelte';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	type Form = {
		name: string;
		expiresAt: string | null;
		allowedModels: string[];
		rpmLimit: number;
		displayProfileId: string;
		rules: MappingRule[];
		ignoreGlobal: boolean;
	};

	function toForm(): Form {
		const k = data.key;
		return {
			name: k.name,
			expiresAt: k.expiresAt,
			allowedModels: [...k.allowedModels],
			rpmLimit: k.rpmLimit,
			displayProfileId: k.displayConfig ? 'custom' : String(k.displayProfileId ?? 0),
			rules: (k.modelRules?.rules ?? []).map((r) => ({ ...r, formats: r.formats ?? [] })),
			ignoreGlobal: k.modelRules?.ignoreGlobal ?? false
		};
	}

	const draft = new Draft<Form>(untrack(toForm));
	let loadedFor = untrack(() => data.key.id);
	// Remount the expiry picker after a reset so it shows the restored value.
	let pickerVersion = $state(0);
	$effect.pre(() => {
		const next = toForm();
		if (data.key.id !== loadedFor) {
			loadedFor = data.key.id;
			draft.base = next;
			draft.reset();
			pickerVersion++;
		} else {
			draft.sync(next);
		}
	});
	const form = $derived(draft.value);
	let saving = $state(false);
	let deleteOpen = $state(false);
	let rotateOpen = $state(false);
	let rotatedSecret = $state('');

	async function rotate() {
		try {
			const { secret } = await admin.rotateKey(data.key.id);
			rotatedSecret = secret;
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}

	const profileLabel = $derived(
		form.displayProfileId === 'custom'
			? 'Its own view'
			: form.displayProfileId === '0'
				? 'No quota view'
				: (data.profiles.find((p) => String(p.id) === form.displayProfileId)?.name ??
					'No quota view')
	);
	const keyRules = $derived<KeyRules>({ rules: form.rules, ignoreGlobal: form.ignoreGlobal });

	async function save() {
		const f = draft.snapshot();
		if (f.expiresAt && f.expiresAt !== data.key.expiresAt && new Date(f.expiresAt) <= new Date()) {
			toast.error('Pick an expiry in the future');
			return;
		}
		saving = true;
		try {
			await api.updateKey(data.key.id, {
				name: f.name,
				allowedModels: f.allowedModels,
				rpmLimit: Math.max(0, Number(f.rpmLimit) || 0),
				...(f.displayProfileId === 'custom'
					? {
							displayProfileId: 0,
							// Keep the existing view; a new one starts from the standard settings.
							displayConfig: data.key.displayConfig ?? data.blankView
						}
					: { displayProfileId: Number(f.displayProfileId), displayConfig: null }),
				modelRules: { rules: f.rules, ignoreGlobal: f.ignoreGlobal },
				...(f.expiresAt !== data.key.expiresAt ? { expiresAt: f.expiresAt } : {})
			});
			draft.commit();
			toast.success('Saved');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			saving = false;
		}
	}

	async function toggleDisabled(v: boolean) {
		try {
			await api.updateKey(data.key.id, { disabled: !v });
			toast.success(v ? 'Key enabled' : 'Key disabled');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}

	async function deleteKey() {
		try {
			await api.deleteKey(data.key.id);
			draft.reset();
			toast.success('Key deleted');
			await goto('/keys', { invalidateAll: true });
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}
</script>

<datalist id="key-models">
	{#each data.models as m (m.canonical)}
		<option value={m.id}></option>
		<option value={m.canonical}></option>
	{/each}
</datalist>

<div class="mx-auto max-w-4xl space-y-6">
	<div class="space-y-2">
		<a
			href="/keys"
			class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
		>
			<ArrowLeftIcon class="size-4" /> API keys
		</a>
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="text-xl font-semibold tracking-tight">{data.key.name || 'Unnamed key'}</h1>
			<code class="text-sm text-muted-foreground">{data.key.prefix}…</code>
			{#if data.key.disabled}<Badge variant="secondary">Disabled</Badge>{/if}
		</div>
		<p class="text-sm text-muted-foreground">
			Owned by
			{#if data.key.ownerType === 'user' && data.key.userId}
				<a href="/users/{data.key.userId}" class="underline underline-offset-4"
					>{data.key.ownerName}</a
				>
			{:else}
				{data.key.ownerName} (admin)
			{/if}
			· created {dateTime(data.key.createdAt)} · used {relativeTime(data.key.lastUsedAt)}
		</p>
	</div>

	<Card.Root>
		<Card.Header><Card.Title>Details</Card.Title></Card.Header>
		<Card.Content>
			<Field.Group>
				<Field.Field>
					<Field.Label>Key</Field.Label>
					<div class="flex flex-wrap items-center gap-2">
						<code class="rounded-md bg-muted px-2 py-1 text-sm">{data.key.prefix}…</code>
						<CopyKeyButton id={data.key.id} copyable={data.key.copyable} label="Copy" />
						<Button variant="outline" size="sm" onclick={() => (rotateOpen = true)}>
							<RefreshCwIcon /> Regenerate
						</Button>
					</div>
					<Field.Description>
						{data.key.copyable
							? 'Admins can copy the full key any time.'
							: "No copy of this key was stored, so it can't be copied. Regenerating gives it a new, copyable secret and keeps all its settings."}
					</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="k-name">Name</Field.Label>
					<Input id="k-name" maxlength={100} bind:value={form.name} />
				</Field.Field>
				<Field.Field>
					<Field.Label>Expires</Field.Label>
					{#key pickerVersion}
						<ExpiryPicker bind:value={form.expiresAt} />
					{/key}
				</Field.Field>
			</Field.Group>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Allowed models</Card.Title>
			<Card.Description>Requests for anything else are refused with a 403.</Card.Description>
		</Card.Header>
		<Card.Content>
			<ModelPicker bind:value={form.allowedModels} catalog={data.models} labs={data.labs} />
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header>
			<Card.Title>Model mapping</Card.Title>
			<Card.Description>
				Rules just for this key, checked before the global ones on the Models page.
			</Card.Description>
		</Card.Header>
		<Card.Content class="space-y-6">
			<Field.Field orientation="horizontal">
				<Field.Content>
					<Field.Label for="ignore-global">Ignore the global rules</Field.Label>
					<Field.Description>Only this key's rules apply.</Field.Description>
				</Field.Content>
				<Switch id="ignore-global" bind:checked={form.ignoreGlobal} />
			</Field.Field>
			<RulesEditor bind:rules={form.rules} catalog={data.models} idPrefix="key" />
			<div class="space-y-2">
				<p class="text-sm font-medium">Try it as this key</p>
				<RouteTester
					keyId={data.key.id}
					rules={keyRules}
					allowed={form.allowedModels}
					listId="key-models"
				/>
			</div>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header><Card.Title>Limits and quota page</Card.Title></Card.Header>
		<Card.Content>
			<Field.Group>
				<Field.Field>
					<Field.Label for="k-rpm">Requests per minute</Field.Label>
					<Input id="k-rpm" type="number" min={0} class="w-32" bind:value={form.rpmLimit} />
					<Field.Description>0 means no limit.</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label>Quota page view</Field.Label>
					<div class="flex flex-wrap items-center gap-2">
						<Select.Root type="single" bind:value={form.displayProfileId}>
							<Select.Trigger class="w-64">{profileLabel}</Select.Trigger>
							<Select.Content>
								<Select.Item value="0" label="No quota view">No quota view</Select.Item>
								<Select.Item value="custom" label="Its own view">Its own view</Select.Item>
								{#if data.profiles.length}
									<Select.Separator />
									<Select.Label>Profiles</Select.Label>
									{#each data.profiles as p (p.id)}
										<Select.Item value={String(p.id)} label={p.name}>{p.name}</Select.Item>
									{/each}
								{/if}
							</Select.Content>
						</Select.Root>
						{#if form.displayProfileId === 'custom' && data.key.displayConfig && !draft.dirty}
							<Button variant="outline" size="sm" href="/keys/{data.key.id}/quota-view"
								>Edit its view</Button
							>
						{:else if form.displayProfileId === 'custom'}
							<span class="text-xs text-muted-foreground">Save, then edit it.</span>
						{:else if form.displayProfileId !== '0'}
							<Button variant="ghost" size="sm" href="/quota-page/profiles/{form.displayProfileId}"
								>Edit profile</Button
							>
						{/if}
					</div>
					<Field.Description>
						What this key's holder sees on the quota page: nothing, a view set just for this key, or
						a shared profile.
					</Field.Description>
				</Field.Field>
			</Field.Group>
		</Card.Content>
	</Card.Root>

	<Card.Root>
		<Card.Header><Card.Title>Access</Card.Title></Card.Header>
		<Card.Content class="space-y-6">
			<Field.Field orientation="horizontal">
				<Field.Content>
					<Field.Label for="k-enabled">Enabled</Field.Label>
					<Field.Description>A disabled key is refused until it's turned back on.</Field.Description
					>
				</Field.Content>
				<Switch id="k-enabled" checked={!data.key.disabled} onCheckedChange={toggleDisabled} />
			</Field.Field>
			<Field.Field orientation="horizontal">
				<Field.Content>
					<Field.Label>Delete key</Field.Label>
					<Field.Description>Clients using it stop working immediately.</Field.Description>
				</Field.Content>
				<Button variant="destructive" onclick={() => (deleteOpen = true)}>Delete</Button>
			</Field.Field>
		</Card.Content>
	</Card.Root>

	<UnsavedBar
		dirty={draft.dirty}
		{saving}
		onSave={save}
		onReset={() => {
			draft.reset();
			pickerVersion++;
		}}
	/>
</div>

<AlertDialog.Root bind:open={rotateOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Regenerate this key?</AlertDialog.Title>
			<AlertDialog.Description>
				It gets a new secret and keeps all its settings. The current secret stops working
				immediately, so update every client using it.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				onclick={() => {
					rotateOpen = false;
					rotate();
				}}
			>
				Regenerate
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>

<Dialog.Root open={!!rotatedSecret} onOpenChange={(v) => !v && (rotatedSecret = '')}>
	<Dialog.Content class="sm:max-w-xl">
		<Dialog.Header>
			<Dialog.Title>New secret</Dialog.Title>
			<Dialog.Description>The old one no longer works.</Dialog.Description>
		</Dialog.Header>
		<Input
			readonly
			value={rotatedSecret}
			class="font-mono text-xs"
			onfocus={(e) => e.currentTarget.select()}
		/>
		<ConnectSnippets apiKey={rotatedSecret} />
		<Dialog.Footer>
			<Button onclick={() => (rotatedSecret = '')}>Done</Button>
		</Dialog.Footer>
	</Dialog.Content>
</Dialog.Root>

<AlertDialog.Root bind:open={deleteOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete this key?</AlertDialog.Title>
			<AlertDialog.Description
				>Clients using it will stop working immediately.</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={deleteKey}>Delete</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
