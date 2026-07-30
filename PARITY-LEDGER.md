# gorouter Final Parity Ledger

Status: Approved as one final package on 2026-07-29. This approval authorizes implementation planning, not unplanned coding.

## 1. Governance

- Original source baseline: `decolua/9router` at commit `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`.
- Source evidence: `audit/00-upstream-audit.md` through `audit/13-recent-requests-behavior.md`.
- User decisions: `DECISIONS.md`, including cancellation and supersession history.
- Architecture boundary proposal: `ARCHITECTURE.md`.
- `Preserve` means observable behavior remains compatible, not that the JavaScript implementation is copied.
- `Change` means the user approved a behavioral or product difference.
- `Remove` means the user explicitly excluded the capability.
- Cancelled decisions are not implementation requirements.
- Approval applies to all domain tables as one package. Coding must not begin from partial table approval.

## 2. HTTP and compatibility contracts

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| OpenAI Chat and compatible `/v1/*` routes | OpenAI-compatible request/response and stream surface | Preserve | 20, 21, 32, 97, 110 | Versioned schemas, golden JSON/SSE fixtures, authentication and error contract tests |
| OpenAI Responses and compact routes | Native Responses semantics and terminal events | Preserve | 21, 32, 105, 312, 371 | Byte/order fixtures for completed, failed, tool, reasoning, abort, and compact behavior |
| Claude Messages and token count | Native Claude input/output semantics | Preserve | 21, 32, 105, 312 | Request/response/stream golden fixtures and count-token contract tests |
| Gemini and Gemini CLI compatibility | `/v1beta` and native Gemini-family behavior | Preserve | 21, 32, 105, 312 | Formal contracts and native stream/terminal fixtures |
| Codex compatibility routes | `/codex/*` normalization and Codex-native behavior | Preserve | 21, 32, 105, 110 | Rewrite/path tests plus Codex request, response, OAuth, and terminal fixtures |
| Double `/v1/v1/*` normalization | Intentional legacy client compatibility | Preserve permanently | 110 | Route normalization contract test |
| Embeddings, images, audio, web, and video APIs | Original modality-specific routes and semantics | Preserve | 20, 21, 24, 97 | Formal contracts and per-modality provider fixtures |
| Original management paths | Original dashboard/CLI management endpoints | Preserve through thin adapters | 20, 97, 354–356 | Compatibility endpoint tests proving same application use cases as Admin API v1 |
| New Admin API | Versioned `/api/admin/v1` for dashboard, CLI, PAT, CI, and integrations | Change: approved addition | 60, 63–69, 354–358 | OpenAPI, generated TypeScript client, authz, audit, and backward-compatibility tests |
| Model API key transports | Bearer, `x-api-key`, `x-goog-api-key`, and `?key=` | Preserve | 127 | Extraction precedence and redaction tests |
| Model route authentication on loopback | Upstream trusted-local behavior replaced by mandatory API key | Change | 72 | Direct and proxied loopback tests reject missing key |
| Model API CORS | Broad client-compatible CORS | Preserve | 128 | Browser preflight and actual-request tests |
| Dashboard/Admin CORS | Same-origin or explicitly configured origins | Change/harden | 128 | Origin allow/deny and CSRF tests |
| Public health | Safe liveness/readiness/minimum version only | Change/harden | 199 | Unauthenticated response contains no sensitive diagnostics |
| Detailed health | Protected by dashboard session or PAT | Change/harden | 199 | Authn/authz tests and complete internal diagnostics contract |

## 3. Trust, authentication, and authorization

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Trusted proxy boundary | Forwarded identity trusted only from approved proxy peers; spoofed headers stripped | Preserve and harden | 22, 134, 306, 313 | Socket-level direct/proxy spoof tests for all trusted headers |
| Trusted proxy configuration | Loopback default plus explicit IP/CIDR | Change: approved configuration model | 134 | CIDR parsing and direct/proxied authorization tests |
| Dashboard login | Original password/JWT model replaced with local password and PG session while retaining optional OIDC | Change/preserve OIDC | 47, 74–79, 375 | Password, lockout, session, logout, recovery, CSRF, cookie, and OIDC tests |
| OIDC dashboard login | Original OIDC remains an optional login mechanism alongside local password | Preserve | 375 | Provider configuration, callback, identity mapping, session, denial, and audit tests |
| Initial password | `12345678`, warning but no mandatory change | Change | 225, 227, 232 | First-run and persistent-warning tests |
| Password storage | Strong one-way hash; cannot be viewed | Change/harden | 47, 74 | Hash verification and no-recovery-of-plaintext tests |
| Password reset | Local `gorouter admin password reset`, revoke all sessions | Change | 74–77 | Local-only command, session revocation, audit tests |
| Dashboard sessions | PG-backed, 30-day sliding inactivity expiry | Change | 47, 76, 77 | Sliding expiry, revocation, concurrent session tests |
| PAT authentication | New full-access Admin API PAT, hash-only, optional expiry, unlimited count | Change: approved addition | 63–69 | One-time reveal, hashing, expiry, revocation, last-used, audit tests |
| PAT host-operation authority | Full remote host-operation authority when feature enabled | Change: approved high-risk policy | 68, 129 | Feature-policy, authentication, audit, and denial-when-disabled tests |
| Model API keys | Hash-only, one-time reveal, optional expiry, full model/provider access | Change/harden | 70, 71 | Hash, expiry, last-used, revoke, and route-access tests |
| Actor context | Session, PAT, local CLI, internal job share actor/origin/capability model | Change: approved internal contract | 357 | Application-service authz and audit attribution tests |
| Login abuse protection | Lockout/backoff retained and hardened | Preserve/harden | 47 | Fake-clock threshold, reset, source-IP, and audit tests |
| CSRF | Required for cookie-authenticated mutations | Change/harden | 47 | Same-site, cross-site, missing/invalid token tests |

## 4. Providers and account lifecycle

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Original provider registry | All original built-in providers remain official | Preserve | 20, 23, 51, 97, 165–167 | Registry parity matrix and live validation before beta/stable |
| Generic compatible providers | Dashboard-addable OpenAI- and Anthropic-compatible providers | Preserve | 51, 309 | Dynamic provider CRUD, discovery, test, routing, and error tests |
| Specialized executors | Protocol-specific providers remain specialized | Preserve | 23, 99, 105, 309 | Golden protocol/OAuth/error fixtures per executor plus live account test |
| New nonstandard providers | Require source change, review, official release; no plugin SDK | Change: approved extension policy | 51 | Registry generation/review/release tests |
| Provider account creation | Save immediately, active/routing-eligible | Preserve | 62, 121 | Create and immediate-route tests |
| Validation status | Active on success; unknown for untested/transient/unsupported discovery; background revalidation | Preserve with clearer status model | 62, 121 | Status-transition and scheduler tests |
| Definitive auth rejection | Disable routing, preserve account for re-auth/audit | Preserve/harden | 62, 337 | 401/403/invalid_grant transition tests |
| Quota/rate state | Keep account with cooldown and fallback | Preserve | 62, 84, 325, 339 | Quota/rate classification and cooldown tests |
| Account dedup | Provider/auth-specific identities; access-token exceptions retained | Preserve | 106, 121, 347 | Fixture tests for each OAuth/API-key/import family |
| Account priority/reorder | Manual priority and original transactional reorder | Preserve | 122, 325, 340 | Deterministic selection and reorder transaction tests |
| Account selection | Priority, cooldown/model lock, same-model account fallback | Preserve | 98, 122, 325, 328 | Attempt-order and final-selection tests |
| Provider model discovery | Preserve discovery, custom aliases/models, and disabled-state protection | Preserve | 51, 188, 190, 192 | Discovery reconciliation tests proving no admin state deletion |
| Test All | Original bounded-concurrency workflow with progress/status/cooldown | Preserve | 193 | Concurrency, progress, cancellation, and transition tests |
| Provider nodes | Original node management | Preserve | 191 | CRUD, validation, routing, and failover tests |
| Proxy pools | Original assignment/rotation/test/cooldown/fallback | Preserve | 125, 175, 327 | Deterministic pool assignment and failure tests |
| Free Tier dashboard | Preserve only upstream-supported Free Tier accounts/workflows | Preserve | 167 | Source-mapped page/API/provider tests; no invented web-cookie category |

## 5. OAuth and token refresh

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Provider-specific OAuth families | Random/fixed loopback, device code, dashboard relay, cookie/PAT, browser-token, IDE import, and other original flows | Preserve | 20, 23, 106, 342 | End-to-end tests per flow family and provider redirect contract |
| Callback ports and redirect URIs | Preserve provider-required fixed/random values | Preserve | 106, 345 | Real-provider and local callback tests |
| OAuth pending state | Process-local behavior replaced by PostgreSQL TTL state for concurrency/status | Change/harden | 17, 19, 343, 346 | Concurrent-session, TTL, CSRF, PKCE, denial, expiry tests |
| Pending flow after restart | Always mark cancelled/failed; do not resume listener/browser flow | Change/harden | 346 | Restart recovery and user-visible status test |
| Token persistence | Provider-specific credentials/metadata retained in provider connection | Preserve implementation-independently | 120, 316, 322, 344 | Round-trip fixtures for every credential shape |
| Provider credentials at rest | Restricted plaintext in PostgreSQL for unattended restart | Change: approved policy | 27, 120, 322 | Permissions, redaction, backup sensitivity, restart tests |
| Proactive refresh | Five minutes before reliable expiry | Preserve/default | 88, 96 | Fake-clock scheduling tests |
| No-expiry refresh | Six-hour refresh default | Change: approved default | 89, 96 | Fake-clock provider-policy tests |
| Refresh concurrency | Global maximum four and per-account singleflight | Change/harden | 90, 91, 348, 349 | Concurrency and dedup tests |
| Request-path wait | Wait 10s; fallback to alternate account if available | Change/harden | 92 | Timing and same-model fallback tests |
| No alternate account | Continue waiting until refresh/provider timeout | Change: approved behavior | 93 | Long-wait cancellation and timeout tests |
| Refresh attempts | 30s per attempt, max three, backoff+jitter | Change/harden | 94–96 | Fake-clock deterministic retry tests |
| Reactive 401/403 | Refresh, persist atomically, bounded retry, then cooldown/fallback | Preserve/harden | 336, 348, 352 | Provider-specific reactive refresh tests |
| Scheduler/manual refresh | Shared coordinator; Run now available through dashboard/Admin API | Change: approved control | 87, 348 | Locking, audit, progress, and no-overlap tests |

## 6. Model resolution, routing, and combos

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Original model grammar | `provider/model`, slashless aliases, combos, built-ins, inferred routes, original resolution order | Preserve | 31, 98, 324 | Collision/order fixture matrix |
| New model grammar | `namespace/model(variant)` extension | Change: approved addition | 31, 324 | Parser and compatibility tests proving original grammar unaffected |
| Single-model fallback | Never silently switch models | Preserve | 98, 329 | Failure tests proving only same-model account fallback |
| Sequential combo | Original order and fallback eligibility | Preserve | 104, 330 | Deterministic attempt order, delay, result tests |
| Round-robin combo | Original sticky rotation semantics | Preserve | 104, 331 | Sequence and restart-state tests |
| Capability auto-switch | Only models within combo; retain fallbacks | Preserve | 104, 332 | Capability/media boundary fixtures |
| Fusion | Original panels, tool mutation, quorum, grace, timeout, judge | Preserve | 104, 333 | Parallel timing, cancellation, degradation, and synthesis tests |
| Fusion degradation | 0→error, 1→direct, multiple→judge | Preserve | 104, 334 | Deterministic panel-result fixtures |
| Routing hot state | In-memory cooldown/model lock/rotation with important PG checkpoints | Change/harden | 109, 115, 326 | Restart-recovery and 7-day transition cleanup tests |
| Retry policy | Provider/error-aware rules, not universal fixed retry | Preserve | 335, 339 | Error classification and delay fixtures per provider family |
| Tie-break | Manual priority/original order | Preserve | 122, 325, 340 | Stable deterministic order tests |
| Routing client error | Sanitized structured error with correlation ID | Change/harden | 145, 313, 338 | Secret-redaction and status/type/code/retryability tests |

## 7. Formats, translation, and streaming

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Original client formats | OpenAI Chat/Responses, Claude, Gemini/Gemini CLI, Codex, and original native formats | Preserve | 21, 32, 105, 312 | Formal schemas plus golden request/response/stream fixtures |
| Direct translators | Preserve exact paths where pivot is lossy | Preserve | 105, 312 | Golden thinking/tool/image/error/terminal tests |
| Native passthrough | Preserve for protocol-native paths | Preserve | 105, 312 | Byte-equivalence and header/status tests |
| Safe typed IR | Allowed only on non-lossy paths | Change: approved internal improvement | 105, 312 | Round-trip proof and explicit bypass tests |
| SSE terminal behavior | Protocol-specific; `[DONE]` not universal | Preserve | 32, 105, 312 | Byte/order fixtures for success/error/cancel |
| Forced stream-to-JSON | Separate protocol-aware reconstruction paths | Preserve | 32, 105, 312 | Tool/reasoning/nonstream golden tests |
| Raw-byte liveness | Stall timer resets on upstream byte activity | Preserve | 116, 312 | Output-silent byte-flow tests |
| Stall timeout | Six minutes default, audited provider overrides only | Preserve/default | 116 | Fake-clock timeout and override tests |
| First-chunk timeout | 200 seconds default, audited provider overrides only | Preserve/default | 118 | Fake-clock first-byte tests |
| Client disconnect | 500ms grace then full context cancellation | Preserve | 113, 314 | Disconnect, cleanup, no-leak, no-duplicate-terminal tests |
| Specialized binary/NDJSON/EventStream | Stay in dedicated adapters; no forced universal representation | Preserve | 23, 105, 309, 312 | Native protocol golden fixtures and fuzz tests |

## 8. Persistence, usage, and operational data

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Primary database | SQLite replaced with PostgreSQL 16–18 | Change | 2, 29, 40, 150, 209–212 | Migration/schema/repository behavior tests across PG 16/17/18 |
| Repository semantics | Domain-specific repositories and use-case transactions | Preserve semantics/change implementation | 99, 310, 315 | Transaction rollback, reorder, dedup, singleton, KV-scope tests |
| Schema migration | Embedded ordered forward migrations, expand-first rollback window | Change/harden | 152, 153, 317 | Upgrade/downgrade-compatibility/safe-mode tests |
| Original user-state migration | No migration; users reconfigure cleanly | Remove | 26 | Installer/docs make no silent import and preserve original installation |
| Manual config import/export | Preserve original partial export and transactional destructive replacement import, including exclusions and confirmations | Preserve | 378 | Versioned payload fixtures, included/excluded-domain tests, destructive confirmation, transaction rollback, and backup separation |
| Usage ingestion | Async durable PG-backed events; accepted event survives restart | Change/harden | 124, 318 | Crash/restart/idempotency/no-loss tests |
| Usage aggregation/pricing | Preserve original counters/dedup/tokens/caching/cost semantics | Preserve | 119, 124, 293–303 | Golden accounting fixtures across formats and failures |
| Unknown pricing | Cost zero and UI marks unconfigured | Change: approved | 148 | Pricing and UI tests |
| Historical pricing | Never recalculate history after price changes | Change: approved | 149 | Historical immutability tests |
| Recent Requests buffer | 50 in-memory entries; Overview max 20 after upstream filtering/dedup/sort | Preserve exactly | 299–304 | Realtime feed ordering/cap/filter/dedup/SSE tests |
| Request details | Rich metadata; bodies only explicit debug opt-in | Preserve/change privacy default | 50, 107, 297, 319 | Redaction, truncation, debug opt-in, retention tests |
| Basic chat history | Browser-local like upstream | Preserve | 158 | localStorage/session recovery tests |
| Application logs | JSON stdout plus PG dashboard logs retained 90 days | Change/extend | 13, 146, 321 | Structured schema, redaction, retention, SSE tests |
| Audit trail | Append-only forever, UI-immutable, exportable | Change: approved addition | 59, 147 | Tamper resistance, actor/origin, sanitized diff, export tests |
| Data retention | Usage/request details/logs 90 days; routing transition history 7 days | Change | 49, 115, 321 | Fake-clock cleanup/idempotency tests |
| Second process | Reject same-DB runtime with PG advisory lock | Change/harden | 79, 154, 323 | Concurrent startup tests |

## 9. Backup, restore, and recovery

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Scheduled backup | Automatic PG backup, 30 daily validated backups | Change | 48, 203, 320 | Scheduled backup, retention, integrity, status tests |
| Restore verification | Backup valid only after successful restore verification | Change/harden | 48, 85, 203, 320 | Isolated restore and semantic verification tests |
| Server PITR | PostgreSQL WAL/PITR | Change: approved | 48, 203 | Recovery point/objective tests |
| Desktop backup | Managed PostgreSQL snapshots | Change: approved | 48, 150–153, 203 | Cross-platform backup/restore tests |
| Backup encryption | No gorouter-level encryption | Change: approved policy | 48, 201, 204 | Access-control and sensitive-data documentation tests |
| Backup download | Dashboard may download scheduler-generated verified backup | Change: approved addition | 201 | Authz, audit, integrity, streaming-download tests |
| Restore control | Local CLI only; remote dashboard/PAT cannot restore | Change/harden | 202 | Locality and denial tests |
| Startup migration failure | Enter recovery/safe mode; never auto-reset | Change/harden | 151–153 | Failure injection and recovery UI/CLI tests |

## 10. Dashboard and frontend

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Product workflows | Preserve original Endpoint, Providers, Combos, Usage, Quota, Token Saver, CLI Tools, Media, Proxy Pools, Skills, Console, Remote, Settings | Preserve | 20, 55, 97, 276–278, 302–305 | Browser workflow parity suite per original page |
| Visual language | OpenRouter shell/component language with gorouter branding/content | Change: approved redesign | 258–278 | Light/dark screenshot and interaction acceptance tests |
| Sidebar order | Original 9Router order | Preserve | 302 | Desktop/mobile navigation tests |
| Endpoint page | Original fields/actions with OpenRouter visual shell | Preserve content/change presentation | 277, 278 | API key, Local/Tunnel/Tailscale, warning, toggle workflow tests |
| Usage page | Original Overview/Details metrics/topology/recent/charts/tables | Preserve | 287–304 | Browser data, filters, realtime, topology, expansion tests |
| Recent Requests UX | Newest-first sticky table, no forced scroll/pause/follow | Preserve exactly | 299–304 | DOM/scroll/SSE tests |
| Responsive behavior | Drawer sidebar, controlled table horizontal scrolling, stacked analytics | Change/harden | 267, 281, 290, 291 | Breakpoint, keyboard, touch, overflow tests |
| Accessibility | WCAG 2.2 AA release gate | Change/harden | 285 | Automated plus manual keyboard/focus/contrast checks |
| Theme | Equal light/dark/system, per browser | Change/design | 262–265, 195 | Theme persistence and contrast tests |
| Localization | English and Indonesian remain primary with English fallback; every original locale remains a parity target | Preserve/extend | 194, 377 | Complete original-locale coverage, primary-locale, and fallback tests |
| Realtime UI | Domain SSE, quiet updates preserving user context | Preserve/change implementation | 292, 359 | Reconnect, snapshot recovery, freshness, scroll/filter tests |
| Forms and details | Dedicated pages for complex flows; summary/sections/tabs | Change/design | 282, 283 | Workflow and responsive tests |
| Feedback states | Contextual skeleton/empty/error, inline-first | Change/design | 272, 284 | Loading/error/retry and retained-data tests |
| Motion | Minimal functional motion with reduced-motion support | Change/design | 286 | Reduced-motion and no-content-gating tests |
| Original dashboard scope | No broad new enterprise product beyond explicitly approved additions | Preserve scope guard | 305 | Ledger and page inventory review |

## 11. CLI, desktop, and host integration

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| One executable | `gorouter` binary with server/CLI/tray/updater/helpers modes | Change | 25, 43 | Cross-platform command contract tests |
| Installer UX | All official methods equal; npm remains `npm install -g gorouter` then `gorouter` | Change | 217–219 | Installer matrix and first-run tests |
| npm package | Thin launcher downloads signed native binary | Change | 218, 220, 221, 234 | OS/arch/checksum/signature/atomic-replacement tests |
| Lifecycle CLI | start/stop/restart/status/logs, JSON and stable exit codes | Change/extend | 235, 237, 239–242 | Command golden tests across platforms |
| Bare `gorouter` | Start if needed, wait healthy, open dashboard; running instance only opens dashboard | Change | 223 | Lifecycle and browser-open tests |
| Background service | Created at first run; closing terminal leaves service running | Change | 224, 229 | Service lifecycle tests |
| Autostart | Server/service and Docker deployments only; npm/desktop do not autostart | Change | 178, 242 | Reboot/startup matrix tests |
| Tray | Runtime/open/update/recovery controls; explicit Exit drains | Preserve/change implementation | 157, 179, 182, 183 | Windows/macOS/Linux tray tests where supported |
| Desktop autostart toggle | Removed | Remove | 178 | UI/CLI do not expose toggle |
| Installer shortcuts | No Desktop/Start Menu shortcuts | Change | 181 | Installer acceptance tests |
| Port conflict | Stable configured port; fail safely, never randomize/kill process | Change | 185, 248 | Occupied-port diagnosis tests |
| Graceful stop/update | Wait indefinitely for streams; local force option | Change | 131–135, 240, 250 | Long-stream drain and force-path tests |
| MITM | Full upstream parity, opt-in, least-privilege helper | Preserve/harden | 33, 160, 215 | Cert lifecycle/trust/security/platform tests |
| Tunnels/Tailscale/public endpoint | Full upstream parity, opt-in | Preserve | 33, 44–46, 161 | Lifecycle/recovery/domain/TLS tests |
| Headroom/Pxpipe/MCP/mutators | Full upstream behavior and controls | Preserve | 33, 108, 159, 162, 174, 364 | Per-feature parity and permissions tests |
| Proxy deployment helpers | Cloudflare/Deno/Vercel helpers retained | Preserve | 175 | Deployment/status/recovery fixtures |
| External helper binaries | Managed automatically only where upstream does | Preserve | 164 | Download/verify/lifecycle/platform tests |
| Docker-incompatible features | Explicitly disabled | Change: approved platform constraint | 216 | Capability report and denial tests |

## 12. Configuration, installation, updates, and packaging

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Config precedence | flags > env > `.env` > DB/dashboard > defaults | Change: approved | 18, 142 | Precedence and UI-override visibility tests |
| Original config aliases | Accept original env names/flags/paths/ports/commands as compatibility aliases | Preserve/compatibility | 41 | Alias contract tests |
| Product name | Official name `gorouter`; no `9router` binary shim | Change | 42, 43 | Package/binary/docs tests |
| One listener | Dashboard/Admin/health/model routes on one configured port | Change/approved shape | 228, 306 | Route and listener tests |
| Bind default | `127.0.0.1`; LAN/public opt-in | Change/harden | 44 | Listener configuration and remote access tests |
| Domain/TLS | Optional wizard; managed ACME for desktop/single, external proxy for advanced one-server deployment | Change | 44–46, 187 | ACME, external config generation, DNS/health tests |
| Public exposure | API and dashboard directly reachable when enabled | Change | 46, 232 | HTTPS/auth/CSRF/lockout tests |
| Update channels | beta/stable only | Change | 54 | Channel and promotion-gate tests |
| Self-update | Opt-in, signed, rollback-compatible | Change | 36, 244, 249–251 | Signature, drain, swap, health, rollback tests |
| External server update | Service manager/deployment tooling executes activation | Change | 133 | Prepared-artifact and external activation tests |
| Windows | Installer + portable ZIP, managed portable PG | Change | 205, 208, 213 | Fresh/reinstall/move/recovery tests |
| macOS | Signed/notarized app + DMG/PKG, bundled verified PG | Change | 206, 212 | Notarization/install/update/recovery tests |
| Linux | DEB/RPM/Docker/tarball; package PG or external according to format | Change | 207, 209–214 | Distro/container/tarball matrix tests |
| Uninstall | Remove app/service/runtime, preserve data/config/backups; explicit purge deletes all | Change | 233, 236, 243 | Uninstall/reinstall/purge tests |

## 13. Scheduler and runtime background jobs

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Central scheduler | Internal scheduler goroutines own mechanics; domains own job logic | Change/harden | 81–87, 366 | Fake-clock lifecycle and registration tests |
| Job schedules | Fixed in source; no admin/env override | Change | 83 | Configuration absence and documented schedule tests |
| Run now | Dashboard/Admin API for every job | Change: approved addition | 87 | Authz, local lock, status, audit tests |
| Job overlap | Per-job local lock | Change/harden | 367 | Timer/manual concurrency tests |
| Restart recovery | PG lease/checkpoint; idempotent resume/rerun | Change/harden | 82 | Crash/restart/duplicate-side-effect tests |
| Job history | Current/last + short history only | Change | 368 | Retention/status/SSE tests |
| Provider jobs | Refresh/quota/revalidation/health with provider-aware cooldown | Preserve/harden | 84, 88–96 | Scheduler and routing-state tests |
| Backup jobs | Retry/backoff and critical dashboard warning | Change/harden | 85 | Failure/recovery/alert tests |
| Retention jobs | Retry/backoff, warning, routing continues | Change/harden | 86 | Failure injection and recovery tests |
| Upstream monitor | Daily grouped issue/diff report, manual porting | Change: approved addition | 52, 112, 140 | Deterministic diff classification tests |

## 14. Observability and auditing

| Capability | Upstream contract | Disposition | Decision refs | Required observable evidence |
| --- | --- | --- | --- | --- |
| Structured logs | JSON stdout plus PG dashboard log storage | Change/extend | 13, 146 | Schema, redaction, correlation, retention tests |
| Prometheus | `/metrics` endpoint | Change: approved addition | 13 | Metric names/cardinality/security tests |
| Console Log | Original live view only, no dashboard download | Preserve | 169 | SSE/reconnect/filter tests |
| Usage/cost/quota | Full upstream dashboard parity | Preserve | 168, 276, 287–304 | Browser/API/accounting parity suite |
| Provider health | Original status/test/cooldown plus approved minimal refresh detail | Preserve/extend | 341, 353, 193 | State and realtime tests |
| Scheduler status | Current/last/short history and Run now | Change: approved | 87, 368 | API/UI/SSE tests |
| Active streams | Dashboard visibility approved | Change: approved addition | 58, 182 | Live count and drain consistency tests |
| Audit log | Append-only, all sensitive/config actions, forever | Change: approved addition | 59, 147 | Immutability/export/redaction/actor tests |
| External alerts | None; dashboard/tray/log only | Change | 197, 198 | No email/webhook/desktop notification behavior |

## 15. Testing, performance, security, and release gates

| Gate | Disposition | Decision refs | Required evidence |
| --- | --- | --- | --- |
| Module source mapping and parity DoD | Required | 99–103, 254–257, 372 | Workflow mapping, source citations, contracts, tests, docs, review |
| Formal API contracts and golden fixtures | Required | 307, 362, 371 | Versioned schemas and source-backed fixtures |
| Original known-failure awareness | Preserve baseline concept, not original failures | 22, 254–257 | No unknown parity regressions; intentional differences documented |
| Provider live matrix | Required before beta and stable | 23, 165, 166 | Real secret-store credentials; every provider/executor validated |
| Performance | p95 router overhead <20ms; ≥2,000 streams; ≥100,000 RPM lightweight endpoints on 4 vCPU/16GB | Required | 37 | Reproducible benchmark profiles and resource evidence |
| Reliability | 72h mixed-load soak, no crashes/deadlocks/leaks/lost state/duplicates | Required | 38 | Soak logs, race/leak/state assertions, restart/failover scenarios applicable to one process |
| Security automation | SAST, dependencies/license/SBOM/secrets/fuzz/race/authz/SSRF/trusted-proxy/signed artifacts/provenance | Required | 39 | CI evidence and signed release artifacts |
| Accessibility | WCAG 2.2 AA | Required | 285 | Automated and manual evidence |
| Cross-platform packaging | Windows/macOS/Linux/Docker/npm matrices | Required | 24, 205–216, 217–221 | Fresh install/update/reinstall/recovery acceptance suites |
| Beta/stable promotion | Stable only after every gate passes | Required | 54 | Immutable release evidence bundle |

## 16. Explicit removals and intentional changes

The following are not accidental omissions. They are user-approved differences from original behavior or earlier tentative plans:

| Item | Final disposition | Decision refs |
| --- | --- | --- |
| Cloud/sync product feature | Remove | 156 |
| Active-active/distributed multi-instance mode | Remove from v1; one process only | 79, 80, 323 |
| Trusted-loopback model API bypass | Remove; API key required everywhere | 72 |
| Desktop OS-login autostart and toggle | Remove | 178, 242 |
| Dashboard log download | Remove | 169 |
| Email/webhook/Discord alerts | Remove | 197 |
| Native desktop notifications | Remove | 198 |
| App-level backup encryption | Remove | 204 |
| Automatic legacy user-state migration | Remove | 26 |
| Arbitrary SSRF restriction for custom provider URLs | Remove protection by explicit policy | 136, 137 |
| Extra runtime secret unlock/passphrase/rotation layer | Remove | 120 |
| Plugin SDK for nonstandard providers | Remove | 51 |
| Docker-only incompatible host/desktop features | Remove in Docker mode | 216 |

## 17. Resolved parity exceptions

The final disposition of every previously open conflict is now explicit:

1. **OIDC is preserved** alongside local-password login under decision #375.
2. **Custom provider URLs remain unrestricted** by SSRF or DNS-rebinding guards under decision #376. This is a documented high-risk policy, not an accidental omission.
3. **Every original locale is preserved** under decision #377; English and Indonesian remain primary and English remains fallback.
4. **Original manual configuration export/import is preserved** under decision #378, including its partial payload and destructive replacement semantics with explicit confirmation and transactional safeguards.
5. **Original cloud/sync is removed** under decision #379 as a deliberate parity exception.

## 18. Approval record

Approval must be explicit and apply to this entire file as one package.

- Ledger status: **Approved as one complete package**
- Approved by: User
- Approval date: 2026-07-29
- Approved against upstream commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Approved decisions through: `#380`
- Approval authorizes implementation planning. Coding still requires a written and reviewed implementation plan.
