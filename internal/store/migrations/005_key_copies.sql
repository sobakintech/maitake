-- Encrypted copy of the full key so admins can copy it later. NULL when not kept.
ALTER TABLE api_keys ADD COLUMN secret_enc BLOB;
