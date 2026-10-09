<script lang="ts">
	import * as Table from '#lib/components/ui/table/index.js';

	type Point = { tick: string; title: string; value: number };

	let {
		data,
		format = (v: number) => v.toLocaleString(),
		label,
		height = 220
	}: { data: Point[]; format?: (v: number) => string; label: string; height?: number } = $props();

	let width = $state(600);
	let hover = $state<number | null>(null);
	let showTable = $state(false);

	const pad = { top: 12, right: 8, bottom: 26, left: 52 };
	const plotW = $derived(Math.max(10, width - pad.left - pad.right));
	const plotH = $derived(height - pad.top - pad.bottom);

	// Round the axis up to a clean step: 1, 2 or 5 × 10^n.
	const ticks = $derived.by(() => {
		const max = Math.max(1, ...data.map((d) => d.value));
		const rough = max / 4;
		const mag = 10 ** Math.floor(Math.log10(rough));
		const step = [1, 2, 5, 10].map((m) => m * mag).find((s) => s >= rough) ?? 10 * mag;
		const top = Math.ceil(max / step) * step;
		const out: number[] = [];
		for (let v = 0; v <= top + step / 2; v += step) out.push(v);
		return out;
	});
	const yMax = $derived(ticks[ticks.length - 1] || 1);
	const band = $derived(plotW / Math.max(1, data.length));
	const barW = $derived(Math.max(2, Math.min(24, band - 2)));
	const labelEvery = $derived(Math.max(1, Math.ceil(56 / band)));

	function y(v: number) {
		return pad.top + plotH - (v / yMax) * plotH;
	}

	// Column with a 4px rounded data-end, square at the baseline.
	function columnPath(i: number, v: number) {
		const x = pad.left + i * band + (band - barW) / 2;
		const top = y(v);
		const base = pad.top + plotH;
		const h = base - top;
		if (h <= 0) return '';
		const r = Math.min(4, barW / 2, h);
		return `M${x},${base} V${top + r} Q${x},${top} ${x + r},${top} H${x + barW - r} Q${x + barW},${top} ${x + barW},${top + r} V${base} Z`;
	}

	const tooltipLeft = $derived(
		hover === null ? 0 : Math.min(width - 160, Math.max(0, pad.left + hover * band + band / 2 - 80))
	);
</script>

<div class="space-y-2">
	<div class="flex justify-end">
		<button
			class="text-xs text-muted-foreground underline-offset-4 hover:text-foreground hover:underline"
			onclick={() => (showTable = !showTable)}
		>
			{showTable ? 'Show chart' : 'Show table'}
		</button>
	</div>
	{#if showTable}
		<div class="max-h-80 overflow-auto rounded-lg border">
			<Table.Root>
				<Table.Header>
					<Table.Row>
						<Table.Head>Period</Table.Head>
						<Table.Head class="text-right">{label}</Table.Head>
					</Table.Row>
				</Table.Header>
				<Table.Body>
					{#each data as d, i (i)}
						<Table.Row>
							<Table.Cell>{d.title}</Table.Cell>
							<Table.Cell class="text-right tabular-nums">{format(d.value)}</Table.Cell>
						</Table.Row>
					{/each}
				</Table.Body>
			</Table.Root>
		</div>
	{:else}
		<div class="relative" bind:clientWidth={width}>
			<svg {width} {height} role="img" aria-label={label}>
				{#each ticks as t (t)}
					<line
						x1={pad.left}
						x2={width - pad.right}
						y1={y(t)}
						y2={y(t)}
						stroke={t === 0 ? 'var(--viz-axis)' : 'var(--viz-grid)'}
						stroke-width="1"
					/>
					<text
						x={pad.left - 8}
						y={y(t)}
						dy="0.32em"
						text-anchor="end"
						class="fill-muted-foreground text-[11px] tabular-nums">{format(t)}</text
					>
				{/each}
				{#each data as d, i (i)}
					<path
						d={columnPath(i, d.value)}
						fill="var(--viz-accent)"
						opacity={hover === null || hover === i ? 1 : 0.55}
					/>
					{#if i % labelEvery === 0}
						<text
							x={pad.left + i * band + band / 2}
							y={height - 8}
							text-anchor="middle"
							class="fill-muted-foreground text-[11px]">{d.tick}</text
						>
					{/if}
					<!-- svelte-ignore a11y_no_static_element_interactions -->
					<rect
						x={pad.left + i * band}
						y={pad.top}
						width={band}
						height={plotH}
						fill="transparent"
						onmouseenter={() => (hover = i)}
						onmouseleave={() => (hover = null)}
					/>
				{/each}
			</svg>
			{#if hover !== null && data[hover]}
				<div
					class="pointer-events-none absolute top-0 w-40 rounded-md border bg-popover px-3 py-2 text-xs shadow-md"
					style="left: {tooltipLeft}px"
				>
					<p class="text-muted-foreground">{data[hover].title}</p>
					<p class="mt-0.5 font-medium">{format(data[hover].value)} {label.toLowerCase()}</p>
				</div>
			{/if}
		</div>
	{/if}
</div>
