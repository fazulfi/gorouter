# Audit 12: Source-Backed Parity Matrix and User Decision Register

## Scope and governance

This report synthesizes the original 9Router audit reports `00` through `11` into one parity matrix. It does not select rewrite architecture, technologies, storage mechanisms, deployment topology, or product scope.

All rewrite decisions remain with the user and are recorded separately in `../DECISIONS.md`. When a current decision differs from upstream behavior, this report identifies the behavioral delta and the questions that must be answered. It does not change the decision.

## Audited source

- Repository: `decolua/9router`
- Exact commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Product version: `0.5.40`
- Read-only clone: `../upstream-original`
- Evidence reports: `00-upstream-audit.md` through `11-runtime-background-jobs.md`

## Evidence rules

- **Confirmed** means directly supported by the audited source or a generated inventory in reports `00`–`11`.
- **Observed** means the surface was inventoried but not every internal branch was semantically traced.
- **Uncertain** means the reports contain incomplete or conflicting evidence.
- **User decision required** means source behavior creates a rewrite choice, but source does not determine that choice.
- **Parity** means preserve the externally observable behavior or explicitly record that it is being replaced. It does not mean copying the original implementation.

## Executive parity matrix

| Domain | Confirmed original behavior | Hidden invariant or edge case | Rewrite parity obligation | Evidence | Decision status |
| --- | --- | --- | --- | --- | --- |
| HTTP surface | 147 route files and roughly 202 method/path combinations were inventoried across compatibility, management, OAuth, media, CLI-tool, tunnel, and operational APIs | Route-file count and method/path count are different measurements; generated/default exports are not necessarily HTTP handlers | Produce an authoritative endpoint contract and account for every original route as preserved, intentionally changed, or intentionally omitted | `01`, sections 4–9 and 18 | User must confirm whether “all endpoints” means original upstream or a chosen subset |
| Compatibility rewrites | `/v1`, `/v1/v1`, `/v1beta`, `/responses`, and `/codex/*` rewrite into internal API routes | `/codex/*` discards its subpath and always reaches Responses; double `/v1` normalization is intentional compatibility | Preserve aliases and normalization or document client-breaking differences | `01`, sections 4, 8.23, 10 | Not yet reviewed against decision 9 |
| Client authentication | LLM API keys are accepted from Bearer, `x-api-key`, `x-goog-api-key`, or `?key=` | Extraction precedence is observable; remote LLM routes still require a key even though route prefixes are public at middleware classification | Preserve accepted locations and precedence unless clients will be migrated | `01`, sections 6.2–6.4; `09`, sections 2–4 | User decision if query-string keys or original key format are removed |
| Trust boundary | TCP peer identity is authoritative; forwarding headers are trusted only from loopback proxy peers; attacker-supplied forwarding/internal headers are stripped and restamped | Local-only authorization depends on `x-9r-real-ip` and `x-9r-via-proxy`; Host alone is not trusted | Reproduce equivalent unspoofable peer/trusted-proxy semantics before local-only operations are exposed | `00`, section 5; `01`, section 5; `09`, sections 1–3 | Implementation mechanism is a user architecture decision |
| Authorization zones | Public, LLM-key-protected, protected management, always-protected, and local-only behavior coexist | Overlapping paths can be both always-protected and local-only; dashboard login policy can be disabled while always-protected operations remain guarded | Preserve an explicit route-policy matrix, not ad-hoc handler checks | `01`, section 6; `09`, section 2 | Current “basic auth + cookie session” decision does not yet specify all zones |
| CORS | LLM compatibility APIs broadly expose CORS; most management APIs remain same-origin | Browser SDK compatibility relies on broad LLM CORS while management security relies on same-origin behavior | Preserve per-surface CORS behavior or consciously tighten it with client impact recorded | `01`, sections 7 and 10 | User decision required |
| Provider registry | 100 static registry entries, five categories, multiple auth modes and service kinds, plus dynamic compatible nodes | Registry imports generate derived transport/model/OAuth/media maps; some aliases and duplicate-looking entries remain uncertain | Build an exact provider-by-provider disposition matrix; do not assume “OpenAI-compatible” covers specialized providers | `02`, sections 1–7 | Provider inclusion/exclusion remains user scope decision |
| Specialized execution | 25 specialized executors plus default fallback handle nonstandard protocols | Binary, protobuf, AWS EventStream, native Responses, web-cookie, local-device, and custom NDJSON paths can lose semantics through a generic bridge | Each specialized executor needs either a faithful implementation or an explicit incompatibility record | `02`, section 5; `05`, sections 4.3–4.4 | User must decide provider waves and acceptable omissions |
| Model resolution | Primary form is `providerAlias/modelId`; no-slash names can resolve combo, user alias, built-in alias, inferred provider, then OpenAI fallback | Parsing splits at first slash; combo lookup precedes ordinary model handling; unknown names can route to OpenAI | Preserve resolution order and collision behavior or publish changed semantics | `00`, section 14; `06`, sections 4.1–4.3 and 6 | Decision 8 adds `{namespace}/{model}({variant})`; exact compatibility is unresolved |
| Model capabilities | Model kind/capability affects routing and auto-switch; kinds extend beyond LLM | Only the trailing user turn drives media capability reordering; search capability code exists but is inactive | Preserve active capability inference and edge behavior before extending it | `02`, section 4; `06`, sections 4.7 and 6 | User decision on whether inactive behavior remains inactive |
| Combo fallback | Sequential fallback tries models/accounts in order and respects error policy | A non-fallback error stops immediately; transient 502/503/504 may delay up to five seconds before next model | Preserve ordering, stop conditions, response statuses, and cooldown timing or record changes | `06`, sections 4.3–4.9 and 8 | No conflict with current decisions, but behavior is not yet specified there |
| Combo round-robin | Rotation uses a process-global Map with sticky count | State resets on process restart and is not shared between processes/machines | Preserve observed distribution or consciously choose a new one and test the delta | `06`, sections 4.5, 4.18, 7 | Single-binary decision removes cross-process ambiguity but persistence semantics remain a user decision |
| Fusion | Panel models run in parallel, tools are removed, tool turns flattened, quorum/grace/hard-timeout applied, judge synthesizes; one success returns directly and zero returns 503 | Fusion performs N+1 upstream calls; timed-out fetches may continue even after their results are ignored | Preserve request mutation, deadlines, degradation behavior, and cancellation policy or explicitly replace them | `06`, sections 4.6, 6, 8 | User decision required for parity depth and resource policy |
| Account fallback | Accounts are tried sequentially; token refresh precedes fallback; model and account locks are separate | Text rules are evaluated before status rules; rate-like failures apply exponential cooldown; account selection internals still have an audit gap | Preserve tested classification/order/cooldown semantics and finish the auth-selection audit before implementation | `06`, sections 4.8–4.14, 6, 13 | Exact selection order still uncertain |
| Executor retry | Network errors map to retry behavior; per-status retries and URL fallback differ | URL fallback and same-URL retry are separate mechanisms; 429 behavior changes when no alternate URL exists | Preserve attempt counts, delay rules, retry veto hooks, and abort propagation per executor | `06`, sections 4.11–4.12, 6, 8 | User may choose configurable rules, but default compatibility must be explicit |
| Input/output formats | Format detection supports 13 identifiers and endpoint/body heuristics across OpenAI, Responses, Claude, Gemini, Vertex, Codex, Antigravity, Kiro, Cursor, Ollama, and CommandCode | Detection order is greedy; ambiguous bodies depend on branch ordering | Preserve detection priority and test ambiguous boundary cases | `05`, sections 4.1–4.4 and 6 | Decision 10 currently describes only OpenAI SSE and is incomplete for upstream parity |
| Translation | Exact direct translators coexist with an OpenAI pivot and native passthrough | OpenAI pivot is documented as lossy for thinking, images, tool IDs, and error semantics; translator availability depends on side-effect imports | Inventory every direct route and preserve it or consciously accept known loss | `05`, sections 4.3–4.4 and 6 | User decision required for supported format pairs |
| SSE termination | OpenAI streams use `[DONE]`; Gemini-family does not; Responses passthrough can synthesize `response.failed`; terminal emission is guarded against duplication | `[DONE]` and stream-close semantics are protocol-specific, not universal | Preserve protocol-specific terminal sequences and one-time terminal guarantees | `05`, sections 4.5–4.7 and 6; `01`, section 9.2 | Decision 10 conflicts if interpreted as the only stream protocol |
| Stream liveness | Stall watchdog resets on raw upstream bytes, not transformed output; common stall timeout is about six minutes | Reasoning/binary streams may receive bytes without producing client-visible output; output-based watchdog would abort valid requests | Track upstream activity separately from downstream output | `00`, section 20; `05`, sections 4.8–4.9 and 6 | Timeout values remain user decisions |
| Cancellation | Per-request abort; client disconnect marked once; upstream abort delayed about 500ms; specialized executors can own cancellation | Some network errors are treated as graceful disconnects; fusion timeout may not cancel hung fetches | Preserve context propagation, cleanup, terminal behavior, and provider-specific cancellation semantics | `00`, section 20; `05`, sections 4.8 and 8 | User decision on exact delay and forced cancellation policy |
| Forced stream-to-JSON | Streaming-only upstreams can be reconstructed into JSON through separate Responses and Chat paths | The two reconstruction paths can differ in tool/reasoning behavior; parser expects OpenAI-style SSE for one generic path | Maintain separate tested protocol logic; do not assume generic reverse proxy is sufficient | `05`, sections 4.7, 8, 10, 13 | Current ReverseProxy-oriented draft does not yet account for translation/reconstruction |
| Usage accounting | Usage is extracted across formats; persistence occurs at stream completion/forced-JSON completion; a 2000-token buffer is added | Failed/disconnected streams and estimated usage have special handling; `logUsage` hook itself is not persistence | Preserve accounting triggers, deduplication, estimates, and pricing side effects or define migration differences | `05`, sections 4.10 and 6; `04`, section 4.10 | User decision on preserving the 2000-token adjustment |
| Modalities | Original exposes LLM, embeddings, image, TTS, STT, voices, web search/fetch, and video operations | Per-modality handlers have independent request, response, timeout, and provider capability rules | “All original functionality” requires modality-specific contracts, not only chat SSE | `00`, sections 7–9; `01`, sections 8.21–8.22 | Decision 9 currently lists only a narrow subset |
| OAuth architecture | Roughly 20 provider OAuth definitions span authorization-code PKCE, device code, browser token, imports, cookies, PATs, and provider-specific flows | Codex 1455 and xAI 56121 use fixed callback proxies; generic CLI flow can use random loopback; dashboard callback relays through postMessage, BroadcastChannel, and localStorage | Preserve each provider's callback and token-exchange contract or record a provider-specific replacement | `02`, section 6; `03`, sections A–C and callback mechanics | Decision 14 intentionally differs; feasibility and client/provider redirect compatibility require user review |
| OAuth transient state | Multiple flows store state/verifier/session in browser/CLI memory or process-local Maps with five-minute proxy timeouts | This is a single-process assumption, not a requirement to copy Maps | Preserve observable timeout, CSRF state, PKCE, status/error transitions, and concurrent-session behavior using the user-chosen mechanism | `03`, sections 4–6; `00`, section 18 | Decisions 17/19 choose PostgreSQL; no source conflict if behavior is preserved |
| Token persistence | OAuth/API credentials are stored in `providerConnections.data` JSON; no encryption wrapper was found in inspected paths | Dedup identity differs by provider/auth type; access-token connections may intentionally avoid dedup | Preserve all provider fields and dedup identities through migration; encryption is a separate user decision | `03`, token persistence; `04`, sections 4.2 and 9 | User must decide encryption-at-rest |
| Token refresh | Refresh lead and function are provider-specific; reactive refresh occurs on 401/403 before fallback; in-flight refresh dedup is process-local | GitHub Copilot uses a two-phase refresh; invalid grant/reuse requires re-login | Preserve per-provider refresh fields, lead times, retry boundaries, and unrecoverable classifications | `03`, sections 7–8; `06`, section 4.13 | User decision on shared refresh coordination in the new process model |
| Database schema | SQLite schema has `_meta` plus ten domain tables, 19 reported indexes, WAL and five-second busy timeout | Report `04` heading says “10 tables including _meta” but lists 11 names; summary says “9 + 1 meta.” The actual schema list must be rechecked before migration | Treat the enumerated names as evidence but verify exact source count before defining PostgreSQL schema | `04`, sections 1 and summary; `00`, section 19 | PostgreSQL is decided; exact schema mapping is pending user review |
| Repository semantics | Repositories implement singleton settings, provider priority reorder, provider-specific connection dedup, KV scopes, usage dedup, buffered request details, pricing cache | Semantics are more important than table shape; request detail writes are gated, truncated, sanitized, batched, and pruned | Reproduce transaction-level behavior and invariants in the chosen database | `04`, section 4 and 10 | Database technology is decided; behavioral mapping remains required |
| Import/export | Export is intentionally partial; import wipes included tables and replaces them transactionally; missing fields can wipe tables | Usage, request details, `_meta`, and disabled models are excluded; import is not a merge | Preserve exact payload and destructive semantics or version the contract explicitly | `04`, section 8 and invariants | User decision required because original behavior is risky |
| Backup | Backup precedes schema upgrades/legacy migration, excludes requestDetails, retains three; restore is manual | The backup and public export are different contracts | Define PostgreSQL backup/restore behavior separately while preserving user-visible export compatibility | `04`, sections 7, 8, 15–17 | User decision required |
| Legacy migration | Four legacy JSON files can be imported once with row-count assertion and rollback marker | Marker and freshness checks prevent repeated imports | If existing users migrate, support or consciously reject legacy migration with an explicit tool/path | `04`, sections 3, 5, 6 | User decides whether clean-room rewrite migrates original installations |
| Browser dashboard | 28 dashboard routes, client-heavy pages, API polling, SSE consumers, OAuth callback channels, and local browser state were inventoried | Report `07` contains unresolved client files and may overstate “theme-only persistence”; first-pass audit found basic-chat localStorage state | Preserve route/workflow/API contracts based on completed per-page tracing, not the conflicting summary claim | `00`, sections 10 and 21; `07`, sections 1–12 | React/Vite is decided; exact product parity remains user scope |
| Dashboard realtime | Usage and console use SSE plus snapshot fetches; multiple health/quota/tunnel workflows poll on fixed intervals | Browser and server both probe tunnel reachability with miss debouncing | Preserve event shapes, snapshot recovery, retry/backoff, and user-visible stale/loading states | `07`, sections 3–5 and invariants | SSE/WebSocket mechanism is a user decision, but wire behavior is observable |
| Dashboard auth | Original supports bcrypt password, JWT cookie, OIDC, login limiter, requireLogin toggle | Default initial password can be `123456`; cookie and lockout behavior have security implications | The new session design may differ, but login/logout/status/lockout/remote access behavior needs an explicit compatibility disposition | `00`, section 12; `09`, sections 6–8 | Decision 6 intentionally replaces JWT; parity details pending |
| CLI artifact | Separate npm CLI launches/supervises standalone server, supplies terminal menus/API client, tray, autostart, update and xAI video command | Server child is detached; CLI communicates via HTTP; startup kills stale processes; runtime dependencies self-heal | Decide whether CLI/host integration is in scope; if yes, inventory every platform contract before implementation | `08`, sections 1–16 | Current decisions mention server/dashboard but do not explicitly disposition CLI parity |
| CLI authentication | CLI token is machine-derived and sent through `x-9r-cli-token` | Token is machine-specific and participates in local-only/always-protected policy | Preserve or consciously replace machine identity and all CLI-authorized operations | `08`, invariants; `09`, sections 2 and 11 | User decision required |
| Host integrations | Tray, launchd/Startup VBS/Linux desktop autostart, update checks, process killing, MITM and child tools are part of product behavior | Windows tray uses PowerShell; autostart always includes `--tray --skip-update`; repeated crash disables MITM | Each host feature needs an explicit included/deferred/removed status | `08`, sections 4–15 | Not addressed by current one-week build scope |
| Runtime initialization | Init-once guards, deferred heavy startup, watchdog, network monitor, quota scheduler, proxy init, console capture, tunnel recovery and process cleanup run in background | Numerous globals/timers own lifecycle; some values and cleanup paths are uncertain or untested | Convert every background job into an explicit lifecycle disposition and preserve externally visible recovery behavior | `11`, sections 1–14 | Single-binary decision is compatible, but job ownership remains user design |
| Runtime observability | Console methods are monkey-patched into a buffer for dashboard streaming; usage/request details provide additional visibility | Flush timer may keep event loop alive; request details default off and prune to 1000 | Structured logs/Prometheus do not automatically replace dashboard console and request-detail contracts | `04`, section 4.11; `11`, sections 8 and summaries | Decision 13 needs a user decision on dashboard log parity |
| Security defaults | Default API-key secret, machine salt, and initial password exist; OAuth/provider credentials appear plaintext at rest | Defaults can be predictable if not overridden; login limiter is per-process | Define secure initialization/migration policy while documenting any compatibility break | `09`, sections 5–8 and decision implications | User security decision required |
| SSRF/proxy safety | Shared SSRF guard and outbound proxy validation were found | Integration of SSRF guard into all executor fetch paths was not proven | Complete call-path audit before claiming protection parity | `09`, sections 9–10, uncertainties, gaps | Unresolved evidence, not a design decision yet |
| Test baseline | At least 156 test files and a committed known-failure gate exist; real/provider/OS tests are not universally green | Regression rule permits known failures but zero unknown failures; several audit tests are static source checks, not runtime tests | Build a behavior-focused parity suite derived from confirmed contracts and preserve a baseline mechanism | `10`, sections 1–7 | Test framework is a user implementation decision |
| Test gaps | Initialization, network monitor, tunnel recovery, updater, console lifecycle, SSRF integration, OIDC failures and several auth overlaps lack runtime coverage | Original absence of tests does not mean behavior is optional | Turn high-risk untested invariants into rewrite acceptance tests before replacement | `09`, gaps; `10`, gaps; `11`, gaps | Prioritization belongs to user |

## Non-negotiable source invariants for any parity claim

These are not architecture choices. They are source-backed conditions that must be preserved or explicitly declared changed before calling the rewrite compatible.

1. **Trusted client identity cannot come from arbitrary forwarding headers.** TCP peer and trusted-proxy boundaries determine whether a request is local.
2. **Route policy is tiered.** Public, LLM-key, protected management, always-protected, and local-only operations are not interchangeable.
3. **Compatibility paths are behavior.** Double `/v1`, `/responses`, `/codex/*`, and Gemini paths affect existing clients.
4. **Model resolution order matters.** Combo names, aliases, slash parsing, provider aliases, inference, and fallback can collide.
5. **Generic OpenAI proxying is insufficient for full parity.** Direct translators and specialized executors preserve semantics that the pivot loses.
6. **Streaming completion is protocol-specific.** `[DONE]`, Gemini close, and Responses terminal events cannot share one unconditional rule.
7. **Liveness is based on upstream byte activity.** Output-silent thinking/binary streams must not be declared stalled solely because no visible content was emitted.
8. **Cancellation must reach upstream work and cleanup paths.** Client disconnect behavior includes a deliberate delay and provider-specific handling.
9. **Credential refresh occurs before account fallback on authentication failures.** Provider-specific refresh and lock/cooldown state affect routing.
10. **Combo behavior includes three distinct strategies.** Fallback, round-robin, and fusion have different mutation, timing, and degradation contracts.
11. **OAuth is provider-specific.** Callback ports, device flow, imports, cookies, PATs, PKCE parameters, and status polling are not one generic flow.
12. **Persistent behavior lives above the physical schema.** Dedup, priority reorder, partial export, destructive import, usage accounting, and buffered observability are domain contracts.
13. **The dashboard and CLI are product surfaces, not incidental tooling.** Their routes, polling, browser state, tray, launcher and local-host operations require explicit disposition.
14. **Background recovery is part of runtime behavior.** Tunnel/network monitoring, quota scheduling, MITM guards, updater and process cleanup are observable.
15. **The original test suite has a known-failure baseline.** A clean-room parity claim needs evidence against confirmed behavior, not merely a green new unit suite.

## Highest-risk rewrite areas

| Risk | Why it is high risk | Required evidence before declaring parity |
| --- | --- | --- |
| Specialized providers | Provider-specific binary/native protocols and auth fields can be silently damaged by generic translation | Golden request/stream/error tests per specialized executor and provider-specific token lifecycle tests |
| Streaming terminal behavior | Small ordering differences cause clients to hang, duplicate terminal events, or parse invalid responses | Byte-level fixtures for OpenAI, Responses, Claude, Gemini and forced-stream conversion, including abort/stall/error cases |
| Account and combo fallback | Retry, cooldown, refresh, model locks, account ordering and combo ordering interact | Deterministic scenario tests proving exact attempt order, delays, selected account/model and final status |
| OAuth callbacks | Browser, CLI, fixed-port, device and import flows have distinct state ownership | End-to-end flow tests per flow family, including concurrent sessions, expiry, CSRF mismatch, provider denial and token persistence |
| Security trust boundary | Header trust mistakes can turn remote callers into local callers | Socket-level proxy/direct tests with spoofed Host/Origin/forwarding/internal headers and every protected tier |
| Data migration | Schema shape alone does not preserve dedup, priority, partial export/import, credentials or usage counters | Source-to-target migration fixtures with row and semantic assertions, rollback, repeated-run behavior and secret handling |
| Frontend workflow parity | Polling/SSE/browser callback/local-state behavior can fail despite API success | Browser tests for provider onboarding, OAuth, usage realtime, tunnel status, login lockout, CLI tools and error recovery |
| Host integration | Tray/autostart/update/MITM/tunnel/MCP require OS and child-process behavior | Platform acceptance matrix with explicit unsupported statuses and lifecycle cleanup tests |
| Background jobs | Timer/global lifecycle bugs create duplicate work, leaks, stale status or missed recovery | Fake-clock/lifecycle tests for start-once, stop, restart, cancellation and shutdown of every job |
| Scope control | “All endpoints” and “OpenAI SSE” currently underdescribe the original product | Signed user disposition matrix mapping every audited domain to preserve/change/defer/remove |

## Conflicting or incomplete evidence that must not become design assumptions

1. **Database table count conflict.** Report `04` lists `_meta` plus ten domain tables but labels it inconsistently as ten total and later nine plus meta. Verify directly from `src/lib/db/schema.js` before defining the PostgreSQL schema.
2. **Dashboard browser persistence conflict.** Report `07` says theme is the only persistent browser state, while report `00` identifies basic-chat sessions/provider/draft in localStorage. The relevant clients require a targeted follow-up read.
3. **OAuth CLI ownership wording.** Some audit wording says CLI starts a local server while exchanging/persisting through server API. Exact provider call paths should be verified per flow before a shared abstraction is designed.
4. **CORS universality.** LLM routes are strongly evidenced; management CORS is mixed. Do not generalize “all APIs have CORS.”
5. **Provider counts and categories.** Registry count 100 is generated evidence, but alias/duplicate-looking entries (`mmf`, `mimo-free`, `kimi-coding`, `alims-intl`) require semantic disposition.
6. **Credential encryption.** No encryption wrapper was found in inspected persistence paths. Phrase this as observed absence, not mathematical proof over every future/provider path.
7. **Usage path documentation.** Source audit says old file-based usage documentation is stale, while one security report repeats possible path divergence. Prefer repository code and perform a targeted path audit before migration.
8. **Account selection internals.** `getProviderCredentials` ordering and mutex behavior were not fully audited in report `06`.
9. **Runtime constants.** Several watchdog/network cooldown values were imported but not read in report `11`.
10. **Frontend coverage.** Several substantial clients, the service worker, request-details tab and pricing page remain incompletely traced.
11. **CLI packaging details.** Tray PowerShell and exact build/MITM packaging scripts were not fully read.
12. **Test totals.** File count and individual assertion count are different; only a non-runtime inventory was performed.

## Current user decisions: source-backed review flags

This section does not change `DECISIONS.md`.

| Decision | Current user-owned choice | Source-backed flag for review |
| --- | --- | --- |
| 1: Scope | Proxy/SSE, provider routing/fallback, admin dashboard; all features from the beginning | Original also includes multiple modalities, CLI/host integration, tunnel/MITM/MCP/Headroom/Pxpipe, updates and background recovery. Their disposition is not recorded. |
| 2: Database | PostgreSQL with pgx | Compatible as a technology choice, but schema/repository parity includes domain semantics, legacy migration, partial export/import, backup, buffered observability and provider-specific dedup. |
| 3: HTTP framework | chi | No source conflict. Trust boundary, rewrites, CORS, streaming and route-policy behavior still require explicit implementation evidence. |
| 4/16: Frontend | React/Vite/shadcn/Tailwind | No source conflict. Page/workflow parity is much broader than generic CRUD tables/forms/charts. |
| 5: Process | Single binary, multi-goroutine | Removes original multi-process ambiguity but does not decide persistence of process-local rotation, refresh locks, login limiter, OAuth sessions, caches or timers across restarts. |
| 6: Auth | Basic login plus PostgreSQL-backed cookie session, no JWT | Intentional implementation change. Need user decisions for requireLogin toggle, OIDC, CLI identity, always-protected/local-only tiers, expiry, lockout and tunnel behavior. |
| 8: Model syntax | `{namespace}/{model}({variant})` | Original primary syntax is first-slash `providerAlias/modelId`, with no-slash alias/combo/inference and provider-specific synthetic variants. Exact grammar and backward compatibility need definition. |
| 9: Endpoints | All endpoints described from 9router-mw | Original upstream inventory is materially larger: approximately 202 method/path combinations and many modalities/management/host surfaces. User must define which inventory is authoritative. |
| 10: SSE | OpenAI chunks plus `[DONE]` | Insufficient as a description of original parity: Gemini, Claude, Responses terminal events, native passthrough, binary formats and forced-stream reconstruction differ. |
| 11: Tenancy | Single tenant, all keys reach configured providers | Broadly consistent with original deployment model; API-key field semantics and management access tiers still need explicit policy. |
| 12: Rate limit | None initially | Original has process-local login limiting and provider/account cooldown/rate handling. “No app rate limiting” must not accidentally remove login abuse control or upstream fallback cooldowns. |
| 13: Observability | JSON stdout plus Prometheus | Does not automatically preserve dashboard console stream, usage history, request details, pricing/cost accounting or provider quota UI. User must choose which original observability products remain. |
| 14: OAuth | Main server only, no separate callback listener | Deliberately differs from fixed/random loopback callback behavior. Provider redirect URI constraints, CLI browser flows and local client callbacks require validation per provider. |
| 15: One-week layered implementation | DB → routing → SSE → admin → frontend | Full upstream parity includes at least 100 providers, 25 specialized executors, broad protocols/modalities, host integration and large test contracts. Scope must be dispositioned before schedule can be evaluated. |
| 17/19: OAuth state | PostgreSQL with expiry, no Redis | No source conflict if CSRF, PKCE, TTL, concurrent sessions, status/error transitions and cleanup behavior are preserved. Original implementation mechanism was in-memory, not a parity requirement. |
| 18: Config | godotenv | No direct source conflict. All original environment keys/defaults and security-sensitive initialization behavior still require an inventory. |

## Decisions required before rewrite implementation can honestly claim full scope

These are questions for the user; the audit does not answer them.

1. Is the authoritative parity target the original upstream route inventory in `01-http-contracts.md`, a 9router-mw subset, or a newly selected subset?
2. Which non-chat modalities are included: embeddings, image, TTS, STT, voices, search/fetch, video and Gemini/Claude/Responses compatibility?
3. Which of the 100 registry providers and 25 specialized executors must be available at first release, and which may be deferred?
4. Must request/response translation preserve every original format pair, or only a named compatibility matrix?
5. Is the original no-slash alias/combo/inference behavior retained alongside the new variant grammar?
6. Are fusion and capability auto-switch included, and must their timing/degradation behavior match?
7. Are original account fallback, model locks, error classifications and cooldown timing compatibility requirements?
8. How should process-local round-robin, refresh dedup, login limiting and caches behave across restart or future horizontal replicas?
9. Which OAuth flow families must be supported: dashboard PKCE, CLI loopback, fixed-port Codex/xAI, device code, imports, cookies, PAT and browser-token flows?
10. If fixed/random loopback callback listeners are removed, how will provider redirect constraints and local CLI browser callbacks be preserved?
11. Is OIDC dashboard login retained in addition to the new PostgreSQL session model?
12. Are CLI token and local-only host operations retained, replaced, or removed?
13. Is the npm CLI/tray/autostart/update product part of the rewrite scope?
14. Are MITM, tunnels/Tailscale, MCP, Headroom, Pxpipe, proxy deployment helpers and CLI-tool configuration in scope?
15. Is migration from original SQLite/legacy JSON required, or is the Go rewrite for fresh installations only?
16. Should original partial/destructive database import/export behavior remain compatible, be versioned, or be intentionally replaced?
17. Are credentials encrypted at rest in PostgreSQL, and what is the key/bootstrap/backup policy?
18. Which observability surfaces remain: usage/cost, request details, console SSE, quota auto-ping, logs and Prometheus?
19. Which original background recovery jobs remain product requirements?
20. What is the accepted test/parity gate for features whose original tests are known-failing, environment-specific or absent?

## Recommended next audit work, not architecture work

The following investigations would close material evidence gaps before implementation planning:

1. Read `src/sse/services/auth.js` completely and document exact account selection, mutex and lock ordering.
2. Reconcile actual table count directly from `src/lib/db/schema.js` and generate a machine-readable schema inventory.
3. Trace all localStorage/sessionStorage keys across frontend clients, especially basic chat and OAuth callback flows.
4. Finish the uncovered dashboard clients and service worker listed in report `07`.
5. Read exact CLI build scripts, `tray.ps1`, MITM packaging and child-process integration.
6. Trace SSRF guard usage through actual executor/proxy fetch call paths.
7. Extract watchdog/network constants and complete lifecycle cleanup ownership.
8. Generate a provider-to-executor-to-format-to-OAuth-to-model-capability matrix rather than summary counts.
9. Generate an acceptance-test catalog linking each parity row to original tests or a required new test.
10. Produce a user-approved disposition ledger: preserve, intentionally change, defer, or remove for every domain in this report.

## Completion status

Reports `00` through `11` have been synthesized. The original source reveals a substantially broader product and protocol surface than the current short scope summary in `DECISIONS.md`. No architecture decision was changed by this report. The next valid step is user review and explicit disposition of the questions above, followed by additional targeted audits for unresolved evidence—not unapproved rewrite implementation.
