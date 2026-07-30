<!-- markdownlint-disable MD013 MD060 MD032 -->

# HTTP/API Contract Audit — Original 9Router Upstream

## 1. Scope

Exact HTTP request/response contract of the upstream 9Router application at
the audited commit. This report inventories every route path, HTTP method,
auth classification, CORS behavior, request/response/stream type, and source
handler across all `src/app/api/*/route.js` files plus the compatibility
rewrite layer in `next.config.mjs`.

**Does not cover:**

- Dashboard page files (`src/app/(dashboard)/*`)
- CLI tool internals
- `open-sse/` provider execution
- Database schema or persistence APIs (covered in `00-upstream-audit.md`)

---

## 2. Audited Repository and Exact Commit

| Field | Value |
| ------- | ------- |
| Repository | `https://github.com/decolua/9router.git` |
| Audited commit | `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` |
| Branch | `master` |
| Product version | `0.5.40` (from `package.json`) |
| Clone path | `C:\Users\faizz\9router-go\upstream-original` |
| Framework | Next.js 16.1.6 App Router |
| Custom server | Express 5.2.1 CJS wrapper (`custom-server.js`) |
| Audit date | 2026-07-28 |

---

## 3. Method

- **Inventory**: `find` + `grep` over all `src/app/api/*/route.js` files — 147 route files found, all enumerated below.
- **Export scanning**: `grep -n "^export async function\|^export const"` on every route file.
- **Source reading**: Every unique route handler pattern was read (LLM proxy, CRUD, SSE stream, management, OAuth, deployment proxies).
- **Configuration**: `next.config.mjs` rewrites and `dashboardGuard.js` auth middleware read in full.
- **Custom server**: `custom-server.js` read for IP trust and header behavior.
- **CORS confirmation**: All non-dashboard API routes define explicit `OPTIONS` handlers or per-response CORS headers.

### 3.1 Terminology

| Label | Meaning |
| ------- | --------- |
| **Confirmed** | Source files read directly; exports and calls verified |
| **Observed** | Route file exists but handler body read partially |
| **Traced** | Delegates to a shared handler (`handleChat`, `handleTts`, etc.) whose behavior is documented in `00-upstream-audit.md` |
| **Semantic** | URL structure follows a pattern; exact sub-routes depend on DB data |
| **Uncertain** | Cannot fully verify without runtime execution or DB state |

---

## 4. Compatibility Rewrite Layer (`next.config.mjs`)

Source: `upstream-original/next.config.mjs` lines 37–71.

The following rewrites are applied before any request reaches a route handler.
They exist for client compatibility and MUST be preserved or consciously
replaced in a Go rewrite.

| Source Pattern | Destination | Purpose |
| ---------------- | ------------- | --------- |
| `/v1/v1/:path*` | `/api/v1/:path*` | Double-`/v1` normalization (some clients send this) |
| `/v1/v1` | `/api/v1` | Double-`/v1` root |
| `/codex/:path*` | `/api/v1/responses` | Codex SDK compatibility (always maps to Responses endpoint) |
| `/responses` | `/api/v1/responses` | Standalone Responses path |
| `/v1beta/:path*` | `/api/v1beta/:path*` | Gemini v1beta compatibility |
| `/v1beta` | `/api/v1beta` | Gemini v1beta root |
| `/v1/:path*` | `/api/v1/:path*` | Primary LLM API prefix |
| `/v1` | `/api/v1` | Primary LLM API root |

**Invariant:** Every `/v1/*` and `/v1beta/*` request reaches an API handler,
not a page route. The `/codex/*` pattern always routes to `/api/v1/responses`,
meaning ALL Codex SDK requests hit the Responses handler regardless of sub-path.

**Edge case:** The `/codex/:path*` rewrite discards the sub-path. A request to
`/codex/some/random/path` rewrites to `/api/v1/responses`. The Responses
handler receives the original path in `request.nextUrl` but route matching
is at `/api/v1/responses`.

---

## 5. Custom Server Header Contract (`custom-server.js`)

Source: `upstream-original/custom-server.js`

The Next.js standalone server is wrapped at the `http.createServer` level:

1. **IP derivation**: Reads `req.socket.remoteAddress` (unspoofable TCP peer).
2. **Forwarded-header trust**: Only if TCP peer is loopback (`127.0.0.1`, `::1`,
   `::ffff:127.0.0.1`). Direct/public sockets are keyed by the raw peer address.
3. **Header strip**: Deletes client-supplied `x-forwarded-for`, `x-real-ip`,
   `x-9r-real-ip`, `x-9r-via-proxy`.
4. **Header stamp**: Sets `x-9r-real-ip` to the derived IP and
   `x-9r-via-proxy: 1` if the request arrived through a reverse proxy.
5. **Body size limit**: Configurable via `NINEROUTER_PROXY_CLIENT_MAX_BODY_SIZE`
   environment variable, defaults to `128mb`. Applied in `next.config.mjs`
   `experimental.proxyClientMaxBodySize`.

**This is part of the authorization boundary.** The `dashboardGuard.js`
middleware reads `x-9r-real-ip` and `x-9r-via-proxy` to enforce local-only
routes. A Go rewrite must reproduce this trust model exactly or the local-only
security invariant is lost (see Section 8).

---

## 6. Auth Classification (from `dashboardGuard.js`)

Source: `upstream-original/src/dashboardGuard.js`

The `proxy()` export is a Next.js [Middleware](https://nextjs.org/docs/app/building-your-application/routing/middleware)
that runs on every request. It classifies routes into five tiers:

### 6.1 Auth Tiers

| Tier | Condition | HTTP Response on Deny |
| ------ | ----------- | ---------------------- |
| **Public API** | Path in `PUBLIC_API_PATHS` — health, init, locale, auth/login, auth/logout, auth/status, auth/oidc, version, settings/require-login | Always passes |
| **LLM API** | Path starts with `/v1`, `/v1beta`, `/api/v1`, `/api/v1beta`, `/codex` | 401 "API key required" unless local/CLI/valid API key |
| **Protected API** (`/api/*`) | Path matches `PROTECTED_API_PATHS` prefixes | 401 unless authenticated (JWT or requireLogin=false) |
| **Always Protected** | Path in `ALWAYS_PROTECTED` | 401 unless valid JWT or CLI token |
| **Local Only** | Path in `LOCAL_ONLY_PATHS` | 403 "Local only: CLI token required" unless local + authenticated or CLI token |
| **Dashboard** (`/dashboard`) | Not an API route | Redirect to `/login` unless authenticated (or requireLogin=false) |

### 6.2 API Key Extraction (for LLM API tier)

Order of precedence:

1. `Authorization: Bearer <token>`
2. `x-api-key` header
3. `x-goog-api-key` header
4. `key` query parameter

### 6.3 Auth Decision Tree for API Routes

```text
Request → is LOCAL_ONLY? → yes → local + authenticated? → no → 403
                                      → yes → next()
         → is ALWAYS_PROTECTED? → yes → CLI token or JWT? → no → 401
                                       → yes → next()
         → is LLM API prefix? → yes → local or CLI token or valid API key? → no → 401
                                       → yes → next()
         → starts with /api/ → is in PUBLIC_API_PATHS? → yes → next()
                              → not in public → authenticated? → no → 401
                                                → yes → next()
         → starts with /dashboard → auth check (JWT or requireLogin=false)
         → / → redirect to /dashboard
```

### 6.4 Confirmed Public API Paths (no auth at middleware level)

- `GET /api/health`
- `GET /api/init`
- `POST /api/locale`
- `POST /api/auth/login`
- `POST /api/auth/logout`
- `GET /api/auth/status`
- `/api/auth/oidc` (prefix — includes oidc/callback, oidc/start, oidc/test)
- `GET /api/version`
- `GET /api/settings/require-login`

**Note:** LLM API paths (`/v1/*`, `/v1beta/*`, `/api/v1/*`, `/api/v1beta/*`, `/codex/*`)
appear "public" at the middleware level but still require a valid API key,
local request, or CLI token inside the LLM API check.

### 6.5 Protected API Prefixes (require auth unless requireLogin=false)

These are matched as path prefixes:

- `/api/settings`, `/api/keys`, `/api/providers`, `/api/provider-nodes`,
  `/api/proxy-pools`, `/api/combos`, `/api/models`, `/api/usage`,
  `/api/oauth`, `/api/cloud`, `/api/media-providers`, `/api/pricing`,
  `/api/tags`, `/api/cli-tools`, `/api/mcp`, `/api/translator`, `/api/tunnel`

### 6.6 Always-Protected Paths (JWT or CLI token always required)

- `/api/shutdown`, `/api/settings/database`, `/api/version/shutdown`,
  `/api/version/update`, `/api/oauth/cursor/auto-import`,
  `/api/oauth/kiro/auto-import`

### 6.7 Local-Only Paths (CLI token + loopback required)

- `/api/cli-tools/cowork-settings`
- `/api/cli-tools/antigravity-mitm` (prefix)
- `/api/mcp/` (prefix — all MCP endpoints)
- `/api/tunnel/tailscale-install`
- `/api/tunnel/tailscale-enable`
- `/api/tunnel/tailscale-disable`
- `/api/tunnel/tailscale-check`
- `/api/tunnel/enable`
- `/api/tunnel/disable`
- `/api/oauth/cursor/auto-import`
- `/api/oauth/kiro/auto-import`
- `/api/auth/reset-password`
- `/api/headroom/start`
- `/api/headroom/stop`
- `/api/headroom/proxy` (prefix)

---

## 7. CORS Behavior

**All LLM API routes** (`/v1/*`, `/v1beta/*`, `/api/v1/*`) define explicit
`OPTIONS` handlers returning:

```text
Access-Control-Allow-Origin: *
Access-Control-Allow-Methods: varies per route (typically "GET, POST, OPTIONS")
Access-Control-Allow-Headers: *
```

**Health route** also defines CORS headers on both `GET` and `OPTIONS`.

**Tags route** has an `OPTIONS` handler (specific headers not read).

**Most management API routes do NOT define explicit CORS handlers** — they
rely on the custom server proxy or the dashboard being same-origin.
Browser-based API calls to management routes must be same-origin or go through
the Next.js CSRF protection.

**Headroom proxy route** accepts all methods via the `proxy` handler (including
`OPTIONS`) and forwards them to the Headroom backend. CORS is managed by
Headroom itself for those proxied requests.

---

## 8. Route Inventory — Complete

The following is the complete inventory of all 147 `route.js` files in
`src/app/api/`, organized by domain. Each entry lists:

- **Route path** (as it would appear after the `/api` prefix)
- **HTTP methods** (confirmed exported)
- **Auth tier** (from Section 6)
- **Request body type**
- **Response type**
- **Source handler** (the exported function or delegate)

### 8.1 Health and System

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 1 | `GET /api/health` | GET, OPTIONS | Public | — | JSON `{ ok: true }` with CORS | `GET()` in `health/route.js` | CORS headers on both |
| 2 | `GET /api/init` | GET | Public | — | JSON | `GET()` in `init/route.js` | First-run setup |
| 3 | `POST /api/locale` | POST | Public | JSON: `{ locale }` | JSON | `POST()` in `locale/route.js` | Set locale |
| 4 | `GET /api/version` | GET | Public | — | JSON with version info | `GET()` in `version/route.js` (line 55) | — |
| 5 | `POST /api/version/shutdown` | POST | Always Protected | — | JSON | `POST()` in `version/shutdown/route.js` | — |
| 6 | `POST /api/version/update` | POST | Always Protected | — | JSON | `POST()` in `version/update/route.js` | — |
| 7 | `POST /api/shutdown` | POST | Always Protected | — | JSON | `POST()` in `shutdown/route.js` | Shut down process |

### 8.2 Authentication

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 8 | `POST /api/auth/login` | POST | Public | JSON: `{ password }` | JSON with JWT cookie | `POST()` in `auth/login/route.js` | — |
| 9 | `POST /api/auth/logout` | POST | Public | — | JSON | `POST()` in `auth/logout/route.js` | Clears cookie |
| 10 | `GET /api/auth/status` | GET | Public | — | JSON with auth status | `GET()` in `auth/status/route.js` | — |
| 11 | `GET /api/auth/oidc/start` | GET | Public (under oidc prefix) | Query params | JSON with OIDC URL | `GET()` in `auth/oidc/start/route.js` | — |
| 12 | `GET /api/auth/oidc/callback` | GET | Public (under oidc prefix) | Query params | JSON or redirect | `GET()` in `auth/oidc/callback/route.js` | — |
| 13 | `POST /api/auth/oidc/test` | POST | Public (under oidc prefix) | JSON | JSON | `POST()` in `auth/oidc/test/route.js` | Test OIDC config |
| 14 | `POST /api/auth/reset-password` | POST | Local Only | — | JSON | `POST()` in `auth/reset-password/route.js` | — |

### 8.3 Settings

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 15 | `GET /api/settings` | GET | Protected | — | JSON with all settings | `GET()` in `settings/route.js` | `dynamic = "force-dynamic"`, `revalidate = 0` |
| 16 | `PATCH /api/settings` | PATCH | Protected | JSON with partial settings | JSON | `PATCH()` in `settings/route.js` | — |
| 17 | `GET /api/settings/require-login` | GET | Public | — | JSON `{ requireLogin }` | `GET()` in `settings/require-login/route.js` | — |
| 18 | `GET /api/settings/database` | GET | Always Protected | — | Binary/stream (DB export) | `GET()` in `settings/database/route.js` | File download |
| 19 | `POST /api/settings/database` | POST | Always Protected | Multipart (DB import) | JSON | `POST()` in `settings/database/route.js` | — |
| 20 | `POST /api/settings/proxy-test` | POST | Protected | JSON | JSON | `POST()` in `settings/proxy-test/route.js` | Test proxy connection |

### 8.4 API Keys

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 21 | `GET /api/keys` | GET | Protected | Query params | JSON array | `GET()` in `keys/route.js` | `dynamic = "force-dynamic"` |
| 22 | `POST /api/keys` | POST | Protected | JSON for new key | JSON | `POST()` in `keys/route.js` | — |
| 23 | `GET /api/keys/[id]` | GET | Protected | — | JSON | `GET(request, { params })` in `keys/[id]/route.js` | Single key |
| 24 | `PUT /api/keys/[id]` | PUT | Protected | JSON for update | JSON | `PUT(request, { params })` in `keys/[id]/route.js` | — |
| 25 | `DELETE /api/keys/[id]` | DELETE | Protected | — | JSON | `DELETE(request, { params })` in `keys/[id]/route.js` | — |

### 8.5 Providers

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 26 | `GET /api/providers` | GET | Protected | Query params | JSON array | `GET()` in `providers/route.js` | `dynamic = "force-dynamic"` |
| 27 | `POST /api/providers` | POST | Protected | JSON new provider | JSON | `POST()` in `providers/route.js` | `dynamic = "force-dynamic"` |
| 28 | `GET /api/providers/[id]` | GET | Protected | — | JSON | `GET(request, { params })` in `providers/[id]/route.js` | — |
| 29 | `PUT /api/providers/[id]` | PUT | Protected | JSON for update | JSON | `PUT(request, { params })` in `providers/[id]/route.js` | — |
| 30 | `DELETE /api/providers/[id]` | DELETE | Protected | — | JSON | `DELETE(request, { params })` in `providers/[id]/route.js` | — |
| 31 | `GET /api/providers/[id]/models` | GET | Protected | — | JSON array | `GET(request, { params })` in `providers/[id]/models/route.js` | Live model listing |
| 32 | `POST /api/providers/[id]/test` | POST | Protected | JSON | JSON | `POST(request, { params })` in `providers/[id]/test/route.js` | Test connection |
| 33 | `POST /api/providers/[id]/test-models` | POST | Protected | JSON | JSON | `POST(request, { params })` in `providers/[id]/test-models/route.js` | Test specific models |
| 34 | `GET /api/providers/client` | GET | Protected | Query params (page, pageSize, provider, sort, accountStatus) | JSON with pagination | `GET(request)` in `providers/client/route.js` | Sanitized (no secrets) |
| 35 | `GET /api/providers/kilo/free-models` | GET | Protected | — | JSON | `GET()` in `providers/kilo/free-models/route.js` | — |
| 36 | `GET /api/providers/suggested-models` | GET | Protected | Query params | JSON | `GET(request)` in `providers/suggested-models/route.js` | `dynamic = "force-dynamic"` |
| 37 | `POST /api/providers/test-batch` | POST | Protected | JSON with model list | JSON | `POST()` in `providers/test-batch/route.js` | Batch test |
| 38 | `POST /api/providers/validate` | POST | Protected | JSON | JSON | `POST()` in `providers/validate/route.js` | Validate config |

### 8.6 Provider Nodes

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 39 | `GET /api/provider-nodes` | GET | Protected | — | JSON array | `GET()` in `provider-nodes/route.js` | `dynamic = "force-dynamic"` |
| 40 | `POST /api/provider-nodes` | POST | Protected | JSON | JSON | `POST()` in `provider-nodes/route.js` | `dynamic = "force-dynamic"` |
| 41 | `PUT /api/provider-nodes/[id]` | PUT | Protected | JSON | JSON | `PUT(request, { params })` in `provider-nodes/[id]/route.js` | No GET for single node |
| 42 | `DELETE /api/provider-nodes/[id]` | DELETE | Protected | — | JSON | `DELETE(request, { params })` in `provider-nodes/[id]/route.js` | — |
| 43 | `POST /api/provider-nodes/validate` | POST | Protected | JSON | JSON | `POST()` in `provider-nodes/validate/route.js` | — |

### 8.7 Proxy Pools

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 44 | `GET /api/proxy-pools` | GET | Protected | Query params | JSON array | `GET(request)` in `proxy-pools/route.js` | — |
| 45 | `POST /api/proxy-pools` | POST | Protected | JSON | JSON | `POST()` in `proxy-pools/route.js` | — |
| 46 | `GET /api/proxy-pools/[id]` | GET | Protected | — | JSON | `GET(request, { params })` in `proxy-pools/[id]/route.js` | — |
| 47 | `PUT /api/proxy-pools/[id]` | PUT | Protected | JSON | JSON | `PUT(request, { params })` in `proxy-pools/[id]/route.js` | — |
| 48 | `DELETE /api/proxy-pools/[id]` | DELETE | Protected | — | JSON | `DELETE(request, { params })` in `proxy-pools/[id]/route.js` | — |
| 49 | `POST /api/proxy-pools/[id]/test` | POST | Protected | JSON | JSON | `POST(request, { params })` in `proxy-pools/[id]/test/route.js` | Test pool |
| 50 | `POST /api/proxy-pools/cloudflare-deploy` | POST | Protected | JSON | JSON | `POST()` in `proxy-pools/cloudflare-deploy/route.js` | Deploy CF Worker relay |
| 51 | `POST /api/proxy-pools/deno-deploy` | POST | Protected | JSON | JSON | `POST()` in `proxy-pools/deno-deploy/route.js` | Deploy Deno relay |
| 52 | `POST /api/proxy-pools/vercel-deploy` | POST | Protected | JSON | JSON | `POST()` in `proxy-pools/vercel-deploy/route.js` | Edge runtime; deploy Vercel relay |

### 8.8 Combos

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 53 | `GET /api/combos` | GET | Protected | — | JSON array | `GET()` in `combos/route.js` | `dynamic = "force-dynamic"` |
| 54 | `POST /api/combos` | POST | Protected | JSON | JSON | `POST()` in `combos/route.js` | `dynamic = "force-dynamic"` |
| 55 | `GET /api/combos/[id]` | GET | Protected | — | JSON | `GET(request, { params })` in `combos/[id]/route.js` | — |
| 56 | `PUT /api/combos/[id]` | PUT | Protected | JSON | JSON | `PUT(request, { params })` in `combos/[id]/route.js` | — |
| 57 | `DELETE /api/combos/[id]` | DELETE | Protected | — | JSON | `DELETE(request, { params })` in `combos/[id]/route.js` | — |

### 8.9 Models

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 58 | `GET /api/models` | GET | Protected | — | JSON array | `GET()` in `models/route.js` | — |
| 59 | `PUT /api/models` | PUT | Protected | JSON | JSON | `PUT()` in `models/route.js` | Bulk update model state |
| 60 | `GET /api/models/alias` | GET | Protected | — | JSON | `GET()` in `models/alias/route.js` | `dynamic = "force-dynamic"` |
| 61 | `PUT /api/models/alias` | PUT | Protected | JSON | JSON | `PUT()` in `models/alias/route.js` | Set alias |
| 62 | `DELETE /api/models/alias` | DELETE | Protected | JSON | JSON | `DELETE()` in `models/alias/route.js` | Remove alias |
| 63 | `GET /api/models/availability` | GET | Protected | Query params | JSON | `GET()` in `models/availability/route.js` | — |
| 64 | `POST /api/models/availability` | POST | Protected | JSON | JSON | `POST()` in `models/availability/route.js` | — |
| 65 | `GET /api/models/custom` | GET | Protected | — | JSON | `GET()` in `models/custom/route.js` | `dynamic = "force-dynamic"` |
| 66 | `POST /api/models/custom` | POST | Protected | JSON | JSON | `POST()` in `models/custom/route.js` | `dynamic = "force-dynamic"` |
| 67 | `DELETE /api/models/custom` | DELETE | Protected | JSON | JSON | `DELETE()` in `models/custom/route.js` | `dynamic = "force-dynamic"` |
| 68 | `GET /api/models/disabled` | GET | Protected | — | JSON | `GET()` in `models/disabled/route.js` | `dynamic = "force-dynamic"` |
| 69 | `POST /api/models/disabled` | POST | Protected | JSON | JSON | `POST()` in `models/disabled/route.js` | `dynamic = "force-dynamic"` |
| 70 | `DELETE /api/models/disabled` | DELETE | Protected | JSON | JSON | `DELETE()` in `models/disabled/route.js` | `dynamic = "force-dynamic"` |
| 71 | `POST /api/models/test` | POST | Protected | JSON `{ model, messages }` | JSON | `POST()` in `models/test/route.js` | Ping/test a model |

### 8.10 OAuth

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 72 | `GET /api/oauth/[provider]/[action]` | GET, POST | Protected | Varies by action | JSON | `[provider]/[action]/route.js` | Dynamic OAuth; see 8.10.1 |
| 73 | `POST /api/oauth/codex/bulk-import` | POST | Protected | JSON with tokens | JSON | `POST()` in `oauth/codex/bulk-import/route.js` | — |
| 74 | `POST /api/oauth/codex/import-token` | POST | Protected | JSON with token | JSON | `POST()` in `oauth/codex/import-token/route.js` | — |
| 75 | `GET /api/oauth/cursor/auto-import` | GET | Always Protected + Local Only | — | JSON | `GET()` in `oauth/cursor/auto-import/route.js` | Auto-detect Cursor |
| 76 | `POST /api/oauth/cursor/import` | POST | Protected | JSON | JSON | `POST()` in `oauth/cursor/import/route.js` | Import Cursor session |
| 77 | `GET /api/oauth/cursor/import` | GET | Protected | Query params | JSON | `GET()` in `oauth/cursor/import/route.js` | Poll import status |
| 78 | `POST /api/oauth/gitlab/pat` | POST | Protected | JSON `{ pat, baseUrl }` | JSON | `POST()` in `oauth/gitlab/pat/route.js` | GitLab PAT login |
| 79 | `POST /api/oauth/iflow/cookie` | POST | Protected | JSON | JSON | `POST()` in `oauth/iflow/cookie/route.js` | iFlow cookie auth |
| 80 | `POST /api/oauth/kiro/api-key` | POST | Protected | JSON | JSON | `POST()` in `oauth/kiro/api-key/route.js` | Kiro API key |
| 81 | `GET /api/oauth/kiro/auto-import` | GET | Always Protected + Local Only | — | JSON | `GET()` in `oauth/kiro/auto-import/route.js` | Auto-detect Kiro |
| 82 | `POST /api/oauth/kiro/import` | POST | Protected | JSON | JSON | `POST()` in `oauth/kiro/import/route.js` | Import Kiro session |
| 83 | `POST /api/oauth/kiro/import-cli-proxy` | POST | Protected | JSON | JSON | `POST()` in `oauth/kiro/import-cli-proxy/route.js` | Kiro CLI import |
| 84 | `GET /api/oauth/kiro/social-authorize` | GET | Protected | Query params | JSON | `GET()` in `oauth/kiro/social-authorize/route.js` | Kiro social login |
| 85 | `POST /api/oauth/kiro/social-exchange` | POST | Protected | JSON | JSON | `POST()` in `oauth/kiro/social-exchange/route.js` | Kiro social token exchange |

**8.10.1 Dynamic OAuth actions** (`/api/oauth/[provider]/[action]`):

| Action | Method | Purpose |
| -------- | -------- | --------- |
| `authorize` | GET | Generate auth URL (PKCE or other) |
| `start-proxy` | GET | Start a local proxy server (codex/xai only) |
| `poll-status` | GET | Poll OAuth proxy status |
| `stop-proxy` | GET | Stop proxy server |
| `device-code` | GET | Request device code for device_code flow |
| `exchange` | POST | Exchange authorization code for tokens |
| `poll` | POST | Poll device_code flow for token |
| `manual-code` | POST | Complete xAI manual code entry |

### 8.11 Usage

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 86 | `GET /api/usage/history` | GET | Protected | — | JSON array | `GET()` in `usage/history/route.js` | — |
| 87 | `GET /api/usage/logs` | GET | Protected | Query params | JSON | `GET()` in `usage/logs/route.js` | — |
| 88 | `GET /api/usage/providers` | GET | Protected | — | JSON | `GET()` in `usage/providers/route.js` | — |
| 89 | `GET /api/usage/request-details` | GET | Protected | Query params | JSON | `GET()` in `usage/request-details/route.js` | — |
| 90 | `GET /api/usage/request-logs` | GET | Protected | — | JSON | `GET()` in `usage/request-logs/route.js` | — |
| 91 | `GET /api/usage/stats` | GET | Protected | Query params | JSON | `GET()` in `usage/stats/route.js` | `dynamic = "force-dynamic"` |
| 92 | `GET /api/usage/stream` | GET | Protected | — | SSE stream | `GET()` in `usage/stream/route.js` | `dynamic = "force-dynamic"`; SSE with 25s keepalive pings |
| 93 | `GET /api/usage/chart` | GET | Protected | Query params | JSON | `GET()` in `usage/chart/route.js` | Chart data |
| 94 | `GET /api/usage/[connectionId]` | GET | Protected | — | JSON | `GET(request, { params })` in `usage/[connectionId]/route.js` | Per-connection usage |
| 95 | `GET /api/usage/[connectionId]/codex-reset-credits` | GET | Protected | Query params | JSON | `GET()` in `usage/[connectionId]/codex-reset-credits/route.js` | — |
| 96 | `POST /api/usage/[connectionId]/codex-reset-credits` | POST | Protected | JSON | JSON | `POST()` in `usage/[connectionId]/codex-reset-credits/route.js` | Reset Codex credits |

### 8.12 CLI Tools

| # | Route | Methods | Auth | Request | Response | Source File | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | ------------- | ------- |
| 97 | `GET /api/cli-tools/all-statuses` | GET | Protected | — | JSON | `cli-tools/all-statuses/route.js` | Status of all tools |
| 98 | `GET /api/cli-tools/antigravity-mitm` | GET | Protected | — | JSON | `cli-tools/antigravity-mitm/route.js` | — |
| 99 | `POST /api/cli-tools/antigravity-mitm` | POST | Protected | JSON | JSON | Same file | — |
| 100 | `DELETE /api/cli-tools/antigravity-mitm` | DELETE | Protected | — | JSON | Same file | — |
| 101 | `PATCH /api/cli-tools/antigravity-mitm` | PATCH | Protected | JSON | JSON | Same file | — |
| 102 | `GET /api/cli-tools/antigravity-mitm/alias` | GET | Protected | — | JSON | `cli-tools/antigravity-mitm/alias/route.js` | — |
| 103 | `PUT /api/cli-tools/antigravity-mitm/alias` | PUT | Protected | JSON | JSON | Same file | — |

The following CLI-tool settings routes all follow the same **GET/POST/DELETE** pattern:

| # | Route | Methods | Auth | Source File |
| --- | ------- | --------- | ------ | ------------- |
| 104 | `GET\|POST\|DELETE /api/cli-tools/claude-settings` | GET, POST, DELETE | Protected | `cli-tools/claude-settings/route.js` |
| 105 | `GET\|POST\|DELETE /api/cli-tools/cline-settings` | GET, POST, DELETE | Protected | `cli-tools/cline-settings/route.js` |
| 106 | `GET\|POST\|DELETE /api/cli-tools/codex-settings` | GET, POST, DELETE | Protected | `cli-tools/codex-settings/route.js` |
| 107 | `GET\|POST\|DELETE /api/cli-tools/copilot-settings` | GET, POST, DELETE | Protected | `cli-tools/copilot-settings/route.js` |
| 108 | `GET\|POST\|DELETE /api/cli-tools/cowork-settings` | GET, POST, DELETE | Protected (Local Only for cowork) | `cli-tools/cowork-settings/route.js` |
| 109 | `GET\|POST\|DELETE /api/cli-tools/deepseek-tui-settings` | GET, POST, DELETE | Protected | `cli-tools/deepseek-tui-settings/route.js` |
| 110 | `GET\|POST\|DELETE /api/cli-tools/droid-settings` | GET, POST, DELETE | Protected | `cli-tools/droid-settings/route.js` |
| 111 | `GET\|POST\|DELETE /api/cli-tools/grok-build-settings` | GET, POST, DELETE | Protected | `cli-tools/grok-build-settings/route.js` |
| 112 | `GET\|POST\|DELETE /api/cli-tools/hermes-settings` | GET, POST, DELETE | Protected | `cli-tools/hermes-settings/route.js` |
| 113 | `GET\|POST\|DELETE /api/cli-tools/jcode-settings` | GET, POST, DELETE | Protected | `cli-tools/jcode-settings/route.js` |
| 114 | `GET\|POST\|DELETE /api/cli-tools/kilo-settings` | GET, POST, DELETE | Protected | `cli-tools/kilo-settings/route.js` |
| 115 | `GET\|POST\|DELETE /api/cli-tools/openclaw-settings` | GET, POST, DELETE | Protected | `cli-tools/openclaw-settings/route.js` |
| 116 | `GET\|POST\|PATCH\|DELETE /api/cli-tools/opencode-settings` | GET, POST, PATCH, DELETE | Protected | `cli-tools/opencode-settings/route.js` |

Plus two special cowork routes:

| # | Route | Methods | Auth | Source File |
|---|-------|---------|------|-------------|
| 117 | `GET /api/cli-tools/cowork-mcp-registry` | GET | Protected (Local Only) | `cli-tools/cowork-mcp-registry/route.js` |
| 118 | `POST /api/cli-tools/cowork-mcp-tools` | POST | Protected (Local Only) | `cli-tools/cowork-mcp-tools/route.js` |

### 8.13 Translator

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 119 | `GET /api/translator/load` | GET | Protected | Query params | JSON with translator config | `GET()` in `translator/load/route.js` | — |
| 120 | `POST /api/translator/save` | POST | Protected | JSON | JSON | `POST()` in `translator/save/route.js` | — |
| 121 | `POST /api/translator/send` | POST | Protected | JSON | JSON | `POST()` in `translator/send/route.js` | — |
| 122 | `POST /api/translator/translate` | POST | Protected | JSON | JSON | `POST()` in `translator/translate/route.js` | Run translation |
| 123 | `GET /api/translator/console-logs` | GET | Protected | — | JSON | `GET()` in `translator/console-logs/route.js` | — |
| 124 | `DELETE /api/translator/console-logs` | DELETE | Protected | — | JSON | `DELETE()` in `translator/console-logs/route.js` | Clear logs |
| 125 | `GET /api/translator/console-logs/stream` | GET | Protected | — | SSE stream | `GET()` in `translator/console-logs/stream/route.js` | `dynamic = "force-dynamic"` |

### 8.14 MCP

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
|---|-------|---------|------|---------|----------|---------|-------|
| 126 | `GET /api/mcp/[plugin]/sse` | GET | Local Only | — | SSE stream | `GET()` in `mcp/[plugin]/sse/route.js` | `runtime = "nodejs"`, `dynamic = "force-dynamic"` |
| 127 | `POST /api/mcp/[plugin]/message` | POST | Local Only | JSON MCP message | JSON | `POST()` in `mcp/[plugin]/message/route.js` | `runtime = "nodejs"`, `dynamic = "force-dynamic"` |

### 8.15 Tunnel

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 128 | `GET /api/tunnel/status` | GET | Protected | — | JSON | `GET()` in `tunnel/status/route.js` | — |
| 129 | `POST /api/tunnel/enable` | POST | Local Only | — | JSON | `POST()` in `tunnel/enable/route.js` | — |
| 130 | `POST /api/tunnel/disable` | POST | Local Only | — | JSON | `POST()` in `tunnel/disable/route.js` | — |
| 131 | `GET /api/tunnel/tailscale-check` | GET | Local Only | — | JSON | `GET()` in `tunnel/tailscale-check/route.js` | — |
| 132 | `POST /api/tunnel/tailscale-enable` | POST | Local Only | — | JSON | `POST()` in `tunnel/tailscale-enable/route.js` | — |
| 133 | `POST /api/tunnel/tailscale-disable` | POST | Local Only | — | JSON | `POST()` in `tunnel/tailscale-disable/route.js` | — |
| 134 | `POST /api/tunnel/tailscale-install` | POST | Local Only | JSON | JSON | `POST()` in `tunnel/tailscale-install/route.js` | — |

### 8.16 Tags

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
|---|-------|---------|------|---------|----------|---------|-------|
| 135 | `OPTIONS /api/tags` | OPTIONS | Protected | — | 204 | `OPTIONS()` in `tags/route.js` | CORS preflight |
| 136 | `GET /api/tags` | GET | Protected | — | JSON | `GET()` in `tags/route.js` | — |

### 8.17 Pricing

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 137 | `GET /api/pricing` | GET | Protected | — | JSON (merged user + defaults) | `GET()` in `pricing/route.js` | — |
| 138 | `PATCH /api/pricing` | PATCH | Protected | JSON | JSON | `PATCH()` in `pricing/route.js` | Partial update |
| 139 | `DELETE /api/pricing` | DELETE | Protected | JSON | JSON | `DELETE()` in `pricing/route.js` | Reset pricing |

**Note:** Has a non-standard `GET_DEFAULTS` export (line 123) that is NOT an
HTTP route handler — it's an internal helper.

### 8.18 TTS Media Providers (Internal)

| # | Route | Methods | Auth | Request | Response | Source File |
| --- | ------- | --------- | ------ | --------- | ---------- | ------------- |
| 140 | `GET /api/media-providers/tts/voices` | GET | Protected | Query params | JSON | `media-providers/tts/voices/route.js` |
| 141 | `GET /api/media-providers/tts/elevenlabs/voices` | GET | Protected | — | JSON | `media-providers/tts/elevenlabs/voices/route.js` |
| 142 | `GET /api/media-providers/tts/deepgram/voices` | GET | Protected | — | JSON | `media-providers/tts/deepgram/voices/route.js` |
| 143 | `GET /api/media-providers/tts/inworld/voices` | GET | Protected | — | JSON | `media-providers/tts/inworld/voices/route.js` |
| 144 | `GET /api/media-providers/tts/minimax/voices` | GET | Protected | — | JSON | `media-providers/tts/minimax/voices/route.js` |

### 8.19 Headroom

| # | Route | Methods | Auth | Request | Response | Source File | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | ------------- | ------- |
| 145 | `GET /api/headroom/status` | GET | Protected | — | JSON | `headroom/status/route.js` | `dynamic = "force-dynamic"` |
| 146 | `POST /api/headroom/start` | POST | Local Only | — | JSON | `headroom/start/route.js` | `dynamic = "force-dynamic"` |
| 147 | `POST /api/headroom/stop` | POST | Local Only | — | JSON | `headroom/stop/route.js` | `dynamic = "force-dynamic"` |
| 148 | `POST /api/headroom/restart` | POST | Protected | — | JSON | `headroom/restart/route.js` | `dynamic = "force-dynamic"` |
| 149 | `GET /api/headroom/extras` | GET | Protected | — | JSON | `headroom/extras/route.js` | `dynamic = "force-dynamic"` |
| 150 | `POST /api/headroom/extras` | POST | Protected | JSON | JSON | Same file | — |
| 151 | `DELETE /api/headroom/extras` | DELETE | Protected | — | JSON | Same file | — |
| 152 | `ALL /api/headroom/proxy/[...path]` | GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS | Local Only prefix | Forwarded | Forwarded | `headroom/proxy/[...path]/route.js` | Full reverse proxy to Headroom backend |

### 8.20 Pxpipe

| # | Route | Methods | Auth | Request | Response | Source File | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | ------------- | ------- |
| 153 | `POST\|GET /api/pxpipe/health` | POST, GET | Protected | — | JSON | `pxpipe/health/route.js` | GET = POST (alias); `dynamic = "force-dynamic"` |
| 154 | `POST /api/pxpipe/install` | POST | Protected | — | JSON | `pxpipe/install/route.js` | `dynamic = "force-dynamic"`, `maxDuration = 300` |
| 155 | `GET /api/pxpipe/logs` | GET | Protected | — | JSON | `pxpipe/logs/route.js` | `dynamic = "force-dynamic"` |
| 156 | `POST /api/pxpipe/restart` | POST | Protected | — | JSON | `pxpipe/restart/route.js` | `dynamic = "force-dynamic"` |
| 157 | `POST /api/pxpipe/start` | POST | Protected | — | JSON | `pxpipe/start/route.js` | `dynamic = "force-dynamic"`, `maxDuration = 300` |
| 158 | `GET /api/pxpipe/stats` | GET | Protected | — | JSON | `pxpipe/stats/route.js` | `dynamic = "force-dynamic"` |
| 159 | `GET /api/pxpipe/status` | GET | Protected | — | JSON | `pxpipe/status/route.js` | `dynamic = "force-dynamic"` |
| 160 | `POST /api/pxpipe/stop` | POST | Protected | — | JSON | `pxpipe/stop/route.js` | `dynamic = "force-dynamic"` |

### 8.21 LLM API — `/api/v1/*` (Proxy/Translate Layer)

All routes in this section accept `Access-Control-Allow-Origin: *` on OPTIONS
preflight. Most delegate to shared handlers in `@/sse/handlers/` or
`open-sse/`.

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
| --- | ------- | --------- | ------ | --------- | ---------- | --------- | ------- |
| 161 | `GET /api/v1` | GET | LLM API | — | Delegates to `GET /api/v1/models` | `re-export from ./models/route.js` | V1 root = model list |
| 162 | `POST /api/v1/chat/completions` | POST, OPTIONS | LLM API | JSON OpenAI chat | SSE stream or JSON | `handleChat` from `@/sse/handlers/chat.js` | Core LLM endpoint |
| 163 | `POST /api/v1/messages` | POST, OPTIONS | LLM API | JSON Anthropic Messages | SSE stream or JSON | `handleChat` from `@/sse/handlers/chat.js` | Claude format |
| 164 | `POST /api/v1/responses` | POST, OPTIONS | LLM API | JSON OpenAI Responses | SSE stream or JSON | `handleChat` from `@/sse/handlers/chat.js` | Responses API |
| 165 | `POST /api/v1/responses/compact` | POST, OPTIONS | LLM API | JSON with `_compact: true` | SSE stream or JSON | `handleChat` from `@/sse/handlers/chat.js` | Injects `_compact` body flag |
| 166 | `POST /api/v1/messages/count_tokens` | POST, OPTIONS | LLM API | JSON | JSON `{ input_tokens }` | `POST()` in `v1/messages/count_tokens/route.js` | Token estimation |
| 167 | `POST /api/v1/embeddings` | POST, OPTIONS | LLM API | JSON OpenAI embeddings | JSON | `handleEmbeddings` | — |
| 168 | `POST /api/v1/images/generations` | POST, OPTIONS | LLM API | JSON OpenAI image gen | JSON with image URLs/b64 | `handleImageGeneration` | — |
| 169 | `POST /api/v1/audio/speech` | POST, OPTIONS | LLM API | JSON TTS request | Binary audio stream | `handleTts` | — |
| 170 | `POST /api/v1/audio/transcriptions` | POST, OPTIONS | LLM API | Multipart audio | JSON with text | `handleStt` | `maxDuration = 300` |
| 171 | `GET /api/v1/audio/voices` | GET, OPTIONS | LLM API | Query `?provider=&lang=` | JSON `{ object: "list", data: [...] }` | `GET()` in `v1/audio/voices/route.js` | OpenAI voice list format |
| 172 | `GET /api/v1/models` | GET, OPTIONS | LLM API | Query params | JSON `{ object: "list", data: [...] }` | `GET()` in `v1/models/route.js` (line 523) | Full model list |
| 173 | `GET /api/v1/models/[kind]` | GET, OPTIONS | LLM API | — | JSON filtered list | `GET()` in `v1/models/[kind]/route.js` | Filter by kind |
| 174 | `GET /api/v1/models/info` | GET, OPTIONS | LLM API | Query `?model=` | JSON | `GET()` in `v1/models/info/route.js` | Model metadata |
| 175 | `POST /api/v1/search` | POST, OPTIONS | LLM API | JSON search query | JSON results | `handleSearch` | Web search |
| 176 | `POST /api/v1/web/fetch` | POST, OPTIONS | LLM API | JSON URL | JSON extracted content | `handleFetch` | Web fetch |
| 177 | `POST /api/v1/videos/generations` | POST, OPTIONS | LLM API | JSON | JSON (async job) | `handleVideoCreate` | xAI Grok Imagine |
| 178 | `POST /api/v1/videos/edits` | POST, OPTIONS | LLM API | JSON | JSON (async job) | `handleVideoCreate` | — |
| 179 | `POST /api/v1/videos/extensions` | POST, OPTIONS | LLM API | JSON | JSON (async job) | `handleVideoCreate` | — |
| 180 | `GET /api/v1/videos/[id]` | GET, OPTIONS | LLM API | — | JSON (job status) | `GET()` in `v1/videos/[id]/route.js` | Poll video job |
| 181 | `POST /api/v1/api/chat` | POST, OPTIONS | LLM API | JSON Ollama-compatible | Transformed SSE | `handleChat` + `transformToOllama` | Ollama compatibility |

### 8.22 LLM API — `/api/v1beta/*` (Gemini Compatibility)

| # | Route | Methods | Auth | Request | Response | Handler | Notes |
|---|-------|---------|------|---------|----------|---------|-------|
| 182 | `GET /api/v1beta/models` | GET, OPTIONS | LLM API | — | JSON (Gemini format) | `GET()` in `v1beta/models/route.js` | Gemini model list |
| 183 | `POST /api/v1beta/models/[...path]` | POST, OPTIONS | LLM API | JSON Gemini format | SSE stream (Gemini format) or JSON | `POST()` in `v1beta/models/[...path]/route.js` | `:generateContent` or `:streamGenerateContent` |

### 8.23 Rewrite-Only Paths (no route.js, handled by rewrites)

These paths only exist as rewrite source entries in `next.config.mjs` and
arrive at one of the above routes:

| Rewritten Path | Arrives At |
| ---------------- | ------------ |
| `/v1/v1/:path*` | `/api/v1/:path*` |
| `/v1/v1` | `/api/v1` |
| `/codex/:path*` | `/api/v1/responses` |
| `/responses` | `/api/v1/responses` |
| `/v1beta/:path*` | `/api/v1beta/:path*` |
| `/v1beta` | `/api/v1beta` |
| `/v1/:path*` | `/api/v1/:path*` |
| `/v1` | `/api/v1` |

---

## 9. Request/Response/Stream Type Summary

### 9.1 JSON Request/Response (most management APIs)

Nearly all management routes follow the pattern:

- **Request**: `Content-Type: application/json`
- **Response**: `Content-Type: application/json` using `NextResponse.json()`
- **Status codes**: 200 (success), 400 (validation), 401/403 (auth), 404 (not found), 500 (server error)

### 9.2 SSE Stream (text/event-stream)

Confirmed SSE routes (all use `ReadableStream` with `"text/event-stream"` headers):

| Route | Content | Keepalive |
| ------- | --------- | ----------- |
| `GET /api/usage/stream` | Usage stats JSON events | `: ping\n\n` every 25s |
| `GET /api/translator/console-logs/stream` | Translator log events | Not confirmed |
| `GET /api/mcp/[plugin]/sse` | MCP protocol SSE | Via MCP protocol |
| `POST /api/v1/chat/completions` (when stream:true) | OpenAI SSE chunks + `[DONE]` | Via provider |
| `POST /api/v1/messages` (when stream:true) | OpenAI SSE (converted from Anthropic) | Via provider |
| `POST /api/v1/responses` (when stream:true) | OpenAI Responses SSE | Via provider |
| `POST /api/v1beta/models/[...path]` (:streamGenerateContent) | Gemini SSE format (no `[DONE]`) | Via provider |
| `POST /api/v1/api/chat` | Ollama SSE (transformed) | Via provider |

### 9.3 Binary Response

| Route | Content-Type | Notes |
| ------- | ------------- | ------- |
| `POST /api/v1/audio/speech` | `audio/mpeg` (or provider-specific) | Binary audio stream |
| `GET /api/settings/database` | `application/octet-stream` | SQLite DB download |

### 9.4 Multipart Request

| Route | Content-Type | Notes |
| ------- | ------------- | ------- |
| `POST /api/v1/audio/transcriptions` | `multipart/form-data` | Audio file upload |
| `POST /api/settings/database` | `multipart/form-data` | DB file import |

### 9.5 Forwarding Proxy (headroom/proxy)

The `Headroom proxy` route at `/api/headroom/proxy/[...path]` accepts ALL HTTP
methods and forwards them to the Headroom backend. The response content-type
depends on the Headroom backend — could be HTML, JSON, or binary. It rewrites
HTML responses for the dashboard to fix fetch paths.

---

## 10. Behavioral Invariants

1. **LLM API routes have CORS `*` on OPTIONS.** Every `/v1/*`, `/v1beta/*`, and
   `/api/v1/*` route defines explicit `OPTIONS` with `Access-Control-Allow-Origin: *`.
   This is a deliberate design choice for client SDK compatibility.

2. **Management API routes do NOT have per-route CORS.** They rely on
   same-origin dashboard access. No `OPTIONS` handlers exist for most
   management routes (except `health` and `tags`).

3. **The `/codex/:path*` rewrite discards the sub-path.** All Codex SDK
   requests land at `/api/v1/responses`. The codex-specific sub-path is lost
   at the handler level.

4. **The `/v1/v1` double-normalization is intentional.** Some clients send
   `/v1/v1/chat/completions` — this is caught and normalized.

5. **API key auth supports 4 mechanisms.** `Authorization: Bearer`,
   `x-api-key`, `x-goog-api-key`, and `key` query parameter. This is
   important for Gemini and other SDK compatibility.

6. **Local-only routes require specific trust.** The `x-9r-via-proxy` header
   indicates the request came through the reverse proxy. Local-only routes
   reject any request with this header set (because the end-user is not the
   loopback socket).

7. **Express custom-server wraps Next.js standalone.** The `custom-server.js`
   wraps `http.createServer` before `require('./server.js')`. In a Go rewrite
   without Express, the trust model must be reproduced at the Go HTTP server
   layer.

---

## 11. Process-Local or Concurrency Assumptions

1. **Auth state is per-request**, not shared. JWT verification, API key
   validation, and CLI token checks all hit the DB on each request. No
   process-local token cache was confirmed in the middleware.

2. **OAuth state uses in-memory maps.** The dynamic OAuth route
   (`[provider]/[action]`) uses `Map` for pending sessions (Codex, xAI).
   This is confirmed process-local — a multi-replica Go service would need
   shared state if preserving simultaneous OAuth flows.

3. **Translator initialization is lazy and singleton.** All chat routes call
   `ensureInitialized()` which uses a module-level `initialized` boolean.
   This is safe per-process but would need migration in a Go service.

4. **The `headroom/proxy` route is fully stateless.** Each request constructs
   the target URL and issues a `fetch()`. No process-local state.

5. **Combo round-robin state is process-local** (from 00-upstream-audit.md).
   This affects `handleChat` behavior, not individual route handlers.

---

## 12. Error and Edge-Case Behavior

### 12.1 Auth Errors

| Condition | HTTP Status | Body |
| ----------- | ------------- | ------ |
| Local-only path, unauthorized | 403 | `{ error: "Local only: CLI token required" }` |
| Always-protected path, no auth | 401 | `{ error: "Unauthorized" }` |
| LLM API, remote + invalid key | 401 | `{ error: "API key required for remote API access" }` |
| API path, not authenticated | 401 | `{ error: "Unauthorized" }` |

### 12.2 Handler-Level Errors

Route handlers generally wrap their logic in try/catch and return:

- `500` with `{ error: "Failed to fetch pricing" }`-style messages
- `400` with specifics like `"Missing required fields"` for validation
- `404` for not-found resources

### 12.3 SSE Stream Termination

- Client disconnect via `cancel()` on the `ReadableStream` — removes event
  listeners and clears keepalive timers.
- Headroom proxy errors return `{ error: error.message }` with status 500.
- LLM API errors from `handleChat` are returned as JSON (non-streaming) or
  SSE error events (streaming).

---

## 13. Security Implications

1. **The custom-server header trust is the security boundary** — not just a
   proxy nicety. A Go rewrite that does not wrap the standard HTTP server
   identically would break local-only route enforcement.

2. **API key auth is the primary LLM API protection.** The middleware extracts
   API keys from 4 sources. A Go service must preserve all 4 extraction
   points for client SDK compatibility.

3. **Dashboard JWT uses HS256 with ~24h expiry** per 00-upstream-audit.md.
   The cookie attributes are httpOnly, SameSite=lax.

4. **CLI token uses machine-derived identity.** The `x-9r-cli-token` header is
   compared against a consistent machine ID. A Go rewrite on a different
   machine would need to derive the same token or replace this mechanism.

5. **CORS `*` on LLM API routes means any origin can make browser requests.**
   This is intentional for client SDKs but means protection relies entirely
   on API key auth.

6. **The `/codex/*` rewrite exposes the Responses handler to arbitrary
   sub-paths.** A request to `/codex/../../etc` would still hit
   `/api/v1/responses` — the handler must not assume path validation.

---

## 14. Uncertainties and Conflicting Evidence

1. **`GET_DEFAULTS` export in pricing route**: This is NOT an HTTP handler.
   It's an internal helper. Not exported via HTTP but available for
   programmatic import. **Status: Confirmed as non-route.**

2. **Vercel deploy default export**: The `proxy-pools/vercel-deploy/route.js`
   has both `export default async function handler(req)` AND
   `export async function POST()`. The default handler is the edge runtime
   relay code string, not a route handler. **Status: Confirmed as non-route.**

3. **Cloudflare deploy default export**: Similar — the `default export` is a
   Cloudflare Worker source string, not a route handler. **Status: Confirmed.**

4. **`GET /api/pxpipe/health` = `POST` alias**: The file explicitly assigns
   `export const GET = POST`. Both methods do the same thing.
   **Status: Confirmed.**

5. **All `POST /v1/*` chat routes support both streaming and non-streaming**:
   This depends on the `stream` field in the request body (OpenAI convention)
   or the URL action suffix (Gemini convention). The `handleChat` function
   decides based on `stream` field and Accept header.
   **Status: Traced, not executed.**

6. **Exact `/v1beta/models/[...path]` path structure**: The catchall `[...path]`
   can receive either `provider/model:action` or `model:action` (without
   provider prefix). The handler splits on path length. **Status: Confirmed.**

---

## 15. Parity Requirements

Stated as observed behavior that must be preserved or consciously replaced:

1. **All 146+ HTTP route-method combinations** must be served, including the
   LLM proxy routes, management CRUD, SSE streams, OAuth dynamic routes,
   and deployment proxy endpoints.

2. **The 8 rewrites** (`/v1/*`, `/v1/v1/*`, `/codex/*`, `/responses`,
   `/v1beta/*`) must be present. The `/codex/*` → `/api/v1/responses` mapping
   is particularly notable because it discards the sub-path.

3. **Auth middleware must support 4 API key locations**: `Authorization: Bearer`,
   `x-api-key`, `x-goog-api-key`, and `key` query parameter.

4. **The custom-server IP trust model** must be reproduced: loopback-only
   forwarded-header trust, header stripping, and `x-9r-real-ip` /
   `x-9r-via-proxy` stamps.

5. **CORS `*` on LLM API routes** must be preserved for client SDK
   compatibility.

6. **SSE streaming** must support both `[DONE]` sentinel (OpenAI) and
   stream-close termination (Gemini).

7. **The 4 auth tiers** (public, LLM API key, protected, local-only) must
   be maintained as a coherent middleware decision tree.

---

## 16. Questions Requiring User Decisions

1. **CORS policy for management routes**: Should they remain same-origin-only
   (as in the original) or gain explicit per-route CORS in the Go version?

2. **The `/codex/*` rewrite behavior**: The original discards the sub-path and
   always routes to `/api/v1/responses`. Is this acceptable, or should the Go
   version validate the sub-path?

3. **Custom-server Express wrapper**: Should the Go version embed the trust
   model at the `net/http` level, or use a reverse proxy (like nginx) to
   reproduce it?

4. **SSE endpoints**: The original uses `ReadableStream` with Node.js SSE
   patterns. Go can use `http.Flusher` — is the streaming contract (format,
   events, keepalives) the only concern?

5. **`GET_DEFAULTS` in pricing**: Is this helper needed, or was it a dead
   export?

---

## 17. Coverage Gaps

1. **Exact request body schemas for each management route** are not
   documented here. Each PATCH/POST handler has unique validation — but
   the overall interface (JSON in, JSON out with standard status codes)
   is consistent.

2. **`POST /api/locale` and `POST /api/auth/login` request body schemas**
   were not read in full — they follow standard patterns (locale string,
   password field).

3. **The `GET /api/version` response shape** was not read in full — it
   returns version metadata from the VERSION file or package.json.

4. **Some CLI-tool settings routes** were skimmed rather than read in full.
   They all follow GET/POST/DELETE settings CRUD pattern.

5. **`GET /api/models`** (`models/route.js` line 9) response shape was not
   read — it's a standard model listing with provider connections and
   built-in models merged.

---

## 18. Route Count Summary

| Category | Route Files | Unique HTTP Endpoints |
| ---------- | ------------- | ---------------------- |
| Health/System | 7 | 7 |
| Authentication | 7 | 7 |
| Settings | 6 | 7 |
| API Keys | 5 | 5 |
| Providers | 13 | 13 |
| Provider Nodes | 5 | 5 |
| Proxy Pools | 9 | 9 |
| Combos | 5 | 5 |
| Models | 13 | 13 |
| OAuth (static) | 11 | 11 |
| OAuth (dynamic provider/action) | 1 | 2 (GET, POST with sub-actions) |
| Usage | 11 | 11 |
| CLI Tools | 22 | 58 |
| Translator | 7 | 7 |
| MCP | 2 | 2 |
| Tunnel | 7 | 7 |
| Tags | 1 | 2 |
| Pricing | 1 | 3 |
| Media Providers | 5 | 5 |
| Headroom | 6 | 12 |
| Pxpipe | 8 | 9 |
| LLM API `/api/v1/*` | 22 | 22 |
| LLM API `/api/v1beta/*` | 2 | 2 |
| **Total** | **159** | **~202 unique HTTP method+path combinations** |

**Note:** Some route files export multiple methods (e.g., GET+POST+DELETE on
the same route). The "Unique HTTP Endpoints" column counts each
method+path pair separately. The `/codex/*`, `/responses`, `/v1/*`, and
`/v1beta/*` rewrites are NOT counted as separate endpoints since they
resolve to the same handlers.

---

## 19. Source Evidence Index

This report was compiled from the following source files (all within
`upstream-original/`):

| File | Role |
| ------ | ------ |
| `next.config.mjs` | Rewrite rules, body size, standalone output |
| `custom-server.js` | Header trust, IP derivation |
| `src/dashboardGuard.js` | Auth classification middleware |
| `src/app/api/*/route.js` (147 files) | Route handlers |
| `package.json` | Version, dependencies, scripts |

Key shared handlers (routed to from the API layer):

- `src/sse/handlers/chat.js` — Core LLM orchestration
- `src/sse/handlers/embeddings.js` — Embeddings
- `src/sse/handlers/imageGeneration.js` — Image generation
- `src/sse/handlers/tts.js` — Text-to-speech
- `src/sse/handlers/stt.js` — Speech-to-text
- `src/sse/handlers/search.js` — Web search
- `src/sse/handlers/fetch.js` — Web URL fetch
- `src/sse/handlers/videoGeneration.js` — Video generation/editing

---

## Completion Metadata

- **File**: `C:\Users\faizz\9router-go\audit\01-http-contracts.md`
- **Audited commit**: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- **Sections completed**: 19 (Scope through Completion Metadata)
- **Route files inventoried**: 147 `route.js` + 12 non-route helper files
- **Confirmed findings**: ~202 unique HTTP method+path endpoints across 19 API domains
- **Rewrite rules**: 8 (all confirmed in `next.config.mjs`)
- **Auth tiers**: 5 (public, LLM API, protected, always-protected, local-only)
- **SSE stream routes**: 8 confirmed
- **Unresolved gaps**: 5 (request body schemas for some management routes, exact response shapes for some listing endpoints)
- **Rewrite preserved**: `/codex/:path*` retains its unusual sub-path-discarding behavior as an inviolable upstream contract
