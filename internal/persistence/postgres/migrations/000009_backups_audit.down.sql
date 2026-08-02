-- 000009_backups_audit.down.sql
-- Rollback migration 000009 in reverse order: job provenance schema, audit
-- privileges, audit query indexes, backups registry.

BEGIN;

DROP INDEX IF EXISTS idx_audit_log_job_provenance;
ALTER TABLE gorouter_audit_log DROP CONSTRAINT IF EXISTS chk_audit_actor_kind;
ALTER TABLE gorouter_audit_log DROP COLUMN IF EXISTS actor_kind;
ALTER TABLE gorouter_audit_log DROP COLUMN IF EXISTS job_id;

REVOKE INSERT, SELECT ON gorouter_audit_log FROM gorouter;
GRANT UPDATE, DELETE ON gorouter_audit_log TO gorouter, PUBLIC;

DROP INDEX IF EXISTS idx_audit_log_resource_time;
DROP INDEX IF EXISTS idx_audit_log_actor_time;

DROP TABLE IF EXISTS gorouter_backups CASCADE;

COMMIT;
