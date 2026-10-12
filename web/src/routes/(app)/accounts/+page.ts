import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ accounts }, { settings, serverTimezone }, { models }] = await Promise.all([
		admin.accounts(fetch),
		admin.settings(fetch),
		admin.models(fetch)
	]);
	return { accounts, settings, models, serverTimezone: serverTimezone ?? '' };
};
