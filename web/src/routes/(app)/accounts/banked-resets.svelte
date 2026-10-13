<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account, type BankedCredit } from '#lib/api.js';
	import { countdown, relativeTime, shortDateTime } from '#lib/format.js';
	import { Badge } from '#lib/components/ui/badge/index.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Label } from '#lib/components/ui/label/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import TimerResetIcon from '@lucide/svelte/icons/timer-reset';
	import LoaderIcon from '@lucide/svelte/icons/loader-2';
	import { toast } from 'svelte-sonner';

	// Every banked reset of an account, inline, each usable on its own.
	let { account }: { account: Account } = $props();

	// Which reset is waiting for a second click to confirm, and which one is being spent.
	let confirming = $state<string | null>(null);
	let using = $state<string | null>(null);

	const banked = $derived(account.quota?.banked);
	const credits = $derived(banked?.credits ?? []);
	const unlisted = $derived(
		Math.max(0, (banked?.available ?? 0) - credits.reduce((n, c) => n + (c.count || 1), 0))
	);
	// Only Codex accounts spend resets on their own.
	const autoUse = $derived(account.provider === 'codex');
	const cooldown = $derived(
		banked?.cooldownUntil && new Date(banked.cooldownUntil).getTime() > Date.now()
			? banked.cooldownUntil
			: null
	);

	function expiringSoon(c: BankedCredit) {
		return !!c.expiresAt && new Date(c.expiresAt).getTime() - Date.now() < 3 * 86_400_000;
	}

	const resultText: Record<string, string> = {
		reset: 'Limits reset',
		already_redeemed: 'Limits reset',
		nothing_to_reset: "Nothing needed resetting, so the reset wasn't used",
		no_credit: 'That reset is no longer available',
		cooldown: 'Resets are cooling down. Try again later',
		unconfirmed:
			"Couldn't confirm the reset. If the account is still limited in a moment, try again"
	};

	async function use(creditId?: string) {
		using = creditId ?? 'any';
		confirming = null;
		try {
			const { result } = await admin.redeem(account.id, creditId);
			let msg = resultText[result.code] ?? `Result: ${result.code}`;
			if (result.code === 'reset' || result.code === 'already_redeemed') toast.success(msg);
			else toast.info(msg);
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			using = null;
		}
	}

	async function setAuto(on: boolean) {
		try {
			await admin.updateAccount(account.id, { redeem: { ...account.redeem, enabled: on } });
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		}
	}
</script>

{#snippet useButton(creditId: string | undefined, label: string)}
	{@const id = creditId ?? 'any'}
	{#if confirming === id}
		<div class="flex shrink-0 gap-1">
			<Button size="xs" variant="ghost" onclick={() => (confirming = null)}>Cancel</Button>
			<Button size="xs" onclick={() => use(creditId)}>Use now</Button>
		</div>
	{:else}
		<Button
			size="xs"
			variant="outline"
			disabled={using !== null || !!cooldown}
			onclick={() => (confirming = id)}
			class="shrink-0"
		>
			{#if using === id}<LoaderIcon class="animate-spin" />{/if}
			{label}
		</Button>
	{/if}
{/snippet}

{#if banked}
	<div class="space-y-2 rounded-lg bg-muted/40 px-3 py-2">
		<div class="flex items-center justify-between gap-3">
			<p class="flex items-center gap-2 text-sm">
				<TimerResetIcon class="size-4 text-muted-foreground" />
				<span>
					<span class="font-medium">{banked.available}</span>
					banked reset{banked.available === 1 ? '' : 's'}
				</span>
			</p>
			{#if autoUse}
				<div class="flex items-center gap-2">
					<Label for="auto-{account.id}" class="text-xs font-normal text-muted-foreground">
						Use automatically
					</Label>
					<Switch
						id="auto-{account.id}"
						size="sm"
						checked={account.redeemEffective.enabled}
						onCheckedChange={(v) => setAuto(v)}
					/>
				</div>
			{/if}
		</div>
		{#if cooldown && banked.available > 0}
			<p class="text-xs text-muted-foreground">Usable again in {countdown(cooldown)}</p>
		{/if}

		{#if banked.available > 0}
			<ul class="divide-y rounded-md border bg-background">
				{#each credits as c, i (c.id)}
					<li class="flex items-center justify-between gap-3 px-3 py-1.5">
						<div class="min-w-0 space-y-0.5 leading-tight">
							<div class="flex flex-wrap items-center gap-x-2 gap-y-1 text-sm">
								<span>{c.title || 'Rate-limit reset'}</span>
								{#if (c.count ?? 1) > 1}
									<Badge variant="outline">×{c.count}</Badge>
								{/if}
								{#if i === 0 && credits.length > 1 && (!autoUse || c.expiresAt)}
									<!-- Claude lists the grant it spends next first; Codex sorts by expiry. -->
									<Badge variant="secondary">{autoUse ? 'Expires first' : 'Used next'}</Badge>
								{/if}
							</div>
							<p class="text-xs {expiringSoon(c) ? 'text-destructive' : 'text-muted-foreground'}">
								{#if c.expiresAt}
									Expires {shortDateTime(c.expiresAt)} · in {countdown(c.expiresAt)}
								{:else}
									No expiry
								{/if}
							</p>
							{#if c.requiresLimit}
								<p class="text-xs text-muted-foreground">Only while a limit is hit</p>
							{/if}
							{#if c.grantedAt}
								<p class="text-xs text-muted-foreground">Granted {relativeTime(c.grantedAt)}</p>
							{/if}
						</div>
						{@render useButton(c.id, 'Use')}
					</li>
				{/each}
				{#if unlisted > 0 || credits.length === 0}
					<li class="flex items-center justify-between gap-3 px-3 py-1.5">
						<p class="text-xs text-muted-foreground">
							{credits.length === 0
								? "Details weren't returned for these."
								: `${unlisted} more not listed.`}
						</p>
						{@render useButton(undefined, 'Use any')}
					</li>
				{/if}
			</ul>
		{/if}
	</div>
{/if}
