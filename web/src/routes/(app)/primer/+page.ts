import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings, serverTimezone }, { models }] = await Promise.all([
		admin.settings(fetch),
		admin.models(fetch)
	]);
	return { settings, models, serverTimezone: serverTimezone ?? '' };
};
