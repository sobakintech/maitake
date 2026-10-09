import { admin, api, type AuditFilter, type UsageRange } from '#lib/api.js';
import type { PageLoad } from './$types';

const ranges: UsageRange[] = ['1h', '24h', '7d', '30d', '90d'];

export const load: PageLoad = async ({ fetch, url }) => {
	const p = url.searchParams;
	const range = p.get('range') as UsageRange;
	const filter: AuditFilter = {
		range: ranges.includes(range) ? range : undefined,
		userId: Number(p.get('userId')) || undefined,
		keyId: Number(p.get('keyId')) || undefined,
		accountId: Number(p.get('accountId')) || undefined,
		model: p.get('model') ?? undefined,
		status: p.get('status') ?? undefined,
		q: p.get('q') ?? undefined
	};
	const [list, { users }, { keys }, { accounts }, { settings }] = await Promise.all([
		admin.audit(filter, fetch),
		api.users(fetch),
		admin.keys(fetch),
		admin.accounts(fetch),
		admin.settings(fetch)
	]);
	return { ...list, filter, users, keys, accounts, auditEnabled: settings.audit.enabled };
};
