-- 000005_proxy_pools.up.sql
-- Proxy pools, pool members, and provider nodes schema.
-- Creates tables required for proxy pool management and provider node routing.

BEGIN;

-- ============================================================
-- Table: gorouter_proxy_pools
-- Named proxy configuration groupings.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_proxy_pools (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name        TEXT NOT NULL UNIQUE,
    description TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- ============================================================
-- Table: gorouter_proxy_pool_members
-- Ordered membership of proxy configs within a pool.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_proxy_pool_members (
    pool_id         UUID NOT NULL REFERENCES gorouter_proxy_pools(id) ON DELETE CASCADE,
    proxy_config_id UUID NOT NULL REFERENCES gorouter_proxy_configs(id),
    position        INT NOT NULL DEFAULT 0,
    PRIMARY KEY (pool_id, proxy_config_id)
);

-- ============================================================
-- Table: gorouter_provider_nodes
-- Per-provider routing endpoints with priority and region affinity.
-- ============================================================
CREATE TABLE IF NOT EXISTS gorouter_provider_nodes (
    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider_id UUID NOT NULL REFERENCES gorouter_providers(id),
    name        TEXT,
    base_url    TEXT,
    region      TEXT,
    priority    INT NOT NULL DEFAULT 0,
    is_active   BOOLEAN NOT NULL DEFAULT true,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

COMMIT;
