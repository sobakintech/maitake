ALTER TABLE api_keys ADD COLUMN model_rules TEXT NOT NULL DEFAULT '';
ALTER TABLE usage_events ADD COLUMN requested_model TEXT NOT NULL DEFAULT '';
