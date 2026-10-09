CREATE TABLE accounts (
	id               INTEGER PRIMARY KEY,
	provider         TEXT    NOT NULL,
	label            TEXT    NOT NULL,
	alias            TEXT    NOT NULL DEFAULT '',
	email            TEXT    NOT NULL DEFAULT '',
	external_id      TEXT    NOT NULL DEFAULT '',
	plan             TEXT    NOT NULL DEFAULT '',
	credentials      TEXT    NOT NULL,
	disabled         INTEGER NOT NULL DEFAULT 0,
	priority         INTEGER NOT NULL DEFAULT 0,
	auto_redeem      INTEGER NOT NULL DEFAULT 0,
	status           TEXT    NOT NULL DEFAULT 'ok',
	status_message   TEXT    NOT NULL DEFAULT '',
	cooldown_until   INTEGER,
	quota            TEXT    NOT NULL DEFAULT '',
	quota_updated_at INTEGER,
	last_used_at     INTEGER,
	created_at       INTEGER NOT NULL,
	updated_at       INTEGER NOT NULL
);

CREATE INDEX accounts_provider ON accounts (provider);

-- No foreign keys: usage history outlives deleted users, keys and accounts.
CREATE TABLE usage_events (
	id                 INTEGER PRIMARY KEY,
	ts                 INTEGER NOT NULL,
	user_id            INTEGER,
	key_id             INTEGER,
	account_id         INTEGER,
	provider           TEXT    NOT NULL,
	model              TEXT    NOT NULL,
	endpoint           TEXT    NOT NULL,
	status             INTEGER NOT NULL,
	stream             INTEGER NOT NULL,
	input_tokens       INTEGER NOT NULL DEFAULT 0,
	output_tokens      INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens   INTEGER NOT NULL DEFAULT 0,
	duration_ms        INTEGER NOT NULL,
	error              TEXT    NOT NULL DEFAULT ''
);

CREATE INDEX usage_events_ts ON usage_events (ts);
CREATE INDEX usage_events_user_ts ON usage_events (user_id, ts);
CREATE INDEX usage_events_key_ts ON usage_events (key_id, ts);

CREATE TABLE display_profiles (
	id         INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL,
	config     TEXT    NOT NULL,
	created_at INTEGER NOT NULL,
	updated_at INTEGER NOT NULL
);

CREATE TABLE settings (
	key   TEXT PRIMARY KEY,
	value TEXT NOT NULL
);

ALTER TABLE api_keys ADD COLUMN allowed_models TEXT NOT NULL DEFAULT '';
ALTER TABLE api_keys ADD COLUMN rpm_limit INTEGER NOT NULL DEFAULT 0;
ALTER TABLE api_keys ADD COLUMN display_profile_id INTEGER REFERENCES display_profiles (id) ON DELETE SET NULL;
