# Phase 1 Foundation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Establish the buildable, testable, secure, contract-first gorouter repository foundation on which every parity feature can be implemented without changing approved boundaries.

**Architecture:** Create the modular-monolith skeleton, bootstrap/config lifecycle, PostgreSQL infrastructure, security middleware primitives, formal contracts, source-backed fixtures, observability primitives, and CI test gates. This phase does not implement complete model routing or product workflows.

**Tech Stack:** Go, chi, pgx/PostgreSQL 16–18, OpenAPI, JSON Schema, React/TypeScript/Vite foundation, Prometheus, Go test/race/fuzz tooling, npm frontend tooling.

## Global Constraints

- Follow root `IMPLEMENTATION-PLAN.md`, `DECISIONS.md`, `ARCHITECTURE.md`, and `PARITY-LEDGER.md`.
- Upstream baseline is commit `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`.
- Keep `internal/app` and `internal/domain` separate.
- One binary/process, many goroutines, one HTTP listener, one PostgreSQL pool.
- No Redis, microservices, active-active topology, or silent architecture additions.
- PostgreSQL 16, 17, and 18 compatibility is mandatory.
- Config precedence is flags > env > `.env` > DB/dashboard > defaults.
- Model APIs require API keys even on loopback; dashboard/admin use session/PAT boundaries.
- Use TDD and atomic commits. No task closes without source mapping and evidence.
- This phase may create repository/scaffold code only when execution is authorized after plan review.

---

## File and responsibility map

| Path | Responsibility |
| --- | --- |
| `go.mod`, `go.sum` | Go module and locked dependencies |
| `cmd/gorouter/main.go` | Minimal executable entry and mode dispatch |
| `internal/bootstrap/{app,config,lifecycle,modes}.go` | Dependency composition, precedence, startup/drain/shutdown |
| `internal/shared/{clock,ids,errors}.go` | Injected deterministic primitives and safe error shape |
| `internal/domain/*` | Initial auth/key/config/job primitives without infrastructure imports |
| `internal/persistence/postgres/*` | pgx pool, advisory lock, transaction boundary, migrations, repository interfaces |
| `internal/transport/httpserver/*` | Server, trusted proxy, correlation, recovery, CORS, auth extraction, health |
| `internal/observability/*` | JSON logging and Prometheus registry |
| `api/*` | Versioned contracts and provenance metadata |
| `frontend/*` | TypeScript/Vite testable shell foundation only |
| `tests/fixtures/upstream/*` | Sanitized source-backed fixtures and manifest |
| `docs/implementation/*` | Traceability and gate evidence |
| `.github/workflows/*` | Repeatable test/security checks |

### Task P1-T01: Initialize repository governance and traceability

**Files:**
- Create: `go.mod`
- Create: `.gitignore`
- Create: `LICENSE`
- Create: `NOTICE`
- Create: `docs/implementation/upstream-map.yaml`
- Create: `docs/implementation/parity-status.yaml`
- Create: `docs/implementation/decision-status.yaml`
- Create: `tests/fixtures/upstream/manifest.json`
- Test: `internal/governance/traceability_test.go`

**Interfaces:**
- Produces: machine-readable baseline SHA, active decision references, parity row ownership, fixture provenance.
- Consumes: approved planning documents and read-only upstream clone.

- [ ] **Step 1: Write the failing governance test**

Define table-driven tests that load all three YAML files and fixture manifest, assert baseline SHA exactly equals `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`, reject duplicate IDs, require owner phase/task/test fields, and reject cancelled decision IDs.

- [ ] **Step 2: Run RED**

Run: `go test ./internal/governance -run TestTraceability -v`

Expected: FAIL because traceability documents and parser do not exist.

- [ ] **Step 3: Add minimum governance implementation and records**

Create a small YAML/JSON loader under `internal/governance` and seed records for every Phase 1-owned ledger row. Include Apache-2.0 license and upstream attribution in `NOTICE`.

- [ ] **Step 4: Run GREEN**

Run: `go test ./internal/governance -v`

Expected: PASS with exact baseline and no missing ownership.

- [ ] **Step 5: Commit**

Commit message: `chore: establish rewrite governance and traceability`

### Task P1-T02: Establish Go entrypoint, modes, and configuration precedence

**Files:**
- Create: `cmd/gorouter/main.go`
- Create: `internal/bootstrap/config.go`
- Create: `internal/bootstrap/modes.go`
- Create: `internal/bootstrap/app.go`
- Create: `internal/bootstrap/config_test.go`
- Create: `.env.example`

**Interfaces:**
- Produces: `type Config`, `LoadConfig(args []string, getenv func(string) string, readDotEnv func() map[string]string, readDB func(context.Context) (DBConfig, error)) (Config, error)`, `type Mode string`, `ParseMode(args []string) (Mode, error)`.
- Consumes: deterministic clock/error helpers from Task P1-T01 only if needed.

- [ ] **Step 1: Write RED tests** for precedence, stable default port, no random-port fallback, supported aliases, `server`, lifecycle, CLI, tray/helper modes, and secret redaction from config errors.
- [ ] **Step 2: Run RED:** `go test ./internal/bootstrap -run 'TestLoadConfig|TestParseMode' -v`; expect missing symbols.
- [ ] **Step 3: Implement minimum parser/loader** with immutable source attribution per field so the UI can later explain overrides.
- [ ] **Step 4: Run GREEN:** `go test ./internal/bootstrap -v`; expect PASS.
- [ ] **Step 5: Commit:** `feat: add deterministic bootstrap configuration`

### Task P1-T03: Create PostgreSQL pool, advisory lock, and transaction contracts

**Files:**
- Create: `internal/persistence/postgres/pool.go`
- Create: `internal/persistence/postgres/lock.go`
- Create: `internal/persistence/postgres/tx.go`
- Create: `internal/persistence/postgres/pool_test.go`
- Create: `internal/persistence/postgres/tx_integration_test.go`
- Create: `tests/integration/postgres/compose.yaml`

**Interfaces:**
- Produces: `Open(ctx context.Context, cfg PoolConfig) (*pgxpool.Pool, error)`, `AcquireRuntimeLock(ctx context.Context, pool *pgxpool.Pool) (release func(context.Context) error, err error)`, `WithinTx(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context, pgx.Tx) error) error`.
- Invariant: second active gorouter process on same database is rejected clearly; goroutine concurrency remains unrestricted.

- [ ] **Step 1: Write RED integration tests** against PG 16/17/18 matrices for pool health, transaction commit/rollback, context cancellation, and advisory-lock rejection.
- [ ] **Step 2: Run RED:** `go test ./internal/persistence/postgres -tags=integration -v`; expect missing implementation.
- [ ] **Step 3: Implement minimum pgx infrastructure** with finite connect timeout, pool metrics hooks, and guaranteed transaction cleanup.
- [ ] **Step 4: Run GREEN** against each supported PG version using the integration compose profile.
- [ ] **Step 5: Commit:** `feat(db): add postgres pool and runtime lock`

### Task P1-T04: Implement migration framework and foundational schema

**Files:**
- Create: `internal/persistence/postgres/migrations/000001_foundation.up.sql`
- Create: `internal/persistence/postgres/migrations/000001_foundation.down.sql`
- Create: `internal/persistence/postgres/migrations/runner.go`
- Create: `internal/persistence/postgres/migrations/runner_test.go`
- Create: `internal/persistence/postgres/schema_contract_test.go`

**Interfaces:**
- Produces: embedded ordered migrations, `Migrate(ctx context.Context, pool *pgxpool.Pool) (Result, error)`, schema metadata supporting one-release rollback compatibility.
- Foundational tables: migration metadata, settings, dashboard admin/session, model API keys, PATs, audit events, provider connection core/JSONB, job state.

- [ ] **Step 1: Write RED tests** for clean apply, idempotent rerun, advisory migration lock, failure rollback, schema version, hash-only key columns, restricted credential columns, UTC timestamps, and absence of automatic legacy SQLite import.
- [ ] **Step 2: Run RED:** `go test ./internal/persistence/postgres/... -run 'TestMigration|TestFoundationSchema' -v`.
- [ ] **Step 3: Implement minimal embedded runner and SQL** using expand-first patterns.
- [ ] **Step 4: Run GREEN** across PG 16/17/18.
- [ ] **Step 5: Commit:** `feat(db): add foundational postgres schema`

### Task P1-T05: Define domain repository and transaction boundaries

**Files:**
- Create: `internal/domain/auth/types.go`
- Create: `internal/domain/keys/types.go`
- Create: `internal/domain/provider/types.go`
- Create: `internal/domain/jobs/types.go`
- Create: `internal/app/tx/manager.go`
- Create: `internal/app/tx/manager_test.go`
- Create: `internal/persistence/postgres/repositories/factory.go`

**Interfaces:**
- Produces transaction-scoped repository factory interfaces; application services own transactions; handlers/CLI cannot import concrete PG repositories.
- Key interfaces include `SessionRepository`, `APIKeyRepository`, `PATRepository`, `ProviderConnectionRepository`, `AuditRepository`, `JobRepository`.

- [ ] **Step 1: Write RED compile-time architecture tests** using package import analysis to reject transport→postgres imports and domain→transport/postgres imports; add transaction rollback behavior test.
- [ ] **Step 2: Run RED:** `go test ./internal/app/tx ./internal/governance -v`.
- [ ] **Step 3: Add interfaces/factory/manager** with transaction-scoped repository construction.
- [ ] **Step 4: Run GREEN** and `go list -deps ./...` architecture check.
- [ ] **Step 5: Commit:** `feat: establish domain repository boundaries`

### Task P1-T06: Implement trusted proxy, correlation, recovery, and CORS primitives

**Files:**
- Create: `internal/transport/httpserver/server.go`
- Create: `internal/transport/httpserver/middleware/trustedproxy.go`
- Create: `internal/transport/httpserver/middleware/correlation.go`
- Create: `internal/transport/httpserver/middleware/recovery.go`
- Create: `internal/transport/httpserver/middleware/cors.go`
- Test: matching `_test.go` files plus `tests/security/trusted_proxy_test.go`

**Interfaces:**
- Produces trusted client identity only from loopback or configured CIDR proxy peers; strips untrusted forwarding/internal headers; correlation ID in request context and response; separate model API versus Admin/dashboard CORS policies.

- [ ] **Step 1: Write socket-level RED tests** reproducing direct spoof attempts and approved reverse-proxy traffic based on upstream `custom-server.js` invariant.
- [ ] **Step 2: Run RED:** `go test ./internal/transport/httpserver/... ./tests/security/... -run 'Trusted|CORS|Recovery' -v`.
- [ ] **Step 3: Implement minimum middleware** with no trust of forwarded headers from unapproved peers.
- [ ] **Step 4: Run GREEN and race tests:** `go test -race ./internal/transport/httpserver/... ./tests/security/...`.
- [ ] **Step 5: Commit:** `feat(http): add trusted proxy security boundary`

### Task P1-T07: Implement key hashing, dashboard password, session, PAT, and actor primitives

**Files:**
- Create: `internal/domain/auth/{password,session,actor}.go`
- Create: `internal/domain/keys/{modelkey,pat}.go`
- Create: `internal/app/auth/service.go`
- Create: `internal/app/keys/service.go`
- Create: `internal/persistence/postgres/repositories/{sessions,keys,pats,audit}.go`
- Test: unit/integration tests for each service and repository

**Interfaces:**
- Produces local password hash verification, 30-day sliding PG session, CSRF token binding, hash-only model keys/PATs with prefix/expiry/last-used/revoke metadata, unified actor context.
- Enforces API keys on model routes even loopback.

- [ ] **Step 1: Write RED tests** for one-time reveal, no plaintext recovery, password `12345678` warning, lockout/backoff, session sliding expiry/revocation, PAT expiry/revoke, model-key extraction transports, actor audit attribution.
- [ ] **Step 2: Run RED:** focused domain/app/repository commands.
- [ ] **Step 3: Implement minimum services/repositories** using strong password and token hashing and transactional audit records.
- [ ] **Step 4: Run GREEN**, race tests, and secret-scan fixtures ensuring raw tokens never appear.
- [ ] **Step 5: Commit:** `feat(auth): add session and token foundations`

### Task P1-T08: Establish structured errors, logging, metrics, and safe health

**Files:**
- Create: `internal/shared/errors.go`
- Create: `internal/observability/logging/logger.go`
- Create: `internal/observability/metrics/registry.go`
- Create: `internal/transport/httpserver/health/{public,detailed}.go`
- Test: `tests/security/error_redaction_test.go` and package tests

**Interfaces:**
- Produces two-layer error contract: sanitized client error and richer secret-free internal diagnostic with correlation ID; JSON stdout logging; Prometheus registry; public safe health and protected detailed health.

- [ ] **Step 1: Write RED tests** for error field contract, credential redaction, public-health minimalism, detailed-health authorization, metrics cardinality boundaries.
- [ ] **Step 2: Run RED** with focused package/security tests.
- [ ] **Step 3: Implement minimum error/log/metrics/health primitives**.
- [ ] **Step 4: Run GREEN and fuzz redaction/parser functions**.
- [ ] **Step 5: Commit:** `feat(obs): add safe diagnostics and health`

### Task P1-T09: Establish formal contracts and fixture provenance

**Files:**
- Create: `api/admin-v1.openapi.yaml`
- Create: `api/model-contracts/{openai,responses,claude,gemini,codex}/README.md`
- Create: `api/model-contracts/native/README.md`
- Create: `api/compatibility/routes.yaml`
- Create: `tools/fixtures/extract.go`
- Create: `tests/parity/contracts_test.go`
- Create: sanitized fixtures under `tests/fixtures/upstream`

**Interfaces:**
- Produces versioned Admin API schema, compatibility route manifest, fixture manifest linking each fixture to exact upstream file/symbol/test.
- Does not force native/binary protocols into lossy JSON schemas.

- [ ] **Step 1: Write RED parity tests** requiring every Phase 2 route/format to have provenance and expected request/response/error/terminal fixtures.
- [ ] **Step 2: Run RED:** `go test ./tests/parity -run TestContractManifest -v`.
- [ ] **Step 3: Add minimum schemas/manifests/fixture extractor** for Phase 2 scope.
- [ ] **Step 4: Run GREEN** and validate OpenAPI/JSON/YAML parsing.
- [ ] **Step 5: Commit:** `test(parity): add source-backed contract fixtures`

### Task P1-T10: Establish frontend, generated client, and design-system foundation

**Files:**
- Create: `frontend/package.json`, `frontend/tsconfig.json`, `frontend/vite.config.ts`
- Create: `frontend/src/app/*`
- Create: `frontend/src/design/{tokens,theme,components}/*`
- Create: `frontend/src/generated/admin-v1.ts`
- Create: `frontend/src/shared/api/client.ts`
- Create: `frontend/src/app/app.test.tsx`
- Create: `frontend/e2e/shell.spec.ts`

**Interfaces:**
- Produces embedded-build-ready React shell, generated Admin API client, approved light/dark tokens, OpenRouter-inspired shell primitives, theme persisted per browser.
- No product workflow is implemented yet.

- [ ] **Step 1: Write RED tests** for shell layout, theme control, accessible navigation/drawer, generated-client freshness.
- [ ] **Step 2: Run RED:** `npm --prefix frontend test` and `npm --prefix frontend run typecheck`.
- [ ] **Step 3: Implement minimum Vite/React/design foundation** with WCAG-compatible token contrast.
- [ ] **Step 4: Run GREEN**, typecheck, build, and Playwright shell test.
- [ ] **Step 5: Commit:** `feat(ui): establish dashboard design foundation`

### Task P1-T11: Establish CI, quality, security, and evidence automation

**Files:**
- Create: `.github/workflows/{test,security,frontend,postgres-matrix}.yml`
- Create: `Makefile`
- Create: `tools/gate/verify.go`
- Create: `docs/implementation/gate-evidence/schema.json`
- Create: `internal/governance/gate_test.go`

**Interfaces:**
- Produces repeatable commands for format, vet, unit, integration, race, fuzz smoke, frontend tests, PG matrix, secret scan, dependency/license/SBOM checks.

- [ ] **Step 1: Write RED tests** that reject missing required jobs/commands and malformed gate evidence.
- [ ] **Step 2: Run RED:** `go test ./internal/governance -run TestGate -v`.
- [ ] **Step 3: Add workflows/Make targets/verifier** with pinned tool versions.
- [ ] **Step 4: Run GREEN** and execute all local-safe targets.
- [ ] **Step 5: Commit:** `ci: establish foundation quality gates`

## Mandatory Exit Gate: Foundation Gate

Phase 1 passes only when all evidence is committed:

- [ ] Repository builds the minimal binary on supported Go toolchain.
- [ ] Traceability maps every Phase 1 ledger row and active decision to owner/tests/docs.
- [ ] PostgreSQL 16/17/18 pool, transactions, migrations, and single-process lock tests pass.
- [ ] No transport package imports concrete PostgreSQL repositories; no domain package imports transport/infrastructure.
- [ ] Trusted-proxy spoofing, CORS split, correlation, recovery, public/detailed health tests pass.
- [ ] Password/session/PAT/model-key primitives pass hashing, expiry, revocation, CSRF, lockout, and redaction tests.
- [ ] JSON logs and metrics contain no secrets and bounded labels.
- [ ] Admin API contract and Phase 2 fixture provenance validate.
- [ ] Frontend shell builds, typechecks, and passes accessibility-focused shell tests in light/dark/mobile.
- [ ] `go test ./...`, `go test -race ./...`, `go vet ./...`, frontend test/typecheck/build, and PG matrix pass.
- [ ] Security scans required at this phase have no unresolved high-severity finding.
- [ ] `docs/implementation/gate-evidence/phase-1.json` records exact commands, SHAs, artifacts, and reviews.
- [ ] User/reviewer explicitly approves the Foundation Gate before Phase 2 begins.
