import { admin, type CapacityBurn, type CapacityUsage, type CapacityWindow } from '#lib/api.js';
import type { PageLoad } from './$types';

const DAY = 86_400_000;

// span is the visible range: a fortnight starting three days back, or three days around now
// for the session window. offset pages a week (or a day) at a time.
function span(window: CapacityWindow, offset: number) {
	const from = new Date();
	if (window === 'session') {
		from.setMinutes(0, 0, 0);
		from.setTime(from.getTime() - DAY);
		from.setDate(from.getDate() + offset);
		return { from, to: new Date(from.getTime() + 3 * DAY) };
	}
	from.setHours(0, 0, 0, 0);
	from.setDate(from.getDate() - 3 + offset * 7);
	const to = new Date(from);
	to.setDate(to.getDate() + 14);
	return { from, to };
}

export const load: PageLoad = async ({ fetch, url }) => {
	const p = url.searchParams;
	const window: CapacityWindow = p.get('window') === 'session' ? 'session' : 'weekly';
	const burn = (
		['1h', '3h', '6h'].includes(p.get('burn') ?? '') ? p.get('burn') : '3h'
	) as CapacityBurn;
	const usage: CapacityUsage = p.get('usage') === 'average' ? 'average' : 'recent';
	const offset = Number(p.get('offset')) || 0;
	const { from, to } = span(window, offset);
	const capacity = await admin.capacity({ window, burn, usage, from, to }, fetch);
	return { capacity, window, burn, usage, offset };
};
