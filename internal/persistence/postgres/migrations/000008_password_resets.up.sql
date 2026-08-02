-- 000008_password_resets.up.sql
-- Password reset requests, local operator CLI only. The table stores an
-- opaque token hash and lifecycle timestamps; the raw reset token is never
-- persisted, and there is no remotely reachable reset path.

BEGIN;

-- ============================================================
-- Table: gorouter_password_resets
-- One row per reset request. token_hash is the opaque hash of the
-- reset token (never the token itself). requested_at records when
-- the request was created; completed_at and revoked_at are the
-- mutually exclusive terminal lifecycle markers applied by the
-- application layer.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_password_resets (
    id           UUID PRIMARY KEY,
    user_id      UUID REFERENCES gorouter_users(id),
    token_hash   TEXT NOT NULL,
    requested_by TEXT,
    requested_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ
);

COMMIT;
