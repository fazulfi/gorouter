-- 000007_console_logs.up.sql
-- Console-log projection: durable, strictly ordered, redacted log entries
-- rendered by the operator console. Raw message text is stored for
-- server-side diagnostics; the console surfaces redacted_message.

BEGIN;

-- ============================================================
-- Table: gorouter_console_logs
-- Append-only projection of console output. seq is an
-- application-assigned strictly increasing sequence used for
-- ordered reads and retention; id is the database-assigned
-- surrogate. retention_until is service-managed (retention is
-- applied by the service, not by the projection).
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_console_logs (
    id               BIGSERIAL PRIMARY KEY,
    seq              BIGINT NOT NULL,
    level            TEXT,
    message          TEXT,
    redacted_message TEXT NOT NULL,
    occurred_at      TIMESTAMPTZ,
    retention_until  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_console_logs_seq ON gorouter_console_logs(seq);

COMMIT;
