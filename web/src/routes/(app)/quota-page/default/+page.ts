import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings }, { accounts }] = await Promise.all([
		admin.settings(fetch),
		admin.accounts(fetch)
	]);
	return { settings, accounts };
};
