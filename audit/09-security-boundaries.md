# 09 — Security Boundaries

## Scope

Trust boundaries, authentication, authorization, local-only enforcement,
credential storage, SSRF guard, child-process gate, cookie security, and
network capability in the original 9Router at the audited commit.

## Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Exact commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Read-only clone: `upstream-original/`

## Method

Manual source inspection of the following files, supplemented by the
test at `tests/unit/dashboard-guard.test.js`.

| File | Role |
| ------ | ------ |
| `custom-server.js` | TCP-socket-derived client IP; forwarding-header trust |
| `src/dashboardGuard.js` | Central request gate: middleware for all routes |
| `src/lib/auth/dashboardSession.js` | JWT creation, verification, password check |
| `src/lib/auth/loginLimiter.js` | In-memory brute-force lockout |
| `src/lib/auth/oidc.js` | OIDC authentication provider |
| `src/sse/services/auth.js` | Provider credential selection, extraction, lock |
| `src/shared/utils/apiKey.js` | API key generation and CRC verification |
| `src/shared/utils/ssrfGuard.js` | Block internal/private targets for outbound fetch |
| `src/lib/network/outboundProxy.js` | Outbound proxy URL validation |
| `src/lib/network/ssrfGuard.js` | (Referenced, not found — see Uncertainties) |
| `src/app/api/auth/*/route.js` | Login/logout/status/OIDC/reset-password handlers |

## Confirmed Findings

### 1. TCP-Socket-Derived Client Identity (custom-server.js)

**File:** `custom-server.js` (lines 1–32)

The application wraps `http.createServer` to intercept every incoming
HTTP request before Next.js sees it:

- Socket-derived peer IP (`req.socket.remoteAddress`) is authoritative.
- Forwarding headers (`X-Forwarded-For`, `X-Real-IP`) are **stripped**
  from incoming headers and replaced with the application's own
  `x-9r-real-ip` header.
- If the TCP peer is a loopback address (`127.0.0.1`, `::1`,
  `::ffff:127.0.0.1`) **and** forwarding headers are present, the
  **first** public XFF value is preserved as the client IP (trusted
  reverse-proxy scenario).
- If the TCP peer is **not** loopback, forwarding headers are discarded
  and the socket IP is used directly. The `x-9r-via-proxy` flag is set
  when any forwarding header was present.

**Behavioral invariant:** The application **never** trusts
attacker-controlled `X-Forwarded-For` from non-loopback peers. A
reverse proxy sitting on the same host is the only trusted case.

### 2. Central Gate Proxy (src/dashboardGuard.js)

**File:** `src/dashboardGuard.js` (260 lines)

The exported `proxy(request)` function is the **single middleware**
for all inbound requests. It classifies every pathname into one of
several zones:

#### 2a. Local-Only Zone (LOCAL_ONLY_PATHS, line 69–85)

Routes that spawn child processes or read host secrets:

```text
/api/cli-tools/cowork-settings
/api/cli-tools/antigravity-mitm
/api/mcp/
/api/tunnel/tailscale-install, enable, disable, check
/api/tunnel/enable, disable
/api/oauth/cursor/auto-import
/api/oauth/kiro/auto-import
/api/auth/reset-password
/api/headroom/start, stop, proxy
```

Access requires either:

- A valid CLI token (`x-9r-cli-token` header matching a machine-ID-derived
  hash); or
- Loopback Host + loopback Origin + JWT session (or `requireLogin=false`).

**Gate function:** `canAccessLocalOnlyRoute()` (line 142–147).

#### 2b. Always-Protected Zone (ALWAYS_PROTECTED, line 38–45)

Requires valid JWT or valid CLI token regardless of `requireLogin`:

```text
/api/shutdown
/api/settings/database
/api/version/shutdown
/api/version/update
/api/oauth/cursor/auto-import
/api/oauth/kiro/auto-import
```

Note: `auto-import` routes appear in **both** ALWAYS_PROTECTED and
LOCAL_ONLY_PATHS. The local-only gate is evaluated first (line 187),
so the always-protected check only runs when local-only already passed.

#### 2c. Public LLM API Zone (PUBLIC_PREFIXES, line 35)

```text
/v1, /v1beta, /api/v1, /api/v1beta, /codex
```

Access requires one of:

- Loopback request (no API key needed); or
- Valid CLI token; or
- Valid API key (Bearer, `x-api-key`, `x-goog-api-key`, or `?key=` query).

**Gate function:** `canAccessPublicLlmApi()` (line 136–140).

#### 2d. Protected Dashboard Routes (pathname.startsWith("/dashboard"))

Three settings control access:

- `requireLogin` (default: `true`)
- `tunnelDashboardAccess` (default: `true`)
- Valid JWT `auth_token` cookie.

If `tunnelDashboardAccess` is disabled, access from the tunnel/tailscale
hostname is redirected to `/login` regardless of JWT validity.

#### 2e. Public API Allow-List (PUBLIC_API_PATHS, line 22–32)

No auth required:

```text
/api/health, /api/init, /api/locale,
/api/auth/login, /api/auth/logout, /api/auth/status,
/api/auth/oidc, /api/version, /api/settings/require-login
```

#### 2f. Protected API Paths (PROTECTED_API_PATHS, line 48–66)

Require auth when `requireLogin=true`, but allow through when disabled:
Settings, keys, providers, nodes, pools, combos, models, usage, OAuth,
cloud, media-providers, pricing, tags, CLI-tools, MCP, translator, tunnel.

### 3. Local Request Determination (isLocalRequest, line 95–114)

Three anchors in priority order:

1. **`x-9r-via-proxy` header** — If set by `custom-server.js`, the
   request came through a loopback reverse proxy. Return `false` because
   the socket is the proxy hop, not the end-user.
2. **`x-9r-real-ip` header** — If present and a loopback hostname,
   return `true`. If present and non-loopback, return `false`.
3. **Host header fallback** — Used only when `custom-server.js` is not
   active (e.g., dev `server.js`). Checks if Host is loopback.
4. **Origin check** — If an Origin header is present, it must resolve
   to a loopback hostname. Blocks CSRF from tunnel/tailscale origins
   even on loopback Host.

### 4. API Key Extraction and Validation

**File:** `src/dashboardGuard.js`, function `extractApiKey` (line 120–128).

Priority order: `Authorization: Bearer` → `x-api-key` →
`x-goog-api-key` → `?key=` query parameter.

**File:** `src/shared/utils/apiKey.js`

API key format: `sk-{machineId}-{keyId}-{crc8}` (new) or `sk-{random8}`
(old). The CRC is an 8-char HMAC-SHA256 using `API_KEY_SECRET` env var
(default: `"endpoint-proxy-api-key-secret"`). Old format keys have no
CRC validation.

**File:** `src/sse/services/auth.js`, function `extractApiKey` (line 297–311)

A **separate** extraction function exists in the SSE auth service that
only checks `Authorization: Bearer` and `x-api-key` (Anthropic style).
This is used for upstream provider credential resolution, not dashboard
gate auth.

### 5. Credential Storage

All provider credentials (OAuth tokens, API keys, refresh tokens) are
stored in SQLite at `${DATA_DIR}/db/data.sqlite`.

**File:** `src/lib/db/paths.js`

- `DB_DIR` = `${DATA_DIR}/db`
- `DATA_FILE` = `${DB_DIR}/data.sqlite`
- Legacy JSON files: `db.json`, `usage.json`, `disabledModels.json`,
  `request-details.json` under `${DATA_DIR}`.

**Known issue (from SUBAGENT-CONTRACT.md reference):** The `usage.json`
and `log.txt` files live under `~/.9router` and **do not** follow
`DATA_DIR` — the `src/lib/usageDb.js` shim re-exports from the new DB
layer, but this path divergence is a legacy behavior.

### 6. JWT Session Authentication

**File:** `src/lib/auth/dashboardSession.js`

- Secret: `JWT_SECRET` env var, or auto-generated 32-byte random hex
  stored at `${DATA_DIR}/jwt-secret` (mode `0o600`).
- Algorithm: HS256.
- Expiration: 24 hours.
- Cookie: `auth_token`, `httpOnly: true`, `sameSite: "lax"`,
  `secure: true` when `AUTH_COOKIE_SECURE=true` or
  `x-forwarded-proto: https`.
- Password verification via bcrypt compare, or `INITIAL_PASSWORD` env
  (default: `"123456"`) when no hash exists.

### 7. Login Rate Limiting

**File:** `src/lib/auth/loginLimiter.js`

In-memory progressive lockout per IP:

- 5 failures → 30s lock.
- 10 failures → 2m lock.
- 15 failures → 10m lock.
- 20+ failures → 30m lock.
- Auto-resets after 1 hour since last failure.

**Process-local state:** The `attempts` Map is in-memory only. A process
restart resets all counters. On a multi-instance deployment, each instance
has its own lockout state.

**Client IP derivation** (line 48–60): Uses `x-9r-real-ip` (trusted
socket-derived), or `X-Forwarded-For` when `TRUST_PROXY=true`, or falls
back to `"unknown"` (single global bucket).

### 8. OIDC Authentication

**File:** `src/lib/auth/oidc.js`

Supports PKCE + nonce flow against any OpenID Provider. Configured via
settings DB fields (`oidcIssuerUrl`, `oidcClientId`, `oidcClientSecret`,
`oidcScopes`, `oidcLoginLabel`). Auth mode can be `"password"`, `"oidc"`,
or `"both"`.

Discovery document fetched at runtime from `${issuerUrl}/.well-known/openid-configuration`.
ID token verified via `jose.createRemoteJWKSet`.

### 9. SSRF Guard

**File:** `src/shared/utils/ssrfGuard.js`

The `assertPublicUrl()` function blocks:

- Hostnames: `localhost`, `ip6-localhost`, `ip6-loopback`.
- Suffixes: `.internal`, `.local`, `.localhost`.
- Private IPv4 ranges: `0.0.0.0/8`, `10.0.0.0/8`, `127.0.0.0/8`,
  `169.254.0.0/16`, `172.16.0.0/12`, `192.168.0.0/16`.
- Private IPv6: loopback (`::1`, `::`), link-local (`fe80::`),
  unique-local (`fc`/`fd`), mapped IPv4 private ranges.

**Uncertain:** The function throws an Error for blocked URLs. The
calling code must catch and return a 400 response. No source evidence
was found for automatic 400 mapping in the SSE auth or executor paths —
the throw may propagate as a 500 if unhandled.

### 10. Outbound Proxy Validation

**File:** `src/lib/network/outboundProxy.js`

`validateProxyUrl()`:

- Rejects URLs containing newlines, backticks, or `$` (shell metachar).
- Only allows schemes: `http:`, `https:`, `socks5:`, `socks4:`,
  `socks5h:`, `socks4a:`.
- Returns null on parse failure.

`applyOutboundProxyEnv()` sets `HTTP_PROXY`, `HTTPS_PROXY`, `ALL_PROXY`
env vars from settings DB. Managed via `NINE_ROUTER_PROXY_MANAGED` marker
to track previously-set values.

### 11. CLI Token Authentication

**File:** `src/dashboardGuard.js` (lines 6–19)

A secondary machine-ID-derived token, cached in process memory
(`cachedCliToken`). Generated via `getConsistentMachineId("9r-cli-auth")`.
Sent in `x-9r-cli-token` header. Used for local-only routes and
always-protected routes where browser JWT is unavailable.

### 12. Exported Test Helpers

**File:** `src/dashboardGuard.js` (lines 175–181)

```javascript
export const __test__ = {
  isLocalRequest,
  isPublicLlmApi,
  extractApiKey,
  canAccessPublicLlmApi,
  canAccessLocalOnlyRoute,
};
```

Enables unit testing of individual gate functions without mocking the
full middleware.

## Security Implications Requiring User Decisions

1. **Default `API_KEY_SECRET`** is `"endpoint-proxy-api-key-secret"`.
   Any deployment not overriding this with a strong secret has
   predictable API key CRCs. User decision: set via env.

2. **Default `MACHINE_ID_SALT`** is `"endpoint-proxy-salt"`. Combined
   with a default `API_KEY_SECRET`, the CLI token and API key CRC are
   both predictable. User decision: set via env.

3. **Default `INITIAL_PASSWORD`** is `"123456"`. First visit with no
   saved password hash accepts this password. User decision: change
   on first login.

4. **SSRF guard throws exceptions** — calling code must catch. If any
   path forgets the try/catch, a blocked URL becomes a 500 instead of
   a 400. User decision: audit all call sites of `assertPublicUrl`.

5. **Login limiter is per-process** — process restart resets counts.
   In Docker or auto-scaling deployments, this is a conscious gap.

6. **OAuth credentials in SQLite** — encrypted at rest only by
   filesystem permissions. No column-level encryption. User decision:
   depends on deployment trust model.

7. **`usage.json` and `log.txt` path divergence** from `DATA_DIR` means
   that setting `DATA_DIR` does not relocate usage logs. OAuth tokens
   go to SQLite (follows DATA_DIR), but usage history may not.

## Uncertainties and Conflicting Evidence

1. **`src/lib/network/ssrfGuard.js`** file was referenced in the
   source-tree listing but was **not found** at that path. The actual
   SSRF guard is at `src/shared/utils/ssrfGuard.js`. No file exists at
   `src/lib/network/ssrfGuard.js` in the audited commit.

2. **SSRF guard usage** — the `assertPublicUrl` function is exported from
   `src/shared/utils/ssrfGuard.js` but no confirmed imports were traced
   in the SSE executor paths. The proxy fetch layer
   (`open-sse/utils/proxyFetch.js`) was not audited in this pass — it may
   apply its own SSRF protection or call `assertPublicUrl`.

3. **Rate-limit enforcement on `/api/auth/login`** — the middleware gate
   (`dashboardGuard.js`) does not itself apply rate limiting. The
   `loginLimiter.js` module is used inside the login route handler
   (`src/app/api/auth/login/route.js`). This was not fully traced.

4. **CORS policy** — `custom-server.js` deletes and rewrites forwarding
   headers but does not set CORS headers. The Next.js middleware and
   individual route handlers set their own CORS. No unified CORS policy
   was found in the middleware layer.

## Parity Requirements

- Preserve TCP-socket-derived client identity with unspoofable peer IP.
- Preserve the four-zone access model (local-only, always-protected,
  public LLM API, protected dashboard).
- Preserve the `x-9r-cli-token` machine-ID-derived secondary auth.
- Preserve the three-anchor `isLocalRequest` check (via-proxy, real-ip,
  Host + Origin).
- Preserve all LOCAL_ONLY_PATHS and ALWAYS_PROTECTED route lists.
- API key format `sk-{machineId}-{keyId}-{crc8}` with HMAC verification.
- JWT dashboard sessions with 24h expiry, HS256, httpOnly cookie.
- OIDC PKCE + nonce flow.
- SSRF guard blocking private/loopback/metadata targets for server-side
  fetch.
- Outbound proxy URL validation rejecting shell metacharacters.
- In-memory login rate limiter (or consciously choose a persistent
  alternative).

## Coverage Gaps

- No automated test for SSRF guard integration with executor paths.
- No test for the `x-9r-cli-token` header validation in
  `dashboard-guard.test.js` (the test file uses the machine ID mock but
  never tests the full flow end-to-end).
- No test for `OIDC` callback error states (invalid nonce, expired
  state cookie, token exchange failure).
- No test for the `always-protected` zone overlap with `local-only` zone.
- No automated verification that `usage.json` and `log.txt` path
  divergence is preserved or resolved in a Go rewrite.
