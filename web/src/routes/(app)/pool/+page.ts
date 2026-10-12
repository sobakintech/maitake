import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const { settings } = await admin.settings(fetch);
	return { settings };
};
