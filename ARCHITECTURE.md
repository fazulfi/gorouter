# gorouter Architecture Design

Status: Approved by the user on 2026-07-29. Implementation planning is authorized; coding still requires a written and reviewed plan.

## 1. Purpose and governance

This document turns the approved decisions in `DECISIONS.md` and the source audit under `audit/` into a coherent architecture proposal for the Go rewrite of original 9Router.

Rules:

- Original 9Router is the behavioral and capability baseline.
- This is a rewrite, not a new product.
- Additions must remain small and explicitly approved.
- A source conflict or decision conflict returns to the user.
- Cancelled decisions are ignored by implementation.
- The visual system follows the approved OpenRouter references while workflows and semantics follow original 9Router.
- This document defines boundaries and dependency direction. It does not authorize coding by itself.

## 2. System shape

gorouter is a modular monolith:

- one Go binary;
- one active process per PostgreSQL database;
- many goroutines inside that process;
- one HTTP listener for dashboard, Admin API, health, and model-compatible APIs;
- PostgreSQL as durable storage;
- embedded React/Vite dashboard assets;
- platform-specific host integrations behind adapters;
- no Redis and no internal microservices.

The process supports execution modes through the same binary:

- server/runtime;
- local CLI;
- tray/desktop launcher;
- lifecycle and recovery commands;
- installer/updater helpers where platform-appropriate.

A PostgreSQL advisory lock rejects a second process attempting to use the same database. This does not limit goroutines or request concurrency inside the active process.

## 3. Architectural layers

### 3.1 Transport layer

Owns protocols and input/output boundaries:

- compatible model APIs and rewrites;
- versioned Admin API at `/api/admin/v1`;
- original management compatibility routes;
- dashboard static assets;
- SSE endpoints;
- health endpoints;
- CLI command parsing.

Transport code performs protocol parsing, authentication extraction, actor creation, response encoding, CORS, and trusted-proxy handling. It does not own routing or persistence rules.

### 3.2 Application layer

Owns use cases and transaction boundaries:

- provider account workflows;
- model routing and combo workflows;
- key and settings management;
- OAuth initiation/callback/status;
- usage and request-detail queries;
- background-job commands;
- host-feature commands;
- dashboard and CLI workflows.

Dashboard handlers, Admin API handlers, local CLI, and original compatibility adapters call the same application services.

### 3.3 Domain layer

Owns rules and state transitions:

- model parsing and resolution;
- account priority, cooldown, locks, and fallback;
- combo modes;
- provider connection state;
- OAuth and refresh state;
- pricing and usage semantics;
- API-key/PAT/session rules;
- scheduler job contracts.

Domain packages do not import HTTP, PostgreSQL, React, or OS-specific implementations.

### 3.4 Provider and protocol engine

Owns upstream integration:

- generic OpenAI-compatible provider adapter;
- generic Anthropic-compatible provider adapter;
- specialized provider executors;
- format detection;
- direct translators;
- safe internal normalization;
- native passthrough;
- protocol-specific stream terminal behavior;
- provider-aware errors and refresh hooks.

Specialized protocols are not forced through a lossy OpenAI-shaped representation.

### 3.5 Infrastructure layer

Owns concrete implementations:

- PostgreSQL repositories and migrations;
- HTTP clients and proxy transports;
- filesystem and process operations;
- platform service managers;
- tray integration;
- updater;
- tunnels, MITM, Headroom, Pxpipe, and MCP host adapters;
- logs and metrics exporters;
- clock, randomness, and external command adapters.

## 4. Target repository tree

This is the proposed tree. Directories are created only when implementation begins.

```text
gorouter/
├── cmd/
│   └── gorouter/
│       └── main.go                    # binary entry and mode dispatch
├── internal/
│   ├── bootstrap/
│   │   ├── app.go                     # dependency composition
│   │   ├── config.go                  # flags > env > .env > DB > defaults
│   │   ├── lifecycle.go               # startup, drain, shutdown
│   │   └── modes.go                   # server, CLI, tray, helpers
│   ├── transport/
│   │   ├── httpserver/
│   │   │   ├── server.go
│   │   │   ├── middleware/
│   │   │   │   ├── trustedproxy.go
│   │   │   │   ├── correlation.go
│   │   │   │   ├── recovery.go
│   │   │   │   ├── cors.go
│   │   │   │   └── auth.go
│   │   │   ├── modelapi/
│   │   │   │   ├── chat.go
│   │   │   │   ├── responses.go
│   │   │   │   ├── messages.go
│   │   │   │   ├── gemini.go
│   │   │   │   ├── models.go
│   │   │   │   ├── embeddings.go
│   │   │   │   ├── images.go
│   │   │   │   ├── audio.go
│   │   │   │   ├── video.go
│   │   │   │   └── web.go
│   │   │   ├── adminapi/
│   │   │   │   └── v1/                # versioned dashboard/CLI/PAT contracts
│   │   │   ├── compatibility/         # thin original management adapters
│   │   │   ├── realtime/              # domain SSE handlers
│   │   │   ├── auth/                  # login/session/logout/recovery HTTP
│   │   │   ├── oauth/                 # browser callbacks and status
│   │   │   ├── health/
│   │   │   └── dashboard/             # embedded frontend delivery
│   │   └── cli/
│   │       ├── root.go
│   │       ├── lifecycle.go
│   │       ├── providers.go
│   │       ├── combos.go
│   │       ├── keys.go
│   │       ├── settings.go
│   │       ├── tools.go
│   │       └── recovery.go
│   ├── app/
│   │   ├── actor/
│   │   ├── endpoint/
│   │   ├── providers/
│   │   ├── routing/
│   │   ├── combos/
│   │   ├── usage/
│   │   ├── quota/
│   │   ├── tokensaver/
│   │   ├── clitools/
│   │   ├── media/
│   │   ├── proxypools/
│   │   ├── skills/
│   │   ├── console/
│   │   ├── remote/
│   │   ├── settings/
│   │   ├── oauth/
│   │   ├── auth/
│   │   ├── jobs/
│   │   └── hostops/
│   ├── domain/
│   │   ├── model/
│   │   ├── provider/
│   │   ├── connection/
│   │   ├── routing/
│   │   ├── combo/
│   │   ├── proxy/
│   │   ├── usage/
│   │   ├── pricing/
│   │   ├── auth/
│   │   ├── oauth/
│   │   ├── keys/
│   │   ├── settings/
│   │   └── jobs/
│   ├── engine/
│   │   ├── orchestrator/
│   │   │   ├── pipeline.go
│   │   │   ├── stages.go
│   │   │   └── result.go
│   │   ├── formats/
│   │   │   ├── detect.go
│   │   │   ├── openai/
│   │   │   ├── responses/
│   │   │   ├── claude/
│   │   │   ├── gemini/
│   │   │   ├── codex/
│   │   │   └── native/
│   │   ├── translators/
│   │   │   ├── registry.go
│   │   │   ├── direct/
│   │   │   └── safeir/
│   │   ├── streaming/
│   │   │   ├── events.go
│   │   │   ├── passthrough.go
│   │   │   ├── terminal.go
│   │   │   ├── watchdog.go
│   │   │   └── cancellation.go
│   │   ├── routing/
│   │   │   ├── resolver.go
│   │   │   ├── accounts.go
│   │   │   ├── fallback.go
│   │   │   ├── cooldown.go
│   │   │   └── state.go
│   │   ├── combos/
│   │   │   ├── sequential.go
│   │   │   ├── roundrobin.go
│   │   │   ├── autoswitch.go
│   │   │   └── fusion.go
│   │   ├── providers/
│   │   │   ├── registry.go
│   │   │   ├── generic/
│   │   │   │   ├── openai.go
│   │   │   │   └── anthropic.go
│   │   │   └── specialized/           # one package per special executor
│   │   ├── refresh/
│   │   │   ├── coordinator.go
│   │   │   ├── singleflight.go
│   │   │   └── policies/
│   │   └── mutators/                  # RTK, Headroom, Caveman, etc.
│   ├── persistence/
│   │   ├── postgres/
│   │   │   ├── pool.go
│   │   │   ├── transaction.go
│   │   │   ├── lock.go
│   │   │   ├── migrations/
│   │   │   └── repositories/
│   │   │       ├── providers.go
│   │   │       ├── routing.go
│   │   │       ├── combos.go
│   │   │       ├── usage.go
│   │   │       ├── oauth.go
│   │   │       ├── auth.go
│   │   │       ├── keys.go
│   │   │       ├── settings.go
│   │   │       ├── logs.go
│   │   │       └── jobs.go
│   │   ├── usagequeue/                 # durable PG-backed usage events
│   │   ├── backup/
│   │   └── retention/
│   ├── scheduler/
│   │   ├── scheduler.go
│   │   ├── registry.go
│   │   ├── lock.go
│   │   └── status.go
│   ├── host/
│   │   ├── service/
│   │   ├── tray/
│   │   ├── updater/
│   │   ├── tunnel/
│   │   ├── mitm/
│   │   ├── headroom/
│   │   ├── pxpipe/
│   │   ├── mcp/
│   │   └── platform/                   # *_windows.go, *_darwin.go, *_linux.go
│   ├── observability/
│   │   ├── logging/
│   │   ├── metrics/
│   │   └── requestmeta/
│   └── shared/
│       ├── clock/
│       ├── errors/
│       ├── ids/
│       └── redact/
├── api/
│   ├── admin-v1.openapi.yaml
│   ├── model-contracts/                # versioned formal contracts per client format
│   │   ├── openai/
│   │   ├── responses/
│   │   ├── claude/
│   │   ├── gemini/
│   │   ├── codex/
│   │   └── native/                     # protocol-specific schemas where representable
│   └── compatibility/                  # source-backed contract and golden fixtures
├── frontend/
│   ├── src/
│   │   ├── app/                        # shell, router, providers
│   │   ├── design/                     # tokens and primitives
│   │   ├── generated/                  # generated Admin API client/types
│   │   ├── shared/
│   │   └── features/
│   │       ├── endpoint/
│   │       ├── providers/
│   │       ├── combos/
│   │       ├── usage/
│   │       ├── quota/
│   │       ├── token-saver/
│   │       ├── cli-tools/
│   │       ├── media-providers/
│   │       ├── proxy-pools/
│   │       ├── skills/
│   │       ├── console-log/
│   │       ├── remote/
│   │       └── settings/
│   ├── public/
│   └── package.json
├── tests/
│   ├── parity/                         # original observable-contract fixtures
│   ├── golden/                         # translation/stream golden cases
│   ├── integration/
│   ├── providers/
│   ├── security/
│   ├── performance/
│   └── soak/
├── packaging/
│   ├── npm/
│   ├── windows/
│   ├── macos/
│   ├── linux/
│   └── docker/
├── docs/
│   ├── architecture/
│   ├── parity/
│   ├── providers/
│   ├── api/
│   ├── deployment/
│   └── operations/
├── tools/
│   ├── contractgen/
│   ├── providergen/
│   └── upstream-monitor/
├── embed.go                            # embeds frontend dist
├── go.mod
├── go.sum
├── LICENSE
└── README.md
```

## 5. Dependency rules

Allowed dependency direction:

```text
transport ──► application ──► domain
                    │             ▲
                    ├──► engine ──┘
                    └──► interfaces
infrastructure ──implements──► interfaces
bootstrap ──wires──► all modules
```

Hard rules:

1. Domain packages do not import transport, PostgreSQL, OS, or frontend packages.
2. HTTP handlers do not query PostgreSQL directly.
3. CLI commands do not query PostgreSQL directly.
4. Compatibility handlers contain mapping only; they call the same application services as Admin API v1.
5. Application services own cross-repository transactions.
6. Provider-specific protocol code stays in provider/format/translator adapters, not orchestration handlers.
7. Specialized stream formats can bypass safe-IR translation when required.
8. Host feature code stays behind feature-specific interfaces and platform adapters.
9. Background scheduler owns job execution mechanics, not domain business rules.
10. Frontend imports generated contracts and feature modules; it does not duplicate backend domain rules.

## 6. Model request pipeline

```text
Client
  │
  ▼
Trusted-proxy normalization
  │
  ▼
Model API authentication and compatibility-path normalization
  │
  ▼
Source-format detection
  │
  ▼
Model grammar resolution
  ├─ provider/model
  ├─ alias
  ├─ combo
  ├─ inferred route
  └─ namespace/model(variant)
  │
  ▼
Request capability analysis
  │
  ▼
Account priority/cooldown/model-lock selection
  │
  ▼
Proxy selection using original behavior
  │
  ▼
Provider target + translator/executor selection
  │
  ├─ native passthrough
  ├─ direct translator
  └─ safe internal normalization
  │
  ▼
Provider request and response/stream handling
  │
  ├─ provider-aware retry
  ├─ 401/403 refresh + bounded retry
  ├─ same-model account fallback
  └─ combo strategy if explicitly selected
  │
  ▼
Client-format response and protocol-specific terminal event
  │
  ├─ async durable usage event
  └─ metadata-only request details
```

Cancellation propagates through every stage using `context.Context`. Client disconnect receives the approved 500ms grace before cancellation. Reader, translator, provider request, timers, goroutines, and finalization must terminate without duplicate terminal events.

## 7. Combo flows

### Sequential

Models run in configured order. The next model is attempted only for errors classified as fallback-eligible by original behavior.

### Round-robin

Rotation and sticky state follow original behavior. Hot state remains in memory; important recovery state is checkpointed to PostgreSQL.

### Capability auto-switch

Only models already inside the selected combo are considered. Request capabilities influence selection without dropping configured fallback candidates.

### Fusion

Panel models execute concurrently. The flow preserves upstream quorum, grace, hard timeout, tools/nonstream handling, and judge synthesis:

- zero panel successes: error;
- one success: direct result;
- multiple successes: judge synthesis.

## 8. OAuth and refresh flows

### OAuth start and callback

```text
Dashboard/CLI starts provider-specific OAuth use-case
  │
  ▼
Create PostgreSQL pending session with TTL
  │
  ▼
Start provider-required callback mechanism
  ├─ random loopback
  ├─ fixed callback port
  ├─ device code
  ├─ dashboard relay
  ├─ cookie/PAT import
  └─ IDE/local import
  │
  ▼
Validate state/PKCE/provider callback
  │
  ▼
Exchange credential
  │
  ▼
Provider-specific dedup
  │
  ▼
Atomic provider-connection transaction
  │
  ▼
Active/unknown/disabled status per approved validation rules
```

Pending OAuth sessions are always cancelled after process restart, even though status is stored in PostgreSQL. Fixed/random callback ports and redirect URIs preserve provider contracts.

### Refresh

One coordinator handles proactive and reactive refresh. It provides:

- per-account singleflight;
- global concurrency limit of four;
- provider-specific policy overrides backed by source audit;
- default refresh five minutes before reliable expiry;
- six-hour refresh for credentials without reliable expiry;
- thirty-second attempt timeout;
- maximum three attempts with backoff and jitter;
- shared result for waiting requests;
- atomic credential and status persistence;
- re-auth-required state for definitive rejection.

A temporary proactive failure does not remove an account while the current access token remains valid.

## 9. Persistence flows

### Transactions

Application/domain services begin transactions for cross-repository workflows and receive transaction-scoped repositories. Memory caches update only after commit.

### Provider records

Common fields use typed columns. Provider-specific credentials and metadata use JSONB. Provider credentials remain restricted plaintext; PAT and model API keys remain hash-only.

### Usage

```text
Completed request
  │
  ▼
Accept durable usage event in PostgreSQL
  │
  ▼
Return/finalize response without waiting for aggregation
  │
  ▼
Usage worker claims event
  │
  ├─ usage history
  ├─ daily aggregates
  ├─ pricing/cost
  └─ Recent Requests feed
  │
  ▼
Mark event processed idempotently
```

Usage, request details, and application logs use the approved 90-day retention. Recent Requests uses a 50-item realtime buffer and displays 20 newest filtered/deduplicated entries.

### Backup

Backups must be point-in-time consistent. A backup is valid only after restore verification. Backup behavior stays operationally separate from manual config export, which is not included.

## 10. Dashboard and Admin API flow

```text
React feature module
  │
  ▼
Generated Admin API v1 client
  │
  ▼
HTTP authentication boundary
  ├─ dashboard cookie session
  └─ PAT
  │
  ▼
Unified actor context
  │
  ▼
Application use-case
  │
  ▼
Domain + transaction-scoped repositories/adapters
```

Realtime views use domain-specific SSE. Mutations use HTTP. Original management endpoints are compatibility adapters into the same use-cases.

The frontend uses feature/domain modules, query cache for server state, local state for forms/filters/theme, and generated TypeScript contracts.

## 11. CLI and host flow

Local CLI commands call the application layer in-process. Remote automation calls Admin API v1 using PAT. Neither bypasses use-case validation or authorization.

Host features each have a dedicated interface and adapter. OS-specific implementations use Go build tags. Docker explicitly disables incompatible desktop/host capabilities.

## 12. Background jobs

A central scheduler owns trigger and execution mechanics. Domain packages own job logic.

```text
Schedule or Run Now
  │
  ▼
Per-job local lock
  │
  ▼
Domain job with context timeout/retry policy
  │
  ▼
Persist current/last status + short history
  │
  ▼
Publish domain SSE status if user-visible
```

The scheduler does not permit overlapping execution of the same job. It does not create a new permanent event platform.

## 13. Security boundaries

Required preserved or approved boundaries:

- forwarded headers trusted only from loopback or configured trusted proxy CIDRs;
- all model-compatible routes require a model API key, including loopback;
- Admin API uses dashboard session or PAT;
- local CLI has explicit local actor context;
- client errors are sanitized and internal diagnostics are separate;
- credentials are redacted from logs, errors, metrics, request details, and audit output;
- high-risk host features remain opt-in and permission-controlled;
- PostgreSQL role and filesystem permissions protect plaintext provider secrets;
- second runtime on the same database is rejected.

## 14. Module Definition of Done

A module is complete only when:

1. original source mapping exists;
2. observable contracts are inventoried;
3. preserve/change/remove disposition is approved;
4. implementation follows approved boundaries;
5. unit tests pass;
6. parity/golden tests pass where applicable;
7. integration and error-path tests pass;
8. streaming/cancellation tests pass where applicable;
9. security and concurrency invariants pass;
10. user-facing documentation is updated;
11. review against upstream source is complete.

## 15. Scope guard

Every item entering the plan must be labelled:

- **Original parity**: required to reproduce original capability or observable behavior.
- **Small approved improvement**: explicitly accepted reliability, security, concurrency, or UI improvement.
- **Not included**: absent or explicitly removed.

No broad enterprise feature may be inferred from this architecture document.

## 16. Approved structural decisions and remaining gates

The user approved these structural choices:

1. Keep the layered package structure shown in this document: `transport → app → domain/engine → persistence/host`.
2. Keep `internal/app` and `internal/domain` separate. Domain owns pure rules and invariants; application owns use cases, coordination, actors, and transaction boundaries.
3. Maintain versioned formal contracts plus source-backed golden fixtures for the Admin API and model-compatible APIs. Protocol-native and binary formats must retain native fixtures and must not be forced into a lossy universal schema.

The user additionally approved:

4. Upstream-to-Go mapping is maintained per module and workflow. Exact upstream files and symbols remain required evidence and parity-test anchors, but the primary ledger is not one row per source symbol.
5. The preserve/change/remove ledger is split into domain tables and approved as one complete package before coding.

The user approved this internal porting sequence:

6. Gate A establishes bootstrap, configuration, PostgreSQL, migrations, authentication, trusted-proxy handling, formal contracts, and test harnesses.
7. Gate B delivers one complete model-request vertical slice, including SSE, cancellation, terminal semantics, fallback, one generic compatible provider, and one high-risk specialized executor.
8. Gate C completes the format, translator, streaming, provider, routing, and combo matrices.
9. Gate D completes the Admin API, dashboard, and CLI domain workflows.
10. Gate E completes host integrations, packaging, the full live-provider matrix, performance, security, and soak gates.

This sequence orders implementation only; it does not reduce the approved super-full first-release scope.

Final gates are resolved:

11. `PARITY-LEDGER.md` was approved as one complete package on 2026-07-29 under decision #380.
12. This architecture document was approved as a whole on 2026-07-29 under decision #381.

Implementation planning may begin. Coding must still wait for a written implementation plan to be completed and reviewed.
