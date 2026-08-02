-- 000006_usage.up.sql
-- Usage aggregates, metadata-only request details, and durable request history.
-- The in-process Recent Requests ring is NOT persisted; gorouter_request_history
-- is the authoritative durable source used to hydrate it.

BEGIN;

-- ============================================================
-- Table: gorouter_usage_daily
-- Per-day usage summary. A day has at most one aggregate row
-- (day is the primary key); the unique key adds the intended
-- (day, provider_id, model_id) granularity.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_usage_daily (
    day               DATE PRIMARY KEY,
    provider_id       UUID,
    model_id          TEXT,
    requests          INT,
    prompt_tokens     BIGINT,
    completion_tokens BIGINT,
    cost              NUMERIC(12,6),
    UNIQUE (day, provider_id, model_id)
);

-- ============================================================
-- Table: gorouter_request_details
-- Metadata-only per-request records; request payloads are never
-- stored. debug_opt_in marks requests whose caller opted into
-- debug metadata collection.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_request_details (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id        UUID,
    provider_id       UUID,
    model             TEXT,
    prompt_tokens     INT,
    completion_tokens INT,
    cost              NUMERIC(12,6),
    status            TEXT,
    error_kind        TEXT,
    occurred_at       TIMESTAMPTZ,
    debug_opt_in      BOOLEAN NOT NULL DEFAULT false
);

-- ============================================================
-- Table: gorouter_request_history
-- Compact durable records powering the Recent Requests display.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_request_history (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    model             TEXT,
    provider          TEXT,
    prompt_tokens     INT,
    completion_tokens INT,
    status            TEXT,
    occurred_at       TIMESTAMPTZ
);

COMMIT;
