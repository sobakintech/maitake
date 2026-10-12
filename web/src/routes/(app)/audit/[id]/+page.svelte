<script lang="ts">
	import { goto } from '$app/navigation';
	import { admin, errorMessage, type AuditBody } from '#lib/api.js';
	import { bytes, dateTime, duration, integer, providerName } from '#lib/format.js';
	import AuditBodyView from '#lib/components/audit-body.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Table from '#lib/components/ui/table/index.js';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import ArrowLeftIcon from '@lucide/svelte/icons/arrow-left';
	import { toast } from 'svelte-sonner';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	const e = $derived(data.entry);
	const p = $derived(data.payload);
	const names = $derived(data.names);
	let deleteOpen = $state(false);

	const keyName = $derived(
		e.keyId ? (names.key[String(e.keyId)] ?? `Deleted key #${e.keyId}`) : '—'
	);
	const userName = $derived(
		e.userId ? (names.user[String(e.userId)] ?? `Deleted user #${e.userId}`) : 'Admin key'
	);
	function accountName(id: number) {
		return id ? (names.account[String(id)] ?? `Deleted account #${id}`) : '—';
	}

	const bodies = $derived(
		(
			[
				['request', 'Request', p.request],
				['upstreamRequest', 'To provider', p.upstreamRequest],
				['upstreamResponse', 'From provider', p.upstreamResponse],
				['response', 'Response', p.response]
			] as [string, string, AuditBody | undefined][]
		).filter((b): b is [string, string, AuditBody] => !!b[2])
	);
	const headerSets = $derived(
		(
			[
				['Request headers', p.requestHeaders],
				['Response headers', p.responseHeaders],
				[
					`Provider response headers${p.upstreamStatus ? ` (${p.upstreamStatus})` : ''}`,
					p.upstreamResponseHeaders
				]
			] as [string, Record<string, string[]> | undefined][]
		).filter((h): h is [string, Record<string, string[]>] => !!h[1])
	);
	const tabs = $derived([
		...bodies.map(([id, label]) => ({ id, label })),
		...(headerSets.length ? [{ id: 'headers', label: 'Headers' }] : [])
	]);
	let tab = $state('');
	const activeTab = $derived(tabs.some((t) => t.id === tab) ? tab : (tabs[0]?.id ?? ''));

	const tier = $derived(
		e.serviceTier || e.servedTier
			? `${e.serviceTier || 'default'} asked, ${e.servedTier || 'not reported'} served`
			: ''
	);
	const target = $derived(`${p.method ?? 'POST'} ${e.endpoint}${p.query ? '?' + p.query : ''}`);

	async function remove() {
		deleteOpen = false;
		try {
			await admin.deleteAuditEntry(e.id);
			toast.success('Entry deleted');
			await goto('/audit', { invalidateAll: true });
		} catch (err) {
			toast.error(errorMessage(err));
		}
	}
</script>

<div class="space-y-6">
	<div class="space-y-2">
		<a
			href="/audit"
			class="inline-flex items-center gap-1 text-sm text-muted-foreground hover:text-foreground"
		>
			<ArrowLeftIcon class="size-4" /> Audit log
		</a>
		<div class="flex flex-wrap items-center gap-3">
			<h1 class="text-xl font-semibold tracking-tight">Request #{e.id}</h1>
			{#if e.status >= 400}
				<Badge variant="destructive">{e.status}</Badge>
			{:else}
				<Badge variant="outline">{e.status}</Badge>
			{/if}
			<Button variant="ghost" size="sm" class="ml-auto" onclick={() => (deleteOpen = true)}
				>Delete</Button
			>
		</div>
		<p class="text-sm text-muted-foreground">{dateTime(e.time)}</p>
	</div>

	{#if e.error}
		<Card.Root class="border-destructive/50">
			<Card.Content class="font-mono text-sm break-words text-destructive">{e.error}</Card.Content>
		</Card.Root>
	{/if}

	<div class="grid gap-6 lg:grid-cols-2">
		<Card.Root>
			<Card.Header><Card.Title>Request</Card.Title></Card.Header>
			<Card.Content>
				<dl class="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm">
					<dt class="text-muted-foreground">Endpoint</dt>
					<dd class="font-mono text-xs break-all">{target}{e.stream ? ' · stream' : ''}</dd>
					<dt class="text-muted-foreground">Key</dt>
					<dd>
						{#if e.keyId && names.key[String(e.keyId)]}
							<a href="/keys/{e.keyId}" class="underline underline-offset-4">{keyName}</a>
						{:else}
							{keyName}
						{/if}
					</dd>
					<dt class="text-muted-foreground">User</dt>
					<dd>
						{#if e.userId && names.user[String(e.userId)]}
							<a href="/users/{e.userId}" class="underline underline-offset-4">{userName}</a>
						{:else}
							{userName}
						{/if}
					</dd>
					<dt class="text-muted-foreground">Client</dt>
					<dd class="break-all">
						<PrivateText>{e.clientIp || '—'}</PrivateText>
						{#if e.userAgent}<span class="block text-xs text-muted-foreground">{e.userAgent}</span
							>{/if}
					</dd>
					<dt class="text-muted-foreground">Model</dt>
					<dd class="font-mono text-xs">
						{#if e.requestedModel && e.requestedModel !== e.model}
							{e.requestedModel} → {e.model}
						{:else}
							{e.model}
						{/if}
						{#if e.provider}<span class="font-sans text-muted-foreground">
								· {providerName[e.provider] ?? e.provider}</span
							>{/if}
					</dd>
					{#if tier}
						<dt class="text-muted-foreground">Tier</dt>
						<dd>{tier}</dd>
					{/if}
				</dl>
			</Card.Content>
		</Card.Root>

		<Card.Root>
			<Card.Header><Card.Title>Outcome</Card.Title></Card.Header>
			<Card.Content>
				<dl class="grid grid-cols-[max-content_1fr] gap-x-6 gap-y-2 text-sm">
					<dt class="text-muted-foreground">Account</dt>
					<dd><PrivateText>{accountName(e.accountId)}</PrivateText></dd>
					<dt class="text-muted-foreground">Duration</dt>
					<dd class="tabular-nums">
						{duration(e.durationMs)}
						{#if e.ttftMs}<span class="text-muted-foreground"
								>· first output after {duration(e.ttftMs)}</span
							>{/if}
					</dd>
					<dt class="text-muted-foreground">Input</dt>
					<dd class="tabular-nums">
						{integer(e.inputTokens)}
						<span class="text-muted-foreground"
							>· {integer(e.cacheReadTokens)} from cache, {integer(e.cacheWriteTokens)} written to it</span
						>
					</dd>
					<dt class="text-muted-foreground">Output</dt>
					<dd class="tabular-nums">
						{integer(e.outputTokens)}
						<span class="text-muted-foreground">· {integer(e.reasoningTokens)} reasoning</span>
					</dd>
					<dt class="text-muted-foreground">Stored</dt>
					<dd class="tabular-nums">{bytes(e.payloadSize)} compressed</dd>
				</dl>
			</Card.Content>
		</Card.Root>
	</div>

	{#if e.attempts.length}
		<Card.Root>
			<Card.Header>
				<Card.Title>Attempts</Card.Title>
				<Card.Description>Each account the request was sent to, in order.</Card.Description>
			</Card.Header>
			<Card.Content>
				<Table.Root>
					<Table.Header>
						<Table.Row>
							<Table.Head class="w-8">#</Table.Head>
							<Table.Head>Account</Table.Head>
							<Table.Head>Status</Table.Head>
							<Table.Head>Error</Table.Head>
							<Table.Head class="text-right">Took</Table.Head>
						</Table.Row>
					</Table.Header>
					<Table.Body>
						{#each e.attempts as a, i (i)}
							<Table.Row>
								<Table.Cell class="text-muted-foreground tabular-nums">{i + 1}</Table.Cell>
								<Table.Cell><PrivateText>{accountName(a.accountId)}</PrivateText></Table.Cell>
								<Table.Cell>
									{#if a.status === 0}
										<Badge variant="destructive">no response</Badge>
									{:else if a.status >= 400}
										<Badge variant="destructive">{a.status}</Badge>
									{:else}
										<Badge variant="outline">{a.status}</Badge>
									{/if}
								</Table.Cell>
								<Table.Cell class="max-w-md text-xs break-words whitespace-normal"
									>{a.error || '—'}</Table.Cell
								>
								<Table.Cell class="text-right text-muted-foreground tabular-nums"
									>{duration(a.durationMs)}</Table.Cell
								>
							</Table.Row>
						{/each}
					</Table.Body>
				</Table.Root>
			</Card.Content>
		</Card.Root>
	{/if}

	<Card.Root>
		<Card.Header>
			<Card.Title>Detail</Card.Title>
			{#if tabs.length === 0}
				<Card.Description>
					No headers or bodies were kept for this request. Choose what's kept in
					<a href="/settings?tab=audit" class="underline underline-offset-4">Settings</a>.
				</Card.Description>
			{/if}
		</Card.Header>
		{#if tabs.length}
			<Card.Content>
				<Tabs.Root value={activeTab} onValueChange={(v) => (tab = v)}>
					<Tabs.List>
						{#each tabs as t (t.id)}
							<Tabs.Trigger value={t.id}>{t.label}</Tabs.Trigger>
						{/each}
					</Tabs.List>
					{#each bodies as [id, , body] (id)}
						<Tabs.Content value={id} class="pt-4">
							<AuditBodyView {body} filename="request-{e.id}-{id}.txt" />
						</Tabs.Content>
					{/each}
					<Tabs.Content value="headers" class="space-y-6 pt-4">
						{#each headerSets as [label, headers] (label)}
							<div class="space-y-2">
								<p class="text-sm font-medium">{label}</p>
								<Table.Root>
									<Table.Body>
										{#each Object.entries(headers).sort( ([a], [b]) => a.localeCompare(b) ) as [name, values] (name)}
											<Table.Row>
												<Table.Cell class="w-1/3 align-top font-mono text-xs">{name}</Table.Cell>
												<Table.Cell
													class="font-mono text-xs break-all whitespace-normal {values[0] ===
													'[redacted]'
														? 'text-muted-foreground italic'
														: ''}">{values.join(', ')}</Table.Cell
												>
											</Table.Row>
										{/each}
									</Table.Body>
								</Table.Root>
							</div>
						{/each}
					</Tabs.Content>
				</Tabs.Root>
			</Card.Content>
		{/if}
	</Card.Root>
</div>

<AlertDialog.Root bind:open={deleteOpen}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete this entry?</AlertDialog.Title>
			<AlertDialog.Description
				>Only the audit record goes; usage history stays.</AlertDialog.Description
			>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action variant="destructive" onclick={remove}>Delete</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
