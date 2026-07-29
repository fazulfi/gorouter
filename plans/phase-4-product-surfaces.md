# Phase 4 Product Surfaces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Deliver complete original 9Router management capability through the versioned Admin API, compatibility management routes, OpenRouter-inspired React dashboard, CLI, scheduler, and approved host integrations.

**Architecture:** Dashboard, Admin API, compatibility routes, local CLI, and internal jobs call the same application services. The frontend is domain-organized and contract-generated; realtime status uses domain SSE; host operations are explicit adapters with policy and audit boundaries.

**Tech Stack:** Go/chi/pgx, OpenAPI-generated TypeScript client, React/Vite/shadcn/ui/Tailwind, Playwright, domain SSE, platform build tags/adapters, scheduler goroutines.

## Global Constraints

- Engine Parity Gate must be approved.
- Preserve original dashboard/CLI/management workflows unless the parity ledger explicitly changes/removes them.
- UI visual language follows approved OpenRouter reference; content/semantics remain original 9Router.
- All original locales are preserved; English and Indonesian are primary docs/user-facing targets with English fallback where required.
- Admin API is `/api/admin/v1`; compatibility management routes are thin mapping adapters.
- Dashboard uses cookie session; CLI local mode calls application use cases; remote automation uses PAT.
- High-risk host features remain opt-in and policy-restricted; full-access PAT may invoke them remotely when enabled.
- Cloud/sync is removed; do not recreate it.
- Manual config export/import remains original partial/destructive behavior, not a new full backup transfer.
- Recent Requests behavior is exact: ring 50, display max 20 after filter/dedup/sort, newest-first SSE, sticky header, no forced scroll/pause/follow.

---

## File and responsibility map

| Path | Responsibility |
| --- | --- |
| `api/admin-v1.openapi.yaml` | Complete versioned Admin API contract |
| `internal/transport/httpserver/adminapi/v1` | Contract handlers only |
| `internal/transport/httpserver/compatibility` | Original management mapping adapters |
| `internal/transport/httpserver/realtime` | Usage/console/provider/job SSE |
| `internal/app/*` | Product workflows shared by UI/API/CLI/jobs |
| `internal/scheduler` | Job scheduling/run-now/status/retry/locking |
| `internal/host/*` | Service/tray/update/tunnel/MITM/Headroom/Pxpipe/MCP adapters |
| `internal/transport/cli` | Local lifecycle and parity command workflows |
| `frontend/src/features/*` | Original sidebar domains in approved visual system |
| `frontend/src/generated` | OpenAPI-generated client/types |
| `frontend/e2e` | Workflow/accessibility/responsive tests |

### Task P4-T01: Complete Admin API contract and actor enforcement

**Files:**
- Complete: `api/admin-v1.openapi.yaml`
- Create: `internal/transport/httpserver/adminapi/v1/*`
- Complete: generated frontend client
- Test: contract/authz/audit tests

**Interfaces:**
- Covers providers, accounts, nodes, proxy pools, keys, combos, aliases, disabled models, pricing, usage, quota, mutators, CLI tools, media, skills, console, remote, settings, OAuth, jobs, host operations, backup download/status.

- [ ] Write RED contract tests for every approved management capability and actor/session/PAT/local-job authorization matrix.
- [ ] Implement handlers as mapping-only calls to application services.
- [ ] Generate TypeScript client and verify clean diff.
- [ ] Run GREEN and audit-event assertions.
- [ ] Commit by domain contract group.

### Task P4-T02: Implement compatibility management adapters

**Files:**
- Create one adapter per management domain under `internal/transport/httpserver/compatibility`, using the concrete domain IDs in `PARITY-LEDGER.md`.
- Test: `tests/parity/management_routes_test.go`

- [ ] Write RED tests comparing original path/method/body/status/error contracts to Admin API-backed use cases.
- [ ] Implement thin adapters with no business logic.
- [ ] Run GREEN and prove both routes produce equivalent state transitions.
- [ ] Commit: `feat(api): preserve management compatibility routes`

### Task P4-T03: Complete dashboard authentication and profile shell

**Files:**
- Create/complete auth HTTP handlers and frontend `features/auth`, app shell, profile dropdown, theme control.
- Test: Go auth integration, React unit, Playwright login/session/CSRF/lockout/recovery.

- [ ] Write RED workflows for initial password warning, local login, optional OIDC, sliding session, logout, password reset revoking sessions, CSRF, mobile drawer, profile/theme menu.
- [ ] Implement application/transport/UI paths.
- [ ] Run GREEN, WCAG checks, light/dark/mobile screenshots.
- [ ] Commit: `feat(ui): add dashboard authentication shell`

### Task P4-T04: Implement Endpoint and Key workflows

**Files:**
- Create: `frontend/src/features/endpoint/*`
- Complete: app/API/CLI key and endpoint services
- Test: Go/React/Playwright parity tests

**Interfaces:**
- Preserves endpoint display, Local/Tunnel/Tailscale controls, warnings, API keys/create/masked rows/dates/toggles, auth transports, PAT distinction, stable port diagnostics.

- [ ] Write RED tests from original Endpoint screenshot/source workflows.
- [ ] Implement OpenRouter visual shell with original semantics.
- [ ] Run GREEN including no-key loopback rejection and copy-safe secret handling.
- [ ] Commit: `feat(ui): port endpoint and key workflows`

### Task P4-T05: Implement Providers, account lifecycle, nodes, and proxy pools UI/API/CLI

**Files:**
- Create: `frontend/src/features/{providers,proxypools}/*`
- Complete: app/transport/CLI provider services
- Test: workflow/provider status/realtime tests

- [ ] Write RED end-to-end workflows for add/edit/delete/reorder/test/import/bulk/cooldown/models/nodes/proxy pools/free tier/Test All.
- [ ] Implement dedicated full-page complex forms and compact tables/status actions.
- [ ] Wire quiet SSE refresh preserving scroll/filter.
- [ ] Run GREEN and accessibility/responsive tests.
- [ ] Commit per coherent provider workflow.

### Task P4-T06: Implement Combos, aliases, custom/disabled models, and pricing surfaces

**Files:**
- Create: `frontend/src/features/{combos,settings/pricing}/*`
- Complete corresponding API/CLI services
- Test: parity and UI workflows

- [ ] Write RED tests for every combo mode, ordering, judge config, alias/custom/disabled reconciliation, built-in pricing plus audited overrides.
- [ ] Implement workflow pages/tables/forms.
- [ ] Run GREEN and verify audit before/after records.
- [ ] Commit: `feat(ui): port model configuration workflows`

### Task P4-T07: Implement Usage Overview, Details, Explore, and Recent Requests

**Files:**
- Create: `frontend/src/features/usage/*`
- Create: `internal/transport/httpserver/realtime/usage.go`
- Complete usage query/application services
- Test: source-backed behavioral and visual tests

**Interfaces:**
- Overview/Details/time presets/metrics/topology/Recent Requests/charts/Tokens-Cost/breakdowns/expandable rows; filters persist across views.
- Recent Requests exact contract from audit 13.

- [ ] Write RED tests for 50 buffer, max 20 display, filter/dedup/sort, SSE full/light updates, 25s keepalive, no auto-scroll, sticky header.
- [ ] Write RED chart/topology/filter/expandable-row UI tests.
- [ ] Implement backend queries/SSE and UI with zoom/pan/reset/inspect topology.
- [ ] Run GREEN, performance, accessibility, responsive tests.
- [ ] Commit: `feat(usage): port operational analytics`

### Task P4-T08: Implement Quota Tracker and provider health/cooldown views

**Files:**
- Create: `frontend/src/features/quota/*`
- Complete quota application/jobs/realtime APIs
- Test: scheduler/provider/UI tests

- [ ] Write RED tests for original quota workflows, cooldown countdown/reset/unlock, quiet refresh, temporary/definitive errors.
- [ ] Implement services/UI.
- [ ] Run GREEN.
- [ ] Commit: `feat(ui): port quota tracker`

### Task P4-T09: Implement Token Saver, Skills, Translator, and mutator controls

**Files:**
- Create feature modules for token saver, skills, translator.
- Complete APIs/CLI/application wiring.
- Test: parity/UI/mutator-order tests

- [ ] Write RED workflows preserving separate original pages and controls.
- [ ] Implement UI and use-case integration without changing engine order/fail-open semantics.
- [ ] Run GREEN.
- [ ] Commit: `feat(ui): port request transformation tools`

### Task P4-T10: Implement Media Providers and modality workflows

**Files:**
- Create: `frontend/src/features/media/*`
- Complete Admin/API/CLI media services
- Test: workflow/provider tests

- [ ] Write RED tests only for workflows actually present upstream; do not invent new playgrounds.
- [ ] Implement pages/details/voices/config/combo/web workflows.
- [ ] Run GREEN and media accessibility/responsive tests.
- [ ] Commit: `feat(ui): port media provider workflows`

### Task P4-T11: Implement Console Log and operational realtime views

**Files:**
- Create: `frontend/src/features/console/*`
- Create: realtime console endpoint and PG log projection
- Test: retention/realtime/redaction tests

- [ ] Write RED tests for original live view/auto-scroll behavior, no dashboard download, 90-day retention, secret redaction, reconnect.
- [ ] Implement stdout+PG projection and SSE UI.
- [ ] Run GREEN under sustained log flow.
- [ ] Commit: `feat(console): add live operational log view`

### Task P4-T12: Implement Remote, tunnels, domain/TLS, and deployment helpers

**Files:**
- Create: `frontend/src/features/remote/*`
- Create/complete host adapters for Tailscale/tunnels/domain/ACME/external proxy configs/Cloudflare/Deno/Vercel helpers
- Test: fake host/platform/integration/UI tests

- [ ] Write RED tests from upstream workflows plus approved domain/TLS behavior.
- [ ] Implement opt-in permission-aware adapters and dashboard/API/CLI controls.
- [ ] Run GREEN; audit every sensitive action; verify disabled policy denies.
- [ ] Commit per host/deployment helper family.

### Task P4-T13: Implement CLI Tools and local CLI parity

**Files:**
- Complete: `internal/transport/cli/*`
- Create: `frontend/src/features/clitools/*`
- Test: CLI golden/integration/UI tests

**Interfaces:**
- Detection/install/config/status/model/endpoint setup/dashboard/terminal menu, lifecycle `start/stop/restart/status/logs`, JSON output and stable exit codes.

- [ ] Write RED CLI golden tests from upstream commands/menus plus approved gorouter lifecycle.
- [ ] Implement local in-process use cases and remote PAT mode where applicable.
- [ ] Run GREEN on Windows/macOS/Linux test environments.
- [ ] Commit: `feat(cli): port management and lifecycle workflows`

### Task P4-T14: Implement scheduler and background-job observability

**Files:**
- Create: `internal/scheduler/{scheduler,runner,state}.go`
- Create: `internal/app/jobs/*`
- Create: `frontend/src/features/settings/jobs/*`
- Test: fake-clock/restart/idempotency/UI tests

**Interfaces:**
- Fixed source schedules; local lock prevents overlap; PG lease/checkpoint where needed; retries/backoff; Run now via dashboard/Admin API/PAT; current/last/short history.

- [ ] Write RED tests for token refresh, quota, revalidation, provider health, retention, backup verification, update check, OAuth cleanup, upstream monitor.
- [ ] Implement scheduler lifecycle and domain-owned runners.
- [ ] Run GREEN, race, restart, duplicate-side-effect tests.
- [ ] Commit: `feat(jobs): add internal scheduler and status`

### Task P4-T15: Implement Settings, audit activity, backup download, and manual config transfer

**Files:**
- Create: `frontend/src/features/settings/*`
- Complete settings/audit/backup/config import-export services/API/CLI
- Test: security/parity/UI tests

**Interfaces:**
- Config precedence visibility, hot/restart-required changes, append-only audit forever, scheduler-generated backup download, local-CLI-only restore, original partial/destructive manual config export/import.

- [ ] Write RED tests distinguishing backup from config transfer and proving no usage/log/session/audit export in manual config archive.
- [ ] Implement use cases/UI with strong confirmations and audit.
- [ ] Run GREEN and secret-handling tests.
- [ ] Commit: `feat(settings): port configuration and recovery workflows`

### Task P4-T16: Implement MITM, MCP, Headroom, Pxpipe, and process-host controls

**Files:**
- Complete: `internal/host/{mitm,mcp,headroom,pxpipe,process}/*`
- Create matching dashboard features/API/CLI
- Test: platform adapter/security/permission/parity tests

- [ ] Write RED tests for cert lifecycle/trust install-remove, child/session management, start/stop/proxy controls, recovery, permission prompts, audit.
- [ ] Implement small least-privilege helpers where approved; main service never runs as root.
- [ ] Run GREEN on supported platforms; Docker denial tests.
- [ ] Commit per host feature.

### Task P4-T17: Embed dashboard and prove cross-surface consistency

**Files:**
- Create: `embed.go`
- Complete bootstrap/dashboard serving
- Create: `tests/integration/productsurfaces/*`
- Update traceability/gate evidence

- [ ] Write RED tests proving dashboard/Admin/compatibility/CLI invoke equivalent application state transitions.
- [ ] Build frontend and embed assets reproducibly.
- [ ] Run integrated workflows across all original sidebar domains.
- [ ] Commit: `test: prove product surface parity`

## Mandatory Exit Gate: Product Parity Gate

- [ ] Admin API v1 contract covers all approved management capabilities; generated client is current.
- [ ] Original management routes pass compatibility tests through same application services.
- [ ] Dashboard auth/OIDC/session/CSRF/lockout/recovery/PAT/API-key workflows pass.
- [ ] All original sidebar domains and workflows are implemented except explicitly removed cloud/sync.
- [ ] Endpoint, Providers, Combos, Usage, Quota, Token Saver, CLI Tools, Media, Proxy Pools, Skills, Console, Remote, Settings pass source-backed end-to-end tests.
- [ ] Recent Requests exact behavior passes audit-13 tests.
- [ ] Scheduler jobs pass timing, retry, run-now, restart, idempotency, and status tests.
- [ ] CLI lifecycle/management parity and stable JSON/exit-code tests pass.
- [ ] Host integrations pass platform/permission/audit tests; Docker-disabled features deny safely.
- [ ] Dashboard light/dark/mobile, tables, analytics, forms, errors, empty/loading states pass WCAG 2.2 AA automated/manual gate.
- [ ] Every sensitive action produces append-only sanitized audit entry.
- [ ] Full frontend typecheck/unit/build/e2e suite passes.
- [ ] Cross-surface consistency tests prove one application layer.
- [ ] `phase-4.json` evidence and product/parity/accessibility/security reviews are approved before Phase 5.
