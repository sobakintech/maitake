<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account, type Provider } from '#lib/api.js';
	import { countdown, providerName, relativeTime, shortDateTime } from '#lib/format.js';
	import AccountQuota from '#lib/components/account-quota.svelte';
	import PrivateText from '#lib/components/private-text.svelte';
	import * as AlertDialog from '#lib/components/ui/alert-dialog/index.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Card from '#lib/components/ui/card/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as DropdownMenu from '#lib/components/ui/dropdown-menu/index.js';
	import * as Empty from '#lib/components/ui/empty/index.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import * as Tabs from '#lib/components/ui/tabs/index.js';
	import * as ToggleGroup from '#lib/components/ui/toggle-group/index.js';
	import { pref } from '#lib/pref.svelte.js';
	import { goto } from '$app/navigation';
	import { page } from '$app/state';
	import AlarmClockIcon from '@lucide/svelte/icons/alarm-clock';
	import AlarmClockOffIcon from '@lucide/svelte/icons/alarm-clock-off';
	import EllipsisIcon from '@lucide/svelte/icons/ellipsis';
	import LayoutGridIcon from '@lucide/svelte/icons/layout-grid';
	import LoaderIcon from '@lucide/svelte/icons/loader-2';
	import Rows3Icon from '@lucide/svelte/icons/rows-3';
	import TimerResetIcon from '@lucide/svelte/icons/timer-reset';
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

	type View = 'rows' | 'cards';
	const viewPref = pref<View>('maitake:accounts-view', ['rows', 'cards']);
	const view = $derived(viewPref.get('cards'));

	function selectTab(v: string) {
		const url = new URL(page.url.href);
		if (v === 'all') url.searchParams.delete('provider');
		else url.searchParams.set('provider', v);
		goto(url, { replace: true, reset: false });
	}

	let bankedId = $state<number | null>(null);
	const bankedAccount = $derived(data.accounts.find((a) => a.id === bankedId) ?? null);

	function nextExpiry(a: Account) {
		const times = (a.quota?.banked?.credits ?? [])
			.map((c) => c.expiresAt)
			.filter(Boolean) as string[];
		return times.sort((x, y) => new Date(x).getTime() - new Date(y).getTime())[0];
	}

	function extraText(a: Account) {
		const x = a.quota?.extra;
		if (!x || !(x.enabled || x.balance) || (!x.unlimited && Number(x.balance ?? 1) === 0))
			return '';
		const what = a.provider === 'claude' ? 'Extra usage' : 'Credits';
		const value = x.unlimited
			? 'unlimited'
			: x.balance
				? x.balance
				: x.usedPercent !== undefined
					? `${Math.round(x.usedPercent)}% used`
					: 'enabled';
		return `${what}: ${value}`;
	}

	let primerOpen = $state(false);
	let primerAccount = $state<Account | null>(null);
	function openPrimer(a: Account) {
		primerAccount = a;
		primerOpen = true;
	}

	function primerTitle(a: Account) {
		if (a.primerBusy) return 'Session primer: starting a session';
		if (!a.primerEffective.enabled) return 'Session primer off';
		if (a.disabled) return 'Session primer paused while the account is disabled';
		return a.primerNext
			? `Session primer on · resets ${shortDateTime(a.primerNext.readyAt)}`
			: 'Session primer on · nothing planned';
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
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			refreshingIds[a.id] = false;
		}
	}

	async function refreshAll() {
		refreshing = true;
		try {
			const { failed } = await admin.refreshAll();
			await invalidateAll();
			if (failed.length > 0) {
				toast.error(
					`${failed.length} ${failed.length === 1 ? 'account' : 'accounts'} failed to refresh`,
					{
						description: failed.map((f) => `${f.label}: ${f.error}`).join('; ')
					}
				);
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
		<div class="flex flex-wrap items-center justify-between gap-3">
			<Tabs.List>
				{#each tabs as t (t)}
					<Tabs.Trigger value={t} class="gap-2 px-3">
						{#if t !== 'all'}<ProviderLogo provider={t} />{/if}
						{t === 'all' ? 'All' : providerName[t]}
						<span class="text-xs text-muted-foreground tabular-nums">{byTab[t].length}</span>
					</Tabs.Trigger>
				{/each}
			</Tabs.List>
			<ToggleGroup.Root
				type="single"
				variant="outline"
				size="sm"
				value={view}
				onValueChange={(v) => v && viewPref.set(v as View)}
			>
				<ToggleGroup.Item value="cards" class="px-2.5" aria-label="Cards" title="Cards">
					<LayoutGridIcon />
				</ToggleGroup.Item>
				<ToggleGroup.Item value="rows" class="px-2.5" aria-label="Rows" title="Rows">
					<Rows3Icon />
				</ToggleGroup.Item>
			</ToggleGroup.Root>
		</div>
		{#each tabs as p (p)}
			<Tabs.Content value={p} class="space-y-6 pt-4">
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
				{#each (p === 'all' ? providers : [p]).filter((g) => byTab[g].length) as g (g)}
					<section class="space-y-2">
						{#if p === 'all'}
							<h2 class="flex items-center gap-2 text-sm font-medium">
								<ProviderLogo provider={g} />
								{providerName[g]}
								<span class="text-xs text-muted-foreground tabular-nums">{byTab[g].length}</span>
							</h2>
						{/if}
						{#if view === 'rows'}
							<div class="divide-y overflow-hidden rounded-xl bg-card ring-1 ring-foreground/10">
								{#each byTab[g] as a (a.id)}
									{@render row(a)}
								{/each}
							</div>
						{:else}
							<div class="columns-sm gap-3 [&>*]:mb-3 [&>*]:break-inside-avoid">
								{#each byTab[g] as a (a.id)}
									{@render card(a)}
								{/each}
							</div>
						{/if}
					</section>
				{/each}
			</Tabs.Content>
		{/each}
	</Tabs.Root>
</div>

{#snippet row(a: Account)}
	{@const st = status(a)}
	{@const email = a.email || a.externalId}
	{@const cooldowns = Object.entries(a.modelCooldowns ?? {})}
	<div
		class="flex flex-wrap items-center gap-x-6 gap-y-3 px-4 py-3 text-sm {a.disabled
			? 'opacity-60'
			: ''}"
	>
		<div class="w-full min-w-0 sm:w-56 sm:shrink-0">
			<p class="flex min-w-0 items-center gap-2 font-medium">
				<PrivateText class="truncate">{a.label}</PrivateText>
				{#if a.quota?.plan || a.plan}<Badge variant="outline">{a.quota?.plan || a.plan}</Badge>{/if}
			</p>
			{#if email && email !== a.label}
				<p class="truncate text-xs text-muted-foreground"><PrivateText>{email}</PrivateText></p>
			{/if}
			<div class="mt-0.5 space-y-0.5">
				<SubscriptionLine account={a} />
				{#if a.quota?.banked?.available}
					{@const n = a.quota.banked.available}
					{@const expires = nextExpiry(a)}
					<button
						type="button"
						class="flex max-w-full items-center gap-1.5 text-xs text-muted-foreground hover:text-foreground"
						onclick={() => (bankedId = a.id)}
					>
						<TimerResetIcon class="size-3.5 shrink-0" />
						<span class="truncate underline-offset-2 hover:underline">
							{n} banked reset{n === 1 ? '' : 's'}{#if expires}
								&nbsp;· <span
									class={new Date(expires).getTime() - Date.now() < 3 * 86_400_000
										? 'text-destructive'
										: ''}>expires in {countdown(expires)}</span
								>{/if}
						</span>
					</button>
				{/if}
			</div>
		</div>
		<div class="min-w-0 flex-1">
			<AccountQuota quota={a.quota} compact />
		</div>
		<div class="ml-auto flex shrink-0 items-center gap-1">
			{#if st}
				<Badge
					variant={st.variant}
					class="mr-1"
					title={a.status !== 'ok' ? a.statusMessage || undefined : undefined}>{st.text}</Badge
				>
			{/if}
			{#if a.hasWebSession && a.subscription?.webExpired}
				<Button
					size="xs"
					variant="outline"
					class="mr-1"
					title="The claude.ai session expired, so the renewal date can't be read"
					onclick={() => edit(a, 'session')}
				>
					<TriangleAlertIcon class="text-destructive" /> Reconnect
				</Button>
			{/if}
			{#if extraText(a)}
				<Badge variant="outline" class="mr-1">{extraText(a)}</Badge>
			{/if}
			{#if cooldowns.length > 0}
				<Badge
					variant="secondary"
					class="mr-1"
					title={cooldowns.map(([m, until]) => `${m}: ${countdown(until)}`).join('\n')}
				>
					{cooldowns.length}
					{cooldowns.length === 1 ? 'model' : 'models'} limited
				</Badge>
			{/if}
			<Button
				size="icon-sm"
				variant="ghost"
				class={a.primerEffective.enabled ? '' : 'text-muted-foreground'}
				aria-label="Session primer"
				title={primerTitle(a)}
				onclick={() => openPrimer(a)}
			>
				{#if a.primerBusy}
					<LoaderIcon class="animate-spin" />
				{:else if a.primerEffective.enabled}
					<AlarmClockIcon />
				{:else}
					<AlarmClockOffIcon />
				{/if}
			</Button>
			{@render refresh(a)}
			{@render actions(a)}
		</div>
	</div>
{/snippet}

{#snippet refresh(a: Account)}
	<Button
		size="icon-sm"
		variant="ghost"
		class="text-muted-foreground"
		aria-label="Refresh"
		title={a.quotaUpdatedAt
			? `Refresh · limits checked ${relativeTime(a.quotaUpdatedAt)}`
			: 'Refresh · limits not checked yet'}
		disabled={refreshingIds[a.id]}
		onclick={() => refreshOne(a)}
	>
		<RefreshCwIcon class={refreshingIds[a.id] ? 'animate-spin' : ''} />
	</Button>
{/snippet}

{#snippet actions(a: Account)}
	<DropdownMenu.Root>
		<DropdownMenu.Trigger>
			{#snippet child({ props })}
				<Button {...props} variant="ghost" size="icon-sm" aria-label="Account actions">
					<EllipsisIcon />
				</Button>
			{/snippet}
		</DropdownMenu.Trigger>
		<DropdownMenu.Content align="end">
			<DropdownMenu.Item onSelect={() => edit(a)}>Edit</DropdownMenu.Item>
			{#if !a.disabled && !a.primerBusy && !sessionRunning(a)}
				<DropdownMenu.Item onSelect={() => primeNow(a)}>Start a session</DropdownMenu.Item>
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
{/snippet}

{#snippet card(a: Account)}
	{@const st = status(a)}
	<Card.Root size="sm" class={a.disabled ? 'opacity-60' : ''}>
		<Card.Header>
			<Card.Title class="flex flex-wrap items-center gap-2">
				<ProviderLogo provider={a.provider} />
				<PrivateText class="truncate">{a.label}</PrivateText>
				{#if a.quota?.plan || a.plan}<Badge variant="outline">{a.quota?.plan || a.plan}</Badge>{/if}
				{#if st}<Badge variant={st.variant}>{st.text}</Badge>{/if}
			</Card.Title>
			{@const email = a.email || a.externalId}
			{@const extra = [
				a.alias && `shown as “${a.alias}”`,
				a.priority !== 0 && `priority ${a.priority}`
			].filter(Boolean)}
			{#if (email && email !== a.label) || extra.length}
				<Card.Description class="truncate text-xs">
					{#if email && email !== a.label}<PrivateText>{email}</PrivateText>{extra.length
							? ' · '
							: ''}{/if}{extra.join(' · ')}
				</Card.Description>
			{/if}
			<SubscriptionLine account={a} />
			<Card.Action class="-mt-1 -mr-1.5 flex">
				{@render refresh(a)}
				{@render actions(a)}
			</Card.Action>
		</Card.Header>
		<Card.Content class="space-y-3">
			{#if a.hasWebSession && a.subscription?.webExpired}
				<div
					class="flex items-start gap-2 rounded-lg border border-destructive/30 bg-destructive/5 px-3 py-1.5 text-xs"
				>
					<TriangleAlertIcon class="mt-px size-3.5 shrink-0 text-destructive" />
					<p class="flex-1">
						The claude.ai session expired, so the renewal date can't be read. Connect a new one.
					</p>
					<Button size="xs" variant="outline" class="shrink-0" onclick={() => edit(a, 'session')}
						>Reconnect</Button
					>
				</div>
			{/if}
			{#if a.statusMessage && a.status !== 'ok'}
				<p class="rounded-md bg-muted/60 px-3 py-1.5 text-xs text-muted-foreground">
					{a.statusMessage}
				</p>
			{/if}
			{#if a.modelCooldowns && Object.keys(a.modelCooldowns).length > 0}
				<div class="space-y-1 rounded-md bg-muted/60 px-3 py-1.5 text-xs">
					<p class="text-muted-foreground">Rate limited for some models only:</p>
					{#each Object.entries(a.modelCooldowns) as [model, until] (model)}
						<p class="flex justify-between gap-3">
							<span class="font-mono">{model}</span>
							<span class="text-muted-foreground tabular-nums">{countdown(until)}</span>
						</p>
					{/each}
				</div>
			{/if}
			<AccountQuota quota={a.quota} dense />
			<PrimerBox account={a} onOpen={() => openPrimer(a)} />
			{#if a.quota?.banked}
				<BankedResets account={a} />
			{/if}
			{@const extra = extraText(a)}
			{#if extra}
				<p class="text-xs text-muted-foreground">{extra}</p>
			{/if}
		</Card.Content>
	</Card.Root>
{/snippet}

<AddAccountDialog bind:open={addOpen} provider={tab === 'all' ? 'claude' : tab} />
<EditAccountDialog
	account={editing}
	bind:open={editOpen}
	focus={editFocus}
	defaultOn={data.settings.pool.redeem.enabled}
/>

<Dialog.Root open={bankedAccount !== null} onOpenChange={(v) => !v && (bankedId = null)}>
	<Dialog.Content class="sm:max-w-md">
		<Dialog.Header>
			<Dialog.Title>Banked resets</Dialog.Title>
			<Dialog.Description><PrivateText>{bankedAccount?.label}</PrivateText></Dialog.Description>
		</Dialog.Header>
		{#if bankedAccount}<BankedResets account={bankedAccount} />{/if}
	</Dialog.Content>
</Dialog.Root>

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
