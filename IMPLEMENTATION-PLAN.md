# gorouter Master Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build gorouter from an empty implementation tree into a stable, cross-platform, production-ready Go rewrite of original 9Router with the approved compatibility, product, reliability, performance, security, and packaging contracts.

**Architecture:** Implement the approved modular monolith from `ARCHITECTURE.md`: one Go binary and active process, many goroutines, one HTTP listener, one PostgreSQL pool, embedded React dashboard, domain-oriented application services, protocol-aware provider engine, and platform adapters. Original upstream behavior is the source baseline; `PARITY-LEDGER.md` defines preserve/change/remove dispositions, while `DECISIONS.md` is the user-owned authority.

**Tech Stack:** Go, chi, pgx/PostgreSQL 16–18, React, TypeScript, Vite, shadcn/ui, Tailwind CSS, OpenAPI, Prometheus, embedded static assets, Go test/race/fuzz/bench tooling, Playwright, platform packaging toolchains.

## Global Constraints

- Baseline source is `decolua/9router` commit `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`.
- `DECISIONS.md`, `ARCHITECTURE.md`, and `PARITY-LEDGER.md` are approved and authoritative; conflicts return to the user.
- Cancelled decisions are audit history and must not become requirements.
- This is a rewrite of original 9Router, not a greenfield product.
- Full upstream capability parity is required for the first stable release except explicit ledger changes/removals.
- Original external contracts are preserved unless an approved decision changes them; bugs are not silently preserved or fixed.
- Use TDD: observe RED, implement minimum GREEN, refactor while green, then commit atomically.
- A phase may not start until the preceding mandatory Exit Gate is signed off with stored evidence.
- No provider is considered complete without source mapping, fixtures, automated tests, and required live validation.
- Never expose provider credentials, PATs, model API keys, OAuth tokens, cookies, or backup contents in logs or evidence.
- PostgreSQL 16, 17, and 18 are supported for server deployments.
- One active process per database is enforced; concurrency occurs through goroutines and one PG pool.
- The dashboard must satisfy WCAG 2.2 AA and the approved OpenRouter-inspired visual system while retaining original 9Router workflows.
- Stable release gates include p95 router overhead under 20ms, at least 2,000 concurrent streams, at least 100,000 RPM for lightweight endpoints on 4 vCPU/16 GB, and a 72-hour mixed-load soak.
- Stable release requires automated SAST, dependency/license/SBOM, secret scan, fuzzing, race detector, authz/SSRF/trusted-proxy tests, signed artifacts, and provenance.
- Beta and stable are the only release channels.
- Do not begin product coding merely because this document exists; execution starts only after the user reviews and authorizes this plan set.

---

## 1. Authoritative inputs

| Document | Purpose |
| --- | --- |
| `DECISIONS.md` | User-owned architecture, product, UX, security, compatibility, release, and lifecycle decisions |
| `ARCHITECTURE.md` | Approved system shape, boundaries, dependency direction, repository tree, and core flows |
| `PARITY-LEDGER.md` | Per-capability preserve/change/remove disposition and required observable evidence |
| `audit/00-upstream-audit.md`–`audit/13-recent-requests-behavior.md` | Source-backed behavior, invariants, risks, and uncertainties |
| `upstream-original/` | Read-only exact upstream source used for mapping and parity fixtures |

## 2. Phase and gate model

A **Phase** is a body of implementation work. An **Exit Gate** is mandatory evidence that the phase is complete and safe to build upon. A phase is never declared complete from code presence alone.

| Phase | Deliverable | Mandatory Exit Gate |
| --- | --- | --- |
| 1. Foundation | Buildable repository, contracts, config, PG foundation, security boundaries, test harnesses | Foundation Gate |
| 2. End-to-End Vertical Slice | One production-shaped model request path through generic and specialized providers, streaming, fallback, usage | Vertical Slice Gate |
| 3. Engine and Provider Matrix | Complete formats, translators, modalities, provider registry/executors, OAuth, routing, combos | Engine Parity Gate |
| 4. Product Surfaces | Admin API, dashboard, CLI, scheduler, host integrations, operational workflows | Product Parity Gate |
| 5. Release Readiness | Packaging, installers, live provider matrix, performance, security, soak, beta, fixes, stable | Stable Release Gate |

Detailed plans:

1. [`plans/phase-1-foundation.md`](plans/phase-1-foundation.md)
2. [`plans/phase-2-vertical-slice.md`](plans/phase-2-vertical-slice.md)
3. [`plans/phase-3-engine-provider-matrix.md`](plans/phase-3-engine-provider-matrix.md)
4. [`plans/phase-4-product-surfaces.md`](plans/phase-4-product-surfaces.md)
5. [`plans/phase-5-release-readiness.md`](plans/phase-5-release-readiness.md)

## 3. Repository-level responsibility map

The implementation must converge on the approved tree in `ARCHITECTURE.md`.

| Area | Responsibility | First owned in |
| --- | --- | --- |
| `cmd/gorouter` | Binary entrypoint and mode dispatch | Phase 1 |
| `internal/bootstrap` | Composition, config precedence, startup, drain, shutdown, modes | Phase 1 |
| `internal/transport` | HTTP/CLI parsing, auth extraction, CORS, trusted proxy, protocol encoding | Phases 1–4 |
| `internal/app` | Use cases and transaction boundaries shared by HTTP, dashboard, CLI, jobs | Phases 1–4 |
| `internal/domain` | Rules and state transitions without transport/DB dependencies | Phases 1–4 |
| `internal/engine` | Request orchestration, formats, translators, streaming, routing, providers, mutators | Phases 2–3 |
| `internal/persistence/postgres` | pgx pool, migrations, advisory lock, repositories, backup, retention | Phases 1–4 |
| `internal/scheduler` | Job lifecycle, locking, retry, run-now, status | Phase 4 |
| `internal/host` | Platform-specific service/tray/updater/tunnel/MITM/helper integrations | Phases 4–5 |
| `internal/observability` | JSON logs, metrics, correlation, dashboard log/event projections | Phases 1–5 |
| `api` | OpenAPI/model schemas and compatibility fixtures | Phases 1–4 |
| `frontend` | Embedded React dashboard using Admin API contracts | Phase 4 |
| `tests` | Parity, golden, integration, provider, security, performance, soak | All phases |
| `packaging` | npm launcher and Windows/macOS/Linux/Docker distribution | Phase 5 |
| `docs` | User, operator, API, architecture, security, troubleshooting docs | All phases, release-complete in Phase 5 |

## 4. Cross-phase traceability

Every implementation task must cite:

- one or more `PARITY-LEDGER.md` rows;
- the governing `DECISIONS.md` numbers;
- exact upstream source files/symbols or a recorded audit section;
- tests or evidence proving observable behavior.

Create and maintain these execution artifacts during Phase 1:

- `docs/implementation/upstream-map.yaml`: upstream file/symbol/workflow → Go package/task/tests.
- `docs/implementation/parity-status.yaml`: ledger row → phase/task/status/evidence.
- `docs/implementation/decision-status.yaml`: active decision → owning phase/task/tests/docs.
- `docs/implementation/gate-evidence/`: immutable phase-gate evidence manifests.
- `tests/fixtures/upstream/`: source-backed sanitized fixtures with provenance metadata.

A task cannot close if its traceability record is missing.

## 5. Dependency graph and parallelism

```text
Phase 1 Foundation
  ├─ contracts and fixtures
  ├─ config/bootstrap/security
  ├─ PostgreSQL foundation
  └─ test/CI foundation
          ↓
Phase 2 Vertical Slice
  ├─ model parsing/routing/account selection
  ├─ generic provider path
  ├─ one specialized high-risk provider path
  ├─ streaming/cancellation/terminal events
  └─ usage durability
          ↓
Phase 3 Engine and Provider Matrix
  ├─ formats/translators/native protocols
  ├─ full provider registry/executors
  ├─ OAuth/refresh families
  ├─ modalities
  └─ combos/fusion/mutators
          ↓
Phase 4 Product Surfaces
  ├─ Admin API and compatibility management routes
  ├─ dashboard and realtime views
  ├─ CLI/lifecycle/scheduler
  └─ host integrations
          ↓
Phase 5 Release Readiness
  ├─ packaging/installers/updater
  ├─ live provider matrix
  ├─ security/accessibility/performance/soak
  ├─ beta and defect closure
  └─ stable release
```

Within a phase, parallel work is allowed only when teams own separate packages/contracts and do not introduce competing interfaces. Shared interfaces are merged first and frozen by tests before consumers proceed.

## 6. Universal task workflow

Every task in the detailed plans follows this sequence:

1. Read the cited upstream source and approved decisions.
2. Update traceability records with exact ownership.
3. Write the smallest failing unit/contract/golden/integration test.
4. Run the exact targeted command and capture expected RED reason.
5. Implement the minimum behavior needed for GREEN.
6. Run targeted tests, then package/domain tests.
7. Run race/fuzz/security checks when the task touches concurrency, parsing, auth, streaming, or secrets.
8. Update user/operator/developer documentation for changed observable behavior.
9. Review against source mapping and ledger row.
10. Commit implementation and its direct tests atomically.

## 7. Global verification commands

These commands become available as the repository foundation is created in Phase 1:

```bash
go test ./...
go test -race ./...
go vet ./...
gofmt -w .
go test ./tests/parity/... ./tests/golden/...
npm --prefix frontend run typecheck
npm --prefix frontend run test
npm --prefix frontend run build
npm --prefix frontend run test:e2e
```

Phase-specific plans add focused commands and expected evidence. Commands requiring PostgreSQL, browsers, credentials, platform signing, or load infrastructure run in controlled integration/release environments, not ad hoc developer machines.

## 8. Gate evidence format

Each phase writes `docs/implementation/gate-evidence/phase-N.json` with:

```json
{
  "phase": 1,
  "baselineCommit": "79918c7830695bbca4a45c9fea4a42c3e9fd73d1",
  "gorouterCommit": "the exact 40-character implementation commit recorded by the gate runner",
  "completedTasks": ["P1-T01"],
  "ledgerRows": ["HTTP-01"],
  "decisionRefs": [306],
  "commands": [{"command": "go test ./...", "exitCode": 0}],
  "artifacts": ["tests/fixtures/upstream/manifest.json"],
  "reviewers": ["implementation", "parity", "security"],
  "approvedAt": "RFC3339 timestamp"
}
```

The actual implementation replaces angle-bracket examples with concrete values. Gate evidence is reviewed and committed; it is not generated from self-claims.

## 9. Change-control rules

- If upstream source and an active decision conflict, stop and ask the user per decision #252.
- If implementation reveals an unrecorded behavior choice, stop and ask; do not silently preserve or change it.
- Bug fixes require an upstream-behavior test, proposed preserve/fix tradeoff, user decision, and regression test.
- A phase gate may explicitly defer only work assigned to a later approved phase. Nothing in the stable parity ledger may be silently deferred past Phase 5.
- No release gate may be weakened to fit current implementation.

## 10. Completion definition

This plan set is complete when:

- all five Exit Gates pass;
- every ledger row has final evidence;
- every active decision has implementation/tests/docs ownership;
- every required provider is live-validated;
- all supported formats/modalities/host workflows are tested;
- cross-platform artifacts are signed and proven installable/recoverable;
- accessibility, security, performance, and soak gates pass;
- beta defects are resolved or explicitly user-dispositioned;
- stable artifacts, SBOM, provenance, checksums, docs, and release notes are published.

Until then, gorouter is not a stable production release.
