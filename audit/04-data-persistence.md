# Audit 04: Data Persistence

## Scope

Complete source-backed investigation of the original 9Router persistence layer: the SQLite schema engine, adapter fallback chain, all repositories, migrations, legacy file compat, backup/import/export/restore semantics, usage/request-detail storage, credential storage (encryption evidence), transaction boundaries, deduplication, shutdown durability, and concurrency assumptions.

## Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (v0.5.40)
- Read-only clone: `../upstream-original`

## Method

- Source read of every file under `src/lib/db/`, `src/lib/dataDir.js`, `src/lib/localDb.js`, `src/lib/usageDb.js`, `src/lib/requestDetailsDb.js`, `src/lib/disabledModelsDb.js`.
- Source read of the ARCHITECTURE.md persistence section and cross-check against actual code.
- Source read of all three DB test files: `db-sqlite-vs-lowdb.test.js`, `db-concurrent.test.js`, `request-details-tab.test.js`.
- Source read of `src/app/api/settings/database/route.js` (import/export API) and `src/app/api/shutdown/route.js`.
- Symbol/function-level tracing through the adapter interface, the migration pipeline, every repo, and the backup module.

---

## 1. SQLite Schema: Tables, Indexes, and PRAGMAs

### Source

- `src/lib/db/schema.js` — `TABLES` object, `PRAGMA_SQL`, `SCHEMA_VERSION = 1`.
- `src/lib/db/migrations/001-initial.js` — migration #1 bootstraps all tables and indexes from `TABLES`.

### PRAGMAs (applied by every adapter on open)

| PRAGMA | Value | Purpose |
| -------- | ------- | --------- |
| `journal_mode` | `WAL` | Write-Ahead Log for concurrent reads during writes |
| `synchronous` | `NORMAL` | Balance durability vs write speed (WAL-safe) |
| `temp_store` | `MEMORY` | Avoid temp-table disk I/O |
| `mmap_size` | `30000000` (~28.6 MB) | Memory-map up to 30 MB for faster reads |
| `cache_size` | `-64000` (~64 MB) | 64 MB page cache (negative = KB) |
| `foreign_keys` | `ON` | Enforce FK constraints (though no FK declarations exist) |
| `busy_timeout` | `5000` (5 s) | Wait 5 s before throwing SQLITE_BUSY |

**Confirmed**: No foreign key constraints are declared in any schema table even though `foreign_keys = ON`. The ON PRAGMA is a no-op.

### Tables (10 tables including _meta)

| Table | Primary Key | Purpose | JSON Column |
| ------- | ------------- | --------- | ------------- |
| `_meta` | `key TEXT PK` | Version tracking, lifecycle counters | No |
| `settings` | `id INTEGER PK CHECK(id=1)` (singleton) | App settings blob | `data TEXT` |
| `providerConnections` | `id TEXT PK` (UUID) | Provider credential rows | `data TEXT` |
| `providerNodes` | `id TEXT PK` (UUID) | Custom compatible nodes | `data TEXT` |
| `proxyPools` | `id TEXT PK` (UUID) | Outbound proxy pool entries | `data TEXT` |
| `apiKeys` | `id TEXT PK` (UUID) | Dashboard API keys | No (flat columns) |
| `combos` | `id TEXT PK` (UUID) | Model combo definitions | `models TEXT` (JSON array) |
| `kv` | `(scope, key)` composite PK | KV store for aliases, custom models, mitm, pricing, disabled models | `value TEXT` |
| `usageHistory` | `id INTEGER PK AUTOINCREMENT` | Per-request usage records | `tokens TEXT`, `meta TEXT` |
| `usageDaily` | `dateKey TEXT PK` (YYYY-MM-DD) | Pre-aggregated daily usage | `data TEXT` |
| `requestDetails` | `id TEXT PK` (timestamp-random-model) | Full request/response observability | `data TEXT` |

### Indexes (19 total)

- `providerConnections`: `idx_pc_provider` (provider), `idx_pc_provider_active` (provider, isActive), `idx_pc_priority` (provider, priority)
- `providerNodes`: `idx_pn_type` (type)
- `proxyPools`: `idx_pp_active` (isActive), `idx_pp_status` (testStatus)
- `apiKeys`: `idx_ak_key` (key)
- `combos`: `idx_combo_name` (name)
- `kv`: `idx_kv_scope` (scope)
- `usageHistory`: `idx_uh_ts` (timestamp DESC), `idx_uh_provider` (provider), `idx_uh_model` (model), `idx_uh_conn` (connectionId)
- `requestDetails`: `idx_rd_ts` (timestamp DESC), `idx_rd_provider` (provider), `idx_rd_model` (model), `idx_rd_conn` (connectionId)

---

## 2. Adapter Fallback Behavior

Source: `src/lib/db/driver.js` — `initAdapter()`, `getAdapter()`, `getAdapterSync()`

**Fallback chain:**

- `bun:sqlite` (Bun only, built-in)
- `better-sqlite3` (Node, in `optionalDependencies`)
- `node:sqlite` (Node >= 22.5, experimental)
- `sql.js` (pure JS WASM, always works)

All four implement the same interface: `run()`, `get()`, `all()`, `exec()`, `transaction()`, `checkpoint()`, `close()`, `raw`.

| Feature | better-sqlite3 | bun:sqlite | node:sqlite | sql.js |
| --------- | --------------- | ------------ | ------------- | -------- |
| Sync API | Yes | Yes | Yes | Yes |
| Transaction | Native wrapper | Native wrapper | SAVEPOINT | SAVEPOINT |
| Persistence | On-disk | On-disk | On-disk | 100ms debounced |
| WAL checkpoint | 60s + shutdown | 60s + shutdown | 60s + shutdown | N/A |
| Shutdown hooks | beforeExit, SIGINT, SIGTERM | same | same | +exit event |

**Critical**: sql.js has 100ms debounce between mutations and disk writes. Crash within debounce loses writes.

---

## 3. Migration Pipeline

Source: `src/lib/db/migrate.js` — `runMigrationOnce(adapter)`

**Pipeline order:**

1. Prune old backups (keep 3 newest)
2. Check freshness (`_meta` table zero rows = fresh)
3. Pre-schema-change backup if `SCHEMA_VERSION > stored backupSchemaVersion`
4. Run versioned migrations (only v1 "initial" exists)
5. Additive sync: `CREATE TABLE IF NOT EXISTS`, diff columns via PRAGMA, add missing, create indexes
6. Stamp `backupSchemaVersion` to current SCHEMA_VERSION
7. Legacy JSON import (one-time, on fresh DB + legacy files exist + no `.migrated-from-json` marker)
   - Row-count assertion with rollback on `MigrationAborted`
   - Writes `.migrated-from-json` marker on success
8. Track app version in `_meta`

Only one migration (v1). Additive sync handles new tables/columns/indexes. Destructive changes need a migration file.

**Guard**: `_migratedAdapters` WeakSet prevents re-running on cached adapter instances.

---

## 4. Repository-by-Repository Breakdown

### 4.1 `settingsRepo.js`

Table: `settings` (singleton row CHECK id=1). 50-key DEFAULT_SETTINGS. Read-merge-write inside transaction. `exportSettings()` returns raw data (no defaults). Concurrent-safe (tested with 50 parallel writes).

### 4.2 `connectionsRepo.js`

Table: `providerConnections`. JSON column stores `accessToken`, `refreshToken`, `expiresAt`, `apiKey`, `providerSpecificData`, etc. UUIDv4 PK. Dedup on OAuth email + provider (special Codex workspace ID logic). Priority reorder inside every create/update/delete. Atomic read-merge-write for token refresh (critical OAuth path, tested with 20 parallel updates).

### 4.3 `nodesRepo.js`

Table: `providerNodes`. Standard CRUD. JSON column for type-specific fields.

### 4.4 `proxyPoolsRepo.js`

Table: `proxyPools`. In-memory sort by `updatedAt DESC`. Test status tracking.

### 4.5 `apiKeysRepo.js`

Table: `apiKeys`. HMAC key generation via `generateApiKeyWithMachine()`. Validation checks `isActive`. Flat columns only.

### 4.6 `combosRepo.js`

Table: `combos` with JSON `models` array. `getComboByName()` for model resolution.

### 4.7 `aliasRepo.js`

KV scopes: `modelAliases`, `customModels`, `mitmAlias`. Atomic check-then-insert for custom models with KV composite PK guard.

### 4.8 `pricingRepo.js`

KV scope `pricing`. User overrides merged on top of `PROVIDER_PRICING` from `open-sse/providers/pricing.js`. 5-second in-memory cache. Per-provider read-merge-write.

### 4.9 `disabledModelsRepo.js`

KV scope `disabledModels`. Key = providerAlias, value = JSON array of disabled model IDs. Set dedup.

### 4.10 `usageRepo.js`

Tables: `usageHistory` + `usageDaily`. Dedup on 7-field exact match before insert. Three writes in one transaction (+ daily upsert + lifetime counter). Cost delegates to `open-sse/providers/pricing.js`. In-memory `_pendingRequests` (not persisted), `_recentRing` (50 cap, initialized from DB), `_connectionMapCache` (30s TTL). Stats via daily summaries (>=7d) or raw history (24h). `appendRequestLog()` is a no-op. ARCHITECTURE.md log.txt reference is stale.

### 4.11 `requestDetailsRepo.js`

Table: `requestDetails`. Gated by `enableObservability2` (default off). Buffered writer: batch size 20, flush interval 5000ms. Shutdown flush on beforeExit/SIGINT/SIGTERM/exit. Auto-prune to 1000 rows. Truncation at 5KB JSON. Header sanitization strips auth headers. Paginated queries.

---

## 5. DATA_DIR and ~/.9router Compatibility

Source: `src/lib/dataDir.js`, `src/lib/db/paths.js`

**Resolution order:** `DATA_DIR` env var (skips Unix paths on Windows, falls back on EACCES) → default: Windows `%APPDATA%/9router`, macOS/Linux `~/.9router`.

**Path layout:**

- `{DATA_DIR}/db/data.sqlite` — main SQLite database
- `{DATA_DIR}/db/backups/` — pre-schema-change backups
- `{DATA_DIR}/db/.migrated-from-json` — marker file
- `{DATA_DIR}/db.json` — legacy (read-only after migration)
- `{DATA_DIR}/usage.json` — legacy (read-only)
- `{DATA_DIR}/disabledModels.json` — legacy (read-only)
- `{DATA_DIR}/request-details.json` — legacy (read-only)

**Confirmed**: `DATA_DIR` and `~/.9router` resolve to the same physical directory when `DATA_DIR` is unset. ARCHITECTURE.md claim about independence is stale.

---

## 6. Legacy Files and Additive Sync

Four legacy JSON files imported once into the main SQLite DB. Row-count assertion per table with `MigrationAborted` rollback on mismatch. `.migrated-from-json` marker prevents re-import. Additive sync auto-creates missing tables, columns, and indexes on every boot.

---

## 7. Backup/Restore

Two triggers: pre-schema-change (when `SCHEMA_VERSION` > stored `backupSchemaVersion`) and pre-legacy-migration. Implementation uses `ATTACH DATABASE` to copy all tables **except** `requestDetails` into a compacted `.sqlite` file. Retention: keep 3 newest backup directories. **No automated restore path.** Recovery is manual copy.

---

## 8. Import/Export

**ExportDb** (`GET /api/settings/database`): full snapshot of settings, providerConnections, providerNodes, proxyPools, apiKeys, combos, modelAliases, customModels, mitmAlias, pricing. Does NOT export usageHistory, usageDaily, requestDetails, _meta, disabledModels.

**ImportDb** (`POST /api/settings/database`): wipes all those tables then re-inserts inside a single transaction. Missing payload keys cause table wipe (destructive partial import). Re-applies outbound proxy settings after import.

**Auth**: password (`x-9r-password`) or CLI token (`x-9r-cli-token`).

---

## 9. Credential Persistence and Encryption

**No encryption-at-rest found.** OAuth tokens (`accessToken`, `refreshToken`, `idToken`) and API keys stored as plaintext JSON in `providerConnections.data`. Dashboard API keys stored as plaintext in `apiKeys.key`. Protection is filesystem-level only.

---

## 10. Transactions and Deduplication

All 11 repos use adapter `.transaction()` for read-merge-write. Dedup strategies:

- **Usage**: exact 7-field match (timestamp, provider, model, connectionId, apiKey, promptTokens, completionTokens)
- **OAuth connections**: email + provider + workspace ID heuristics
- **API key connections**: provider + name match
- **Custom models**: check-then-insert with KV composite PK
- **Disabled models**: Set merge
- **Aliases/pricing**: INSERT OR REPLACE (last-write-wins)

---

## 11. Shutdown Durability

WAL adapters (better/bun/node): checkpoint WAL on `beforeExit`/`SIGINT`/`SIGTERM`. sql.js: persist via `db.export()` + `writeFileSync` on same signals + `exit`. requestDetailsRepo: separate shutdown handler flushes write buffer.

**Gaps**: SIGKILL/power loss loses up to 60s (WAL checkpoint interval) or 100ms (sql.js debounce). Request details buffer loses up to 5s (flush interval).

**Uncertain**: requestDetailsRepo's `exit` handler is `async` with `await`, but Node.js `exit` events run synchronous code only. May be ineffective.

---

## 12. Concurrency Assumptions

Single-process only. Global state per-process. Synchronous better-sqlite3 API prevents intra-process transaction races. 5-second `busy_timeout` for inter-process contention. No row-level locking or retry loops.

Concurrent tests validate: 100 parallel saves (no count loss), 50 parallel settings (no merge loss), 20 parallel `updateProviderConnection` (all fields preserved), 30 parallel custom model adds (exactly 1 wins), 30 parallel pricing updates (all merge).

---

## 13. Stale Documentation in ARCHITECTURE.md

Six stale claims:

| Stale Claim | Actual |
| ------------- | -------- |
| `DB[(db.json)]` in diagram | `data.sqlite` |
| `UDB[(usage.json + log.txt)]` in diagram | SQLite tables |
| localDb.js references `db.json` | Shim re-exporting from `db/index.js` |
| File: `${DATA_DIR}/db.json` | `{DATA_DIR}/db/data.sqlite` |
| Files: `~/.9router/usage.json`, `~/.9router/log.txt` | No longer written |
| usageDb independent of `DATA_DIR` | Re-exports from main SQLite DB |

---

## 14. Coverage Gaps

1. No encryption-at-rest
2. No automated restore path from backups
3. sql.js crash recovery untested
4. Multi-process concurrency untested
5. exit event handler async uncertainty (untested)
6. `.migrated-from-json` marker edge case undocumented
7. importDb partial-payload destruction (no guard)
8. Observability toggle gap (`enableObservability2` vs `enableObservability`)
9. Usage dedup COALESCE imprecision
10. No streaming backup (memory-expensive for large DBs)

---

## 15. Invariants

1. SCHEMA_VERSION=1 bump triggers exactly one pre-schema backup
2. settings table has 0 or 1 rows (enforced by CHECK id=1)
3. `_meta.schemaVersion` drives migration; `SCHEMA_VERSION` drives backup
4. Backups always exclude requestDetails
5. All four shim files (localDb, usageDb, requestDetailsDb, disabledModelsDb) have zero logic
6. Legacy files read-only after migration
7. importDb always wipes then re-inserts (never merges)
8. exportDb never exports usage or request-detail data

---

## 16. Parity Requirements (Observed Behavior)

- Store provider connections with OAuth tokens and API keys in local DB (no encryption, or consciously add)
- Support additive schema sync on boot (new tables/columns/indexes)
- Support legacy JSON import with row-count assertion and rollback
- Support full export/import of non-usage entity types via password-authenticated API
- Store usage history with dedup-on-insert to prevent double-counting
- Store request-detail observability with buffered batched writes, auto-prune, size truncation, header sanitization
- Backup database (excluding requestDetails) before schema changes; retain 3 newest backups
- Persist all state in single SQLite file at `{DATA_DIR}/db/data.sqlite`
- Provide singleton-settings-row with defaults merge
- Provide KV table pattern (scope, key, value) for aliases, custom models, mitm, pricing, disabled models
- Provide connection priority auto-reordering inside transactions
- Provide OAuth connection dedup (email + workspaceId + username heuristics)

---

## 17. User Questions

1. **Encryption-at-rest**: Encrypt credentials at rest? Original does not.
2. **Multi-process support**: Support multiple instances sharing data directory?
3. **Automated restore**: Automated restore from backups? Original is manual-only.
4. **Import contract**: Preserve destructive partial-import or merge?
5. **Observability defaults**: Request-detail recording on or off by default?
6. **Backup inclusion**: Continue excluding requestDetails from backups?
7. **Legacy import**: Preserve `.migrated-from-json` marker behavior?
8. **Usage dedup**: Preserve exact-match dedup or switch to upsert-based?

---

## 18. Test Coverage

| File | Lines | Coverage |
| ------ | ------- | ---------- |
| `db-sqlite-vs-lowdb.test.js` | 287 | API parity: CRUD for all repos, export/import roundtrip, pricing, chart data |
| `db-concurrent.test.js` | 171 | Concurrency: parallel writes, OAuth race, custom model race, pricing race, daily summary |
| `request-details-tab.test.js` | 252 | Crash safety: corrupt data, pagination, truncation, backup exclusion, API validation |

**Not tested**: sql.js crash recovery, multi-process concurrency, backup restoration, legacy migration flow, importDb partial payload, exportDb shape contract, buffer under load, WAL timer behavior, PRAGMA after init.

---

## Summary

- **Schema**: 9 tables + 1 meta, 19 indexes, 7 PRAGMAs, SCHEMA_VERSION=1
- **Adapters**: 4-stage fallback chain (bun:sqlite → better-sqlite3 → node:sqlite → sql.js)
- **Repos**: 11 files, all use adapter transaction()
- **Credentials**: Plaintext, no encryption-at-rest
- **Legacy**: 4 JSON files, one-time import with assertion and rollback
- **Backup**: ATTACH DATABASE, excludes requestDetails, retains 3 newest
- **Import/export**: Full snapshot, destructive replace, password/CLI-token auth
- **Shutdown**: WAL checkpoint or debounced serialize
- **Concurrency**: Single-process only, synchronous adapters prevent intra-process races

---

*Audited at commit `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (v0.5.40).*
