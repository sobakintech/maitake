<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account } from '#lib/api.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import * as Dialog from '#lib/components/ui/dialog/index.js';
	import * as Field from '#lib/components/ui/field/index.js';
	import { Input } from '#lib/components/ui/input/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import * as Select from '#lib/components/ui/select/index.js';
	import RedeemPolicyFields from '#lib/components/redeem-policy-fields.svelte';
	import { numbers } from '#lib/redeem.js';
	import type { RedeemOverride, RedeemPolicy } from '#lib/types.js';
	import ProviderLogo from '#lib/components/provider-logo.svelte';
	import { toast } from 'svelte-sonner';
	import PrivateText from '#lib/components/private-text.svelte';
	import ChevronDownIcon from '@lucide/svelte/icons/chevron-down';

	let {
		account,
		open = $bindable(false),
		defaultOn = false,
		focus
	}: {
		account: Account | null;
		open: boolean;
		defaultOn?: boolean;
		// Section to jump to on open.
		focus?: 'session';
	} = $props();

	let label = $state('');
	let alias = $state('');
	let priority = $state(0);
	// Banked resets: on/off/default, plus optional custom rules for this account.
	let redeemMode = $state<'default' | 'on' | 'off'>('default');
	let customRules = $state(false);
	let rules = $state<RedeemPolicy>({} as RedeemPolicy);
	let error = $state('');
	let busy = $state(false);

	$effect(() => {
		if (open && account) {
			label = account.label;
			alias = account.alias;
			priority = account.priority;
			const o = account.redeem ?? {};
			redeemMode = o.enabled === undefined ? 'default' : o.enabled ? 'on' : 'off';
			customRules = Object.keys(o).some((k) => k !== 'enabled');
			rules = { ...account.redeemEffective };
			error = '';
			session = '';
			guide = !account.hasWebSession || !!account.subscription?.webExpired;
		}
	});

	async function save(e: SubmitEvent) {
		e.preventDefault();
		if (!account) return;
		busy = true;
		try {
			await admin.updateAccount(account.id, {
				label,
				alias,
				priority: Number(priority) || 0,
				...(account.provider === 'codex' ? { redeem: redeemOverride() } : {})
			});
			toast.success('Saved');
			open = false;
			await invalidateAll();
		} catch (err) {
			error = errorMessage(err);
		} finally {
			busy = false;
		}
	}
	function redeemOverride(): RedeemOverride {
		const o: RedeemOverride = customRules ? numbers({ ...rules }) : {};
		delete o.enabled;
		if (redeemMode !== 'default') o.enabled = redeemMode === 'on';
		return o;
	}

	const redeemModes = $derived([
		{
			value: 'default',
			label: `Use the default (${defaultOn ? 'on' : 'off'})`
		},
		{ value: 'on', label: 'On for this account' },
		{ value: 'off', label: 'Off for this account' }
	]);

	let session = $state('');
	let sessionBusy = $state(false);
	let guide = $state(false);
	const web = $derived(account?.subscription);

	async function saveSession(value: string) {
		if (!account) return;
		sessionBusy = true;
		try {
			const { account: a } = await admin.setWebSession(account.id, value);
			session = '';
			const sub = a.subscription;
			if (!value) toast.success('claude.ai session removed');
			else if (sub?.webError) toast.error(`Saved, but: ${sub.webError}`);
			else if (sub?.until)
				toast.success(`Connected · renews ${new Date(sub.until).toLocaleDateString()}`);
			else toast.success('Connected');
			await invalidateAll();
		} catch (err) {
			toast.error(errorMessage(err));
		} finally {
			sessionBusy = false;
		}
	}
</script>

<Dialog.Root bind:open>
	<Dialog.Content
		class="max-h-[90vh] overflow-y-auto sm:max-w-lg"
		onOpenAutoFocus={(e) => {
			if (focus !== 'session') return;
			e.preventDefault();
			document.getElementById('web-session')?.focus();
		}}
	>
		<form onsubmit={save} class="space-y-6">
			<Dialog.Header>
				<Dialog.Title class="flex items-center gap-2">
					{#if account}<ProviderLogo provider={account.provider} />{/if} Edit account
				</Dialog.Title>
				<Dialog.Description><PrivateText>{account?.email}</PrivateText></Dialog.Description>
			</Dialog.Header>
			<Field.Group>
				<Field.Field>
					<Field.Label for="label">Label</Field.Label>
					<Input id="label" required maxlength={100} bind:value={label} />
					<Field.Description>Only admins see this.</Field.Description>
				</Field.Field>
				<Field.Field>
					<Field.Label for="alias">Public name</Field.Label>
					<Input id="alias" maxlength={100} placeholder="e.g. Max #1" bind:value={alias} />
					<Field.Description
						>Shown on the quota page when a profile names accounts by public name.</Field.Description
					>
				</Field.Field>
				<Field.Field>
					<Field.Label for="priority">Priority</Field.Label>
					<Input id="priority" type="number" bind:value={priority} class="w-28" />
					<Field.Description>
						Higher priority accounts are used first; equal ones take turns.
					</Field.Description>
				</Field.Field>
				{#if account?.provider === 'codex'}
					<Field.Separator />
					<Field.Set>
						<Field.Legend>Banked resets</Field.Legend>
						<Select.Root type="single" bind:value={redeemMode}>
							<Select.Trigger class="w-full">
								{redeemModes.find((m) => m.value === redeemMode)?.label}
							</Select.Trigger>
							<Select.Content>
								{#each redeemModes as m (m.value)}
									<Select.Item value={m.value} label={m.label}>{m.label}</Select.Item>
								{/each}
							</Select.Content>
						</Select.Root>
						<Field.Field orientation="horizontal">
							<Field.Content>
								<Field.Label for="custom-rules">Custom rules for this account</Field.Label>
								<Field.Description
									>Otherwise the rules in Settings → Account pool apply.</Field.Description
								>
							</Field.Content>
							<Switch id="custom-rules" bind:checked={customRules} />
						</Field.Field>
						{#if customRules}
							<RedeemPolicyFields bind:policy={rules} idPrefix="acct-redeem" />
						{/if}
					</Field.Set>
				{/if}
				{#if account?.provider === 'claude'}
					<Field.Separator />
					<Field.Field>
						<Field.Label for="web-session" class="text-base">claude.ai session</Field.Label>
						<div class="flex gap-2">
							<Input
								id="web-session"
								type="password"
								autocomplete="off"
								placeholder={account.hasWebSession
									? 'Connected; paste a new one to replace it'
									: 'sk-ant-sid01-…'}
								bind:value={session}
							/>
							<Button
								type="button"
								variant="outline"
								disabled={!session.trim() || sessionBusy}
								onclick={() => saveSession(session)}>Connect</Button
							>
							{#if account.hasWebSession}
								<Button
									type="button"
									variant="ghost"
									disabled={sessionBusy}
									onclick={() => saveSession('')}>Remove</Button
								>
							{/if}
						</div>
						{#if account.hasWebSession && web?.webExpired}
							<Field.Error
								>This session stopped working. Get a new one with the steps below.</Field.Error
							>
						{:else if account.hasWebSession && web?.webError}
							<p class="text-sm" style="color: var(--viz-warning)">{web.webError}</p>
						{/if}
						<Field.Description>
							Only used to read the real renewal date, which Anthropic doesn't share with Claude
							Code's login. maitake checks it every 6 hours and keeps any fresh key claude.ai hands
							out. It gives full access to the claude.ai account, so it stays on the server and is
							never shown again.
						</Field.Description>
						<button
							type="button"
							class="flex items-center gap-1 text-sm font-medium"
							aria-expanded={guide}
							onclick={() => (guide = !guide)}
						>
							How to get one
							<ChevronDownIcon
								class="size-4 text-muted-foreground transition-transform {guide
									? 'rotate-180'
									: ''}"
							/>
						</button>
						{#if guide}
							<ol
								class="list-decimal space-y-1.5 pl-5 text-sm text-muted-foreground marker:text-muted-foreground"
							>
								<li>
									Open a private (incognito) window and sign in to claude.ai with this account. That
									sign-in gets its own key, so signing out in your everyday browser won't break it.
								</li>
								<li>
									Press F12 and open <span class="text-foreground"
										>Application → Cookies → https://claude.ai</span
									>. In Firefox it's <span class="text-foreground">Storage → Cookies</span>.
								</li>
								<li>
									Copy the <code>sessionKey</code> value (it starts with
									<code>sk-ant-sid01-</code>), paste it above and press Connect.
								</li>
								<li>
									Close the private window <span class="text-foreground">without signing out</span>.
									Signing out revokes the key.
								</li>
							</ol>
						{/if}
					</Field.Field>
				{/if}
				{#if error}<Field.Error>{error}</Field.Error>{/if}
			</Field.Group>
			<Dialog.Footer>
				<Button type="submit" disabled={busy}>Save</Button>
			</Dialog.Footer>
		</form>
	</Dialog.Content>
</Dialog.Root>
