-- 000001_foundation.up.sql
-- Foundation schema for gorouter
-- Creates all core tables: settings, users, sessions, api_keys, pats, audit_log, providers, jobs

BEGIN;

-- ============================================================
-- Table: gorouter_settings
-- Key-value configuration store with JSONB values
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_settings (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    key         VARCHAR(255) UNIQUE NOT NULL,
    value       JSONB NOT NULL,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Table: gorouter_users
-- Dashboard administrators and authenticated users
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_users (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email         VARCHAR(255) UNIQUE NOT NULL,
    password_hash VARCHAR(255) NOT NULL,
    display_name  VARCHAR(255),
    is_admin      BOOLEAN NOT NULL DEFAULT false,
    is_active     BOOLEAN NOT NULL DEFAULT true,
    last_login_at TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed default admin user (password: placeholder, must be changed on first login)
INSERT INTO gorouter_users (email, password_hash, display_name, is_admin)
VALUES ('admin@gorouter.local', '$2a$10$placeholderchangeme', 'Admin', true)
ON CONFLICT (email) DO NOTHING;

-- ============================================================
-- Table: gorouter_sessions
-- Web dashboard sessions with sliding expiry
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_sessions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES gorouter_users(id) ON DELETE CASCADE,
    token_hash VARCHAR(255) NOT NULL,
    ip_address INET,
    user_agent TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    revoked_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_sessions_user_id ON gorouter_sessions(user_id);
CREATE INDEX IF NOT EXISTS idx_sessions_expires_at ON gorouter_sessions(expires_at);
CREATE INDEX IF NOT EXISTS idx_sessions_revoked_at ON gorouter_sessions(revoked_at);

-- ============================================================
-- Table: gorouter_api_keys
-- Model API keys for external LLM access
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_api_keys (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES gorouter_users(id) ON DELETE CASCADE,
    key_prefix   VARCHAR(8) NOT NULL,
    key_hash     VARCHAR(255) NOT NULL,
    name         VARCHAR(255) NOT NULL,
    scopes       TEXT[] NOT NULL DEFAULT '{}',
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_api_keys_user_id ON gorouter_api_keys(user_id);
CREATE INDEX IF NOT EXISTS idx_api_keys_revoked_at ON gorouter_api_keys(revoked_at);

-- ============================================================
-- Table: gorouter_pats
-- Personal Access Tokens for CLI and automation
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_pats (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID NOT NULL REFERENCES gorouter_users(id) ON DELETE CASCADE,
    token_hash   VARCHAR(255) NOT NULL,
    description  TEXT,
    expires_at   TIMESTAMPTZ,
    last_used_at TIMESTAMPTZ,
    revoked_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_pats_user_id ON gorouter_pats(user_id);
CREATE INDEX IF NOT EXISTS idx_pats_revoked_at ON gorouter_pats(revoked_at);

-- ============================================================
-- Table: gorouter_audit_log
-- Immutable audit trail for security-sensitive operations
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_audit_log (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id      UUID REFERENCES gorouter_users(id),
    action        VARCHAR(255) NOT NULL,
    resource_type VARCHAR(255) NOT NULL,
    resource_id   UUID,
    details       JSONB,
    ip_address    INET,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_audit_log_actor_id ON gorouter_audit_log(actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_log_action_occurred ON gorouter_audit_log(action, occurred_at);
CREATE INDEX IF NOT EXISTS idx_audit_log_resource ON gorouter_audit_log(resource_type, resource_id);

-- ============================================================
-- Table: gorouter_providers
-- LLM provider connection configurations
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_providers (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name               VARCHAR(255) NOT NULL,
    type               VARCHAR(50) NOT NULL,
    base_url           VARCHAR(512) NOT NULL,
    api_key_encrypted  TEXT,
    config             JSONB DEFAULT '{}',
    is_enabled         BOOLEAN NOT NULL DEFAULT true,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_providers_type ON gorouter_providers(type);
CREATE INDEX IF NOT EXISTS idx_providers_is_enabled ON gorouter_providers(is_enabled);

-- ============================================================
-- Table: gorouter_jobs
-- Background job queue with retry and scheduling
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_jobs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    type          VARCHAR(255) NOT NULL,
    status        VARCHAR(50) NOT NULL DEFAULT 'pending',
    payload       JSONB,
    result        JSONB,
    error_message TEXT,
    attempts      INT NOT NULL DEFAULT 0,
    max_attempts  INT NOT NULL DEFAULT 3,
    scheduled_at  TIMESTAMPTZ,
    started_at    TIMESTAMPTZ,
    completed_at  TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON gorouter_jobs(status);
CREATE INDEX IF NOT EXISTS idx_jobs_type_status ON gorouter_jobs(type, status);
CREATE INDEX IF NOT EXISTS idx_jobs_scheduled_at ON gorouter_jobs(scheduled_at);

COMMIT;
