<script lang="ts">
	import type { CapacityPoint, CapacityProvider, CapacityWindow } from '#lib/types.js';
	import PrivateText from './private-text.svelte';

	type Line = { id: number; name: string; color: string; recorded: Pt[]; projected: Pt[] };
	type Pt = { t: number; v: number };

	let {
		data,
		from,
		to,
		now,
		window: win,
		color,
		accounts = null,
		height = 260
	}: {
		data: CapacityProvider;
		from: string;
		to: string;
		now: string;
		window: CapacityWindow;
		color: string;
		// When set, each account's own window is drawn instead of the pool.
		accounts?: { id: number; name: string; color: string }[] | null;
		height?: number;
	} = $props();

	const uid = $props.id();
	let width = $state(600);
	let svg = $state<SVGSVGElement>();
	let hoverT = $state<number | null>(null);

	const pad = { top: 22, right: 12, bottom: 26, left: 44 };
	const plotW = $derived(Math.max(10, width - pad.left - pad.right));
	const plotH = $derived(height - pad.top - pad.bottom);
	const base = $derived(pad.top + plotH);
	const t0 = $derived(new Date(from).getTime());
	const t1 = $derived(new Date(to).getTime());
	const tNow = $derived(new Date(now).getTime());

	const x = (t: number) => pad.left + ((t - t0) / (t1 - t0)) * plotW;
	const y = (v: number) => base - (Math.max(0, Math.min(100, v)) / 100) * plotH;
	const pts = (s: CapacityPoint[]): Pt[] => s.map((p) => ({ t: new Date(p.t).getTime(), v: p.v }));

	const lines = $derived<Line[]>(
		accounts
			? accounts.flatMap((a) => {
					const acct = data.accounts.find((x) => x.id === a.id);
					return acct && acct.remaining !== null
						? [{ ...a, recorded: pts(acct.recorded), projected: pts(acct.projected) }]
						: [];
				})
			: [
					{
						id: 0,
						name: 'Pool',
						color,
						recorded: pts(data.recorded),
						projected: pts(data.projected)
					}
				]
	);

	function path(s: Pt[]) {
		return s.map((p, i) => `${i ? 'L' : 'M'}${x(p.t).toFixed(1)},${y(p.v).toFixed(1)}`).join('');
	}
	function area(s: Pt[]) {
		if (s.length < 2) return '';
		return `${path(s)}L${x(s[s.length - 1].t).toFixed(1)},${base}L${x(s[0].t).toFixed(1)},${base}Z`;
	}

	// Value at t, taking the later point of a step.
	function valueAt(s: Pt[], t: number): number | null {
		if (!s.length || t < s[0].t || t > s[s.length - 1].t) return null;
		let i = 0;
		while (i + 1 < s.length && s[i + 1].t <= t) i++;
		if (i + 1 < s.length && s[i].t < t) {
			const a = s[i],
				b = s[i + 1];
			return a.v + ((b.v - a.v) * (t - a.t)) / (b.t - a.t);
		}
		return s[i].v;
	}

	const ticks = [0, 25, 50, 75, 100];

	// Local midnight k days after the day containing t.
	function midnight(t: number, k: number, hour = 0) {
		const d = new Date(t);
		return new Date(d.getFullYear(), d.getMonth(), d.getDate() + k, hour).getTime();
	}

	// Day lines and labels; the session view also marks every six hours.
	const days = $derived.by(() => {
		const out: { start: number; at: number; label: string }[] = [];
		for (let k = 0; midnight(t0, k) < t1; k++) {
			const start = midnight(t0, k);
			const end = midnight(t0, k + 1);
			const shown = Math.min(end, t1) - Math.max(start, t0) > (end - start) / 3;
			const label = new Date(start).toLocaleDateString(undefined, {
				weekday: 'short',
				day: 'numeric'
			});
			// The session view labels each midnight; the weekly view centers a label on each day.
			if (win === 'session') out.push({ start, at: start, label: start > t0 ? label : '' });
			else
				out.push({
					start,
					at: (Math.max(start, t0) + Math.min(end, t1)) / 2,
					label: shown ? label : ''
				});
		}
		return out;
	});
	const hours = $derived.by(() => {
		if (win !== 'session') return [];
		const out: number[] = [];
		for (let k = 0; midnight(t0, k) < t1; k++)
			for (const h of [6, 12, 18]) {
				const t = midnight(t0, k, h);
				if (t > t0 && t < t1) out.push(t);
			}
		return out;
	});

	const emptyAt = $derived.by(() => {
		const e = data.stats.poolEmptyAt;
		if (accounts || !e) return null;
		const t = new Date(e).getTime();
		return t >= t0 && t <= t1 ? t : null;
	});

	// Reset labels, skipping any that would crowd the previous one or the empty marker.
	const resetLabels = $derived.by(() => {
		if (accounts) return [];
		const out: { x: number; y: number; text: string }[] = [];
		let lastX = -Infinity;
		const all = [...lines[0].recorded, ...lines[0].projected];
		for (const r of data.resets) {
			if (r.gain < 1) continue;
			const t = new Date(r.t).getTime();
			if (t < t0 || t > t1) continue;
			const lx = x(t);
			if (lx - lastX < 40 || (emptyAt !== null && Math.abs(lx - x(emptyAt)) < 80)) continue;
			let v = 0;
			for (const p of all) if (p.t === t) v = Math.max(v, p.v);
			out.push({ x: lx, y: Math.max(pad.top + 10, y(v) - 8), text: `+${Math.round(r.gain)}%` });
			lastX = lx;
		}
		return out;
	});

	function timeLabel(t: number) {
		return new Date(t).toLocaleString(undefined, {
			weekday: 'short',
			hour: 'numeric',
			minute: '2-digit'
		});
	}

	function onMove(e: PointerEvent) {
		if (!svg) return;
		const px = e.clientX - svg.getBoundingClientRect().left;
		const t = t0 + ((px - pad.left) / plotW) * (t1 - t0);
		hoverT = Math.max(t0, Math.min(t1, t));
	}

	const hover = $derived.by(() => {
		if (hoverT === null) return null;
		const t = hoverT;
		const future = t > tNow;
		const rows = lines
			.map((l) => ({ line: l, v: valueAt(future ? l.projected : l.recorded, t) }))
			.filter((r): r is { line: Line; v: number } => r.v !== null);
		return { t, future, rows };
	});
	const tooltipLeft = $derived(
		hoverT === null ? 0 : Math.min(width - 208, Math.max(0, x(hoverT) + 12))
	);
</script>

<div class="relative" bind:clientWidth={width}>
	<svg bind:this={svg} {width} {height} role="img" aria-label="{data.name} capacity over time">
		<defs>
			<clipPath id="{uid}-plot">
				<rect x={pad.left} y={pad.top - 4} width={plotW} height={plotH + 4} />
			</clipPath>
			<linearGradient id="{uid}-fill" x1="0" x2="0" y1="0" y2="1">
				<stop offset="0%" stop-color={color} stop-opacity="0.22" />
				<stop offset="100%" stop-color={color} stop-opacity="0" />
			</linearGradient>
			<linearGradient id="{uid}-fill-proj" x1="0" x2="0" y1="0" y2="1">
				<stop offset="0%" stop-color={color} stop-opacity="0.1" />
				<stop offset="100%" stop-color={color} stop-opacity="0" />
			</linearGradient>
		</defs>

		{#each days as d (d.start)}
			{#if d.start > t0}
				<line
					x1={x(d.start)}
					x2={x(d.start)}
					y1={pad.top}
					y2={base}
					stroke="var(--viz-grid)"
					stroke-width="1"
				/>
			{/if}
			{#if d.label}
				<text
					x={x(d.at)}
					y={height - 8}
					text-anchor="middle"
					class={[
						'text-[11px]',
						win === 'session' ? 'fill-foreground font-medium' : 'fill-muted-foreground'
					]}>{d.label}</text
				>
			{/if}
		{/each}
		{#each hours as h (h)}
			<line
				x1={x(h)}
				x2={x(h)}
				y1={pad.top}
				y2={base}
				stroke="var(--viz-grid)"
				stroke-width="1"
				stroke-dasharray="2 4"
			/>
			<text x={x(h)} y={height - 8} text-anchor="middle" class="fill-muted-foreground text-[11px]"
				>{new Date(h).toLocaleTimeString(undefined, { hour: 'numeric' })}</text
			>
		{/each}
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
				class="fill-muted-foreground text-[11px] tabular-nums">{t}%</text
			>
		{/each}

		<g clip-path="url(#{uid}-plot)">
			{#if !accounts}
				<path d={area(lines[0].recorded)} fill="url(#{uid}-fill)" />
				<path d={area(lines[0].projected)} fill="url(#{uid}-fill-proj)" />
			{/if}
			{#each lines as l (l.id)}
				<path
					d={path(l.recorded)}
					fill="none"
					stroke={l.color}
					stroke-width="2"
					stroke-linejoin="round"
					stroke-linecap="round"
				/>
				<path
					d={path(l.projected)}
					fill="none"
					stroke={l.color}
					stroke-width="2"
					stroke-dasharray="5 4"
					stroke-linejoin="round"
				/>
			{/each}
		</g>

		{#if tNow >= t0 && tNow <= t1}
			<line
				x1={x(tNow)}
				x2={x(tNow)}
				y1={pad.top - 4}
				y2={base}
				stroke="var(--viz-muted)"
				stroke-width="1"
			/>
			<text
				x={x(tNow)}
				y={pad.top - 10}
				text-anchor="middle"
				class="fill-muted-foreground text-[11px]">Now</text
			>
		{/if}

		{#each resetLabels as r (r.x)}
			<text
				x={r.x}
				y={r.y}
				text-anchor="middle"
				class="fill-muted-foreground text-[11px] tabular-nums">{r.text}</text
			>
		{/each}

		{#if emptyAt !== null}
			<circle
				cx={x(emptyAt)}
				cy={y(0)}
				r="5"
				fill="var(--card)"
				stroke="var(--foreground)"
				stroke-width="2"
			/>
			<text
				x={Math.min(width - pad.right, Math.max(pad.left, x(emptyAt)))}
				y={y(0) - 12}
				text-anchor={x(emptyAt) > width - 120
					? 'end'
					: x(emptyAt) < pad.left + 120
						? 'start'
						: 'middle'}
				class="fill-foreground text-[11px] font-medium">Empty {timeLabel(emptyAt)}</text
			>
		{/if}

		{#if hover}
			<line
				x1={x(hover.t)}
				x2={x(hover.t)}
				y1={pad.top}
				y2={base}
				stroke="var(--viz-axis)"
				stroke-width="1"
			/>
			{#each hover.rows as r (r.line.id)}
				<circle
					cx={x(hover.t)}
					cy={y(r.v)}
					r="4"
					fill={r.line.color}
					stroke="var(--card)"
					stroke-width="2"
				/>
			{/each}
		{/if}

		<!-- svelte-ignore a11y_no_static_element_interactions -->
		<rect
			x={pad.left}
			y={pad.top}
			width={plotW}
			height={plotH}
			fill="transparent"
			onpointermove={onMove}
			onpointerleave={() => (hoverT = null)}
		/>
	</svg>
	{#if hover && hover.rows.length}
		<div
			class="pointer-events-none absolute top-0 w-52 rounded-md border bg-popover px-3 py-2 text-xs shadow-md"
			style="left: {tooltipLeft}px"
		>
			<p class="text-muted-foreground">
				{timeLabel(hover.t)}{hover.future ? ' · projected' : ''}
			</p>
			{#each hover.rows as r (r.line.id)}
				<div class="mt-1 flex items-center gap-2">
					<span class="size-2 shrink-0 rounded-full" style="background: {r.line.color}"></span>
					{#if accounts}
						<PrivateText class="min-w-0 flex-1 truncate">{r.line.name}</PrivateText>
					{:else}
						<span class="flex-1">{r.line.name}</span>
					{/if}
					<span class="font-medium tabular-nums">{r.v.toFixed(r.v < 10 ? 1 : 0)}%</span>
				</div>
			{/each}
		</div>
	{/if}
</div>
