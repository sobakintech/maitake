import { admin, ApiError } from '#lib/api.js';
import { error } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	try {
		const [{ key }, { accounts }, { settings }] = await Promise.all([
			admin.key(Number(params.id), fetch),
			admin.accounts(fetch),
			admin.settings(fetch)
		]);
		return { key, accounts, defaultView: settings.quotaPage.defaultView };
	} catch (e) {
		if (e instanceof ApiError && (e.status === 404 || e.status === 400))
			error(404, 'Key not found');
		throw e;
	}
};
