-- 000002_engine.up.sql
-- Engine schema for gorouter
-- Creates provider accounts, cooldowns, proxies, models catalog, and usage tracking tables

BEGIN;

-- ============================================================
-- Table: gorouter_provider_accounts
-- Individual API accounts under a provider with auth and priority
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_provider_accounts (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id     UUID NOT NULL REFERENCES gorouter_providers(id) ON DELETE CASCADE,
    label           VARCHAR(255) NOT NULL,
    auth_type       VARCHAR(50) NOT NULL,
    credential_ref  VARCHAR(512) NOT NULL,
    priority        INT NOT NULL DEFAULT 0,
    is_enabled      BOOLEAN NOT NULL DEFAULT true,
    max_concurrent  INT NOT NULL DEFAULT 0,
    model_filters   TEXT[] DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_provider_accounts_provider_id ON gorouter_provider_accounts(provider_id);
CREATE INDEX IF NOT EXISTS idx_provider_accounts_is_enabled ON gorouter_provider_accounts(is_enabled);

-- ============================================================
-- Table: gorouter_account_cooldowns
-- Cooldown tracking for rate-limited or errored accounts
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_account_cooldowns (
    account_id  UUID PRIMARY KEY REFERENCES gorouter_provider_accounts(id) ON DELETE CASCADE,
    reason      VARCHAR(255) NOT NULL,
    started_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at  TIMESTAMPTZ NOT NULL,
    retry_count INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Table: gorouter_proxy_configs
-- Proxy configurations for provider account connections
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_proxy_configs (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    account_id  UUID NOT NULL REFERENCES gorouter_provider_accounts(id) ON DELETE CASCADE,
    url         VARCHAR(512) NOT NULL,
    username    TEXT,
    password    TEXT,
    is_enabled  BOOLEAN NOT NULL DEFAULT true,
    priority    INT NOT NULL DEFAULT 0,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_proxy_configs_account_id ON gorouter_proxy_configs(account_id);

-- ============================================================
-- Table: gorouter_provider_models
-- Models catalog per provider with capability flags
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_provider_models (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id   UUID NOT NULL REFERENCES gorouter_providers(id) ON DELETE CASCADE,
    model_id      VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255),
    capabilities  TEXT[] NOT NULL DEFAULT '{}',
    max_tokens    BIGINT,
    is_builtin    BOOLEAN NOT NULL DEFAULT false,
    is_enabled    BOOLEAN NOT NULL DEFAULT true,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE(provider_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_provider_models_provider_id ON gorouter_provider_models(provider_id);
CREATE INDEX IF NOT EXISTS idx_provider_models_capabilities ON gorouter_provider_models USING GIN(capabilities);

-- ============================================================
-- Table: gorouter_usage_tracking
-- Immutable per-request usage records for billing and monitoring
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_usage_tracking (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id        UUID NOT NULL,
    account_id        UUID NOT NULL REFERENCES gorouter_provider_accounts(id),
    provider_id       UUID NOT NULL REFERENCES gorouter_providers(id),
    model             VARCHAR(255) NOT NULL,
    prompt_tokens     INT NOT NULL DEFAULT 0,
    completion_tokens INT NOT NULL DEFAULT 0,
    total_tokens      INT NOT NULL DEFAULT 0,
    cost              NUMERIC(12,6) NOT NULL DEFAULT 0,
    status_code       INT NOT NULL,
    error_code        VARCHAR(50),
    latency_ms        BIGINT NOT NULL,
    streamed          BOOLEAN NOT NULL DEFAULT false,
    occurred_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_usage_tracking_account_id ON gorouter_usage_tracking(account_id);
CREATE INDEX IF NOT EXISTS idx_usage_tracking_occurred_at ON gorouter_usage_tracking(occurred_at);
CREATE INDEX IF NOT EXISTS idx_usage_tracking_model ON gorouter_usage_tracking(model);

COMMIT;
