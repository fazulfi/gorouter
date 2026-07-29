# Phase 2 End-to-End Vertical Slice Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Prove one complete production-shaped model request path from authenticated client through model resolution, account/proxy selection, generic and high-risk specialized provider execution, streaming/cancellation/terminal semantics, fallback, and durable usage finalization.

**Architecture:** Implement the approved directed orchestration pipeline with explicit stages and safe protocol bypasses. The slice includes OpenAI Chat as the primary client format, one generic OpenAI-compatible provider, and one source-audited specialized executor chosen for high semantic risk; it establishes reusable interfaces without claiming complete provider parity.

**Tech Stack:** Go/chi, net/http, pgx, context cancellation, SSE, source-backed golden fixtures, fake upstream servers, Go race/fuzz/benchmark tooling.

## Global Constraints

- Phase 1 Foundation Gate must be approved.
- Preserve original model resolution order and same-model account fallback semantics.
- Single-model requests never switch model automatically; cross-model switching belongs to explicit combos.
- Specialized provider behavior may bypass common normalization when required.
- Client disconnect uses upstream 500ms grace, then cancels readers/writers/translators/timers/goroutines.
- Stall watchdog observes raw upstream bytes and defaults to six minutes; first-chunk timeout defaults to 200 seconds.
- API key is required even loopback.
- Accepted usage event must survive restart; request does not wait for aggregation.
- No task may generalize beyond demonstrated Phase 2 contracts.

---

## File and responsibility map

| Path | Responsibility |
| --- | --- |
| `internal/domain/model` | Model reference grammar, aliases, variants, capability primitives |
| `internal/domain/provider` | Connection/account/proxy selection state |
| `internal/domain/routing` | Attempt plans and failure classification |
| `internal/engine/orchestrator` | Directed pipeline and stage contracts |
| `internal/engine/formats/openai` | OpenAI Chat request/response/SSE types |
| `internal/engine/translators` | Slice translation contracts |
| `internal/engine/streaming` | Raw-byte liveness, terminal events, cancellation |
| `internal/engine/providers/generic` | Generic OpenAI-compatible executor |
| `internal/engine/providers/specialized/codex` | High-risk Codex executor selected for the vertical slice |
| `internal/app/routing` | Use case composing repositories and engine |
| `internal/transport/httpserver/modelapi/chat.go` | `/v1/chat/completions` transport |
| `internal/persistence/postgres/repositories` | Provider/model/key/usage slice repositories |
| `internal/persistence/usagequeue` | Durable asynchronous usage acceptance/aggregation |
| `tests/parity`, `tests/golden`, `tests/integration` | Source-backed vertical-slice proof |

### Task P2-T01: Implement original model grammar and resolution order

**Files:**
- Create: `internal/domain/model/reference.go`
- Create: `internal/domain/model/parser.go`
- Create: `internal/domain/model/resolver.go`
- Test: corresponding unit tests and `tests/parity/model_resolution_test.go`

**Interfaces:**
- Produces: `ParseReference(raw string) (Reference, error)` and `Resolve(ctx context.Context, ref Reference, catalog Catalog) (ResolvedModel, error)`.
- Supports original `provider/model`, slashless alias/combo/built-in/inferred resolution order plus approved `namespace/model(variant)` extension.

- [ ] Write failing table/golden tests sourced from upstream model resolution files and ledger rows.
- [ ] Run RED: `go test ./internal/domain/model ./tests/parity -run Model -v`.
- [ ] Implement deterministic parser/resolver without network/DB imports.
- [ ] Run GREEN and fuzz `ParseReference` for panic/ambiguity safety.
- [ ] Commit: `feat(routing): implement model reference resolution`

### Task P2-T02: Implement provider connection, account priority, cooldown, and proxy selection domain

**Files:**
- Create: `internal/domain/provider/{connection,status,selection}.go`
- Create: `internal/domain/proxy/{pool,selection}.go`
- Create: `internal/domain/routing/attempt.go`
- Test: unit/parity tests

**Interfaces:**
- Produces ordered `[]Attempt` containing same model, eligible accounts, selected proxy, credential state, cooldown reason.
- Preserves manual priority/original order; skips cooldown/model-lock accounts; no account round-robin.

- [ ] Write RED tests for exact upstream attempt order, cooldown skipping, unknown-active accounts, definitive-disabled accounts, proxy rotation/fallback, and tie handling.
- [ ] Run RED with focused packages.
- [ ] Implement pure domain selection functions with injected clock.
- [ ] Run GREEN and race tests for concurrent state snapshots.
- [ ] Commit: `feat(routing): add account and proxy selection`

### Task P2-T03: Create provider/translator/streaming interfaces

**Files:**
- Create: `internal/engine/providers/provider.go`
- Create: `internal/engine/formats/format.go`
- Create: `internal/engine/translators/translator.go`
- Create: `internal/engine/streaming/events.go`
- Create: `internal/engine/orchestrator/stages.go`
- Test: compile-time interface and lifecycle tests

**Interfaces:**
- Defines request `Envelope`, provider `Executor`, direct `Translator`, native passthrough result, stream event/terminal contract, stage input/output, safe error layers.
- Interfaces carry `context.Context`, correlation ID, source/target format, capabilities, attempt metadata, and raw-byte activity callback.

- [ ] Write RED compile tests and state-machine tests proving terminal exactly once and context propagation.
- [ ] Run RED.
- [ ] Implement minimum interfaces and event state machine.
- [ ] Run GREEN plus race tests.
- [ ] Commit: `feat(engine): define provider and stream contracts`

### Task P2-T04: Implement OpenAI Chat transport and format parser

**Files:**
- Create: `internal/engine/formats/openai/chat.go`
- Create: `internal/engine/formats/openai/sse.go`
- Create: `internal/transport/httpserver/modelapi/chat.go`
- Create: `internal/transport/httpserver/modelapi/routes.go`
- Test: golden/HTTP tests

**Interfaces:**
- Consumes model API actor and routing application service.
- Produces OpenAI-compatible JSON/SSE and sanitized structured errors; supports legacy `/v1/v1/chat/completions` normalization through mapping-only adapter.

- [ ] Write RED golden tests for JSON parse, stream negotiation, tool fields, errors, CORS, API-key transports, `[DONE]`, and double-v1 route.
- [ ] Run RED.
- [ ] Implement minimum handler/codec/route mapping.
- [ ] Run GREEN and HTTP fuzz tests for malformed bodies/headers.
- [ ] Commit: `feat(api): add OpenAI chat transport`

### Task P2-T05: Implement generic OpenAI-compatible executor

**Files:**
- Create: `internal/engine/providers/generic/openai.go`
- Create: `internal/engine/providers/generic/client.go`
- Create: `internal/engine/providers/generic/errors.go`
- Test: fake upstream integration/golden tests

**Interfaces:**
- Implements `providers.Executor` for configured OpenAI-compatible base URL and auth.
- Exposes raw-byte activity, status/headers, normalized retry classification, JSON/nonstream/SSE handling.

- [ ] Write RED integration tests using `httptest.Server` for stream chunks, fragmented SSE, 401/403, 429, 5xx, disconnect, slow first chunk, stall activity, malformed terminal.
- [ ] Run RED.
- [ ] Implement minimum executor using context-aware `http.Client` and no credential logging.
- [ ] Run GREEN, race tests, and connection-leak assertions.
- [ ] Commit: `feat(provider): add generic OpenAI executor`

### Task P2-T06: Port one high-risk specialized executor

**Files:**
- Create: `internal/engine/providers/specialized/codex/*`
- Create: `tests/golden/providers/codex/*`
- Create: `docs/implementation/providers/codex.md`

**Interfaces:**
- Implements provider/executor and any direct translator/native protocol interfaces required by exact upstream behavior.
- Codex is the selected high-risk executor because its source-backed contracts include specialized request/response semantics, OAuth behavior, model variants, errors, and terminal events. This task cannot invent a universal abstraction from one provider.

- [ ] Record exact Codex upstream files/symbols/protocol fixtures cited by the audit and fixture manifest.
- [ ] Write RED golden tests for Codex request encoding, stream decoding, thinking/tools/errors/terminal/cancellation peculiarities.
- [ ] Run RED: `go test ./internal/engine/providers/specialized/codex ./tests/golden/providers/codex -v`; expect fixture mismatches or missing implementation.
- [ ] Port minimum source-backed Codex executor behavior.
- [ ] Run GREEN with the same command and compare byte/event ordering with upstream fixtures.
- [ ] Commit: `feat(provider): port Codex executor`

### Task P2-T07: Implement orchestrator pipeline and safe bypass

**Files:**
- Create: `internal/engine/orchestrator/{pipeline,result}.go`
- Create: `internal/app/routing/service.go`
- Test: unit/integration/parity tests

**Interfaces:**
- Pipeline stages: Auth context accepted → source detection → normalization → model resolution → account/proxy selection → translation/executor → retry/fallback → stream/response → usage finalization.
- Native/specialized paths may bypass normalization while retaining common auth/routing/finalization.

- [ ] Write RED scenario tests for generic stream, specialized bypass, same-model account fallback, final error, context cancellation, exactly-once finalization.
- [ ] Run RED.
- [ ] Implement pipeline with explicit stage results and no hidden globals.
- [ ] Run GREEN and race tests under concurrent attempts.
- [ ] Commit: `feat(engine): add model request pipeline`

### Task P2-T08: Implement stream lifecycle, liveness, and terminal semantics

**Files:**
- Create: `internal/engine/streaming/{session,watchdog,writer,cancel}.go`
- Test: `tests/integration/streaming/*` and package tests

**Interfaces:**
- Produces exactly-once protocol terminal, raw-byte stall watchdog, 200s first-chunk timeout, 6m stall default, 500ms disconnect grace, complete cleanup.

- [ ] Write fake-clock RED tests for first chunk, raw-byte-but-no-output activity, true stall, disconnect/reconnect grace, cancel propagation, writer failure, duplicate terminal prevention.
- [ ] Run RED.
- [ ] Implement session lifecycle using contexts/timers and protocol terminal encoder.
- [ ] Run GREEN, race detector, and goroutine-leak test.
- [ ] Commit: `feat(stream): implement cancellation and liveness`

### Task P2-T09: Implement provider-aware retry and same-model account fallback

**Files:**
- Create: `internal/domain/routing/failure.go`
- Create: `internal/engine/orchestrator/retry.go`
- Create: `internal/app/providers/state.go`
- Test: parity/integration tests

**Interfaces:**
- Classifies transient/auth/quota/model/provider errors; refresh hook on 401/403; bounded request retry; account cooldown; next account for same model; no cross-model fallback outside combo.

- [ ] Write RED tests matching upstream classifications and attempt order, including 401 refresh success/failure, 429 cooldown, transient 5xx, terminal no-fallback errors.
- [ ] Run RED.
- [ ] Implement minimum classifier/retry coordinator with injected clock/sleeper.
- [ ] Run GREEN and race tests.
- [ ] Commit: `feat(routing): add provider-aware account fallback`

### Task P2-T10: Implement durable asynchronous usage acceptance

**Files:**
- Create: `internal/domain/usage/event.go`
- Create: `internal/persistence/usagequeue/{queue,worker}.go`
- Create: `internal/persistence/postgres/repositories/usage.go`
- Create: migration `000002_usage.up.sql`
- Test: unit/integration/restart tests

**Interfaces:**
- `Accept(ctx context.Context, event usage.Event) error` durably stores accepted event before request lifecycle releases it; aggregation happens asynchronously/idempotently; metadata-only request details by default.

- [ ] Write RED tests for crash after acceptance, restart replay, idempotent aggregation, cost zero for unknown pricing, no prompt/body capture, exactly-once observable totals.
- [ ] Run RED against PG integration environment.
- [ ] Implement queue/worker/repository and wire orchestrator finalizer.
- [ ] Run GREEN, race, and forced-restart tests.
- [ ] Commit: `feat(usage): add durable asynchronous accounting`

### Task P2-T11: Prove the integrated vertical slice

**Files:**
- Create: `tests/integration/verticalslice/chat_test.go`
- Create: `tests/parity/verticalslice_test.go`
- Create: `tests/performance/verticalslice_bench_test.go`
- Update: traceability and gate evidence records

**Interfaces:**
- Exercises real HTTP server, PG, fake generic/specialized upstreams, API key, streaming, fallback, cancellation, durable usage.

- [ ] Write RED end-to-end scenarios before final wiring.
- [ ] Run RED and confirm missing integrated behavior rather than test setup errors.
- [ ] Wire bootstrap routes/services/providers/repositories.
- [ ] Run GREEN: JSON and SSE happy path, specialized path, fallback, abort, restart usage replay, secret redaction.
- [ ] Commit: `test: prove end-to-end model request slice`

## Mandatory Exit Gate: Vertical Slice Gate

- [ ] One API-key-authenticated `/v1/chat/completions` request succeeds through real server/PG/fake upstream.
- [ ] Legacy `/v1/v1/*` normalization passes.
- [ ] Original model grammar plus namespace/variant extension resolves deterministically.
- [ ] Same-model account priority/cooldown/proxy fallback matches fixtures; no implicit cross-model fallback.
- [ ] Generic OpenAI-compatible executor passes JSON/SSE/error/cancel tests.
- [ ] One high-risk specialized executor passes native/direct golden fixtures.
- [ ] Stream terminal event is emitted exactly once; `[DONE]` ordering is correct; no goroutine/socket/timer leak.
- [ ] First-chunk, raw-byte stall, and 500ms disconnect grace behavior pass fake-clock and integration tests.
- [ ] Provider-aware 401/403 refresh hook, quota/transient classification, retry, cooldown, and fallback pass.
- [ ] Accepted usage survives forced process restart and aggregates idempotently without body capture.
- [ ] Race detector passes all vertical-slice packages.
- [ ] Traceability records and `phase-2.json` gate evidence are complete.
- [ ] Parity reviewer and user/reviewer approve before Phase 3.
.json` gate evidence are complete.
- [ ] Parity reviewer and user/reviewer approve before Phase 3.
