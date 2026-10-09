import type {
	ApiFormat,
	AuditEntry,
	AuditFilter,
	AuditPayload,
	UsageNames,
	CatalogModel,
	KeyRules,
	Route,
	RoutingSettings,
	Account,
	AccountPatch,
	CodexResetsSnapshot,
	DisplayProfile,
	ProfileConfig,
	Provider,
	QuotaView,
	RedeemResult,
	Settings,
	UsageRange,
	UsageResponse
} from './types.js';

export type * from './types.js';

export type Admin = { id: number; username: string; createdAt: string };

export type Session = { setupRequired: boolean; admin: Admin | null };

export type User = {
	id: number;
	name: string;
	note: string;
	disabled: boolean;
	createdAt: string;
	keyCount: number;
};

export type ApiKey = {
	id: number;
	userId: number | null;
	adminId: number | null;
	name: string;
	prefix: string;
	disabled: boolean;
	createdAt: string;
	expiresAt: string | null;
	lastUsedAt: string | null;
	allowedModels: string[];
	rpmLimit: number;
	displayProfileId: number | null;
	// The key's own quota page view; it overrides the profile.
	displayConfig: ProfileConfig | null;
	modelRules: KeyRules | null;
	copyable: boolean;
	// Overrides the global audit log switch for this key.
	auditMode: '' | 'on' | 'off';
};

export class ApiError extends Error {
	constructor(
		public status: number,
		message: string
	) {
		super(message);
	}
}

async function request<T>(
	method: string,
	path: string,
	body?: unknown,
	f: typeof fetch = fetch
): Promise<T> {
	const res = await f(`/api${path}`, {
		method,
		headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
		body: body === undefined ? undefined : JSON.stringify(body)
	});
	if (res.status === 401 && path.startsWith('/admin/')) {
		// Session expired mid-use: full reload so the root load re-reads the session and redirects to login.
		window.location.assign('/login');
	}
	if (res.status === 204) return undefined as T;
	const data = await res.json().catch(() => ({}));
	if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText);
	return data as T;
}

export const api = {
	session: (f?: typeof fetch) => request<Session>('GET', '/session', undefined, f),
	setup: (token: string, username: string, password: string) =>
		request<{ admin: Admin }>('POST', '/setup', { token, username, password }),
	login: (username: string, password: string) =>
		request<{ admin: Admin }>('POST', '/login', { username, password }),
	logout: () => request<void>('POST', '/logout'),

	users: (f?: typeof fetch) => request<{ users: User[] }>('GET', '/admin/users', undefined, f),
	user: (id: number, f?: typeof fetch) =>
		request<{ user: User; keys: ApiKey[] }>('GET', `/admin/users/${id}`, undefined, f),
	createUser: (name: string, note: string) =>
		request<{ user: User }>('POST', '/admin/users', { name, note }),
	updateUser: (id: number, patch: Partial<Pick<User, 'name' | 'note' | 'disabled'>>) =>
		request<{ user: User }>('PATCH', `/admin/users/${id}`, patch),
	deleteUser: (id: number) => request<void>('DELETE', `/admin/users/${id}`),

	createKey: (userId: number, name: string, expiresAt: string | null) =>
		request<{ key: ApiKey; secret: string }>('POST', `/admin/users/${userId}/keys`, {
			name,
			expiresAt
		}),
	updateKey: (
		id: number,
		patch: Partial<
			Pick<
				ApiKey,
				| 'name'
				| 'disabled'
				| 'allowedModels'
				| 'rpmLimit'
				| 'expiresAt'
				| 'modelRules'
				| 'auditMode'
			>
		> & {
			displayProfileId?: number;
			displayConfig?: ProfileConfig | null;
		}
	) => request<{ key: ApiKey }>('PATCH', `/admin/keys/${id}`, patch),
	deleteKey: (id: number) => request<void>('DELETE', `/admin/keys/${id}`)
};

export type OwnedKey = ApiKey & { ownerType: 'user' | 'admin'; ownerName: string };

export const admin = {
	keys: (f?: typeof fetch) => request<{ keys: OwnedKey[] }>('GET', '/admin/keys', undefined, f),
	keySecret: (id: number) => request<{ secret: string }>('GET', `/admin/keys/${id}/secret`),
	rotateKey: (id: number) =>
		request<{ key: ApiKey; secret: string }>('POST', `/admin/keys/${id}/rotate`),
	clearKeyCopies: () => request<{ cleared: number }>('DELETE', '/admin/key-copies'),
	key: (id: number, f?: typeof fetch) =>
		request<{ key: OwnedKey }>('GET', `/admin/keys/${id}`, undefined, f),
	models: (f?: typeof fetch) =>
		request<{ models: CatalogModel[]; labs: { id: string; name: string }[] }>(
			'GET',
			'/admin/models',
			undefined,
			f
		),
	resolve: (body: {
		model: string;
		format: ApiFormat;
		keyId?: number;
		rules?: KeyRules;
		allowed?: string[];
		routing?: RoutingSettings;
	}) =>
		request<{ route: Route; error?: string; notAllowed?: boolean }>(
			'POST',
			'/admin/models/resolve',
			body
		),
	createKey: (body: { name: string; expiresAt: string | null; userId?: number }) =>
		request<{ key: ApiKey; secret: string }>('POST', '/admin/keys', body),

	accounts: (f?: typeof fetch) =>
		request<{ accounts: Account[] }>('GET', '/admin/accounts', undefined, f),
	startOAuth: (provider: Provider) =>
		request<{ sessionId: string; authUrl: string; redirectUri: string }>(
			'POST',
			'/admin/accounts/oauth/start',
			{ provider }
		),
	completeOAuth: (sessionId: string, callback: string) =>
		request<{ account: Account }>('POST', '/admin/accounts/oauth/complete', {
			sessionId,
			callback
		}),
	startDevice: () =>
		request<{ sessionId: string; userCode: string; verificationUrl: string; expiresAt: string }>(
			'POST',
			'/admin/accounts/device/start'
		),
	deviceStatus: (id: string) =>
		request<{ status: 'pending' | 'done' | 'error'; error?: string; account?: Account }>(
			'GET',
			`/admin/accounts/device/${id}`
		),
	cancelDevice: (id: string) => request<void>('DELETE', `/admin/accounts/device/${id}`),
	importAccount: (provider: Provider, json: string) =>
		request<{ account: Account }>('POST', '/admin/accounts/import', { provider, json }),
	updateAccount: (id: number, patch: AccountPatch) =>
		request<{ account: Account }>('PATCH', `/admin/accounts/${id}`, patch),
	deleteAccount: (id: number) => request<void>('DELETE', `/admin/accounts/${id}`),
	setWebSession: (id: number, session: string) =>
		request<{ account: Account }>('PUT', `/admin/accounts/${id}/web-session`, { session }),
	refreshAccount: (id: number) =>
		request<{ account: Account }>('POST', `/admin/accounts/${id}/refresh`),
	redeem: (id: number, creditId?: string) =>
		request<{ result: RedeemResult; account: Account }>(
			'POST',
			`/admin/accounts/${id}/redeem`,
			creditId ? { creditId } : {}
		),
	refreshAll: () =>
		request<{ refreshed: number; failed: { id: number; label: string; error: string }[] }>(
			'POST',
			'/admin/accounts/refresh-all'
		),

	usage: (
		params: { range: UsageRange; userId?: number; keyId?: number; accountId?: number },
		f?: typeof fetch
	) => {
		const q = new URLSearchParams({
			range: params.range,
			tzOffset: String(-new Date().getTimezoneOffset())
		});
		if (params.userId) q.set('userId', String(params.userId));
		if (params.keyId) q.set('keyId', String(params.keyId));
		if (params.accountId) q.set('accountId', String(params.accountId));
		return request<UsageResponse>('GET', `/admin/usage?${q}`, undefined, f);
	},

	audit: (filter: AuditFilter & { before?: number; limit?: number }, f?: typeof fetch) =>
		request<{
			entries: AuditEntry[];
			next: number;
			stats: { entries: number; bytes: number };
			names: UsageNames;
		}>('GET', `/admin/audit?${auditQuery(filter)}`, undefined, f),
	auditEntry: (id: number, f?: typeof fetch) =>
		request<{ entry: AuditEntry; payload: AuditPayload; names: UsageNames }>(
			'GET',
			`/admin/audit/${id}`,
			undefined,
			f
		),
	deleteAuditEntry: (id: number) => request<void>('DELETE', `/admin/audit/${id}`),
	clearAudit: (filter: AuditFilter) =>
		request<{ deleted: number }>('DELETE', `/admin/audit?${auditQuery(filter)}`),

	settings: (f?: typeof fetch) =>
		request<{ settings: Settings; outboundProxy: boolean }>('GET', '/admin/settings', undefined, f),
	saveSettings: (settings: Settings) =>
		request<{ settings: Settings }>('PUT', '/admin/settings', settings),
	profiles: (f?: typeof fetch) =>
		request<{ profiles: DisplayProfile[]; defaults: ProfileConfig }>(
			'GET',
			'/admin/profiles',
			undefined,
			f
		),
	createProfile: (name: string, config: ProfileConfig) =>
		request<{ profile: DisplayProfile }>('POST', '/admin/profiles', { name, config }),
	updateProfile: (id: number, patch: { name?: string; config?: ProfileConfig }) =>
		request<{ profile: DisplayProfile }>('PATCH', `/admin/profiles/${id}`, patch),
	deleteProfile: (id: number) => request<void>('DELETE', `/admin/profiles/${id}`),
	previewProfile: (config: ProfileConfig) =>
		request<{ view: QuotaView }>('POST', '/admin/profiles/preview', { config }),
	codexResets: (f?: typeof fetch) =>
		request<{ codexResets: CodexResetsSnapshot }>('GET', '/admin/codex-resets', undefined, f),
	refreshCodexResets: () => request<void>('POST', '/admin/codex-resets/refresh')
};

function auditQuery(filter: Record<string, string | number | undefined>): URLSearchParams {
	const q = new URLSearchParams();
	for (const [k, v] of Object.entries(filter)) if (v) q.set(k, String(v));
	return q;
}

export async function publicQuota(key: string | null): Promise<QuotaView> {
	const res = await fetch('/api/quota', {
		headers: key ? { Authorization: `Bearer ${key}` } : undefined
	});
	const data = await res.json().catch(() => ({}));
	if (!res.ok) throw new ApiError(res.status, data.error ?? res.statusText);
	return data.view as QuotaView;
}

export async function quotaMeta(): Promise<{
	enabled: boolean;
	access: 'key' | 'public';
	title: string;
}> {
	const res = await fetch('/api/quota/meta');
	return res.json();
}

export function errorMessage(e: unknown): string {
	return e instanceof Error ? e.message : String(e);
}
