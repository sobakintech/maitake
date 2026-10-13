<script lang="ts">
	import type { Level, QuotaSnapshot, QuotaWindow } from '#lib/types.js';
	import { money } from '#lib/format.js';
	import QuotaMeter from './quota-meter.svelte';

	let { quota }: { quota: QuotaSnapshot | null } = $props();

	function remaining(w: QuotaWindow) {
		if (w.resetsAt && new Date(w.resetsAt) <= new Date()) return 100;
		return w.limited ? 0 : Math.max(0, Math.min(100, 100 - w.usedPercent));
	}

	function level(r: number): Level {
		if (r <= 0) return 'out';
		if (r <= 20) return 'low';
		if (r <= 50) return 'ok';
		return 'plenty';
	}

	function values(w: QuotaWindow, r: number) {
		const a = w.amount;
		if (a) {
			const used = r >= 100 ? 0 : a.used;
			const limit = money(a.limit, a.currency);
			return {
				left: `${money(Math.max(0, a.limit - used), a.currency)} of ${limit} left`,
				used: `${money(used, a.currency)} of ${limit} used`
			};
		}
		return { left: `${Math.round(r)}% left`, used: `${Math.round(100 - r)}% used` };
	}
</script>

{#if quota?.windows?.length}
	<div class="space-y-3">
		{#each quota.windows as w (w.id)}
			{@const r = remaining(w)}
			<QuotaMeter
				label={w.label}
				remaining={r}
				level={level(r)}
				values={values(w, r)}
				resetsAt={w.resetsAt}
				pace={r >= 100 ? undefined : w.pace}
				detail={w.detail ??
					(w.kind === 'session' && !w.resetsAt && !w.usedPercent
						? 'Not started; the next request starts it'
						: undefined)}
			/>
		{/each}
	</div>
{:else}
	<p class="text-sm text-muted-foreground">
		{quota?.error ? `Couldn't load limits: ${quota.error}` : 'No limit data yet.'}
	</p>
{/if}
