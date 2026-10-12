import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings, serverTimezone }, { accounts }, { models }] = await Promise.all([
		admin.settings(fetch),
		admin.accounts(fetch),
		admin.models(fetch)
	]);
	return { settings, accounts, models, serverTimezone: serverTimezone ?? '' };
};
