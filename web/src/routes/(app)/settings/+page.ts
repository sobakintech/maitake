import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const { settings, outboundProxy } = await admin.settings(fetch);
	return { settings, outboundProxy };
};
