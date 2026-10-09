import { admin, api, type UsageRange } from '#lib/api.js';
import type { PageLoad } from './$types';

const ranges: UsageRange[] = ['1h', '24h', '7d', '30d', '90d'];

export const load: PageLoad = async ({ fetch, url }) => {
	const r = url.searchParams.get('range') as UsageRange;
	const range = ranges.includes(r) ? r : '7d';
	const userId = Number(url.searchParams.get('userId')) || undefined;
	const accountId = Number(url.searchParams.get('accountId')) || undefined;
	const [usage, { users }, { accounts }] = await Promise.all([
		admin.usage({ range, userId, accountId }, fetch),
		api.users(fetch),
		admin.accounts(fetch)
	]);
	return { usage, users, accounts, range, userId, accountId };
};
