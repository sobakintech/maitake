import type { Account, ResetsSnapshot, Provider } from './types.js';
import type { OwnedKey } from './api.js';

export type EventKind = 'limit' | 'banked' | 'subscription' | 'key' | 'cooldown' | 'codex';

export type TimelineEvent = {
	id: string;
	at: Date;
	kind: EventKind;
	title: string;
	detail?: string;
	href?: string;
	provider?: Provider;
};

export const kinds: { id: EventKind; label: string }[] = [
	{ id: 'limit', label: 'Limit resets' },
	{ id: 'banked', label: 'Banked reset expiries' },
	{ id: 'subscription', label: 'Subscriptions' },
	{ id: 'key', label: 'Key expiries' },
	{ id: 'cooldown', label: 'Cooldowns ending' },
	{ id: 'codex', label: 'Announced Codex resets' }
];

export function buildEvents(
	accounts: Account[],
	keys: OwnedKey[],
	resets: ResetsSnapshot | null,
	now = new Date()
): TimelineEvent[] {
	const out: TimelineEvent[] = [];
	const future = (iso?: string | null) => !!iso && new Date(iso) > now;

	for (const a of accounts) {
		if (a.disabled) continue;
		const href = `/accounts?provider=${a.provider}`;
		for (const w of a.quota?.windows ?? []) {
			if (!future(w.resetsAt)) continue;
			out.push({
				id: `limit-${a.id}-${w.id}`,
				at: new Date(w.resetsAt!),
				kind: 'limit',
				title: `${w.label} limit resets`,
				detail: `${a.label} · ${Math.round(100 - w.usedPercent)}% left now`,
				href,
				provider: a.provider
			});
		}
		for (const c of a.quota?.banked?.credits ?? []) {
			if (!future(c.expiresAt)) continue;
			out.push({
				id: `banked-${a.id}-${c.id}`,
				at: new Date(c.expiresAt!),
				kind: 'banked',
				title: 'Banked reset expires',
				detail: `${a.label}${c.title ? ` · ${c.title}` : ''}`,
				href,
				provider: a.provider
			});
		}
		const sub = a.subscription;
		if (sub?.until) {
			const ended = new Date(sub.until) <= now;
			if (!ended) {
				out.push({
					id: `sub-${a.id}`,
					at: new Date(sub.until),
					kind: 'subscription',
					title: 'Subscription renews or ends',
					detail: `${a.label} · ${a.quota?.plan || a.plan}`,
					href,
					provider: a.provider
				});
			}
		}
		if (a.status === 'cooldown' && future(a.cooldownUntil)) {
			out.push({
				id: `cool-${a.id}`,
				at: new Date(a.cooldownUntil!),
				kind: 'cooldown',
				title: 'Cooldown ends',
				detail: a.label,
				href,
				provider: a.provider
			});
		}
	}

	for (const k of keys) {
		if (k.disabled || !future(k.expiresAt)) continue;
		out.push({
			id: `key-${k.id}`,
			at: new Date(k.expiresAt!),
			kind: 'key',
			title: `Key “${k.name || k.prefix}” expires`,
			detail: `Owned by ${k.ownerName}`,
			href: `/keys/${k.id}`
		});
	}

	const scheduled = resets?.status?.scheduled_reset;
	if (scheduled?.scheduled_for && future(scheduled.scheduled_for)) {
		out.push({
			id: `codex-${scheduled.id}`,
			at: new Date(scheduled.scheduled_for),
			kind: 'codex',
			title: `Announced ${scheduled.reset_type} Codex reset`,
			detail: scheduled.text,
			href: '/quota-page',
			provider: 'codex'
		});
	}

	return out.sort((a, b) => a.at.getTime() - b.at.getTime());
}

export function dayKey(d: Date) {
	return `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}`;
}
