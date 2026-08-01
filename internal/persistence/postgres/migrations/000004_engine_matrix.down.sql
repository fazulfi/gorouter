-- 000004_engine_matrix.down.sql
-- Rollback engine matrix schema in reverse dependency order.

BEGIN;

DROP TABLE IF EXISTS gorouter_combo_members CASCADE;
DROP TABLE IF EXISTS gorouter_combo_definitions CASCADE;
DROP TABLE IF EXISTS gorouter_model_aliases CASCADE;
DROP TABLE IF EXISTS gorouter_runtime_state CASCADE;
DROP INDEX IF EXISTS idx_oauth_sessions_provider;
DROP INDEX IF EXISTS idx_oauth_sessions_state;
DROP INDEX IF EXISTS idx_oauth_sessions_status;
DROP INDEX IF EXISTS idx_oauth_sessions_expires;
DROP INDEX IF EXISTS idx_oauth_sessions_completed;
DROP TABLE IF EXISTS gorouter_oauth_sessions CASCADE;

COMMIT;
