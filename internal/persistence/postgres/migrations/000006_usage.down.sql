-- 000006_usage.down.sql
-- Rollback usage aggregates, request details, and request history.

BEGIN;

DROP TABLE IF EXISTS gorouter_request_history CASCADE;
DROP TABLE IF EXISTS gorouter_request_details CASCADE;
DROP TABLE IF EXISTS gorouter_usage_daily CASCADE;

COMMIT;
