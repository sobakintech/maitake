<script lang="ts">
	type Point = { tick: string; title: string; value: number };

	let {
		data,
		format = (v: number) => v.toLocaleString(),
		label,
		height = 220
	}: { data: Point[]; format?: (v: number) => string; label: string; height?: number } = $props();

	const uid = $props.id();
	let width = $state(600);
	let hover = $state<number | null>(null);
	let svg = $state<SVGSVGElement>();

	const pad = { top: 12, right: 12, bottom: 26, left: 52 };
	const plotW = $derived(Math.max(10, width - pad.left - pad.right));
	const plotH = $derived(height - pad.top - pad.bottom);
	const base = $derived(pad.top + plotH);

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
	const step = $derived(data.length > 1 ? plotW / (data.length - 1) : 0);
	const labelEvery = $derived(step ? Math.max(1, Math.ceil(64 / step)) : 1);

	function x(i: number) {
		return data.length > 1 ? pad.left + i * step : pad.left + plotW / 2;
	}

	function y(v: number) {
		return base - (v / yMax) * plotH;
	}

	const line = $derived(data.map((d, i) => `${i ? 'L' : 'M'}${x(i)},${y(d.value)}`).join(' '));
	const area = $derived(
		data.length > 1 ? `${line} L${x(data.length - 1)},${base} L${x(0)},${base} Z` : ''
	);

	function onMove(e: PointerEvent) {
		if (!svg || !data.length) return;
		const px = e.clientX - svg.getBoundingClientRect().left;
		hover = step ? Math.max(0, Math.min(data.length - 1, Math.round((px - pad.left) / step))) : 0;
	}

	const tooltipLeft = $derived(
		hover === null ? 0 : Math.min(width - 160, Math.max(0, x(hover) - 80))
	);
</script>

<div class="relative" bind:clientWidth={width}>
	<svg bind:this={svg} {width} {height} role="img" aria-label={label}>
		<defs>
			<linearGradient id="{uid}-fill" x1="0" x2="0" y1="0" y2="1">
				<stop offset="0%" stop-color="var(--viz-accent)" stop-opacity="0.22" />
				<stop offset="100%" stop-color="var(--viz-accent)" stop-opacity="0" />
			</linearGradient>
		</defs>
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
			{#if i % labelEvery === 0}
				<text
					x={x(i)}
					y={height - 8}
					text-anchor={data.length < 2
						? 'middle'
						: i === 0
							? 'start'
							: i === data.length - 1
								? 'end'
								: 'middle'}
					class="fill-muted-foreground text-[11px]">{d.tick}</text
				>
			{/if}
		{/each}
		{#if area}<path d={area} fill="url(#{uid}-fill)" />{/if}
		<path
			d={line}
			fill="none"
			stroke="var(--viz-accent)"
			stroke-width="2"
			stroke-linejoin="round"
			stroke-linecap="round"
		/>
		{#if data.length < 3}
			{#each data as d, i (i)}
				<circle cx={x(i)} cy={y(d.value)} r="4" fill="var(--viz-accent)" />
			{/each}
		{/if}
		{#if hover !== null && data[hover]}
			<line
				x1={x(hover)}
				x2={x(hover)}
				y1={pad.top}
				y2={base}
				stroke="var(--viz-axis)"
				stroke-width="1"
			/>
			<circle
				cx={x(hover)}
				cy={y(data[hover].value)}
				r="5"
				fill="var(--viz-accent)"
				stroke="var(--card)"
				stroke-width="2"
			/>
		{/if}
		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<rect
			x={pad.left - 6}
			y={pad.top}
			width={plotW + 12}
			height={plotH}
			fill="transparent"
			onpointermove={onMove}
			onpointerleave={() => (hover = null)}
		/>
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
