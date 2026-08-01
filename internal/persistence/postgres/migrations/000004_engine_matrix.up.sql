-- 000004_engine_matrix.up.sql
-- Phase 3 engine matrix schema
-- Creates OAuth sessions, runtime state checkpoints, model aliases,
-- and combo definitions tables required by P3-T06 through P3-T10.
-- Expand-first design: Phase 2 code remains readable during rollback window.
-- Down migration is safe and reverses all changes.

BEGIN;

-- ============================================================
-- Table: gorouter_oauth_sessions
-- Tracks all OAuth/import flow sessions across 20 flow families.
-- Supports: loopback, fixed-callback proxy, device-code polling,
-- dashboard relay, cookie/PAT/IDE import flows.
-- Security: token_hash stores SHA-256 of access token (never raw).
--           state is CSRF token; code_verifier is PKCE challenge.
--           expired/replayed callbacks are rejected pre-mutation.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_oauth_sessions (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id       UUID NOT NULL REFERENCES gorouter_providers(id) ON DELETE CASCADE,
    account_id        UUID REFERENCES gorouter_provider_accounts(id) ON DELETE SET NULL,
    state             VARCHAR(512),
    code_verifier     VARCHAR(512),
    redirect_uri      TEXT,
    flow_id           VARCHAR(64) NOT NULL,
    mechanism         VARCHAR(64) NOT NULL,
    status            VARCHAR(32) NOT NULL DEFAULT 'pending',
    error_detail      TEXT,
    token_hash        VARCHAR(128),
    refresh_token     TEXT,
    token_expiry      TIMESTAMPTZ,
    device_code       TEXT,
    user_code         VARCHAR(128),
    verification_uri  TEXT,
    expires_at        TIMESTAMPTZ NOT NULL,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_oauth_sessions_provider
    ON gorouter_oauth_sessions(provider_id);
CREATE INDEX IF NOT EXISTS idx_oauth_sessions_state
    ON gorouter_oauth_sessions(state);
CREATE INDEX IF NOT EXISTS idx_oauth_sessions_status
    ON gorouter_oauth_sessions(status);
CREATE INDEX IF NOT EXISTS idx_oauth_sessions_expires
    ON gorouter_oauth_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_oauth_sessions_completed
    ON gorouter_oauth_sessions(completed_at);

-- ============================================================
-- Table: gorouter_runtime_state
-- Key-value checkpoint storage for runtime state (cooldowns,
-- account health, model locks, refresh timestamps) that must
-- survive restart. Hot state lives in memory; important
-- checkpoints are persisted here for deterministic recovery.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_runtime_state (
    key         VARCHAR(255) PRIMARY KEY,
    value       JSONB NOT NULL DEFAULT '{}',
    ttl         TIMESTAMPTZ,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_runtime_state_ttl
    ON gorouter_runtime_state(ttl)
    WHERE ttl IS NOT NULL;

-- ============================================================
-- Table: gorouter_model_aliases
-- User-defined model aliases mapping custom names to provider models.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_model_aliases (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    alias       VARCHAR(255) NOT NULL UNIQUE,
    target      VARCHAR(512) NOT NULL,
    provider_id UUID REFERENCES gorouter_providers(id) ON DELETE CASCADE,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_model_aliases_alias
    ON gorouter_model_aliases(alias);

-- ============================================================
-- Table: gorouter_combo_definitions
-- Combo mode definitions for multi-provider routing strategies.
-- Supports: sequential fallback, round-robin, auto-switch, fusion.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_combo_definitions (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        VARCHAR(255) NOT NULL UNIQUE,
    strategy    VARCHAR(64) NOT NULL,
    config      JSONB NOT NULL DEFAULT '{}',
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_combo_definitions_strategy
    ON gorouter_combo_definitions(strategy);

-- ============================================================
-- Table: gorouter_combo_members
-- Members (providers/accounts/models) assigned to a combo definition.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_combo_members (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    combo_id    UUID NOT NULL REFERENCES gorouter_combo_definitions(id) ON DELETE CASCADE,
    provider_id UUID REFERENCES gorouter_providers(id) ON DELETE CASCADE,
    account_id  UUID REFERENCES gorouter_provider_accounts(id) ON DELETE CASCADE,
    model_ref   VARCHAR(255),
    priority    INT NOT NULL DEFAULT 0,
    weight      INT NOT NULL DEFAULT 1,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_combo_members_combo
    ON gorouter_combo_members(combo_id);

COMMIT;
