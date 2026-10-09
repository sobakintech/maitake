import { admin } from '#lib/api.js';
import { error } from '@sveltejs/kit';
import type { PageLoad } from './$types';

export const load: PageLoad = async ({ fetch, params }) => {
	const [{ profiles }, { accounts }] = await Promise.all([
		admin.profiles(fetch),
		admin.accounts(fetch)
	]);
	const profile = profiles.find((p) => p.id === Number(params.id));
	if (!profile) error(404, 'Profile not found');
	return { profile, accounts };
};
