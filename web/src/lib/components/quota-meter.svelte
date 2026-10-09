<script lang="ts">
	import type { Level } from '#lib/types.js';
	import { countdown, shortDateTime } from '#lib/format.js';
	import { quotaMode, type QuotaMode } from '#lib/quota-mode.svelte.js';
	import FlipValue from './flip-value.svelte';
	import TriangleAlertIcon from '@lucide/svelte/icons/triangle-alert';
	import OctagonXIcon from '@lucide/svelte/icons/octagon-x';

	let {
		label,
		remaining,
		level,
		values,
		modeFallback = 'left',
		resetsAt,
		resetFormat = 'both',
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
		detail?: string;
	} = $props();

	const mode = $derived(quotaMode.get(modeFallback));
	const otherMode = $derived(mode === 'left' ? 'used' : 'left');
	function resetText(at: string) {
		if (resetFormat === 'exact') return `Resets ${shortDateTime(at)}`;
		if (resetFormat === 'relative') return `Resets in ${countdown(at)}`;
		return `Resets ${shortDateTime(at)} · in ${countdown(at)}`;
	}

	const fallbackWidth: Record<Level, number> = { plenty: 85, ok: 50, low: 15, out: 0 };
	const left = $derived(Math.max(0, Math.min(100, remaining ?? fallbackWidth[level])));
	const width = $derived(values && mode === 'used' && remaining !== null ? 100 - left : left);
	const fill = $derived(
		level === 'out'
			? 'var(--viz-critical)'
			: level === 'low'
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
</script>

<div class="space-y-1.5">
	<div class="flex items-baseline justify-between gap-3 text-sm">
		<span class="truncate text-muted-foreground">{label}</span>
		<span class="flex shrink-0 items-center gap-1 font-medium">
			{#if level === 'out'}
				<OctagonXIcon class="size-3.5" style="color: var(--viz-critical)" />
			{:else if level === 'low'}
				<TriangleAlertIcon class="size-3.5" style="color: var(--viz-warning)" />
			{/if}
			{#if values}
				<FlipValue {text} alt={values[otherMode]} onflip={() => quotaMode.set(otherMode)} />
			{:else}
				{text}
			{/if}
		</span>
	</div>
	<div
		class="h-2 overflow-hidden rounded-full"
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
	{#if showReset || detail}
		<p class="text-xs text-muted-foreground">
			{#if showReset && resetsAt}
				{resetText(resetsAt)}
			{/if}{#if detail}{showReset ? ' · ' : ''}{detail}{/if}
		</p>
	{/if}
</div>
