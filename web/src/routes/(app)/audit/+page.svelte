<script lang="ts">
	import { goto, invalidateAll } from '$app/navigation';
	import { page } from '$app/state';
	import { admin, errorMessage, type AuditEntry, type UsageRange } from '#lib/api.js';
	import { bytes, compact, duration, integer, shortDateTime } from '#lib/format.js';
	import PrivateText from '#lib/components/private-text.svelte';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import SearchIcon from '@lucide/svelte/icons/search';
	import { untrack } from 'svelte';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	// Pages loaded with "Load more" are appended here; a new filter starts over.
	let entries = $state<AuditEntry[]>(untrack(() => data.entries));
	let next = $state(untrack(() => data.next));
	let loading = $state(false);
	let clearOpen = $state(false);
	let search = $state(untrack(() => data.filter.q ?? ''));
	$effect.pre(() => {
		entries = data.entries;
		next = data.next;
	});

	const ranges: { value: UsageRange | 'all'; label: string }[] = [
		{ value: '1h', label: '1h' },
		{ value: '24h', label: '24h' },
		{ value: '7d', label: '7d' },
		{ value: '30d', label: '30d' },
		{ value: 'all', label: 'All' }
	];
	const statusLabels: Record<string, string> = {
		all: 'Any status',
		errors: 'Failed',
		ok: 'Successful'
	};

	function setParam(name: string, value: string | undefined) {
		const url = new URL(page.url.href);
		if (value && value !== '0' && value !== 'all') url.searchParams.set(name, value);
		else url.searchParams.delete(name);
		goto(url, { replace: true });
	}

	const f = $derived(data.filter);
	const names = $derived(data.names);
	const keyLabel = $derived(
		f.keyId ? (names.key[String(f.keyId)] ?? `Deleted key #${f.keyId}`) : 'All keys'
	);
	const userLabel = $derived(data.users.find((u) => u.id === f.userId)?.name ?? 'All users');
	const accountLabel = $derived(
		data.accounts.find((a) => a.id === f.accountId)?.label ?? 'All accounts'
	);
	const keyOptions = $derived(
		data.keys.filter((k) => !f.userId || k.userId === f.userId || k.id === f.keyId)
	);
	const statusValue = $derived(
		f.status && !(f.status in statusLabels) ? f.status : (f.status ?? 'all')
	);
	const filtered = $derived(
		!!(f.range || f.userId || f.keyId || f.accountId || f.model || f.status || f.q)
	);

	function keyName(e: AuditEntry) {
		if (!e.keyId) return '—';
		return names.key[String(e.keyId)] ?? `Deleted key #${e.keyId}`;
	}

	async function loadMore() {
		loading = true;
		try {
			const res = await admin.audit({ ...data.filter, before: next });
			entries = [...entries, ...res.entries];
			next = res.next;
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			loading = false;
		}
	}

	async function clearMatching() {
		clearOpen = false;
		try {
			await admin.clearAudit(data.filter);
			toast.success('Deleted');
			await invalidateAll();
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-start justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">Audit log</h1>
			<p class="text-sm text-muted-foreground">
				Requests kept in full: headers, bodies and every account tried.
				{integer(data.stats.entries)}
				{data.stats.entries === 1 ? 'entry' : 'entries'}, {bytes(data.stats.bytes)} stored.
			</p>
		</div>
		<div class="flex gap-2">
			<Button variant="outline" href="/settings?tab=audit">Settings</Button>
			<Button
				variant="destructive"
				disabled={entries.length === 0}
				onclick={() => (clearOpen = true)}
			>
				{filtered ? 'Delete matching' : 'Delete all'}
			</Button>
		</div>
	</div>

	{#if !data.auditEnabled}
		<Card.Root>
			<Card.Content class="text-sm text-muted-foreground">
				Recording is off. Turn it on in
				<a href="/settings?tab=audit" class="underline underline-offset-4">Settings</a>, or for a
				single key on its page.
			</Card.Content>
		</Card.Root>
	{/if}

	<div class="flex flex-wrap items-center gap-3">
		<ToggleGroup.Root
			type="single"
			variant="outline"
			value={f.range ?? 'all'}
			onValueChange={(v) => v && setParam('range', v)}
		>
			{#each ranges as r (r.value)}
				<ToggleGroup.Item value={r.value} class="px-3">{r.label}</ToggleGroup.Item>
			{/each}
		</ToggleGroup.Root>
		<Select.Root
			type="single"
			value={String(f.userId ?? 0)}
			onValueChange={(v) => setParam('userId', v)}
		>
			<Select.Trigger class="w-40"><span class="truncate">{userLabel}</span></Select.Trigger>
			<Select.Content>
				<Select.Item value="0" label="All users">All users</Select.Item>
				{#each data.users as u (u.id)}
					<Select.Item value={String(u.id)} label={u.name}>{u.name}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<Select.Root
			type="single"
			value={String(f.keyId ?? 0)}
			onValueChange={(v) => setParam('keyId', v)}
		>
			<Select.Trigger class="w-56"><span class="truncate">{keyLabel}</span></Select.Trigger>
			<Select.Content>
				<Select.Item value="0" label="All keys">All keys</Select.Item>
				{#each keyOptions as k (k.id)}
					{@const label = `${k.name || k.prefix + '…'} (${k.ownerName})`}
					<Select.Item value={String(k.id)} {label}>{label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<Select.Root
			type="single"
			value={String(f.accountId ?? 0)}
			onValueChange={(v) => setParam('accountId', v)}
		>
			<Select.Trigger class="w-44"
				><span class="truncate"><PrivateText>{accountLabel}</PrivateText></span></Select.Trigger
			>
			<Select.Content>
				<Select.Item value="0" label="All accounts">All accounts</Select.Item>
				{#each data.accounts as a (a.id)}
					<Select.Item value={String(a.id)} label={a.label}>{a.label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<Select.Root type="single" value={statusValue} onValueChange={(v) => setParam('status', v)}>
			<Select.Trigger class="w-36"
				>{statusLabels[statusValue] ?? `Status ${statusValue}`}</Select.Trigger
			>
			<Select.Content>
				{#each Object.entries(statusLabels) as [v, label] (v)}
					<Select.Item value={v} {label}>{label}</Select.Item>
				{/each}
			</Select.Content>
		</Select.Root>
		<form
			class="relative min-w-48 flex-1"
			onsubmit={(e) => {
				e.preventDefault();
				setParam('q', search.trim());
			}}
		>
			<SearchIcon
				class="pointer-events-none absolute top-1/2 left-2.5 size-4 -translate-y-1/2 text-muted-foreground"
			/>
			<Input
				bind:value={search}
				class="pl-8"
				placeholder="Model, error, endpoint, IP, user agent…"
				onblur={() => search.trim() !== (f.q ?? '') && setParam('q', search.trim())}
			/>
		</form>
	</div>
	{#if f.model}
		<p class="text-sm text-muted-foreground">
			Model <code>{f.model}</code> only ·
			<button class="underline underline-offset-4" onclick={() => setParam('model', undefined)}
				>show all</button
			>
		</p>
	{/if}

	<Card.Root>
		<Card.Content>
			{#if entries.length === 0}
				<p class="py-6 text-center text-sm text-muted-foreground">
					{filtered ? 'Nothing matches these filters.' : 'Nothing recorded yet.'}
				</p>
			{:else}
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head>Time</Table.Head>
							<Table.Head>Key</Table.Head>
							<Table.Head>Model</Table.Head>
							<Table.Head>Endpoint</Table.Head>
							<Table.Head>Account</Table.Head>
							<Table.Head>Status</Table.Head>
							<Table.Head class="text-right">In</Table.Head>
							<Table.Head class="text-right">Out</Table.Head>
							<Table.Head class="text-right">Duration</Table.Head>
							<Table.Head class="text-right">Size</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each entries as e (e.id)}
							<Table.Row class="cursor-pointer" onclick={() => goto(`/audit/${e.id}`)}>
								<Table.Cell class="whitespace-nowrap">
									<a href="/audit/{e.id}" class="text-muted-foreground hover:text-foreground"
										>{shortDateTime(e.time)}</a
									>
								</Table.Cell>
								<Table.Cell class="max-w-48 truncate">{keyName(e)}</Table.Cell>
								<Table.Cell class="max-w-56 truncate font-mono text-xs">
									{#if e.requestedModel && e.requestedModel !== e.model}
										<span class="text-muted-foreground" title="Requested {e.requestedModel}"
											>{e.requestedModel} →</span
										>
									{/if}
									{e.model}
								</Table.Cell>
								<Table.Cell class="font-mono text-xs text-muted-foreground"
									>{e.endpoint}{e.stream ? ' · stream' : ''}</Table.Cell
								>
								<Table.Cell class="max-w-36 truncate">
									<PrivateText>{names.account[String(e.accountId)] ?? '—'}</PrivateText>
									{#if e.attempts.length > 1}
										<span class="text-xs text-muted-foreground">({e.attempts.length} tries)</span>
									{/if}
								</Table.Cell>
								<Table.Cell>
									{#if e.status >= 400}
										<Badge variant="destructive" title={e.error}>{e.status}</Badge>
									{:else}
										<Badge variant="outline">{e.status}</Badge>
									{/if}
								</Table.Cell>
								<Table.Cell class="text-right tabular-nums"
									>{compact(e.inputTokens + e.cacheReadTokens + e.cacheWriteTokens)}</Table.Cell
								>
								<Table.Cell class="text-right tabular-nums">{compact(e.outputTokens)}</Table.Cell>
								<Table.Cell class="text-right text-muted-foreground tabular-nums"
									>{duration(e.durationMs)}</Table.Cell
								>
								<Table.Cell class="text-right text-muted-foreground tabular-nums"
									>{bytes(e.payloadSize)}</Table.Cell
								>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
				{#if next}
					<div class="flex justify-center pt-4">
						<Button variant="outline" disabled={loading} onclick={loadMore}>Load more</Button>
					</div>
				{/if}
			{/if}
		</Card.Content>
	</Card.Root>
</div>

<AlertDialog.Root bind:open={clearOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title
				>{filtered ? 'Delete every matching entry?' : 'Delete every entry?'}</AlertDialog.Title
			>
			<AlertDialog.Description>
				{filtered
					? 'Everything matching the current filters is removed, including entries not loaded on this page.'
					: 'The whole audit log is emptied.'} Usage history isn't affected.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={clearMatching}>Delete</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
