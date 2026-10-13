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
		detail,
		compact = false,
		dense = false
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
		// One narrow cell for a table row: label and value on top, then the bar and the reset.
		compact?: boolean;
		// The same layout with tighter type and spacing.
		dense?: boolean;
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

{#snippet bar(height: string)}
	<div class="relative">
		<div
			class="{height} overflow-hidden rounded-full"
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
				class="absolute top-1/2 h-2.5 w-0.5 -translate-1/2 rounded-full bg-muted-foreground"
				style="left: clamp(1px, {tick}%, calc(100% - 1px))"
				title="Even pace"
				aria-hidden="true"
			></span>
		{/if}
	</div>
{/snippet}

{#snippet value()}
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
{/snippet}

{#if compact}
	<div class="min-w-36 flex-1 space-y-1 text-xs" title={paceTitle ?? (paceText || undefined)}>
		<div class="flex items-baseline justify-between gap-2">
			<span class="truncate text-muted-foreground">{label}</span>
			<span class="flex shrink-0 items-center gap-1 font-medium">
				{#if pace?.status === 'over' && level !== 'out'}
					<FlameIcon class="size-3 self-center" style="color: var(--viz-critical)" />
				{/if}
				{@render value()}
			</span>
		</div>
		{@render bar('h-1')}
		<p
			class="truncate text-muted-foreground"
			title={showReset && resetsAt ? shortDateTime(resetsAt) : detail}
		>
			{#if showReset && resetsAt}{resetText(resetsAt)}{:else if detail}{detail}{:else}&nbsp;{/if}
		</p>
	</div>
{:else}
	<div class={dense ? 'space-y-1' : 'space-y-1.5'}>
		<div
			class="flex items-baseline justify-between gap-3 {dense
				? 'text-[13px] leading-tight'
				: 'text-sm'}"
		>
			<span class="truncate font-semibold">{label}</span>
			{#if paceText}
				<span class="flex shrink-0 items-center gap-1 text-muted-foreground" title={paceTitle}>
					{#if pace?.status === 'over'}
						<FlameIcon class="size-3.5 self-center" style="color: var(--viz-critical)" />
					{/if}{paceText}
				</span>
			{/if}
		</div>
		{@render bar('h-1.5')}
		<div
			class="flex items-baseline justify-between gap-3 text-xs text-muted-foreground {dense
				? 'leading-tight'
				: ''}"
		>
			<span
				class="flex shrink-0 items-center gap-1 font-medium text-foreground {dense
					? 'text-[13px]'
					: 'text-sm'}"
			>
				{@render value()}
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
{/if}
