import { api } from '#lib/api.js';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch }) => {
	const { users } = await api.users(fetch);
	return { users };
};
