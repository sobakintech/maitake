import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings, outboundProxy, serverTimezone }, { updates }, { models }] = await Promise.all([
		admin.settings(fetch),
		admin.updates(fetch),
		admin.models(fetch)
	]);
	return { settings, outboundProxy, serverTimezone: serverTimezone ?? '', updates, models };
};
