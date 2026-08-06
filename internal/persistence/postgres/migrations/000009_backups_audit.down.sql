-- 000009_backups_audit.down.sql
-- Rollback migration 000009 in reverse order: job provenance schema, audit
-- privileges, audit query indexes, backups registry. The privilege statements
-- mirror the up migration exactly: the down removes what the up granted
-- (INSERT, SELECT for the runtime role) and never re-grants UPDATE or DELETE,
-- which no deployment state held before 000009 (no GRANT exists in
-- 000001-000008; PUBLIC had zero privileges on the audit log).

BEGIN;

DROP INDEX IF EXISTS idx_audit_log_job_provenance;
ALTER TABLE gorouter_audit_log DROP CONSTRAINT IF EXISTS chk_audit_actor_kind;
ALTER TABLE gorouter_audit_log DROP COLUMN IF EXISTS actor_kind;
ALTER TABLE gorouter_audit_log DROP COLUMN IF EXISTS job_id;

REVOKE INSERT, SELECT ON gorouter_audit_log FROM gorouter;
REVOKE UPDATE, DELETE ON gorouter_audit_log FROM gorouter, PUBLIC;

DROP INDEX IF EXISTS idx_audit_log_resource_time;
DROP INDEX IF EXISTS idx_audit_log_actor_time;

DROP TABLE IF EXISTS gorouter_backups CASCADE;

COMMIT;
