<script lang="ts">
	import type { Level, Pace } from '#lib/types.js';
	import { countdown, shortDateTime } from '#lib/format.js';
	import { quotaMode, type QuotaMode } from '#lib/quota-mode.svelte.js';
	import FlipValue from './flip-value.svelte';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import OctagonXIcon from '@lucide/svelte/icons/octagon-x';
	import FlameIcon from '@lucide/svelte/icons/flame';

	let {
		label,
		remaining,
		level,
		values,
		modeFallback = 'left',
		resetsAt,
		resetFormat = 'both',
		pace,
		detail
	}: {
		label: string;
		// Percent remaining to draw, or null to show the level only (no fill width is revealed).
		remaining: number | null;
		level: Level;
		// The value both ways; clicking flips between them, hovering previews the other.
		values?: { left: string; used: string };
		modeFallback?: QuotaMode;
		resetsAt?: string | null;
		resetFormat?: 'both' | 'relative' | 'exact' | 'hidden';
		pace?: Pace;
		detail?: string;
	} = $props();

	const mode = $derived(quotaMode.get(modeFallback));
	const otherMode = $derived(mode === 'left' ? 'used' : 'left');
	function resetText(at: string) {
		if (resetFormat === 'exact') return `Resets ${shortDateTime(at)}`;
		if (resetFormat === 'relative') return `Resets in ${countdown(at)}`;
		return `Resets in ${countdown(at)} · ${shortDateTime(at)}`;
	}

	const fallbackWidth: Record<Level, number> = { plenty: 85, ok: 50, low: 15, out: 0 };
	const left = $derived(Math.max(0, Math.min(100, remaining ?? fallbackWidth[level])));
	const width = $derived(values && mode === 'used' && remaining !== null ? 100 - left : left);
	const fill = $derived(
		level === 'out' || pace?.status === 'over'
			? 'var(--viz-critical)'
			: level === 'low' || pace?.status === 'close'
				? 'var(--viz-warning)'
				: 'var(--viz-accent)'
	);
	const levelText: Record<Level, string> = {
		plenty: 'Plenty left',
		ok: 'OK',
		low: 'Low',
		out: 'Used up'
	};
	const text = $derived(values ? values[mode] : levelText[level]);
	const showReset = $derived(!!resetsAt && resetFormat !== 'hidden');

	const pacePercent = (v: number) => `~${Math.max(1, Math.round(v))}%`;
	const paceText = $derived.by(() => {
		if (!pace || level === 'out') return '';
		const projected = pace.projectedUsed;
		if (pace.status === 'over') {
			if (!pace.runsOutAt) return projected === undefined ? 'Likely to run out' : 'Limit soon';
			const at = countdown(pace.runsOutAt);
			return at === 'now' ? 'Limit soon' : `Limit in ${at}`;
		}
		if (projected === undefined) return pace.status === 'close' ? 'Cutting it close' : 'On pace';
		if (pace.status === 'close') return `${pacePercent(100 - projected)} spare`;
		return `${pacePercent(100 - projected)} left at reset`;
	});
	const paceTitle = $derived.by(() => {
		const projected = pace?.projectedUsed;
		if (!paceText || projected === undefined || pace?.status === 'on_track') return undefined;
		if (projected > 100) return `${pacePercent(projected - 100)} over limit at reset`;
		return `${pacePercent(projected)} used at reset`;
	});
	// The even-pace mark, in the same direction the bar is drawn.
	const tick = $derived(
		!paceText || pace?.elapsed === undefined || remaining === null
			? null
			: values && mode === 'used'
				? pace.elapsed
				: 100 - pace.elapsed
	);
</script>

<div class="space-y-1.5">
	<div class="flex items-baseline justify-between gap-3 text-sm">
		<span class="truncate font-semibold">{label}</span>
		{#if paceText}
			<span class="flex shrink-0 items-center gap-1 text-muted-foreground" title={paceTitle}>
				{#if pace?.status === 'over'}
					<FlameIcon class="size-3.5 self-center" style="color: var(--viz-critical)" />
				{/if}{paceText}
			</span>
		{/if}
	</div>
	<div class="relative">
		<div
			class="h-1.5 overflow-hidden rounded-full"
			style="background: var(--viz-track)"
			role="meter"
			aria-label={label}
			aria-valuemin={0}
			aria-valuemax={100}
			aria-valuenow={remaining === null ? undefined : width}
			aria-valuetext={text}
		>
			<div
				class="h-full rounded-full transition-[width]"
				style="width: {width}%; background: {fill}"
			></div>
		</div>
		{#if tick !== null}
			<span
				class="absolute -top-0.5 h-2.5 w-0.5 -translate-x-1/2 rounded-full bg-muted-foreground"
				style="left: clamp(1px, {tick}%, calc(100% - 1px))"
				title="Even pace"
				aria-hidden="true"
			></span>
		{/if}
	</div>
	<div class="flex items-baseline justify-between gap-3 text-xs text-muted-foreground">
		<span class="flex shrink-0 items-center gap-1 text-sm font-medium text-foreground">
			{#if level === 'out'}
				<OctagonXIcon class="size-3.5 self-center" style="color: var(--viz-critical)" />
			{:else if level === 'low'}
				<TriangleAlertIcon class="size-3.5 self-center" style="color: var(--viz-warning)" />
			{/if}
			{#if values}
				<FlipValue {text} alt={values[otherMode]} onflip={() => quotaMode.set(otherMode)} />
			{:else}
				{text}
			{/if}
		</span>
		{#if showReset || detail}
			<p class="text-right">
				{#if showReset && resetsAt}
					{resetText(resetsAt)}
				{/if}{#if detail}{showReset ? ' · ' : ''}{detail}{/if}
			</p>
		{/if}
	</div>
</div>
