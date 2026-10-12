<script lang="ts">
	import type { Account } from '#lib/types.js';
	import { countdown } from '#lib/format.js';
	import CalendarIcon from '@lucide/svelte/icons/calendar';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';

	let { account }: { account: Account } = $props();

	const sub = $derived(account.subscription);

	function day(iso: string) {
		return new Date(iso).toLocaleDateString(undefined, {
			month: 'short',
			day: 'numeric',
			year: new Date(iso).getFullYear() === new Date().getFullYear() ? undefined : 'numeric'
		});
	}

	const text = $derived.by(() => {
		if (!sub) return '';
		if (sub.until) {
			const ended = new Date(sub.until) <= new Date();
			if (ended) return `Subscription ended ${day(sub.until)}`;
			// Claude's date comes from claude.ai billing: the next charge. ChatGPT's is the paid period end.
			const what =
				account.provider === 'claude' && sub.status === 'active' ? 'Renews' : 'Renews or ends';
			return `${what} ${day(sub.until)} · in ${countdown(sub.until)}`;
		}
		// Without a real renewal date, only say something when the subscription isn't active.
		return sub.status && sub.status !== 'active'
			? `Subscription ${sub.status.replace(/_/g, ' ')}`
			: '';
	});
</script>

{#if text}
	<p
		class="flex items-center gap-1.5 text-xs text-muted-foreground"
		title={sub?.until
			? account.provider === 'claude'
				? 'From claude.ai billing'
				: 'From ChatGPT: the end of the current paid period'
			: undefined}
	>
		<CalendarIcon class="size-3.5 shrink-0" />
		{text}
	</p>
{/if}
{#if account.hasWebSession && sub?.webError && !sub.webExpired}
	<p class="flex items-center gap-1.5 text-xs" style="color: var(--viz-warning)">
		<TriangleAlertIcon class="size-3.5 shrink-0" />
		Couldn't read the renewal date: {sub.webError}
	</p>
{/if}
