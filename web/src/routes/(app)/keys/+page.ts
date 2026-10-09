import { admin, api } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const [{ keys }, { users }, { profiles }] = await Promise.all([
		admin.keys(fetch),
		api.users(fetch),
		admin.profiles(fetch)
	]);
	return { keys, users, profiles };
};
