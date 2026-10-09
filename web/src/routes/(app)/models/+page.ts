import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings }, { models, labs }, { accounts }] = await Promise.all([
		admin.settings(fetch),
		admin.models(fetch),
		admin.accounts(fetch)
	]);
	return { settings, models, labs, accounts };
};
