-- 000002_engine.down.sql
-- Rollback engine schema
-- Drops all engine tables in reverse dependency order

BEGIN;

DROP TABLE IF EXISTS gorouter_usage_tracking CASCADE;
DROP TABLE IF EXISTS gorouter_provider_models CASCADE;
DROP TABLE IF EXISTS gorouter_proxy_configs CASCADE;
DROP TABLE IF EXISTS gorouter_account_cooldowns CASCADE;
DROP TABLE IF EXISTS gorouter_provider_accounts CASCADE;

COMMIT;
