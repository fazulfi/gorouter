-- 000008_password_resets.down.sql
-- Rollback the password reset requests table.

BEGIN;

DROP TABLE IF EXISTS gorouter_password_resets CASCADE;

COMMIT;
