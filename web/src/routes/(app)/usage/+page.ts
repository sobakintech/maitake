import { redirect } from '@sveltejs/kit';
import type { PageLoad } from './$types';

// Usage lives on the overview now.
export const load: PageLoad = ({ url }) => {
	redirect(307, `/overview${url.search}`);
};
