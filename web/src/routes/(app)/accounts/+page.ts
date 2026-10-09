import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ accounts }, { settings }] = await Promise.all([
		admin.accounts(fetch),
		admin.settings(fetch)
	]);
	return { accounts, redeemDefault: settings.pool.redeem.enabled };
};
