import type { CatalogModel, PrimerRun, PrimerPolicy, PrimerSchedule, Provider } from './types.js';

export const defaultModels: Record<string, string> = {
	claude: 'haiku',
	codex: 'gpt-5.6-luna(low)'
};

export const weekdays = ['Sun', 'Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat'];

const numeric = [
	'delaySeconds',
	'verifySeconds',
	'retries',
	'retrySeconds',
	'graceMinutes'
] as const;

// Number inputs hand back strings; coerce the numeric policy fields in place.
export function primerNumbers(p: PrimerPolicy): PrimerPolicy {
	for (const k of numeric) p[k] = Number(p[k]) || 0;
	return p;
}

export function minutes(clock: string): number {
	const [h, m] = clock.split(':').map(Number);
	return (h || 0) * 60 + (m || 0);
}

export function clock(mins: number): string {
	const m = ((mins % 1440) + 1440) % 1440;
	return `${String(Math.floor(m / 60)).padStart(2, '0')}:${String(m % 60).padStart(2, '0')}`;
}

export function hours(mins: number): string {
	const h = mins / 60;
	return `${Number.isInteger(h) ? h : h.toFixed(1)} hour${h === 1 ? '' : 's'}`;
}

// pingTime is when the ping for a fresh session at `at` goes out.
export function pingTime(at: string, sessionMinutes = 300) {
	const ping = minutes(at) - sessionMinutes;
	return { ping: clock(ping), dayBefore: ping < 0 };
}

export function daysLabel(days: number[]): string {
	if (!days.length || days.length === 7) return 'every day';
	const key = [...days].sort().join('');
	if (key === '12345') return 'weekdays';
	if (key === '06') return 'weekends';
	return [...days]
		.sort()
		.map((d) => weekdays[d])
		.join(', ');
}

// overlaps finds times whose ping lands inside the session an earlier time already started.
export function overlaps(times: string[], sessionMinutes = 300): string[] {
	const out: string[] = [];
	for (const a of times)
		for (const b of times) {
			const gap = (minutes(b) - minutes(a) + 1440) % 1440;
			if (gap > 0 && gap < sessionMinutes)
				out.push(`${a} and ${b} are less than ${hours(sessionMinutes)} apart, so ${b} comes late.`);
		}
	return out;
}

const list = (xs: string[]) =>
	xs.length > 1 ? `${xs.slice(0, -1).join(', ')} and ${xs.at(-1)}` : (xs[0] ?? '');

export function describeSchedule(s: PrimerSchedule): string {
	const times = [...s.times].sort();
	return `Resets at ${list(times)}, ${daysLabel(s.days)}${s.timezone ? ` (${s.timezone})` : ''}`;
}

// describePolicy lists a policy's schedules in plain words, one line each.
export function describePolicy(p: PrimerPolicy): string[] {
	return p.schedules.filter((s) => s.times.length).map(describeSchedule);
}

export const outcomeLabel: Record<PrimerRun['outcome'], string> = {
	started: 'Started',
	sent: 'Sent',
	skipped: 'Skipped',
	failed: 'Failed'
};

export const triggerLabel: Record<PrimerRun['trigger'], string> = {
	slot: 'Scheduled',
	manual: 'Manual'
};

const lines = ['haiku', 'sonnet', 'opus', 'fable'];

export type ModelOption = { value: string; label: string; newest?: boolean };

// modelOptions are the ping models offered for a provider: Claude's lines (always the newest
// model of each), then the catalog's models; Codex models at their lowest effort.
export function modelOptions(provider: Provider, catalog: CatalogModel[]): ModelOption[] {
	const out: ModelOption[] =
		provider === 'claude'
			? lines.map((l) => ({
					value: l,
					label: `Newest ${l[0].toUpperCase()}${l.slice(1)}`,
					newest: true
				}))
			: [];
	for (const m of catalog) {
		if (m.provider !== provider || m.disabled) continue;
		const effort = provider === 'codex' ? m.efforts?.[0] : undefined;
		out.push(
			effort
				? { value: `${m.id}(${effort})`, label: `${m.name}, ${effort} effort` }
				: { value: m.id, label: m.name }
		);
	}
	return out;
}

export function modelLabel(value: string, options: ModelOption[]): string {
	return options.find((o) => o.value === value)?.label ?? value;
}

// clone deep-copies plain data; structuredClone rejects Svelte state proxies.
export const clone = <T>(v: T): T => JSON.parse(JSON.stringify(v));
