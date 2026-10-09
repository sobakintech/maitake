import { redirect } from '@sveltejs/kit';
import type { LayoutLoad } from './$types';

export const load: LayoutLoad = async ({ parent }) => {
	const { session } = await parent();
	if (session.setupRequired) redirect(307, '/setup');
	if (!session.admin) redirect(307, '/login');
	return { admin: session.admin };
};
