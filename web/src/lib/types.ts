export type Provider = 'claude' | 'codex';

export type QuotaWindow = {
	id: string;
	kind: 'session' | 'weekly' | 'model_weekly' | 'other';
	label: string;
	usedPercent: number;
	resetsAt?: string;
	windowMinutes?: number;
	limited?: boolean;
	detail?: string;
	amount?: Amount;
};

export type Amount = { currency: string; limit: number; used: number };

export type BankedCredit = {
	id: string;
	type?: string;
	title?: string;
	grantedAt?: string;
	expiresAt?: string;
};

export type QuotaSnapshot = {
	plan?: string;
	windows: QuotaWindow[];
	banked?: { available: number; credits?: BankedCredit[] };
	extra?: {
		enabled: boolean;
		unlimited?: boolean;
		usedPercent?: number;
		used?: number;
		limit?: number;
		balance?: string;
	};
	updatedAt: string;
	source?: string;
	error?: string;
	fetchedAt?: string;
};

export type Account = {
	id: number;
	provider: Provider;
	label: string;
	alias: string;
	email: string;
	externalId: string;
	plan: string;
	disabled: boolean;
	priority: number;
	status: 'ok' | 'cooldown' | 'error' | 'reauth';
	statusMessage: string;
	cooldownUntil: string | null;
	quotaUpdatedAt: string | null;
	lastUsedAt: string | null;
	createdAt: string;
	updatedAt: string;
	quota: QuotaSnapshot | null;
	tokenExpiresAt: string | null;
	lastRefresh: string | null;
	subscription: Subscription | null;
	hasWebSession: boolean;
	redeem: RedeemOverride;
	redeemEffective: RedeemPolicy;
	modelCooldowns?: Record<string, string>;
};

export type Subscription = {
	status?: string;
	since?: string;
	until?: string;
	billing?: string;
	checkedAt: string;
	webError?: string;
};

export type AccountPatch = Partial<Pick<Account, 'label' | 'alias' | 'disabled' | 'priority'>> & {
	redeem?: RedeemOverride;
};

export type RedeemPolicy = {
	enabled: boolean;
	trigger: 'any' | 'weekly' | 'session';
	minWaitMinutes: number;
	reserve: number;
	order: 'soonest_expiry' | 'latest_expiry';
	gapMinutes: number;
	maxPerDay: number;
	expiringHours: number;
	expiringMinUsedPercent: number;
};

// An account's own rules; missing fields use the pool default.
export type RedeemOverride = Partial<RedeemPolicy>;

export type UsageTotals = {
	requests: number;
	errors: number;
	inputTokens: number;
	outputTokens: number;
	cacheReadTokens: number;
	cacheWriteTokens: number;
	reasoningTokens: number;
	avgTtftMs: number;
};

export type UsageBucket = UsageTotals & { key: string; name?: string };

export type UsageEvent = {
	time: string;
	userId: number;
	keyId: number;
	accountId: number;
	provider: string;
	model: string;
	endpoint: string;
	status: number;
	stream: boolean;
	inputTokens: number;
	outputTokens: number;
	cacheReadTokens: number;
	cacheWriteTokens: number;
	reasoningTokens: number;
	durationMs: number;
	error: string;
	requestedModel: string;
	serviceTier: string;
	servedTier: string;
	ttftMs: number;
};

export type UsageRange = '1h' | '24h' | '7d' | '30d' | '90d';

export type UsageResponse = {
	range: UsageRange;
	since: string;
	until: string;
	totals: UsageTotals;
	series: UsageBucket[];
	seriesBy: 'hour' | 'day';
	byModel: UsageBucket[];
	byProvider: UsageBucket[];
	byUser: UsageBucket[];
	byKey: UsageBucket[];
	byAccount: UsageBucket[];
	names: UsageNames;
};

// Labels for ids in usage and audit records. In `user`, admin keys appear as "k<key id>".
export type UsageNames = {
	user: Record<string, string>;
	key: Record<string, string>;
	account: Record<string, string>;
};

export type PageSettings = {
	enabled: boolean;
	access: 'key' | 'public';
	title: string;
	notice: string;
	// Shown without a key when access is public.
	publicProfileId: number;
};

export type Settings = {
	quotaPage: PageSettings;
	pool: PoolSettings;
	codexResets: { enabled: boolean; intervalMinutes: number };
	usageRetentionDays: number;
	routing: RoutingSettings;
	keys: { storeCopies: boolean };
	claude: CloakSettings;
};

export type PoolSettings = {
	quotaPollMinutes: number;
	redeem: RedeemPolicy;
	maxRetries: number;
	bootstrapSeconds: number;
	keepaliveSeconds: number;
	errorRules: ErrorRule[];
};

export type ErrorRule = {
	id: string;
	enabled: boolean;
	provider: '' | Provider;
	status: number;
	match: string;
	action: 'stop' | 'retry';
	cooldown: '' | 'none' | 'account' | 'model';
	cooldownSeconds: number;
	note: string;
};

export type CloakSettings = {
	toolAliases: boolean;
	cacheBreakpoints: boolean;
	longCacheTTL: boolean;
	sensitiveWords: string[];
	tlsFingerprint: boolean;
};

export type ProfileConfig = {
	providers: Provider[];
	mode: 'pooled' | 'accounts' | 'both';
	poolAggregate: 'average' | 'min' | 'max' | 'total';
	accountNaming: 'anonymous' | 'label' | 'alias' | 'hidden';
	hiddenAccounts: number[];
	windows: { session: boolean; weekly: boolean; modelWeekly: boolean; other: boolean };
	extra: boolean;
	showPlan: boolean;
	showStatus: boolean;
	showAmounts: boolean;
	numberFormat: 'remaining' | 'used' | 'bar' | 'level';
	resetFormat: 'both' | 'relative' | 'exact' | 'hidden';
	showUpdatedAt: boolean;
	banked: 'count' | 'hidden' | 'count_expiry';
	bankedMode: 'pooled' | 'accounts';
	codexResets: { latest: boolean; scheduled: boolean; forecast: boolean; stats: boolean };
};

export type DisplayProfile = {
	id: number;
	name: string;
	config: ProfileConfig;
	createdAt: string;
	updatedAt: string;
	keyCount: number;
	isPublic: boolean;
};

export type Level = 'plenty' | 'ok' | 'low' | 'out';

export type WindowView = {
	id: string;
	kind: string;
	label: string;
	value?: number;
	unit?: 'percent_remaining' | 'percent_used' | 'accounts';
	level: Level;
	resetsAt?: string;
	amount?: Amount;
};

export type BankedView = { available: number; expiries?: string[] };

export type EntryView = {
	name?: string;
	plan?: string;
	status?: string;
	accountCount?: number;
	windows: WindowView[];
	banked?: BankedView;
	extra?: { enabled: boolean; unlimited?: boolean; usedPercent?: number };
	updatedAt?: string;
};

export type ResetSource = { type: string; author?: string; url?: string };

export type CodexReset = {
	id: string;
	status?: string;
	reset_type: 'regular' | 'banked';
	announced_at: string;
	scheduled_for?: string | null;
	text: string;
	source: ResetSource;
};

export type CodexWatch = {
	level: 'elevated' | 'strong';
	reset_chance_percent: number | null;
	forecast_window: string;
	observed_at: string;
	expires_at: string;
	text: string;
	source: ResetSource;
};

export type CodexResetStats = {
	total: number;
	last_reset_at: string | null;
	days_since_last: number | null;
	avg_interval_days: number | null;
};

export type ProviderView = {
	id: Provider;
	name: string;
	pool?: EntryView;
	accounts?: EntryView[];
	banked?: BankedView;
};

export type QuotaView = {
	title: string;
	notice: string;
	numberFormat: ProfileConfig['numberFormat'];
	resetFormat: ProfileConfig['resetFormat'];
	showUpdatedAt: boolean;
	providers: ProviderView[];
	codexResets?: {
		latest?: CodexReset;
		scheduled?: CodexReset;
		forecast?: CodexWatch;
		stats?: CodexResetStats;
	};
	generatedAt: string;
};

export type CodexResetsSnapshot = {
	status: {
		latest_reset: CodexReset | null;
		scheduled_reset: CodexReset | null;
		active_watch: CodexWatch | null;
		stats: CodexResetStats;
	} | null;
	fetchedAt: string | null;
	error?: string;
};

export type RedeemResult = { code: string; windowsReset?: string[] };

export type Strategy = 'round_robin' | 'fill' | 'most_remaining' | 'random';

export type ApiFormat = 'anthropic' | 'openai' | 'responses' | 'openrouter';

export type ModelOverride = {
	disabled: boolean;
	name: string;
	accounts: number[] | null;
	strategy: Strategy | '';
	effort: string;
};

export type CustomModel = { id: string; provider: Provider; name: string; contextLength: number };

export type MappingRule = {
	id: string;
	enabled: boolean;
	match: string;
	target: string;
	effort: string;
	formats: ApiFormat[] | null;
	note: string;
};

export type KeyRules = { rules: MappingRule[] | null; ignoreGlobal: boolean };

export type RoutingSettings = {
	strategy: Strategy;
	providerStrategy: Partial<Record<Provider, Strategy>>;
	affinity: 'session' | 'key' | 'off';
	stickyMinutes: number;
	modelList: 'bare' | 'canonical' | 'both';
	unknownModels: boolean;
	reasoningField: 'both' | 'reasoning' | 'reasoning_content';
	models: Record<string, ModelOverride>;
	custom: CustomModel[];
	mappings: MappingRule[];
	payloadRules: PayloadRule[];
	images: ImageSettings;
};

export type PayloadRule = {
	id: string;
	enabled: boolean;
	match: string;
	provider: '' | Provider;
	formats: ApiFormat[] | null;
	mode: 'default' | 'override' | 'remove';
	path: string;
	value: unknown;
	note: string;
};

export type ImageSettings = {
	enabled: boolean;
	model: string;
	toolModel: string;
	autoTool: boolean;
};

export type CatalogModel = {
	id: string;
	canonical: string;
	lab: string;
	provider: Provider;
	name: string;
	contextLength: number;
	maxOutput: number;
	efforts?: string[];
	custom: boolean;
	disabled: boolean;
	override: ModelOverride;
	strategy: Strategy;
	accounts: number;
};

export type Route = {
	requested: string;
	mapped?: string;
	rule?: 'key' | 'global';
	model: { id: string; canonical: string; lab: string; provider: Provider; name: string };
	effort?: string;
	defaultEffort?: string;
	accounts?: number[];
	strategy: Strategy;
	inCatalog: boolean;
};
