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
	pace?: Pace;
};

// Where a window lands at reset if usage keeps its rate so far. Numbers are left out when the
// quota page profile hides them.
export type Pace = {
	status: 'on_track' | 'close' | 'over';
	projectedUsed?: number;
	elapsed?: number;
	runsOutAt?: string;
};

export type Amount = { currency: string; limit: number; used: number };

export type BankedCredit = {
	id: string;
	type?: string;
	title?: string;
	grantedAt?: string;
	expiresAt?: string;
	count?: number;
	clears?: string[];
	requiresLimit?: boolean;
};

export type QuotaSnapshot = {
	plan?: string;
	windows: QuotaWindow[];
	banked?: { available: number; credits?: BankedCredit[]; cooldownUntil?: string };
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
	primer: AccountPrimer;
	primerEffective: PrimerEffective;
	primerNext: PrimerPlan | null;
	primerLast: PrimerRun | null;
	primerBusy: boolean;
	modelCooldowns?: Record<string, string>;
};

export type Subscription = {
	status?: string;
	since?: string;
	until?: string;
	billing?: string;
	checkedAt: string;
	webError?: string;
	webExpired?: boolean;
};

export type AccountPatch = Partial<Pick<Account, 'label' | 'alias' | 'disabled' | 'priority'>> & {
	redeem?: RedeemOverride;
	primer?: AccountPrimer;
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

export type PrimerSchedule = {
	// Local "HH:MM" times a fresh session should be ready.
	times: string[];
	// 0 = Sunday; empty means every day.
	days: number[];
	// IANA zone; missing uses the server's.
	timezone?: string;
};

export type PrimerPolicy = {
	schedules: PrimerSchedule[];
	delaySeconds: number;
	verify: boolean;
	verifySeconds: number;
	retries: number;
	retrySeconds: number;
	graceMinutes: number;
};

// An account's primer setup; model and schedule replace the pool's when set.
export type AccountPrimer = {
	enabled: boolean;
	model?: string;
	schedule?: PrimerPolicy;
};

// source is whose schedule applies: the account's own, its provider's, or the shared one.
export type PrimerEffective = PrimerPolicy & {
	enabled: boolean;
	model: string;
	source: 'account' | 'provider' | 'shared';
};

export type PrimerSettings = {
	schedule: PrimerPolicy;
	// Replace the shared schedule for one provider's accounts.
	providers: Partial<Record<Provider, PrimerPolicy>>;
	// Ping model per provider; missing ones use the built-in default.
	models: Partial<Record<Provider, string>>;
	prompt: string;
};

export type PrimerPlan = { at: string; readyAt: string };

export type PrimerRun = {
	id: number;
	accountId: number;
	time: string;
	trigger: 'slot' | 'manual';
	plannedAt?: string;
	outcome: 'started' | 'sent' | 'skipped' | 'failed';
	message: string;
	pings: number;
	resetsAt?: string;
};

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

export type AuditSettings = {
	enabled: boolean;
	record: 'all' | 'errors' | 'success';
	samplePercent: number;
	models: string[];
	request: boolean;
	upstreamRequest: boolean;
	upstreamResponse: boolean;
	response: boolean;
	headers: boolean;
	redactHeaders: string[];
	maxBodyKB: number;
	retentionDays: number;
	maxEntries: number;
	maxStorageMB: number;
};

export type AuditAttempt = {
	accountId: number;
	status: number;
	error?: string;
	durationMs: number;
};

export type AuditEntry = UsageEvent & {
	id: number;
	clientIp: string;
	userAgent: string;
	attempts: AuditAttempt[];
	payloadSize: number;
};

export type AuditBody = { text: string; size: number; truncated: boolean };

export type AuditPayload = {
	method?: string;
	query?: string;
	requestHeaders?: Record<string, string[]>;
	responseHeaders?: Record<string, string[]>;
	upstreamStatus?: number;
	upstreamResponseHeaders?: Record<string, string[]>;
	request?: AuditBody;
	upstreamRequest?: AuditBody;
	upstreamResponse?: AuditBody;
	response?: AuditBody;
};

export type AuditFilter = {
	range?: UsageRange;
	userId?: number;
	keyId?: number;
	accountId?: number;
	model?: string;
	status?: string;
	q?: string;
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
	codexResets: FeedSettings;
	claudeResets: FeedSettings;
	usageRetentionDays: number;
	routing: RoutingSettings;
	keys: { storeCopies: boolean };
	claude: CloakSettings;
	audit: AuditSettings;
	updates: { enabled: boolean; intervalHours: number };
};

export type FeedSettings = { enabled: boolean; intervalMinutes: number };

export type PoolSettings = {
	quotaPollMinutes: number;
	redeem: RedeemPolicy;
	maxRetries: number;
	bootstrapSeconds: number;
	keepaliveSeconds: number;
	errorRules: ErrorRule[];
	primer: PrimerSettings;
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
	pace: 'warnings' | 'always' | 'hidden';
	banked: 'count' | 'hidden' | 'count_expiry';
	bankedMode: 'pooled' | 'accounts';
	codexResets: ResetsDisplay;
	claudeResets: ResetsDisplay;
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
	pace?: Pace;
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

export type FeedReset = {
	id: string;
	status?: string;
	reset_type: 'regular' | 'banked';
	announced_at: string;
	scheduled_for?: string | null;
	text: string;
	source: ResetSource;
	scope?: string;
	usable_until?: string;
};

export type FeedWatch = {
	level: 'elevated' | 'strong';
	reset_chance_percent: number | null;
	forecast_window: string;
	observed_at: string;
	expires_at: string;
	text: string;
	source: ResetSource;
};

export type FeedStats = {
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
	codexResets?: ResetsView;
	claudeResets?: ResetsView;
	generatedAt: string;
};

export type Release = {
	version: string;
	url: string;
	publishedAt: string;
	sections: { title: string; items: string[] }[];
};

export type UpdatesSnapshot = {
	current: string;
	latest: Release | null;
	newer: Release[];
	checkedAt: string | null;
	error?: string;
};

export type ResetsView = {
	latest?: FeedReset;
	scheduled?: FeedReset;
	forecast?: FeedWatch;
	stats?: FeedStats;
};

export type ResetsSnapshot = {
	status: {
		latest_reset: FeedReset | null;
		scheduled_reset: FeedReset | null;
		active_watch: FeedWatch | null;
		stats: FeedStats;
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

export type ResetsDisplay = {
	latest: boolean;
	scheduled: boolean;
	forecast: boolean;
	stats: boolean;
};
