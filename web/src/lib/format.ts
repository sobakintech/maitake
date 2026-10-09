const rtf = new Intl.RelativeTimeFormat(undefined, { numeric: 'auto' });

const units: [Intl.RelativeTimeFormatUnit, number][] = [
	['year', 365 * 24 * 3600],
	['month', 30 * 24 * 3600],
	['week', 7 * 24 * 3600],
	['day', 24 * 3600],
	['hour', 3600],
	['minute', 60]
];

export function relativeTime(iso: string | null, fallback = 'never'): string {
	if (!iso) return fallback;
	const diff = (new Date(iso).getTime() - Date.now()) / 1000;
	for (const [unit, secs] of units) {
		if (Math.abs(diff) >= secs) return rtf.format(Math.round(diff / secs), unit);
	}
	return 'just now';
}

export function dateTime(iso: string): string {
	return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' });
}

const compactFmt = new Intl.NumberFormat(undefined, {
	notation: 'compact',
	maximumFractionDigits: 1
});
const intFmt = new Intl.NumberFormat();

export function compact(n: number): string {
	return Math.abs(n) < 10_000 ? intFmt.format(n) : compactFmt.format(n);
}

export function integer(n: number): string {
	return intFmt.format(n);
}

// countdown renders the time until iso, e.g. "2h 15m" or "3d 4h".
export function countdown(iso: string | null | undefined): string {
	if (!iso) return '';
	let secs = Math.max(0, Math.round((new Date(iso).getTime() - Date.now()) / 1000));
	const d = Math.floor(secs / 86400);
	secs -= d * 86400;
	const h = Math.floor(secs / 3600);
	secs -= h * 3600;
	const m = Math.floor(secs / 60);
	if (d > 0) return `${d}d ${h}h`;
	if (h > 0) return `${h}h ${m}m`;
	if (m > 0) return `${m}m`;
	return 'now';
}

export function shortDateTime(iso: string): string {
	return new Date(iso).toLocaleString(undefined, {
		month: 'short',
		day: 'numeric',
		hour: 'numeric',
		minute: '2-digit'
	});
}

export function duration(ms: number): string {
	if (ms < 1000) return `${ms} ms`;
	if (ms < 60_000) return `${(ms / 1000).toFixed(1)} s`;
	return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`;
}

export const providerName: Record<string, string> = { claude: 'Claude', codex: 'Codex' };

export function money(n: number, currency = 'USD'): string {
	return new Intl.NumberFormat(undefined, {
		style: 'currency',
		currency,
		maximumFractionDigits: Number.isInteger(n) ? 0 : 2
	}).format(n);
}

export function bytes(n: number): string {
	if (n < 1024) return `${n} B`;
	if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
	if (n < 1024 * 1024 * 1024) return `${(n / 1024 / 1024).toFixed(1)} MB`;
	return `${(n / 1024 / 1024 / 1024).toFixed(2)} GB`;
}
