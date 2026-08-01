# Provider Live Validation

Status: P3-T14 (automated provider matrix harness + live-validation preparation).

This document defines the gorouter provider test harness (`tests/providers/harness/`),
its per-provider manifests (`tests/providers/manifests/`), and the live-validation
procedure for the protected lab. Normal CI runs the harness in mock/golden mode;
real-provider validation runs only in the protected lab/CI lane.

## 1. Harness overview

The harness executes the audited provider matrix (100 rows from
`docs/implementation/provider-matrix.yaml`, P3-T01) in three modes:

| Mode | Where | Behavior |
|------|-------|----------|
| `mock` | Normal CI (default) | Controlled `httptest` server per provider, inert placeholder credentials, golden fixture response, artifact comparison. No real network, no real credentials. |
| `live` | Protected lab/CI | Real credentials from `GOROUTER_LIVE_*` env vars or the injected secret store; probes the provider endpoint; validates the response envelope shape. Skips (never false-passes) when the credential is unavailable. |
| `dry-run` | Anywhere | Never executes network calls. Every provider is reported `SKIP` with an explicit reason. |

Owned surfaces:

- `tests/providers/harness/*.go` — harness library (`package harness`) and its tests.
- `tests/providers/harness/testdata/fixtures/*.json` — golden response fixtures, one per wire format.
- `tests/providers/manifests/*.yaml` — one manifest per matrix row (100 files, 1:1 with `provider-matrix.yaml`).
- `docs/implementation/provider-live-validation.md` — this document.

## 2. Credential convention

Live credentials are resolved from the environment or a secrets manager through a
`CredentialSource` (`EnvSource` for env vars, `MemSource` for injected stores).

- Live credential env var per provider: `GOROUTER_LIVE_<PROVIDER_ID>_KEY`, where
  `PROVIDER_ID` is upper-snake (dashes become underscores).
  Example: `openai` → `GOROUTER_LIVE_OPENAI_KEY`, `black-forest-labs` →
  `GOROUTER_LIVE_BLACK_FOREST_LABS_KEY`.
- Providers with `auth_type: none` (noAuth) declare no credential env var; the
  manifest omits `credential_env_var` and the harness never sends an auth header.
- Optional live endpoint override per provider: `GOROUTER_LIVE_<PROVIDER_ID>_BASE_URL`.
  Without an override the harness probes the canonical endpoint for the provider's
  primary format (`openai_chat`/`openai_compat` → `https://api.openai.com/v1/chat/completions`,
  `codex_responses` → `https://api.openai.com/v1/responses`, `anthropic` →
  `https://api.anthropic.com/v1/messages`, `gemini` →
  `https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-flash:generateContent`).
- Mock mode never uses these env vars: it synthesizes the inert placeholder
  `sk-gorouter-mock-<provider_id>` per provider and redacts it from all output.

Auth transport per manifest (decision #127 preserves all original transports):

| auth_type | Transport |
|-----------|-----------|
| `none` | No auth header |
| `cookie` | `Cookie: session=<credential>` |
| `apikey`/`oauth`/`pat` | `Authorization: Bearer <credential>`; `X-Api-Key` for anthropic-format providers; `X-Goog-Api-Key` for gemini-format providers |

## 3. Manifest schema

Each file in `tests/providers/manifests/<provider_id>.yaml` mirrors exactly one row
of `docs/implementation/provider-matrix.yaml`:

```yaml
provider_id: openai
provider_type: openai
auth_type: apikey
credential_env_var: GOROUTER_LIVE_OPENAI_KEY
timeout: 30s
retries: 2
mock_fixtures:
  FormatOpenAIChat: tests/providers/harness/testdata/fixtures/openai_chat.json
  FormatOpenAICompat: tests/providers/harness/testdata/fixtures/openai_compat.json
```

| Field | Meaning |
|-------|---------|
| `provider_id` | Matrix row id (file name). |
| `provider_type` | Must equal the matrix row's `provider_type`. |
| `auth_type` | Must equal the matrix row's `auth_type` (`apikey`/`oauth`/`cookie`/`none`/`pat`). |
| `credential_env_var` | `GOROUTER_LIVE_<ID>_KEY`; omitted for `auth_type: none`. |
| `timeout` | Per-probe context deadline (Go duration). |
| `retries` | Retries on 429/5xx/transport errors (0–5; 401/403 and timeouts are never retried). |
| `mock_fixtures` | One repo-relative fixture path per format the provider supports; keys must equal the matrix row's `supported_formats` exactly. |

`TestManifestInventory` enforces the 1:1 cover: 100 matrix rows = 100 generated
`RegistryRows` entries (`internal/engine/providers/registry/generated.go`, P3-T13) =
100 manifests, with matching identity, auth type, format coverage, fixture validity,
and credential naming. Adding a provider requires a matrix row first; the harness
manifest must then mirror it.

## 4. Harness behavior contract

- **Credential lookup:** live mode resolves the manifest's env var through the
  `CredentialSource` (env or secret store); missing credentials produce `SKIP`
  with the env var named — never a false `PASS` and never a spurious `FAIL`.
- **Provider isolation:** each provider runs against its own mock server and its
  own placeholder credential; `AuthHeaders` is derived per manifest, the mock
  server rejects requests carrying another provider's credential, and one
  provider's failure never affects others (matrix subtests run in parallel).
- **Result sanitisation:** every `Result.Artifact`, log line, and error returned
  by the harness passes through the `Redactor`; credential-like strings become
  `[REDACTED]`.
- **Timeout:** each probe attempt carries the manifest `timeout` deadline; a
  deadline breach fails the run without retrying.
- **Retry:** 429 and 5xx responses and transport errors are retried up to
  `retries` times with capped backoff; 4xx auth errors fail immediately.
- **Artifact comparison:** mock mode compares the provider artifact against the
  golden fixture with JSON semantic equality (`CompareArtifacts`, key-order and
  whitespace insensitive, first-diff path reported). Live mode validates the
  envelope shape instead (`ValidateArtifactShape`: `choices`/`output`/`content`/
  `candidates` non-empty, or a structured `error` object).

## 5. Redaction and the leak-fail gate

The harness strips all credential-like strings from output: Bearer/Basic tokens,
OpenAI-style `sk-` keys, GitHub (`ghp_`/`gho_`/`ghu_`/`ghs_`/`ghr_`), Slack
(`xoxb-`/`xoxa-`/`xoxp-`/`xoxr-`/`xoxs-`), GitLab PATs (`glpat-`), Google API keys
(`AIza`), AWS access key ids (`AKIA`), JWTs (`eyJ...`), session cookies, API key
headers (`x-api-key`, `x-goog-api-key`, `api-key`), and `?key=`/`token=` query
parameters. Exact secret values (including every provider's placeholder) are
replaced before pattern matching.

The leak-fail gate (`go test -count=1 -run LeakFail ./tests/providers/harness/...`,
design §14.2.9/§15.4) runs the mock matrix and live dry-run in a subprocess and
scans every byte of stdout/stderr for the credential patterns and the exact
placeholder values. Any match fails the run. The gate also asserts the
`[REDACTED]` marker appears in the output, proving the redactor was exercised
(no vacuous passes). The subprocess excludes the gate itself, so there is no
recursion.

## 6. Running the harness

```bash
# Full mock/golden matrix + live dry-run + leak-fail gate (normal CI)
go test -count=1 ./tests/providers/...
go test -count=1 -run LeakFail ./tests/providers/harness/...

# Verbose harness run (RED/GREEN evidence)
go test -count=1 -v ./tests/providers/harness/...

# Race detector
go test -race -count=1 ./tests/providers/harness/...
```

Live validation in the protected lab:

```bash
# Inject credentials (env or secret store), then:
go test -count=1 -run TestLiveDryRun_UnavailableCredentialsSkip -v ./tests/providers/harness/...
```

With credentials present the live leg executes real probes and reports `PASS` or
explicit `SKIP`; with credentials absent every provider reports `SKIP` — the
suite never false-passes and never fails for missing credentials.

## 7. Security notes

- Mock mode uses only inert placeholder values; no real credential is ever
  constructed outside live mode.
- The harness never logs request bodies, response bodies beyond the redacted
  artifact, or upstream error payloads.
- Live credentials must be injected into the protected lab environment or secret
  store only; they are never committed, never echoed, and always redacted from
  output by the leak-fail gate.
- OAuth-flow providers can be validated live only through the protected lab's
  OAuth bootstrap; the harness treats their tokens like any other credential.
