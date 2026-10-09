import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ accounts }, { keys }, { codexResets }] = await Promise.all([
		admin.accounts(fetch),
		admin.keys(fetch),
		admin.codexResets(fetch)
	]);
	return { accounts, keys, codexResets };
};
