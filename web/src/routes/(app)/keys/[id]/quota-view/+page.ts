import { admin, ApiError } from '#lib/api.js';
import { error } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	try {
		const [{ key }, { accounts }, { defaults }] = await Promise.all([
			admin.key(Number(params.id), fetch),
			admin.accounts(fetch),
			admin.profiles(fetch)
		]);
		return { key, accounts, blankView: defaults };
	} catch (e) {
		if (e instanceof ApiError && (e.status === 404 || e.status === 400))
			error(404, 'Key not found');
		throw e;
	}
};
