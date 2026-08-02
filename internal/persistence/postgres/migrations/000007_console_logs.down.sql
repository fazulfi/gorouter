-- 000007_console_logs.down.sql
-- Rollback the console-log projection. The seq index drops with the table.

BEGIN;

DROP TABLE IF EXISTS gorouter_console_logs CASCADE;

COMMIT;
