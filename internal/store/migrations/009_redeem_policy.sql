ALTER TABLE accounts ADD COLUMN redeem_policy TEXT NOT NULL DEFAULT '';
UPDATE accounts SET redeem_policy = '{"enabled":true}' WHERE auto_redeem = 1;
