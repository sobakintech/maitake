import { admin, api, ApiError } from '#lib/api.js';
import { error } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ params, fetch }) => {
	const id = Number(params.id);
	try {
		const [user, { profiles }, usage] = await Promise.all([
			api.user(id, fetch),
			admin.profiles(fetch),
			admin.usage({ range: '7d', userId: id }, fetch)
		]);
		return { ...user, profiles, keyUsage: usage.byKey };
	} catch (e) {
		if (e instanceof ApiError && (e.status === 404 || e.status === 400))
			error(404, 'User not found');
		throw e;
	}
};
