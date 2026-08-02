-- 000009_backups_audit.up.sql
-- Backups registry, audit query indexes, audit immutability and job audit
-- provenance. The audit trail stays append-only: the runtime role receives
-- INSERT and SELECT only, and schema changes land exclusively through the
-- dedicated DDL role connection.

BEGIN;

-- ============================================================
-- Table: gorouter_backups
-- Registry of validated disaster-recovery backups. sha256 is the content
-- hash, bytes the size, generated_by the producer. verified_at records
-- validation and restore_verified_at records a successful shadow-restore
-- proof; both start NULL. The registry is append-only after creation.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_backups (
    id                  UUID PRIMARY KEY,
    path                TEXT NOT NULL,
    sha256              TEXT NOT NULL,
    bytes               BIGINT NOT NULL,
    generated_by        TEXT,
    verified_at         TIMESTAMPTZ,
    restore_verified_at TIMESTAMPTZ,
    created_at          TIMESTAMPTZ
);

-- ============================================================
-- Audit query indexes for the read paths (list/export by actor and by
-- resource, newest first).
-- ============================================================
CREATE INDEX IF NOT EXISTS idx_audit_log_actor_time
    ON gorouter_audit_log(actor_id, occurred_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource_time
    ON gorouter_audit_log(resource_type, resource_id, occurred_at);

-- ============================================================
-- Audit immutability at the role level: the runtime role keeps INSERT and
-- SELECT only; UPDATE and DELETE are revoked from the runtime role and from
-- PUBLIC so no other connection can mutate the trail. Retention never
-- touches the audit log.
-- ============================================================
REVOKE UPDATE, DELETE ON gorouter_audit_log FROM gorouter, PUBLIC;
GRANT INSERT, SELECT ON gorouter_audit_log TO gorouter;

-- ============================================================
-- Job audit provenance: job rows carry job_id and actor_kind ('job') with
-- actor_id NULL, so the actor_id foreign key stays reserved for human, PAT
-- and session actors. The integrity CHECK allows only the two actor kinds
-- and requires actor_id IS NULL for job rows.
-- ============================================================
ALTER TABLE gorouter_audit_log ADD COLUMN IF NOT EXISTS job_id UUID;
ALTER TABLE gorouter_audit_log ADD COLUMN IF NOT EXISTS actor_kind VARCHAR(32) NOT NULL DEFAULT 'user';
ALTER TABLE gorouter_audit_log DROP CONSTRAINT IF EXISTS chk_audit_actor_kind;
ALTER TABLE gorouter_audit_log ADD CONSTRAINT chk_audit_actor_kind
    CHECK (actor_kind IN ('user','job') AND (actor_kind <> 'job' OR actor_id IS NULL));
CREATE INDEX IF NOT EXISTS idx_audit_log_job_provenance
    ON gorouter_audit_log(actor_kind, job_id)
    WHERE actor_kind = 'job';

COMMIT;
