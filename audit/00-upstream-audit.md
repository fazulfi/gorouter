<!-- markdownlint-disable MD013 -->

# Original 9Router Upstream Audit

## Audit identity

- Upstream repository: `https://github.com/decolua/9router.git`
- Audited commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Branch: `master`
- Local read-only clone: `C:\Users\faizz\9router-go\upstream-original`
- Product version at the audited commit: `9router-app` `0.5.40`
- Audit purpose: understand original 9Router behavior deeply before a clean-room Go rewrite.

## Governance

This audit records facts, contracts, invariants, risks, and unanswered questions from the original source. It does not choose or change the rewrite architecture.

All architecture, system design, scope, and technology decisions remain with the user. `DECISIONS.md` is user-owned. When source evidence conflicts with a tentative decision, the audit flags the conflict for review rather than modifying the decision.

## Technology and artifacts

Confirmed stack:

- Next.js 16.1.6 App Router
- React 19.2.4
- Express 5.2.1 custom server
- `http-proxy-middleware`
- `jose` and `bcryptjs`
- SQLite through Bun SQLite, better-sqlite3, node:sqlite, or sql.js fallback
- No Redis or PostgreSQL dependency in the audited upstream

The project ships two distinct artifacts:

1. Root gateway/dashboard application.
2. A separate `cli/` npm launcher with terminal UI, tray, autostart, update, and packaging behavior.

## Runtime request flow

The main LLM path is:

1. Next rewrite maps compatibility endpoints into `/api/v1/*`.
2. A Next route delegates to `src/sse/handlers/chat.js`.
3. The app layer resolves API keys, model names, combos, accounts, and token refresh.
4. It calls `open-sse/handlers/chatCore.js`.
5. `chatCore` detects source format, chooses target protocol and provider executor, applies translators and token-saving hooks, executes the provider request, and dispatches streaming or non-streaming output.
6. Stream handling propagates cancellation, terminal events, errors, and usage details.

`src/sse` is product/application glue. `open-sse` is the provider-oriented execution and translation engine.

## Security boundary in the custom server

`custom-server.js` preserves a critical trust invariant:

- Forwarding headers are trusted only when the TCP peer is the loopback reverse proxy.
- Untrusted incoming forwarding and internal headers are stripped.
- The server stamps trusted internal headers such as `x-9r-real-ip` and `x-9r-via-proxy`.
- Dashboard local/remote authorization depends on these trusted values.

This is not cosmetic proxy behavior. It is part of the authorization boundary.

## Rewrites and compatibility aliases

`next.config.mjs` confirms:

- Standalone output.
- SQLite packages externalized.
- Request-body limit configurable by environment, defaulting to approximately 128 MB.
- Compatibility rewrites for `/v1/v1/*`, `/codex/*`, `/responses`, `/v1beta/*`, and `/v1/*`.

Double-`/v1` normalization and alternate response paths are observable compatibility behavior.

## Public API surface

The original source contains at least 100 route files. Confirmed protocol and modality surfaces include:

### Language and response protocols

- OpenAI chat completions
- OpenAI Responses and response compaction
- Anthropic/Claude messages
- Claude token counting
- Gemini-compatible v1beta endpoints
- Model listing, model kinds, and model information

### Other modalities

- Embeddings
- Image generation
- Text-to-speech
- Speech-to-text/transcriptions
- Voice listing
- Web search
- Web fetch
- Video generation
- Video editing
- Video extension
- Video status polling

### Management and operational APIs

- Health, initialization, version, update, and shutdown
- Settings, authentication, OIDC, locale, tags, and cloud-related areas
- Provider connection CRUD, validation, testing, and live model discovery
- Provider nodes
- Proxy pools and deployment helpers
- API keys
- Combos
- Aliases, custom models, and disabled models
- Pricing
- Usage history, daily usage, request details, logs, and usage streams
- OAuth and provider-specific import flows
- Tunnels and Tailscale controls
- Headroom
- Pxpipe
- MCP SSE and message endpoints
- Media-provider voice APIs
- CLI-tool settings
- Translator and console streams

An exact generated route inventory is still required. The broad list above is confirmed but may not yet be exhaustive.

## Dashboard surface

The clone contains 28 dashboard page files, including:

- Dashboard home and basic chat
- CLI tools and details
- Combos
- Console logs
- Endpoint configuration
- Media providers and detail/combo/web views
- MITM
- Profile
- Provider creation and detail pages
- Proxy pools
- Pxpipe
- Quota
- Skills
- Token saver
- Translator
- Usage
- Pricing/settings
- Login
- Landing
- OAuth callback

Many workflows poll APIs or consume SSE. Basic chat persists sessions, active session/provider, and drafts in browser localStorage.

## CLI surface

The CLI contains roughly 24 JavaScript files and is more than an API client. It includes:

- Standalone server launcher
- Terminal UI
- Settings, provider, combo, API-key, and CLI-tool menus
- Endpoint and model selector
- Tray and autostart management
- Update behavior
- xAI video command
- API client
- Build and postinstall packaging hooks

Confirmed arguments include port, host, no-browser, log, tray, skip-update, help, and version. The CLI launches and supervises the application, waits for TCP readiness, opens UI/tray, and manages stale processes.

## Dashboard and API authentication

`src/dashboardGuard.js` establishes the following contracts:

- Public API allowlist includes health, initialization, locale, authentication, version, and require-login checks.
- LLM prefixes are public route surfaces, but remote requests still require a valid API key unless local/CLI trust applies.
- Management APIs default to protected.
- Sensitive operations are always protected.
- Host-level and process-spawning operations are local-only.
- API keys can arrive through Bearer authorization, `x-api-key`, `x-goog-api-key`, or a `key` query parameter.
- Dashboard sessions use an HS256 JWT cookie, approximately 24-hour lifetime, httpOnly, SameSite lax, and secure behavior based on forwarded HTTPS.
- Passwords use bcrypt.
- The default initial password can be `123456` unless overridden.
- CLI identity uses a machine-derived token path.
- Dashboard access through tunnels can be disabled independently.

## Provider registry and execution

The original has approximately 100 static provider registry entries. Registry metadata covers transport, OAuth, media capabilities, model definitions, and display information.

Auth styles include:

- API key
- OAuth
- Free tier
- Dual/mixed auth for some providers

Service kinds include LLM, embedding, TTS, STT, image, image-to-text, web search, and web fetch.

Generic OpenAI-compatible and Anthropic-compatible providers can use common execution paths. Nonstandard protocols require specialized executors. Confirmed specialized executor families include Codex, Cursor, Kiro, Antigravity, Gemini CLI, GitHub, Grok CLI/web, Qwen, Qoder, iFlow, Kimchi, Vertex, OpenCode/OpenCode Go, Perplexity web, Ollama local, CommandCode, Xiaomi token plan, Mimo free, and CodeBuddy CN.

Binary, protobuf, AWS EventStream, and provider-specific NDJSON protocols cannot be assumed to round-trip safely through a generic OpenAI representation.

## Model naming and routing

Confirmed contracts:

- Primary model syntax is `{providerAlias}/{modelId}`.
- Parsing splits on the first slash.
- A name without a slash can be a user alias, built-in alias, or combo.
- Provider aliases resolve to canonical providers.
- Kiro normalizes selected dashed version IDs to dotted IDs.
- Codex model listings can synthesize `-review` variants that map to base upstream models.
- Model kinds and capabilities affect routing.
- Unknown model kind can be inferred through naming heuristics.

Input format is detected from endpoint first and request body second. Supported ecosystems include OpenAI chat and Responses, Claude, Gemini, Gemini CLI, Vertex, Codex, Antigravity, Kiro, Cursor, Ollama, and CommandCode.

Exact source-to-target translators can bypass the generic OpenAI intermediate representation when the intermediate would lose thinking, tools, images, errors, or binary protocol semantics.

## Chat orchestration

`src/sse/handlers/chat.js` performs product-level orchestration:

- Parses JSON and validates API access.
- Requires/resolves model names.
- Handles bypass paths.
- Expands combos.
- Selects fallback, round-robin, or fusion behavior.
- Iterates available accounts for each candidate model.
- Refreshes tokens and loads provider project IDs before request execution.
- Calls `chatCore`.
- Marks failed accounts unavailable and moves to the next connection when policy permits.

`open-sse/handlers/chatCore.js` performs protocol and provider execution:

- Detects source format.
- Resolves target transport, upstream model ID, strip lists, and thinking overrides.
- Negotiates streaming versus JSON using request body, Accept header, provider requirements, and client quirks.
- Uses native passthrough where safe.
- Removes unsupported modalities.
- Prefetches remote images.
- Translates and deduplicates tools.
- Applies RTK, Headroom, Caveman, Ponytail, and Pxpipe hooks, generally fail-open.
- Executes the provider-specific executor.
- Logs request details and usage.
- On 401/403, attempts credential refresh and one retry.
- Normalizes upstream errors.
- Handles forced-SSE-to-JSON, non-streaming, and streaming output paths.

## Combo behavior

Confirmed combo modes:

- Sequential fallback
- Round-robin with sticky count
- Capability-based auto-switching
- Fusion/panel execution with judge synthesis

Round-robin state is stored in an in-memory Map. This is a process-local behavior and a concurrency invariant that must be explicitly considered later.

Fusion behavior includes parallel panel calls, tool stripping and forced non-streaming panel requests, quorum plus grace timing, a hard timeout, and judge synthesis. It degrades to direct output when only one panel succeeds and to 503 when none succeeds.

Account/model fallback classifies status and textual errors, uses model-level locks, exponential cooldowns, and provider reset timestamps. Some transient combo failures wait briefly before trying the next model.

## OAuth and token lifecycle

OAuth is not one uniform flow.

Confirmed mechanisms include:

- Generic PKCE with a random loopback callback listener
- Codex fixed localhost callback port 1455
- xAI fixed localhost callback port 56121
- Device-code polling providers
- Multiple Kiro login methods
- Cursor import from local IDE state
- Dashboard callback relay through postMessage, BroadcastChannel, and localStorage with origin checks

Original source uses in-memory pending state for multiple OAuth flows. This is evidence of a single-process assumption, not a rewrite architecture decision.

Provider credentials are stored inside `providerConnections.data` JSON. Observed fields include access tokens, refresh tokens, expiry, API key, ID token, project ID, and provider-specific identifiers.

No database encryption wrapper was found in the inspected persistence path. The source-backed wording is: credentials appear to be plaintext at rest in the inspected database/repository path. A final audit must avoid claiming universal proof until all write/read call paths are checked.

Refresh behavior is provider-specific. Refresh can happen near expiry or after provider-defined staleness. Codex includes an approximately eight-day stale-age rule. In-flight refresh is deduplicated by a process-local Map. Invalid-grant/reused-token failures require user re-login.

Connection deduplication is provider/auth specific:

- Many OAuth connections dedupe by email or workspace.
- Codex can dedupe by account ID.
- API-key connections use provider and name semantics.
- Access-token connections may intentionally avoid deduplication.

## Persistence

SQLite fallback order:

1. Bun SQLite
2. better-sqlite3
3. node:sqlite
4. sql.js

Schema version 1 uses WAL, NORMAL synchronous mode, memory temp storage, mmap/cache settings, foreign keys, and a 5000 ms busy timeout.

Confirmed tables:

- `_meta`
- `settings`
- `providerConnections`
- `providerNodes`
- `proxyPools`
- `apiKeys`
- `combos`
- `kv`
- `usageHistory`
- `usageDaily`
- `requestDetails`

Schema synchronization is additive for tables, columns, and indexes. Destructive changes require explicit migrations. Safety backups occur before schema version upgrades. Legacy JSON import and backup/pruning paths exist.

Database export/import is partial by contract:

- Export includes settings, connections, nodes, pools, API keys, combos, and selected kv scopes.
- Export excludes usage history and request details.
- Import is destructive and transactional for included domains.
- `_meta` is retained.
- SQLite `INSERT OR REPLACE` semantics are used.

Some compatibility usage/log files may still live under `~/.9router` independently of DATA_DIR. This needs exact final tracing before migration design.

## Streaming and cancellation

`open-sse/utils/streamHandler.js` confirms:

- A per-request AbortController.
- Client disconnect is marked once.
- Upstream abort is delayed by about 500 ms.
- Network reset, Abort, ETIMEDOUT, and EPIPE can be treated as graceful client-close scenarios.
- Structured terminal events are emitted at most once when required, including Responses failure plus `[DONE]` behavior.
- Cancellation propagates to the reader and writer.

The stall watchdog is based on raw upstream byte activity, not transformed output. This is necessary because binary or reasoning streams can receive bytes while producing no visible downstream output. Every raw chunk resets the timer. Default stall timeout is approximately six minutes. First-chunk timeout is roughly 200 seconds in common runtime settings, with provider exceptions.

Specialized executors can implement their own cancellation behavior. Streaming parity therefore cannot be represented as only generic OpenAI SSE framing.

## Process-local mutable state

Confirmed process-local state includes:

- Combo round-robin position
- Credential refresh locks
- OAuth pending sessions
- Login limiter attempts
- Proxy rotation
- Provider model caches
- MCP child/session registry
- Console/EventEmitter buffers
- Usage statistics timers
- Pxpipe timelines
- MITM certificate and ALPN caches
- Quota autopinger intervals
- Network and watchdog timers

These are behaviorally relevant under a different concurrency/process model.

SQLite adapters also maintain statement caches, checkpoint timers, and signal handlers. Request details buffer writes and flush on process exit. sql.js has delayed persistence.

## Frontend behavior and browser state

At least 57 client files use local/browser storage, callback channels, API calls, polling, or SSE.

User-visible flows include:

- Provider quota refresh
- Model availability refresh
- Endpoint/tunnel probes
- Console and usage streams
- Cooldown countdowns
- OAuth polling
- CLI-tool controls
- Bulk Codex import
- Compatible provider nodes
- Custom models
- Connection proxy-pool assignment

OAuth callback uses postMessage, BroadcastChannel, and localStorage fallback. Login uses an approximately five-second fetch timeout and retry countdown.

## Tests and known baseline

The repository contains at least 156 test files and approximately 1,634 test declarations.

Coverage includes:

- Translator and golden round-trip behavior
- Thinking, tools, and image semantics
- Binary and specialized executors
- OAuth and token refresh
- Provider catalogs, models, and capabilities
- Combos, fusion, and auto-switching
- Database drivers, migration, concurrency, and benchmarks
- Dashboard guard and security
- RTK, Headroom, and Pxpipe
- Usage and request details
- Streaming terminal events and cancellation
- Media modalities
- CLI commands
- MITM

The raw upstream suite is intentionally not all green on a plain checkout. Existing guidance cites a large known-failure baseline, missing optional/cloud paths, network-dependent tests, and credential-requiring real-provider tests. Future parity validation must compare against a committed known baseline rather than demand absolute all-green results.

## Packaging and host integration

The root standalone app and CLI package have separate version/build concerns. CLI packaging includes SQLite, tray, and MITM runtime handling.

The launcher can:

- Kill stale processes.
- Spawn the server.
- Wait for TCP readiness.
- Open the dashboard or tray.
- Handle updates and autostart.

Runtime APIs and features can spawn or manage child processes, including updater, tunnels, MITM, Headroom, Pxpipe, and MCP.

## User-owned decisions that need source-backed review

These are flags only. They do not change `DECISIONS.md`.

1. “All current endpoints” was previously framed around 9router-mw. Original upstream has a significantly broader, multi-modal API and management surface.
2. “OpenAI-compatible SSE” alone does not describe the original protocol surface. Original behavior includes Claude, Gemini, Responses terminal semantics, forced streaming conversion, native passthrough, and specialized binary protocols.
3. A main-server-only OAuth design differs from original random and fixed loopback callback listeners. Whether to preserve or replace that behavior is the user's decision.
4. Original OAuth state is often process-local. This documents an assumption but does not decide whether PostgreSQL, Redis, or another mechanism should replace it.
5. Go, chi, PostgreSQL, React/Vite, single tenancy, and no initial app rate limit remain user decisions and are not changed by this audit.

## Confirmed uncertainties and remaining audit work

- Generate an exact complete HTTP route inventory rather than relying on truncated glob output.
- Verify CORS behavior for each compatibility route family.
- Generate exact provider count, auth type, service kind, transport, and executor matrix.
- Trace every OAuth service and its CLI/dashboard/server ownership.
- Trace all usage accounting, pricing, persistence, and compatibility-file paths.
- Map initialization routines and every background interval/watchdog.
- Produce an exact frontend API/polling/SSE/storage matrix.
- Produce an exact CLI command/menu/process matrix.
- Catalog test commands and known failure baseline from committed scripts.
- Verify all credential persistence paths before making a final at-rest encryption statement.
- Map deployment, update, backup, import/export, and rollback behavior precisely.

## Audit status

This is the persisted first-pass source-backed audit. It is intentionally not a final architecture or rewrite plan. Remaining work must be appended in dedicated Markdown files, with exact file paths and symbols, before presenting decisions to the user.
