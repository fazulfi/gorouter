# Phase 3 Engine and Provider Matrix Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Complete the source-backed protocol engine, all original client formats/modalities/providers/specialized executors, OAuth/refresh families, routing/combo modes, mutators, and provider matrix required for full parity.

**Architecture:** Extend the proven Phase 2 pipeline through registries and provider-specific adapters without flattening specialized behavior. Work is organized by behavior families and generated parity manifests; each provider remains incomplete until source mapping, automated fixtures, status/error/OAuth behavior, and live-test definition exist.

**Tech Stack:** Go, net/http, pgx, SSE/NDJSON/binary codecs, source-backed fixtures, fake upstream servers, provider credential test harness, Go fuzz/race/golden tooling.

## Global Constraints

- Vertical Slice Gate must be approved.
- Preserve all original formats, modalities, built-in providers, specialized executors, OAuth families, model resolution, routing, combo modes, and mutators.
- Direct translators/native passthrough remain available; no universal lossy IR.
- Every provider/executor must be live-validated before beta and stable, but normal commits use sanitized fixtures/mocks.
- Provider secrets come only from CI/lab secret stores and are never committed or logged.
- Generic dashboard-addable OpenAI/Anthropic-compatible providers remain supported; nonstandard providers require source code and release.
- Account selection remains priority/cooldown based, not health-weighted or round-robin.
- User decisions govern any upstream bug/design discrepancy; stop rather than silently change behavior.

---

## File and responsibility map

| Path | Responsibility |
| --- | --- |
| `internal/engine/formats/*` | All source/target format detection and codecs |
| `internal/engine/translators/{registry,direct,safeir}` | Direct and safe-pivot translation graph |
| `internal/engine/providers/registry` | Built-in provider metadata/capabilities/auth/media models |
| `internal/engine/providers/generic` | Dynamic compatible provider support |
| `internal/engine/providers/specialized/*` | Protocol/provider-specific executors |
| `internal/engine/oauth/*` | Provider-specific OAuth/import/device/loopback flows |
| `internal/engine/refresh/*` | Proactive/reactive refresh coordinator and singleflight |
| `internal/engine/routing`, `combos` | Aliases, capabilities, cooldowns, combo modes |
| `internal/engine/mutators/*` | RTK, Headroom, Caveman, Ponytail, Pxpipe and ordering |
| `internal/app/providers`, `oauth`, `routing`, `combos`, `media` | Use cases and transactions |
| `tests/providers`, `tests/golden`, `tests/parity` | Provider and protocol evidence |
| `docs/implementation/providers` | Source mapping/live-test contract per provider |

### Task P3-T01: Generate authoritative provider/executor/format manifests

**Files:**
- Create: `tools/upstreammap/providers.go`
- Create: `docs/implementation/provider-matrix.yaml`
- Create: `docs/implementation/format-matrix.yaml`
- Create: `docs/implementation/oauth-matrix.yaml`
- Create: `tests/parity/matrix_test.go`

**Interfaces:**
- Produces immutable IDs and ownership for all 100 audited registry entries, 25 specialized executors, service kinds, auth categories, formats, translators, OAuth flows, tests, docs, and live-test requirements.

- [ ] Write RED tests comparing generated manifests to source-backed audit counts and exact registry fixture list.
- [ ] Run RED.
- [ ] Implement extractor/curated manifest with baseline SHA and symbol citations.
- [ ] Run GREEN; fail on unowned provider/format/executor.
- [ ] Commit: `test(parity): define provider and protocol matrices`

### Task P3-T02: Complete format detection and client codec families

**Files:**
- Create/complete: `internal/engine/formats/{openai,responses,claude,gemini,codex,native}/*`
- Create: endpoint/shape detection registry
- Test: per-format golden/fuzz tests

**Interfaces:**
- Detects source format by endpoint first, then body shape; preserves protocol-specific JSON/SSE/NDJSON/binary/terminal semantics.

- [ ] Write RED fixtures for all audited format identifiers and ambiguous shapes.
- [ ] Run RED.
- [ ] Implement codecs/detection one family at a time, committing each family with tests.
- [ ] Run GREEN, fuzz parsers, and ensure malformed inputs return sanitized errors.
- [ ] Commit each completed format family with `feat(format): add FAMILY compatibility`, replacing `FAMILY` with the exact registry ID from `format-matrix.yaml`.

### Task P3-T03: Complete translator graph and native passthrough

**Files:**
- Complete: `internal/engine/translators/registry.go`
- Create direct-translator files under `internal/engine/translators/direct`; each filename is the concrete source and target format IDs from `format-matrix.yaml` joined as `SOURCE_to_TARGET.go`.
- Create: `internal/engine/translators/safeir/*`
- Test: golden roundtrip and semantic preservation suites

**Interfaces:**
- Selects direct translator before safe pivot; permits native passthrough; rejects unsupported lossy transformations.

- [ ] Build RED graph coverage test requiring every audited source-target pair to resolve explicitly.
- [ ] Port direct translators with thinking/tools/images/errors/terminal fixtures.
- [ ] Implement safe IR only for proven non-lossy paths.
- [ ] Run all golden ordering/roundtrip tests and fuzz event decoders.
- [ ] Commit per translator family.

### Task P3-T04: Complete generic provider adapters and dynamic compatible providers

**Files:**
- Complete: `internal/engine/providers/generic/*`
- Create: `internal/app/providers/compatible.go`
- Create: repositories for compatible nodes/models/validation
- Test: integration/parity tests

**Interfaces:**
- Supports dashboard-defined OpenAI/Anthropic-compatible providers, model discovery, validation, custom URLs including approved unrestricted internal addresses, account/proxy selection, and original permissive validation semantics.

- [ ] Write RED tests for `/models`/fallback probes, 401/403 definitive rejection, temporary/unsupported unknown status, custom embeddings probe, custom model preservation, unrestricted URL behavior.
- [ ] Implement minimum application/provider/repository paths.
- [ ] Run GREEN and document intentional SSRF/rebinding policy from decisions.
- [ ] Commit: `feat(provider): complete compatible provider workflows`

### Task P3-T05: Port specialized executors by protocol family

**Files:**
- Create: one package per specialized executor under `internal/engine/providers/specialized`
- Create: matching fixtures/docs/live-test definitions

**Interfaces:**
- Each executor implements common orchestration boundaries plus native codecs/direct translators needed by its protocol.

- [ ] Group executors by audited transport family: OpenAI-variant, event-stream/NDJSON, binary/protobuf, web/session-cookie, IDE/local import, media-specific.
- [ ] For each executor, write RED request/stream/error/cancel fixtures from upstream source/tests.
- [ ] Port minimum behavior and run GREEN.
- [ ] Add provider metadata, OAuth/refresh hooks, and live-test contract.
- [ ] Review exact source mapping and commit executor atomically.

No executor is marked complete from registry presence alone.

### Task P3-T06: Implement provider-specific OAuth and import flow families

**Files:**
- Create: `internal/engine/oauth/{loopback,fixedcallback,devicecode,dashboardrelay,cookie,pat,ideimport}/*`
- Create: provider-specific modules
- Create: `internal/app/oauth/service.go`
- Create: OAuth session/repository migrations
- Test: end-to-end fake provider/browser callback tests

**Interfaces:**
- Supports random/fixed callback ports, device-code polling, dashboard relay, cookie/PAT/browser token, IDE/local auto-import, PKCE/state/TTL, status polling, cancellation.
- Pending PG state supports concurrency/status but unreconstructible listener/browser flows fail after restart rather than resume.

- [ ] Write RED tests for every flow family and provider redirect contract.
- [ ] Implement shared safe primitives without forcing flows into one universal mechanism.
- [ ] Port provider-specific details and dedup semantics.
- [ ] Run GREEN, race tests, CSRF/PKCE/state-replay tests, and restart-cancellation tests.
- [ ] Commit per flow family/provider group.

### Task P3-T07: Implement unified proactive/reactive refresh coordinator

**Files:**
- Create: `internal/engine/refresh/{coordinator,singleflight,policy}.go`
- Create: provider refresh adapters
- Create: `internal/app/providers/refresh.go`
- Test: fake-clock/concurrency/provider tests

**Interfaces:**
- Single coordinator serves scheduler and request path; per-account singleflight; default 5m-before-expiry or 6h without reliable expiry; global max 4; 10s request wait then fallback if available; 30s attempt, max 3, backoff+jitter; provider-specific audited overrides.

- [ ] Write RED timing/concurrency tests for all approved limits and definitive versus transient failures.
- [ ] Implement coordinator/policies with injected clock/sleeper.
- [ ] Port provider refresh handlers and atomic credential/status persistence.
- [ ] Run GREEN and race tests under mass-start jitter.
- [ ] Commit: `feat(oauth): add provider refresh coordinator`

### Task P3-T08: Complete account state, cooldown, locks, and fallback classification

**Files:**
- Complete: `internal/domain/provider`, `internal/domain/routing`, `internal/app/providers/state.go`
- Create: checkpoint migration/repository
- Test: parity/restart/concurrency tests

**Interfaces:**
- Hot state in memory; important cooldown/model-lock/health checkpoints in PG; deterministic restore; status transitions audited.

- [ ] Write RED fixture tests for every upstream failure category and restart checkpoint.
- [ ] Implement classification/checkpoint/restore.
- [ ] Run GREEN and race tests.
- [ ] Commit: `feat(routing): complete provider failure state`

### Task P3-T09: Implement aliases, disabled models, inferred routes, and complete model catalog

**Files:**
- Create/complete domain/app/repository packages for aliases, custom models, disabled models, provider nodes, model metadata/capabilities.
- Test: resolution/discovery/reconciliation parity tests

- [ ] Write RED tests proving discovery never deletes admin state and original resolution order remains stable.
- [ ] Implement transactionally and integrate registry/discovery.
- [ ] Run GREEN.
- [ ] Commit: `feat(models): complete catalog and alias parity`

### Task P3-T10: Implement all combo modes

**Files:**
- Create: `internal/engine/combos/{sequential,roundrobin,autoswitch,fusion}.go`
- Create: `internal/app/combos/service.go`
- Create: combo repositories/migrations
- Test: parity/concurrency/golden tests

**Interfaces:**
- Sequential fallback, sticky round-robin, capability auto-switch, fusion panels/quorum/8s grace/90s hard timeout/judge synthesis; degradation 0→error, 1→direct, multiple→judge.

- [ ] Write RED tests from upstream combo fixtures including media capabilities, cancellation, deterministic tie/order, and restart behavior.
- [ ] Implement each mode separately and run GREEN before next mode.
- [ ] Run race/leak tests for fusion fan-out.
- [ ] Commit per combo mode.

### Task P3-T11: Port request mutators in upstream order

**Files:**
- Create: `internal/engine/mutators/{pipeline,rtk,headroom,caveman,ponytail,pxpipe}.go`
- Test: parity/golden/fail-open tests

**Interfaces:**
- Mutates request in approved upstream order; each feature independently enabled; failures are fail-open where upstream does so; cancellation/usage semantics preserved.

- [ ] Write RED before/after fixtures and failure fixtures per mutator.
- [ ] Port one mutator at a time.
- [ ] Run GREEN and ensure no cross-request mutable state.
- [ ] Commit per mutator family.

### Task P3-T12: Complete modalities

**Files:**
- Complete transport/formats/providers/app paths for embeddings, images, image-to-text, TTS, STT, voices, web search/fetch, video generation/edit/extension/status.
- Test: modality contracts/provider fixtures

- [ ] Write RED request/response/error fixtures per upstream endpoint/provider combination.
- [ ] Implement shared orchestration only where semantics align; preserve provider-specific media behavior.
- [ ] Run GREEN, upload/body limit, cancellation, binary streaming, and cleanup tests.
- [ ] Commit per modality.

### Task P3-T13: Complete provider registry generation and coverage enforcement

**Files:**
- Create: `internal/engine/providers/registry/generated.go`
- Create: `tools/registrygen/main.go`
- Update provider matrix and parity tests

- [ ] Write RED tests requiring every audited provider/model/service/auth entry to be represented exactly once.
- [ ] Implement deterministic generator and review generated diff.
- [ ] Run GREEN and ensure manual edits to generated file fail CI.
- [ ] Commit: `build(provider): generate complete registry`

### Task P3-T14: Execute automated provider matrix and prepare live validation

**Files:**
- Create: `tests/providers/harness/*`
- Create: `tests/providers/manifests/*.yaml`
- Create: `docs/implementation/provider-live-validation.md`
- Update traceability/gate evidence

**Interfaces:**
- Harness runs sanitized fixture/mock suite on normal commits and real-account suite in protected lab/CI without exposing secrets.

- [ ] Write RED harness tests for credential lookup, provider isolation, result sanitization, timeout, retry, and artifact format.
- [ ] Implement harness.
- [ ] Run full mock/golden matrix.
- [ ] Dry-run live harness with unavailable credentials producing SKIP, never false PASS.
- [ ] Commit: `test(provider): add complete matrix harness`

## Mandatory Exit Gate: Engine Parity Gate

- [ ] Provider/format/OAuth matrices account for every audited upstream entry with source and test ownership.
- [ ] All original client formats and protocol terminal semantics pass golden fixtures.
- [ ] Direct translators/native passthrough prevent lossy thinking/tool/image/error/binary transformations.
- [ ] All generic and specialized executors pass automated fixtures, error, retry, cancellation, and leak tests.
- [ ] Every OAuth/import family passes end-to-end fake provider tests; state/PKCE/TTL/restart behavior is proven.
- [ ] Refresh timing/concurrency/singleflight/provider override rules pass fake-clock/race tests.
- [ ] Account status/cooldown/model-lock/fallback and PG recovery match parity fixtures.
- [ ] All aliases/custom/disabled/node/discovery behavior passes reconciliation tests.
- [ ] All combo modes pass semantics, cancellation, timeout, degradation, determinism, and race tests.
- [ ] All mutators pass order/fail-open/golden tests.
- [ ] All modalities pass API/provider fixtures.
- [ ] Full automated provider matrix is green; required live tests are defined and runnable.
- [ ] No unowned parity ledger row in engine/provider domains.
- [ ] `phase-3.json` evidence and reviews are approved before Phase 4.
 before Phase 4.
