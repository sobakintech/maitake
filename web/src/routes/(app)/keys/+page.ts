import { admin, api } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ keys }, { users }, { profiles }, usage] = await Promise.all([
		admin.keys(fetch),
		api.users(fetch),
		admin.profiles(fetch),
		admin.usage({ range: '7d' }, fetch)
	]);
	return { keys, users, profiles, keyUsage: usage.byKey };
};
