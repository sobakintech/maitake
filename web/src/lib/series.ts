import type { UsageBucket, UsageResponse } from './types.js';

export type Metric = 'requests' | 'tokens';

function value(b: UsageBucket | undefined, metric: Metric) {
	if (!b) return 0;
	return metric === 'requests'
		? b.requests
		: b.inputTokens + b.outputTokens + b.cacheReadTokens + b.cacheWriteTokens;
}

// fillSeries returns one point per hour/day across the range, including empty buckets.
export function fillSeries(u: UsageResponse, metric: Metric) {
	const byKey = new Map(u.series.map((b) => [Number(b.key), b]));
	const hourly = u.seriesBy === 'hour';
	const step = hourly ? 3_600_000 : 86_400_000;
	const off = -new Date().getTimezoneOffset() * 60_000;
	const bucket = (t: number) =>
		hourly ? Math.floor(t / step) * step : Math.floor((t + off) / step) * step - off;
	const out: { tick: string; title: string; value: number }[] = [];
	const end = bucket(new Date(u.until).getTime());
	for (let t = bucket(new Date(u.since).getTime()); t <= end; t += step) {
		const d = new Date(t);
		out.push({
			tick: hourly
				? d.toLocaleTimeString(undefined, { hour: 'numeric' })
				: d.toLocaleDateString(undefined, { month: 'short', day: 'numeric' }),
			title: hourly
				? d.toLocaleString(undefined, { weekday: 'short', hour: 'numeric', minute: '2-digit' })
				: d.toLocaleDateString(undefined, { weekday: 'short', month: 'short', day: 'numeric' }),
			value: value(byKey.get(t), metric)
		});
	}
	return out;
}

export function totalTokens(b: UsageBucket) {
	return b.inputTokens + b.outputTokens + b.cacheReadTokens + b.cacheWriteTokens;
}
