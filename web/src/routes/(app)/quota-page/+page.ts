import { admin } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ settings }, { profiles }, { codexResets }, { claudeResets }] = await Promise.all([
		admin.settings(fetch),
		admin.profiles(fetch),
		admin.codexResets(fetch),
		admin.claudeResets(fetch)
	]);
	return { settings, profiles, codexResets, claudeResets };
};
