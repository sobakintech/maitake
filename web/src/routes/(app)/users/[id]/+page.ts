import { admin, api, ApiError } from '#lib/api.js';
import { error } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, fetch }) => {
	const id = Number(params.id);
	try {
		const [user, { profiles }] = await Promise.all([api.user(id, fetch), admin.profiles(fetch)]);
		return { ...user, profiles };
	} catch (e) {
		if (e instanceof ApiError && (e.status === 404 || e.status === 400))
			error(404, 'User not found');
		throw e;
	}
};
