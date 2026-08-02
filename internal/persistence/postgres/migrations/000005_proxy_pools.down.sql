-- 000005_proxy_pools.down.sql
-- Rollback proxy pools, pool members, and provider nodes in reverse dependency order.

BEGIN;

DROP TABLE IF EXISTS gorouter_proxy_pool_members CASCADE;
DROP TABLE IF EXISTS gorouter_proxy_pools CASCADE;
DROP TABLE IF EXISTS gorouter_provider_nodes CASCADE;

COMMIT;
