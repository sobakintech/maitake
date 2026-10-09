ALTER TABLE api_keys ADD COLUMN audit_mode TEXT NOT NULL DEFAULT '';

CREATE TABLE audit_log (
	id                 INTEGER PRIMARY KEY,
	ts                 INTEGER NOT NULL,
	user_id            INTEGER,
	key_id             INTEGER,
	account_id         INTEGER,
	provider           TEXT    NOT NULL DEFAULT '',
	model              TEXT    NOT NULL DEFAULT '',
	requested_model    TEXT    NOT NULL DEFAULT '',
	endpoint           TEXT    NOT NULL DEFAULT '',
	status             INTEGER NOT NULL,
	stream             INTEGER NOT NULL DEFAULT 0,
	input_tokens       INTEGER NOT NULL DEFAULT 0,
	output_tokens      INTEGER NOT NULL DEFAULT 0,
	cache_read_tokens  INTEGER NOT NULL DEFAULT 0,
	cache_write_tokens INTEGER NOT NULL DEFAULT 0,
	reasoning_tokens   INTEGER NOT NULL DEFAULT 0,
	duration_ms        INTEGER NOT NULL DEFAULT 0,
	ttft_ms            INTEGER NOT NULL DEFAULT 0,
	error              TEXT    NOT NULL DEFAULT '',
	service_tier       TEXT    NOT NULL DEFAULT '',
	served_tier        TEXT    NOT NULL DEFAULT '',
	client_ip          TEXT    NOT NULL DEFAULT '',
	user_agent         TEXT    NOT NULL DEFAULT '',
	attempts           TEXT    NOT NULL DEFAULT '[]',
	-- gzip-compressed JSON with headers and bodies
	payload            BLOB,
	payload_size       INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX audit_log_ts ON audit_log (ts);
CREATE INDEX audit_log_key_ts ON audit_log (key_id, ts);
CREATE INDEX audit_log_user_ts ON audit_log (user_id, ts);
