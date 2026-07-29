-- 000001_foundation.down.sql
-- Rollback foundation schema
-- Drops all tables in reverse dependency order

BEGIN;

DROP TABLE IF EXISTS gorouter_jobs CASCADE;
DROP TABLE IF EXISTS gorouter_providers CASCADE;
DROP TABLE IF EXISTS gorouter_audit_log CASCADE;
DROP TABLE IF EXISTS gorouter_pats CASCADE;
DROP TABLE IF EXISTS gorouter_api_keys CASCADE;
DROP TABLE IF EXISTS gorouter_sessions CASCADE;
DROP TABLE IF EXISTS gorouter_users CASCADE;
DROP TABLE IF EXISTS gorouter_settings CASCADE;

COMMIT;
