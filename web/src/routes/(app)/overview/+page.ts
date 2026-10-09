import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [usage, { accounts }] = await Promise.all([
		admin.usage({ range: '24h' }, fetch),
		admin.accounts(fetch)
	]);
	return { usage, accounts };
};
