import { admin, ApiError } from '#lib/api.js';
import { error } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	try {
		return await admin.auditEntry(Number(params.id), fetch);
	} catch (e) {
		if (e instanceof ApiError && (e.status === 404 || e.status === 400))
			error(404, 'Entry not found');
		throw e;
	}
};
