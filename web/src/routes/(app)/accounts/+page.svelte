<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account, type Provider } from '#lib/api.js';
	import { countdown, providerName, relativeTime } from '#lib/format.js';
	import AccountQuota from '#lib/components/account-quota.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import PlusIcon from '@lucide/svelte/icons/plus';
	import RefreshCwIcon from '@lucide/svelte/icons/refresh-cw';
	import { onMount } from 'svelte';
	import { toast } from 'svelte-sonner';
	import AddAccountDialog from './add-account-dialog.svelte';
	import EditAccountDialog from './edit-account-dialog.svelte';
	import BankedResets from './banked-resets.svelte';
	import SubscriptionLine from './subscription-line.svelte';
	import PrimerBox from './primer-box.svelte';
	import PrimerDialog from './primer-dialog.svelte';
	import { primeNow } from '#lib/prime-now.js';
	import ServerIcon from '@lucide/svelte/icons/server';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import type { PageProps } from './$types';

	let { data }: PageProps = $props();

	let addOpen = $state(false);
	let editOpen = $state(false);
	let editing = $state<Account | null>(null);
	let deleting = $state<Account | null>(null);

	type Tab = 'all' | Provider;
	const providers: Provider[] = ['claude', 'codex'];
	const tabs: Tab[] = ['all', ...providers];
	const tab = $derived<Tab>(
		(providers as string[]).includes(page.url.searchParams.get('provider') ?? '')
			? (page.url.searchParams.get('provider') as Provider)
			: 'all'
	);
	const byTab = $derived({
		all: data.accounts,
		claude: data.accounts.filter((a) => a.provider === 'claude'),
		codex: data.accounts.filter((a) => a.provider === 'codex')
	} as Record<Tab, Account[]>);

	function selectTab(v: string) {
		const url = new URL(page.url.href);
		if (v === 'all') url.searchParams.delete('provider');
		else url.searchParams.set('provider', v);
		goto(url, { replace: true, reset: false });
	}

	let primerOpen = $state(false);
	let primerAccount = $state<Account | null>(null);
	function openPrimer(a: Account) {
		primerAccount = a;
		primerOpen = true;
	}

	const sessionRunning = (a: Account) => {
		const w = a.quota?.windows.find((w) => w.kind === 'session');
		return !!w?.resetsAt && new Date(w.resetsAt) > new Date();
	};

	let editFocus = $state<'session' | undefined>();
	function edit(a: Account, focus?: 'session') {
		editing = a;
		editFocus = focus;
		editOpen = true;
	}

	// Keep quota bars and countdowns current while the page is open.
	onMount(() => {
		const t = setInterval(() => invalidateAll(), 30_000);
		return () => clearInterval(t);
	});

	let refreshing = $state(false);
	let refreshingIds = $state<Record<number, boolean>>({});

	async function refreshOne(a: Account) {
		refreshingIds[a.id] = true;
		try {
			await admin.refreshAccount(a.id);
			await invalidateAll();
			toast.success(`Refreshed ${a.label}`);
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			refreshingIds[a.id] = false;
		}
	}

	async function refreshAll() {
		refreshing = true;
		try {
			const { refreshed, failed } = await admin.refreshAll();
			await invalidateAll();
			if (failed.length === 0) {
				toast.success(`Refreshed ${refreshed} ${refreshed === 1 ? 'account' : 'accounts'}`);
			} else {
				toast.warning(`Refreshed ${refreshed}, ${failed.length} failed`, {
					description: failed.map((f) => `${f.label}: ${f.error}`).join('; ')
				});
			}
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			refreshing = false;
		}
	}

	async function act(fn: () => Promise<unknown>, ok?: string) {
		try {
			await fn();
			if (ok) toast.success(ok);
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}

	const emptyTitle: Record<Tab, string> = {
		all: 'No accounts yet',
		claude: 'No Claude accounts yet',
		codex: 'No Codex accounts yet'
	};
	const emptyText: Record<Tab, string> = {
		all: 'Add a Claude or ChatGPT subscription to start serving requests.',
		claude: 'Add a Claude Pro or Max subscription to serve Claude models.',
		codex: 'Add a ChatGPT subscription to serve Codex models.'
	};

	// Only states that need attention get a badge; a working account needs none.
	function status(
		a: Account
	): { text: string; variant: 'outline' | 'secondary' | 'destructive' } | null {
		if (a.disabled) return { text: 'Disabled', variant: 'secondary' };
		switch (a.status) {
			case 'cooldown':
				return {
					text: a.cooldownUntil ? `Cooling down · ${countdown(a.cooldownUntil)}` : 'Cooling down',
					variant: 'secondary'
				};
			case 'error':
				return { text: 'Error', variant: 'destructive' };
			case 'reauth':
				return { text: 'Needs login', variant: 'destructive' };
		}
		return null;
	}
</script>

<div class="space-y-6">
	<div class="flex flex-wrap items-center justify-between gap-4">
		<div>
			<h1 class="text-xl font-semibold tracking-tight">Accounts</h1>
			<p class="text-sm text-muted-foreground">
				Claude and ChatGPT subscriptions that serve requests.
			</p>
		</div>
		<div class="flex gap-2">
			{#if data.accounts.length > 0}
				<Button variant="outline" disabled={refreshing} onclick={refreshAll}>
					<RefreshCwIcon class={refreshing ? 'animate-spin' : ''} /> Refresh all
				</Button>
			{/if}
			<Button onclick={() => (addOpen = true)}>
				<PlusIcon /> Add account
			</Button>
		</div>
	</div>

	<Tabs.Root value={tab} onValueChange={selectTab}>
		<Tabs.List>
			{#each tabs as t (t)}
				<Tabs.Trigger value={t} class="gap-2 px-3">
					{#if t !== 'all'}<ProviderLogo provider={t} />{/if}
					{t === 'all' ? 'All' : providerName[t]}
					<span class="text-xs text-muted-foreground tabular-nums">{byTab[t].length}</span>
				</Tabs.Trigger>
			{/each}
		</Tabs.List>
		{#each tabs as p (p)}
			<Tabs.Content value={p} class="pt-4">
				{#if byTab[p].length === 0}
					<Empty.Root class="border">
						<Empty.Header>
							<Empty.Media variant="icon">
								{#if p === 'all'}<ServerIcon />{:else}<ProviderLogo
										provider={p}
										class="size-6"
									/>{/if}
							</Empty.Media>
							<Empty.Title>{emptyTitle[p]}</Empty.Title>
							<Empty.Description>{emptyText[p]}</Empty.Description>
						</Empty.Header>
						<Empty.Content>
							<Button onclick={() => (addOpen = true)}>
								<PlusIcon /> Add account
							</Button>
						</Empty.Content>
					</Empty.Root>
				{/if}
				<div class="gap-4 lg:columns-2 [&>*]:mb-4 [&>*]:break-inside-avoid">
					{#each byTab[p] as a (a.id)}
						{@const st = status(a)}
						<Card.Root class={a.disabled ? 'opacity-60' : ''}>
							<Card.Header>
								<Card.Title class="flex flex-wrap items-center gap-2">
									<ProviderLogo provider={a.provider} />
									<PrivateText class="truncate">{a.label}</PrivateText>
									{#if a.quota?.plan || a.plan}<Badge variant="outline"
											>{a.quota?.plan || a.plan}</Badge
										>{/if}
									{#if st}<Badge variant={st.variant}>{st.text}</Badge>{/if}
								</Card.Title>
								{@const email = a.email || a.externalId}
								{#if (email && email !== a.label) || a.alias || a.priority !== 0}
									<Card.Description class="truncate">
										{#if email && email !== a.label}<PrivateText>{email}</PrivateText
											>{/if}{#if a.alias}
											{email && email !== a.label ? ' · ' : ''}shown as “{a.alias}”{/if}{#if a.priority !== 0}
											{(email && email !== a.label) || a.alias ? ' · ' : ''}priority {a.priority}{/if}
									</Card.Description>
								{/if}
								<SubscriptionLine account={a} />
								<Card.Action>
									<DropdownMenu.Root>
										<DropdownMenu.Trigger>
											{#snippet child({ props })}
												<Button
													{...props}
													variant="ghost"
													size="icon-sm"
													aria-label="Account actions"
												>
													<EllipsisIcon />
												</Button>
											{/snippet}
										</DropdownMenu.Trigger>
										<DropdownMenu.Content align="end">
											<DropdownMenu.Item onSelect={() => edit(a)}>Edit</DropdownMenu.Item>
											{#if !a.disabled && !a.primerBusy && !sessionRunning(a)}
												<DropdownMenu.Item onSelect={() => primeNow(a)}>
													Start a session
												</DropdownMenu.Item>
											{/if}
											<DropdownMenu.Item
												onSelect={() =>
													act(
														() => admin.updateAccount(a.id, { disabled: !a.disabled }),
														a.disabled ? 'Enabled' : 'Disabled'
													)}
											>
												{a.disabled ? 'Enable' : 'Disable'}
											</DropdownMenu.Item>
											<DropdownMenu.Separator />
											<DropdownMenu.Item variant="destructive" onSelect={() => (deleting = a)}>
												Delete
											</DropdownMenu.Item>
										</DropdownMenu.Content>
									</DropdownMenu.Root>
								</Card.Action>
							</Card.Header>
							<Card.Content class="space-y-4">
								{#if a.hasWebSession && a.subscription?.webExpired}
									<div
										class="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-2 text-sm"
									>
										<TriangleAlertIcon class="mt-0.5 size-4 shrink-0 text-destructive" />
										<p class="flex-1">
											The claude.ai session expired, so the renewal date can't be read. Connect a
											new one.
										</p>
										<Button
											size="xs"
											variant="outline"
											class="shrink-0"
											onclick={() => edit(a, 'session')}>Reconnect</Button
										>
									</div>
								{/if}
								{#if a.statusMessage && a.status !== 'ok'}
									<p class="rounded-md bg-muted/60 px-3 py-2 text-xs text-muted-foreground">
										{a.statusMessage}
									</p>
								{/if}
								{#if a.modelCooldowns && Object.keys(a.modelCooldowns).length > 0}
									<div class="space-y-1 rounded-md bg-muted/60 px-3 py-2 text-xs">
										<p class="text-muted-foreground">Rate limited for some models only:</p>
										{#each Object.entries(a.modelCooldowns) as [model, until] (model)}
											<p class="flex justify-between gap-3">
												<span class="font-mono">{model}</span>
												<span class="text-muted-foreground tabular-nums">{countdown(until)}</span>
											</p>
										{/each}
									</div>
								{/if}
								<AccountQuota quota={a.quota} />
								<PrimerBox account={a} onOpen={() => openPrimer(a)} />
								{#if a.quota?.banked}
									<BankedResets account={a} />
								{/if}
								{#if a.quota?.extra && (a.quota.extra.enabled || a.quota.extra.balance) && (a.quota.extra.unlimited || Number(a.quota.extra.balance ?? 1) !== 0)}
									<p class="text-xs text-muted-foreground">
										{a.provider === 'claude' ? 'Extra usage' : 'Credits'}:
										{a.quota.extra.unlimited
											? 'unlimited'
											: a.quota.extra.balance
												? a.quota.extra.balance
												: a.quota.extra.usedPercent !== undefined
													? `${Math.round(a.quota.extra.usedPercent)}% used`
													: 'enabled'}
									</p>
								{/if}
							</Card.Content>
							<Card.Footer class="flex items-center gap-3 text-xs text-muted-foreground">
								<span class="flex-1">
									{a.lastUsedAt ? `Last used ${relativeTime(a.lastUsedAt)}` : 'Not used yet'}
								</span>
								<Button
									variant="ghost"
									size="sm"
									class="-my-1 -mr-2 shrink-0 text-muted-foreground"
									title={a.quotaUpdatedAt
										? `Limits checked ${relativeTime(a.quotaUpdatedAt)}`
										: 'Limits not checked yet'}
									disabled={refreshingIds[a.id]}
									onclick={() => refreshOne(a)}
								>
									<RefreshCwIcon class={refreshingIds[a.id] ? 'animate-spin' : ''} /> Refresh
								</Button>
							</Card.Footer>
						</Card.Root>
					{/each}
				</div>
			</Tabs.Content>
		{/each}
	</Tabs.Root>
</div>

<AddAccountDialog bind:open={addOpen} provider={tab === 'all' ? 'claude' : tab} />
<EditAccountDialog
	account={editing}
	bind:open={editOpen}
	focus={editFocus}
	defaultOn={data.settings.pool.redeem.enabled}
/>

<PrimerDialog
	account={primerAccount}
	bind:open={primerOpen}
	shared={data.settings.pool.primer}
	catalog={data.models}
	serverTimezone={data.serverTimezone}
/>

<AlertDialog.Root open={deleting !== null} onOpenChange={(v) => !v && (deleting = null)}>
	<AlertDialog.Content>
		<AlertDialog.Header>
			<AlertDialog.Title>Delete {deleting?.label}?</AlertDialog.Title>
			<AlertDialog.Description>
				Its tokens are removed from maitake. The subscription itself isn't affected, and you can log
				in again later.
			</AlertDialog.Description>
		</AlertDialog.Header>
		<AlertDialog.Footer>
			<AlertDialog.Cancel>Cancel</AlertDialog.Cancel>
			<AlertDialog.Action
				variant="destructive"
				onclick={() => {
					const a = deleting;
					deleting = null;
					if (a) act(() => admin.deleteAccount(a.id), 'Account deleted');
				}}
			>
				Delete
			</AlertDialog.Action>
		</AlertDialog.Footer>
	</AlertDialog.Content>
</AlertDialog.Root>
