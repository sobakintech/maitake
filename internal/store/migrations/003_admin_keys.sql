-- Keys can belong to a user or directly to an admin. SQLite can't relax a NOT NULL column in
-- place, so the table is rebuilt.
CREATE TABLE api_keys_new (
	id                 INTEGER PRIMARY KEY,
	user_id            INTEGER REFERENCES users (id) ON DELETE CASCADE,
	admin_id           INTEGER REFERENCES admins (id) ON DELETE CASCADE,
	name               TEXT    NOT NULL DEFAULT '',
	prefix             TEXT    NOT NULL,
	key_hash           BLOB    NOT NULL UNIQUE,
	disabled           INTEGER NOT NULL DEFAULT 0,
	created_at         INTEGER NOT NULL,
	expires_at         INTEGER,
	last_used_at       INTEGER,
	allowed_models     TEXT    NOT NULL DEFAULT '',
	rpm_limit          INTEGER NOT NULL DEFAULT 0,
	display_profile_id INTEGER REFERENCES display_profiles (id) ON DELETE SET NULL,
	CHECK ((user_id IS NULL) <> (admin_id IS NULL))
);

INSERT INTO api_keys_new (id, user_id, name, prefix, key_hash, disabled, created_at, expires_at, last_used_at,
	allowed_models, rpm_limit, display_profile_id)
SELECT id, user_id, name, prefix, key_hash, disabled, created_at, expires_at, last_used_at,
	allowed_models, rpm_limit, display_profile_id
FROM api_keys;

DROP TABLE api_keys;
ALTER TABLE api_keys_new RENAME TO api_keys;

CREATE INDEX api_keys_user_id ON api_keys (user_id);
CREATE INDEX api_keys_admin_id ON api_keys (admin_id);
