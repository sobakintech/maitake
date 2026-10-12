import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings, outboundProxy }, { updates }] = await Promise.all([
		admin.settings(fetch),
		admin.updates(fetch)
	]);
	return { settings, outboundProxy, updates };
};
