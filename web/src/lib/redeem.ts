import type { RedeemOverride } from './types.js';

const numeric = [
	'minWaitMinutes',
	'reserve',
	'gapMinutes',
	'maxPerDay',
	'expiringHours',
	'expiringMinUsedPercent'
] as const;

// Number inputs hand back strings; coerce the numeric policy fields in place.
export function numbers<T extends RedeemOverride>(p: T): T {
	for (const k of numeric) {
		if (p[k] !== undefined) p[k] = Number(p[k]) || 0;
	}
	return p;
}
