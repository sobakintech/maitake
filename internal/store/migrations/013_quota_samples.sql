-- History of quota windows for the capacity chart. No foreign key: samples are pruned by age.
CREATE TABLE quota_samples (
	account_id INTEGER NOT NULL,
	window_id  TEXT    NOT NULL,
	ts         INTEGER NOT NULL,
	kind       TEXT    NOT NULL,
	used       REAL    NOT NULL,
	resets_at  INTEGER,
	minutes    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX quota_samples_ts ON quota_samples (ts);
CREATE INDEX quota_samples_account_window_ts ON quota_samples (account_id, window_id, ts);
