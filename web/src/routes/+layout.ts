import { api } from '#lib/api.js';
import type { LayoutLoad } from './$types';

export const ssr = false;
export const prerender = false;

export const load: LayoutLoad = async ({ fetch }) => {
	return { session: await api.session(fetch) };
};
