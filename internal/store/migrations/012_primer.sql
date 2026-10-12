ALTER TABLE accounts ADD COLUMN primer_policy TEXT NOT NULL DEFAULT '';

CREATE TABLE primer_runs (
	id         INTEGER PRIMARY KEY,
	account_id INTEGER NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
	ts         INTEGER NOT NULL,
	trigger    TEXT    NOT NULL,
	planned_at INTEGER,
	outcome    TEXT    NOT NULL,
	message    TEXT    NOT NULL DEFAULT '',
	pings      INTEGER NOT NULL DEFAULT 0,
	resets_at  INTEGER
);

CREATE INDEX primer_runs_account_ts ON primer_runs (account_id, ts);
CREATE INDEX primer_runs_ts ON primer_runs (ts);
