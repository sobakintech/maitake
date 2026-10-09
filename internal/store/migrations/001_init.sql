CREATE TABLE admins (
	id            INTEGER PRIMARY KEY,
	username      TEXT    NOT NULL UNIQUE COLLATE NOCASE,
	password_hash TEXT    NOT NULL,
	created_at    INTEGER NOT NULL
);

CREATE TABLE sessions (
	token_hash BLOB    PRIMARY KEY,
	admin_id   INTEGER NOT NULL REFERENCES admins (id) ON DELETE CASCADE,
	created_at INTEGER NOT NULL,
	expires_at INTEGER NOT NULL
);

CREATE TABLE users (
	id         INTEGER PRIMARY KEY,
	name       TEXT    NOT NULL,
	note       TEXT    NOT NULL DEFAULT '',
	disabled   INTEGER NOT NULL DEFAULT 0,
	created_at INTEGER NOT NULL
);

CREATE TABLE api_keys (
	id           INTEGER PRIMARY KEY,
	user_id      INTEGER NOT NULL REFERENCES users (id) ON DELETE CASCADE,
	name         TEXT    NOT NULL DEFAULT '',
	prefix       TEXT    NOT NULL,
	key_hash     BLOB    NOT NULL UNIQUE,
	disabled     INTEGER NOT NULL DEFAULT 0,
	created_at   INTEGER NOT NULL,
	expires_at   INTEGER,
	last_used_at INTEGER
);

CREATE INDEX api_keys_user_id ON api_keys (user_id);
