# 10 — Tests Baseline

## Scope

Complete inventory of test scripts and suites, known-failure baseline,
regression verification workflow, test configuration, and discovered
testing gaps at the audited commit.

## Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Exact commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Read-only clone: `upstream-original/`

## Method

- Full file inventory under `tests/` (156 `.test.js` files).
- Read test configuration (`vitest.config.js`, `package.json`).
- Read baseline and regression verification scripts.
- Read `CLAUDE.md` for documented test-execution workflow.
- **No tests were executed** during this audit (no build, install, or
  runtime).

## Confirmed Findings

### 1. Test Framework and Configuration

**File:** `tests/vitest.config.js`

- Framework: Vitest v4 (`^4.0.0` in `devDependencies`).
- Environment: `node` (not jsdom).
- Globals: enabled (`globals: true`).
- Include pattern: `**/*.test.js`.
- Excludes: `node_modules`, `.claude/`, `dist/`.
- Max concurrency: 60.
- Vitest is installed as a dependency **only** under `tests/` — it is
  not in the root `package.json`.

**File:** `tests/package.json`

```json
{
  "scripts": {
    "test": "NODE_PATH=/tmp/node_modules /tmp/node_modules/.bin/vitest run --reporter=verbose",
    "test:watch": "NODE_PATH=/tmp/node_modules /tmp/node_modules/.bin/vitest --reporter=verbose"
  }
}
```

**Critical:** The `package.json` test scripts hardcode Unix paths
(`/tmp/node_modules`) — a workaround for npm workspace hoisting from
the root Next.js project. This does **not** work on Windows or on any
system without vitest pre-installed at `/tmp/node_modules`.

**Recommended execution (from CLAUDE.md):**

```bash
cd tests
npm install             # installs vitest under tests/node_modules
npx vitest run          # auto-discovers vitest.config.js
npx vitest run unit/capabilities.test.js   # single file
```

The `vitest.config.js` correctly resolves `@/` → `src/` and
`open-sse/` → `../open-sse/` via alias configuration.

### 2. Test File Inventory

**Total:** 156 test files across three directories.

#### 2a. `tests/unit/` — 138 test files

Unit tests for individual modules. Includes:

| Category | Count | Example files |
| ---------- | ------- | -------------- |
| Executor retry/error | 3 | `base-executor-retry.test.js`, `executor-const-guard.test.js`, `finish-reason-concern.test.js` |
| Provider-specific | 35+ | `antigravity-*.test.js`, `codex-*.test.js`, `grok-*.test.js`, `kiro-*.test.js`, `gemini-*.test.js`, `xai-*.test.js`, `kimchi.test.js`, `venice-provider.test.js` |
| Translator | 8 | `openai-to-*.test.js`, `commandcode-to-openai.test.js`, `translator-request-normalization.test.js` |
| RTK token saver | 6 | `rtk.test.js`, `rtk.e2e.test.js`, `rtk.multi-provider.e2e.test.js`, `rtkFindWindows.test.js`, `rtkKiro.test.js` |
| Dashboard guard | 1 | `dashboard-guard.test.js` |
| Security audit | 1 | `security-audit.test.js` (AUDIT-001 through AUDIT-018) |
| DB layer | 5 | `db-driver-chain.test.js`, `db-migration-chain.test.js`, `db-concurrent.test.js`, `db-benchmark.test.js`, `db-sqlite-vs-lowdb.test.js` |
| Combo/fallback | 3 | `combo-autoswitch.test.js`, `combo-fusion.test.js`, `combo-routing.test.js` |
| Headroom | 4 | `headroom.test.js`, `headroom-chat-core.test.js`, `headroom-detect.test.js`, `headroom-responses-format.test.js` |
| Capabilities | 3 | `capabilities.test.js`, `capabilities-opus-context.test.js`, `capabilities-service-kind.test.js` |
| Session/usage | 5 | `session-manager.test.js`, `usage-concern.test.js`, `usage-dispatch.test.js`, `quota-auto-ping.test.js`, `token-refresh-dispatch.test.js` |
| Model routing | 4 | `model-routing.test.js`, `model-test-routing.test.js`, `model-name-regex.test.js`, `hf-model-routing.test.js` |
| Misc | 55+ | `pxpipe.test.js`, `qoder.test.js`, `mitm-root-ca.test.js`, `image-generation.test.js`, `prefetch-images.test.js`, etc. |

#### 2b. `tests/translator/` — 15 test files (excluding `real/`)

| File | Focus |
| ------ | ------- |
| `bugs-antigravity.test.js` | Antigravity-specific regression tests |
| `bugs-claudeCode-context.test.js` | Claude Code context window regressions |
| `bugs-codexCli-responses.test.js` | Codex CLI response regressions |
| `bugs-gemini-cursor-commandcode.test.js` | Gemini/Cursor/CommandCode cross-format |
| `bugs-kiro.test.js` | Kiro-specific regression tests |
| `bugs-openai-bridge.test.js` | OpenAI bridge regressions |
| `bugs-toClaude-context.test.js` | Outbound-to-Claude context regressions |
| `claude-kiro-direct.test.js` | Direct Claude↔Kiro translator |
| `coverage-all-models.test.js` | Model coverage matrix |
| `format-roundtrip.test.js` | Format round-trip tests |
| `golden-request.test.js` | Golden request snapshot |
| `golden-response-stream.test.js` | Golden response stream snapshot |
| `golden-translator-concerns.test.js` | Golden translator edge cases |
| `golden-url-header.test.js` | Golden URL/header combinations |
| `thinking-unified.test.js` | Unified thinking format |

Snapshots are stored under `tests/translator/__snapshots__/`.

#### 2c. `tests/translator/real/` — 8 test files (live provider calls)

**These require credentials and are skipped in CI.**

| File | Scope |
| ------ | ------- |
| `all-formats.real.test.js` | All formats live |
| `antigravity-models.real.test.js` | Antigravity models live |
| `file-base64-survey.real.test.js` | File/base64 survey |
| `nvidia-thinking.e2e.test.js` | NVIDIA thinking e2e |
| `provider-cases.real.test.js` | Provider-specific cases |
| `smoke-providers.real.test.js` | Provider smoke tests |
| `thinking.real.test.js` | Thinking format live |
| `vision-capability-survey.real.test.js` | Vision capability survey |

#### 2d. `tests/__baseline__/` — Baseline and regression tools

| File | Purpose |
| ------ | --------- |
| `known-fails.txt` | Explicit list of expected test failures (25 entries) |
| `verify-no-regression.mjs` | Gate script: fails if any current failure is NOT in known-fails |
| `verify-providers.mjs` | Provider registry snapshot comparison |
| `verify-alias.mjs` | Alias registry snapshot comparison |
| `verify-oauth-urls.mjs` | OAuth URL snapshot comparison |
| `snapshot-providers.mjs` | Generate provider snapshots |
| `providers-baseline.json` | Committed provider snapshot |
| `alias-baseline.json` | Committed alias snapshot |
| `oauth-urls-baseline.json` | Committed OAuth URL snapshot |
| `current.json` | Current run results |
| `baseline-results.json` | Historical baseline results |

### 3. Known-Failure Baseline

**File:** `tests/__baseline__/known-fails.txt` (25 entries)

#### 3a. Never-green tests by cause

| Cause | Count | Test files |
| ------- | ------- | ----------- |
| O/S dependent (cursor/oauth file paths, platform detection) | 8 | `oauth-cursor-auto-import.test.js` |
| RTK default-off toggle | 1 | `rtk.test.js` |
| RTK compress disabled state | 1 | `rtk.test.js` |
| RTK compression scenarios (10 variants) | 10 | `rtk.test.js` |
| Claude header forwarding to api.anthropic.com | 1 | `claude-header-forwarding.test.js` |
| Antigravity MITM agent/Default model | 1 | `antigravity-mitm.test.js` |
| OpenAI-to-Claude empty Read pages tool | 1 | `openai-to-claude.test.js` |
| Translator request normalization (4 variants) | 4 | `translator-request-normalization.test.js` |

#### 3b. Never-run tests (always skipped in CI)

| Cause | Count | Test files |
| ------- | ------- | ----------- |
| `*.real.test.js` (live provider calls) | 8 | Under `tests/translator/real/` |
| Missing `cloud/` worker directory | 1 | `unit/embeddings.cloud.test.js` |
| xAI endpoint fetch timeout | 1 | `unit/xai-oauth-service.test.js` |

**Total known non-green:** ~25 catalogued + ~10 always-skipped ≈ ~35
out of an estimated ~1002 tests (from CLAUDE.md: ~938 pass, ~64 fail).

### 4. Regression Verification Workflow

**File:** `tests/__baseline__/verify-no-regression.mjs`

The gate works by:

1. Reading a Vitest JSON results file (generated by `vitest run
   --reporter=json`).
2. Extracting all failed tests: `f.assertionResults.filter(a =>
   a.status === "failed")`.
3. Matching each failed test against the `known-fails.txt` set using the
   format `{filePath} :: {fullName}`.
4. If any failure is NOT in known-fails → exit code 1 (regression
   detected).
5. If all failures are known → exit code 0 (no regression).

**Usage:**

```bash
npx vitest run --reporter=json > current.json
node tests/__baseline__/verify-no-regression.mjs current.json
```

**Behavioral invariant:** The regression gate tolerates **any number**
of known failures but **zero** unknown failures. Adding a new test that
fails requires adding it to `known-fails.txt`.

### 5. Snapshot-Based Regression Tools

Three snapshot pairs exist for provider/alias/OAuth registry validation:

- `snapshot-providers.mjs` — generates `providers-baseline.json`.
- `verify-providers.mjs` — compares current registry against baseline.
- `verify-alias.mjs` — compares alias registry against baseline.
- `verify-oauth-urls.mjs` — compares OAuth URLs against baseline.

These are likely run manually (no CI config was found in the audited
tree beyond the `.github/` directory, which was not audited).

### 6. Security Audit Tests

**File:** `tests/unit/security-audit.test.js`

Audit regression tests (referenced as AUDIT-001 through AUDIT-018):

| Audit ID | Description | Source file tested |
| ---------- | ------------- | ------------------- |
| AUDIT-001 | Synchronous restart guard in MITM | `src/mitm/manager.js` |
| AUDIT-002 | API key masking in usage stats | `src/lib/db/repos/usageRepo.js` |
| AUDIT-003 | Proxy URL validation | `src/lib/network/outboundProxy.js` |
| AUDIT-004 | Atomic lock file for MITM startup | `src/mitm/manager.js` |
| AUDIT-018 | XSS escaping in OAuth callback | `src/lib/oauth/utils/server.js` |

These tests verify source-code properties (function presence, string
patterns, ordering) not runtime behavior.

### 7. Dashboard Guard Tests

**File:** `tests/unit/dashboard-guard.test.js` (279 lines)

Comprehensive mock-based tests covering:

- Public LLM API access from loopback (allows without key).
- Remote Host-spoof with real non-loopback IP (rejects).
- Loopback peer IP regardless of Host header (allows).
- Remote rewritten LLM API paths (rejects without key).
- Remote LLM API with valid Bearer, `x-api-key`, `x-goog-api-key`,
  and query-parameter key (allows).
- Local-only route rejection from remote Host.
- Local-only route on loopback with `requireLogin=true`, no JWT.
- Local-only route on loopback with `requireLogin=false`.
- Local-only route from tunnel host (rejects).
- Local-only route with non-loopback Origin (CSRF block).
- Local-only route with valid CLI token.
- API key extraction priority: Bearer > x-api-key > x-goog-api-key > ?key=.

**Confidence:** High. The test file covers the major gate paths with
good edge-case coverage.

## Uncertainties and Conflicting Evidence

1. **Total test count discrepancy:** CLAUDE.md states "~938 pass, ~64
   fail" but this inventory found 156 test files. The counts likely
   reflect individual `it()` assertions, not files. A Vitest JSON output
   would resolve this. This was not generated (no run).

2. **The `mimo-free.live.test.js` file** is in `tests/unit/` but its
   `.live.test.js` suffix suggests it requires live credentials. It was
   not inspected.

3. **No `.github/workflows/` CI configuration was audited** in this
   pass. The regression gate scripts exist but their invocation
   frequency is uncertain.

4. **The `tests/README.md`** focuses only on embeddings tests — it is
   stale for the full suite. The CLAUDE.md description is more current.

5. **`tests/unit/embeddings.cloud.test.js`** imports from
   `cloud/src/handlers/embeddings.js` which is not in this repository,
   so it always fails.

## Parity Requirements

- Preserve the regression gate workflow (`verify-no-regression.mjs`
  - `known-fails.txt`).
- Preserve the known-failures at the same granularity (individual
  assertion names with file-path prefix).
- Preserve snapshot-based verification for provider registry, alias
  registry, and OAuth URLs.
- Preserve mock-based testing for the dashboard guard (no live server
  needed).
- Test runner must support path alias resolution (`@/` → `src/`,
  `open-sse/` → `../open-sse/`).
- Security audit tests that verify source-code properties (function
  presence, ordering) must be mapped to equivalent Go checks.

## Coverage Gaps

- No test for the `always-protected` zone routes (`/api/shutdown`,
  `/api/settings/database`, `/api/version/shutdown`).
- No test for CLI token header (`x-9r-cli-token`) in the guard test
  (the mock provides the machine ID but the CLI token derivation path
  is not tested).
- No test for OAuth callback XSS prevention integrated with the handler
  (only static source analysis).
- No test for SSRF guard integration with executor fetch paths.
- No automated test for the update/spawn flow.
- No test for tunnel watchdog initialization, network monitor, or
  restart cooldowns.
- No test for console log capture buffer lifecycle.
- No test for the `configureQuotaAutoPing` settings-based enable/disable
  logic.
