<script lang="ts">
	import { invalidateAll } from '$app/navigation';
	import { admin, errorMessage, type Account } from '#lib/api.js';
	import { relativeTime, shortDateTime } from '#lib/format.js';
	import { Button } from '#lib/components/ui/button/index.js';
	import { Switch } from '#lib/components/ui/switch/index.js';
	import AlarmClockIcon from '@lucide/svelte/icons/alarm-clock';
	import LoaderIcon from '@lucide/svelte/icons/loader-2';
	import Settings2Icon from '@lucide/svelte/icons/settings-2';
	import { toast } from 'svelte-sonner';

	// The session primer for one account: on/off and what it does next.
	let { account, onOpen }: { account: Account; onOpen: () => void } = $props();

	let saving = $state(false);

	const eff = $derived(account.primerEffective);
	const own = $derived(!!account.primer?.schedule);
	const next = $derived(account.primerNext);
	const empty = $derived(!eff.schedules.some((s) => s.times.length));
	const failed = $derived.by(() => {
		const l = account.primerLast;
		return l?.outcome === 'failed' && Date.now() - new Date(l.time).getTime() < 24 * 3600 * 1000
			? l
			: null;
	});

	const status = $derived.by(() => {
		if (account.disabled) return 'Paused while the account is disabled';
		if (next) return `Resets ${shortDateTime(next.readyAt)}`;
		if (empty) return 'No times set';
		return 'Nothing planned this week';
	});

	async function setEnabled(on: boolean) {
		saving = true;
		try {
			await admin.updateAccount(account.id, { primer: { ...account.primer, enabled: on } });
			if (on && empty)
				toast.info('Session primer on, but its schedule has no times yet', {
					action: { label: 'Set times', onClick: onOpen },
					duration: 10_000
				});
			else toast.success(on ? 'Session primer on' : 'Session primer off');
			await invalidateAll();
		} catch (e) {
			toast.error(errorMessage(e));
		} finally {
			saving = false;
		}
	}
</script>

<div class="flex items-center gap-2 rounded-lg bg-muted/40 py-1.5 pr-1.5 pl-3 text-sm">
	<AlarmClockIcon class="size-4 shrink-0 text-muted-foreground" />
	<div class="min-w-0 flex-1">
		<p class="flex items-baseline gap-2">
			<span class="shrink-0">Session primer</span>
			{#if account.primerBusy}
				<span class="flex items-center gap-1 truncate text-xs text-muted-foreground">
					<LoaderIcon class="size-3 shrink-0 animate-spin" /> Starting a session
				</span>
			{:else if eff.enabled}
				<span
					class="truncate text-xs text-muted-foreground"
					title={next ? `Pings ${shortDateTime(next.at)}` : undefined}
				>
					{status}{own ? ' · own schedule' : ''}
				</span>
			{/if}
		</p>
		{#if failed}
			<p class="truncate text-xs" style="color: var(--viz-critical)" title={failed.message}>
				Failed {relativeTime(failed.time)}: {failed.message}
			</p>
		{/if}
	</div>
	<Switch
		size="sm"
		aria-label="Session primer"
		checked={eff.enabled}
		disabled={saving}
		onCheckedChange={setEnabled}
	/>
	<Button
		size="icon-xs"
		variant="ghost"
		class="text-muted-foreground"
		aria-label="Session primer settings"
		onclick={onOpen}
	>
		<Settings2Icon />
	</Button>
</div>
