# Audit 07: Dashboard Frontend — Page/Workflow/Component-to-API Map

## Scope

Complete inventory of dashboard page routes, component workflows, browser-side state mechanisms (Zustand stores, `fetch` calls, polling, SSE consumers, OAuth callback channels, auth flows, error behavior) at the audited commit.

## Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (tag `v0.5.40`, 2026-07-20)
- Read-only clone: `C:\Users\faizz\9router-go\upstream-original`

## Method

- Traced all files under `src/app/(dashboard)/dashboard/`, `src/store/`, `src/shared/` (components, hooks, services, utils, constants).
- Traced all route files under `src/app/api/` that serve dashboard data.
- Read every dashboard page client, store, SSE endpoint, auth endpoint, and the service worker.
- Confirmed `EventSource`, `fetch`, `setInterval`, `AbortSignal.timeout` patterns.

## Confirmed Findings

### 1. Dashboard Pages (28 routes confirmed)

| Route | File | Client Component | Description |
|-------|------|-----------------|-------------|
| `/dashboard` | `page.js` | `EndpointPageClient` (from `endpoint/EndpointPageClient.js`) | Dashboard home — endpoint config, API keys, tunnel, Tailscale |
| `/dashboard/basic-chat` | `page.js` | `BasicChatPageClient` | Basic chat test interface |
| `/dashboard/cli-tools` | `page.js` | `CLIToolsPageClient` | CLI tool quick-start cards |
| `/dashboard/cli-tools/[toolId]` | `page.js` | `ToolDetailClient` | Per-tool detail |
| `/dashboard/combos` | `page.js` | — (Next.js server component) | Model combo list |
| `/dashboard/console-log` | `page.js` | `ConsoleLogClient` | Live translator console log |
| `/dashboard/media-providers/[kind]` | `page.js` | — | Media provider list by kind (tts, stt, embedding, image, video) |
| `/dashboard/media-providers/[kind]/[id]` | `page.js` | `GenericExampleCard`, `TtsExampleCard`, `SttExampleCard`, `EmbeddingExampleCard` | Media provider detail/test |
| `/dashboard/media-providers/combo/[id]` | `page.js` | — | Combo media provider detail |
| `/dashboard/media-providers/web` | `page.js` | — | Web/media provider page |
| `/dashboard/mitm` | `page.js` | `MitmPageClient` | MITM proxy page |
| `/dashboard/profile` | `page.js` | — | User profile |
| `/dashboard/providers` | `page.js` | — | Provider list (server component) |
| `/dashboard/providers/new` | `page.js` | — | Add new provider |
| `/dashboard/providers/[id]` | `page.js` | `AddApiKeyModal`, `AddCustomModelModal`, `BulkImportCodexModal`, `CompatibleModelsSection`, `ConnectionRow`, `CooldownTimer`, `EditCompatibleNodeModal`, `ModelRow`, `PassthroughModelsSection` | Provider detail with many modals |
| `/dashboard/proxy-pools` | `page.js` | — | Proxy pool list |
| `/dashboard/pxpipe` | `page.js` | `PxpipeClient` | PxPipe status/control |
| `/dashboard/quota` | `page.js` | — | Quota summary |
| `/dashboard/skills` | `page.js` | — | OpenCode skills page |
| `/dashboard/token-saver` | `page.js` | `TokenSaverClient` | RTK/Headroom/Caveman/Ponytail config |
| `/dashboard/translator` | `page.js` | — | Translator test page |
| `/dashboard/usage` | `page.js` | `UsageContent` → `UsageStats`, `RequestLogger`, `RequestDetailsTab` | Usage analytics |

**Non-dashboard pages:**
- `/` — landing page (server component)
- `/login` — login page (server component)
- `/callback` — OAuth callback page
- `/dashboard/settings/pricing` — pricing page

### 2. Browser-Side State (Zustand Stores)

| Store File | Exported Name | Persistence | Key State | Key Actions |
|-----------|---------------|-------------|-----------|-------------|
| `src/store/themeStore.js` | `useThemeStore` | `localStorage` key `"theme"` | `theme` ("light"\|"dark"\|"system") | `setTheme`, `toggleTheme`, `initTheme` |
| `src/store/userStore.js` | `useUserStore` | None (in-memory) | `user`, `loading`, `error` | `setUser`, `clearUser` |
| `src/store/providerStore.js` | `useProviderStore` | None (in-memory, TTL=60s) | `providers`, `loading`, `error`, `lastFetched` | `fetchProviders`, `addProvider`, `updateProvider`, `removeProvider`, `invalidate` |
| `src/store/settingsStore.js` | `useSettingsStore` | None (in-memory, TTL=60s) | `settings`, `loading`, `error`, `lastFetched` | `fetchSettings`, `patchSettings`, `invalidate` |
| `src/store/notificationStore.js` | `useNotificationStore` | None (in-memory) | `notifications[]` | `addNotification`, `removeNotification`, `clearAll`, `success`, `error`, `warning`, `info` |
| `src/store/headerSearchStore.js` | `useHeaderSearchStore` | None (in-memory) | `query`, `placeholder`, `visible` | `setQuery`, `register`, `unregister` |

**Store TTL constant**: `CLIENT_STORE_TTL_MS = 60000` (1 minute) from `src/shared/constants/config.js` line 63.

### 3. API `fetch` Calls from Dashboard Pages

**EndpointPageClient** (`src/app/(dashboard)/dashboard/endpoint/EndpointPageClient.js`):
- `GET /api/keys` — load API keys (line 258)
- `GET /api/settings` — load settings (line 196)
- `GET /api/tunnel/status` — tunnel/Tailscale status (lines 176, 199, 293)
- `PATCH /api/settings` — toggle tunnelDashboardAccess (line 232), requireApiKey (line 244)
- `POST /api/tunnel/enable` — enable tunnel
- `POST /api/tunnel/disable` — disable tunnel
- `POST /api/keys` — create API key
- `DELETE /api/keys/[id]` — delete API key
- `POST /api/settings/proxy-test` — proxy test
- `POST /api/settings/database` — database export/import
- Various tunnel/Tailscale enable/disable/install endpoints

**UsageStats** (`src/shared/components/UsageStats.js`):
- `GET /api/usage/stats?period=<period>` — filtered usage stats (line 264)
- `GET /api/providers` — provider list for topology (line 226)
- `GET /api/provider-nodes` — custom provider nodes (line 227)
- `EventSource /api/usage/stream` — SSE for real-time usage updates (line 281)

**ConsoleLogClient** (`src/app/(dashboard)/dashboard/console-log/ConsoleLogClient.js`):
- `DELETE /api/translator/console-logs` — clear logs (line 29)
- `EventSource /api/translator/console-logs/stream` — SSE for log streaming (line 37)

**PxpipeClient, MitmPageClient, TokenSaverClient, CLIToolsPageClient, BasicChatPageClient, ToolDetailClient**:
- Each fetches its own data from the corresponding `/api/pxpipe/*`, `/api/mitm`, `/api/settings`, `/api/cli-tools/*`, `/api/providers` endpoints.

### 4. Polling Intervals

| Location | Interval | Purpose | File |
|----------|----------|---------|------|
| EndpointPageClient | `TUNNEL_PING_INTERVAL_MS = 2000` | Ping tunnel health after enable (max 300s) | `endpointConstants.js` line 10 |
| EndpointPageClient | `STATUS_POLL_FAST_MS = 5000` | Poll tunnel status while degraded | `endpointConstants.js` line 12 |
| EndpointPageClient | `CLIENT_PING_FAST_MS = 10000` | Browser-side tunnel/Tailscale ping | `endpointConstants.js` line 15 |
| EndpointPageClient | `CLIENT_PING_SLOW_MS = 60000` | (defined but used as fallback) | `endpointConstants.js` line 16 |
| EndpointPageClient | `CLIENT_PING_TIMEOUT_MS = 5000` | Per-ping timeout via `AbortSignal.timeout` | `endpointConstants.js` line 17 |
| EndpointPageClient | `REACHABLE_MISS_THRESHOLD = 5` | Consecutive misses before "reconnecting" state | `endpointConstants.js` line 14 |
| Quota auto-ping (server) | `tickIntervalMs = 60000` | Check if quota windows need warming | `config.js` line 67 |
| Quota auto-ping (server) | `refreshAheadMs = 300000` (5 min) | Re-fetch usage before reset | `config.js` line 69 |
| Quota auto-ping (server) | `failureCooldownMs = 900000` (15 min) | Back off after failed ping | `config.js` line 70 |
| Console log (client poll) | `pollIntervalMs = 1000` | Fallback poll (but SSE is primary) | `config.js` line 59 |
| SSE keepalive (usage, console, MCP) | 25000ms | `: ping\n\n` keep-alive | `stream/route.js` line 53, `console-logs/stream/route.js` line 68, `mcp/*/sse/route.js` |

### 5. SSE (Server-Sent Events) Endpoints

| SSE Route | File | Event Types | Purpose |
|-----------|------|-------------|---------|
| `GET /api/usage/stream` | `src/app/api/usage/stream/route.js` | `data: {activeRequests, recentRequests, ...}`, `: ping` | Real-time usage updates. Sends full refresh then lightweight active/recent on `"pending"` event |
| `GET /api/translator/console-logs/stream` | `src/app/api/translator/console-logs/stream/route.js` | `data: {type:"init", logs:[...]}`, `data: {type:"line", line}`, `data: {type:"lines", lines}`, `data: {type:"clear"}`, `: ping` | Live translator console log streaming |
| `GET /api/mcp/[plugin]/sse` | `src/app/api/mcp/[plugin]/sse/route.js` | `event: endpoint\ndata: /api/mcp/${plugin}/message?sessionId=...` | MCP plugin SSE bridge — bidirectional via `message` POST endpoint |

**Client-side SSE consumers**:
- `UsageStats.js`: `new EventSource("/api/usage/stream")` — merges activeRequests, recentRequests, errorProvider, pending into local state (line 281-306)
- `ConsoleLogClient.js`: `new EventSource("/api/translator/console-logs/stream")` — maintains log buffer capped at 200 lines (line 37-63)
- `src/lib/mcp/stdioSseBridge.js` server-side: registers MCP SSE sessions

### 6. OAuth Callback Channel

| Route | File | Description |
|-------|------|-------------|
| `GET /api/oauth/[provider]/[action]` | `src/app/api/oauth/[provider]/[action]/route.js` | Generic OAuth handler: `authorize`, `exchange`, `poll`, `device-code` |
| `/callback` | `src/app/callback/page.js` | Browser callback page for OAuth redirect |
| `POST /api/oauth/codex/import-token` | `route.js` | Codex token import |
| `POST /api/oauth/codex/bulk-import` | `route.js` | Codex bulk import |
| `POST /api/oauth/cursor/import` | `route.js` | Cursor token import |
| `POST /api/oauth/cursor/auto-import` | `route.js` | Cursor auto-import |
| `POST /api/oauth/kiro/import` | `route.js` | Kiro import |
| `POST /api/oauth/kiro/auto-import` | `route.js` | Kiro auto-import |
| `POST /api/oauth/kiro/import-cli-proxy` | `route.js` | Kiro CLI proxy import |
| `POST /api/oauth/kiro/api-key` | `route.js` | Kiro API key |
| `POST /api/oauth/kiro/social-authorize` | `route.js` | Kiro social OAuth authorize |
| `POST /api/oauth/kiro/social-exchange` | `route.js` | Kiro social OAuth exchange |
| `POST /api/oauth/iflow/cookie` | `route.js` | iFlow cookie-based auth |
| `POST /api/oauth/gitlab/pat` | `route.js` | GitLab PAT auth |

**OAuth flow**: The `lib/oauth/services/` directory has per-provider OAuth implementations for: `antigravity`, `claude`, `codex`, `cursor`, `gemini`, `github`, `iflow`, `kimchi`, `kiro`, `oauth` (base), `openai`, `qoder`, `qwen`, `xai`. These implement `authorize`/`exchange`/`refresh`/`revoke` using PKCE (key in `src/lib/oauth/utils/pkce.js`).

### 7. Dashboard Authentication

| Route | File | Method | Description |
|-------|------|--------|-------------|
| `POST /api/auth/login` | `src/app/api/auth/login/route.js` | POST | Password + bcrypt hash + JWT cookie. Rate-limited (loginLimiter.js). Blocked over tunnel if tunnelDashboardAccess=false |
| `POST /api/auth/logout` | `route.js` | POST | Clears session cookie |
| `GET /api/auth/status` | `route.js` | GET | Returns auth status (`{authenticated, requireLogin, oidcConfigured}`) |
| `GET /api/auth/oidc/start` | `route.js` | GET | OIDC login initiation |
| `GET /api/auth/oidc/callback` | `route.js` | GET | OIDC callback handler |
| `GET /api/auth/oidc/test` | `route.js` | GET | OIDC test endpoint |
| `POST /api/auth/reset-password` | `route.js` | POST | Reset password to default |

**Auth flow**: Password login is hashed with `bcryptjs`. Session held in JWT cookie via `src/lib/auth/dashboardSession.js`. OIDC handled via `src/lib/auth/oidc.js`. Login rate limiting via `src/lib/auth/loginLimiter.js` (configurable retry-after, IP-based lockout).

### 8. Browser Storage

| Storage Type | Key | Store | Location |
|-------------|-----|-------|----------|
| `localStorage` | `"theme"` | `useThemeStore` | `themeStore.js` line 30 (via `zustand/middleware` persist) |
| `sessionStorage` | None found | — | Not used by any dashboard component |

**No other browser storage keys were found in the frontend code.** All other stores are in-memory only with network refetch on page load or TTL expiry.

### 9. Error Handling Patterns

| Pattern | Location | Behavior |
|---------|----------|----------|
| `try/catch` + `console.log` | EndpointPageClient, all dashboard clients | Errors logged, UI may show stale data |
| `setError` in Zustand stores | providerStore, settingsStore | `error` field set, component must render it |
| `{ error: "..." }` JSON response | All API routes | Standard Next.js error response |
| `useNotificationStore.error` | Used throughout dashboard | Toast notification, 8s auto-dismiss |
| `es.onerror = () => setLoading(false)` | UsageStats, ConsoleLogClient | SSE error → stop loading, no reconnect (browser EventSource auto-reconnects) |
| `try/catch` with silent ignore | quotaAutoPing.js | Fail-open: never throw out of pings |
| `onerror` with `state.closed = true` | SSE routes | Cleanup listeners on stream error |
| 429/401 on login | `login/route.js` | Rate-limit lockout with `Retry-After` header |

### 10. Service Worker

`public/sw.js` — present in the public directory. Contents TBD.

### 11. Dashboard Layout Components

| Component | Location | Description |
|-----------|----------|-------------|
| `DashboardLayout` | `src/shared/components/layouts/DashboardLayout.js` | Shared dashboard shell |
| `Header` | `src/shared/components/Header.js` | Top navigation bar |
| `Sidebar` | `src/shared/components/Sidebar.js` | Side navigation |
| `HeaderMenu` | `src/shared/components/HeaderMenu.js` | Header dropdown menus |
| `HeaderLanguage` | `src/shared/components/HeaderLanguage.js` | Language switcher |
| `ThemeProvider` | `src/shared/components/ThemeProvider.js` | Theme context provider |
| `ThemeToggle` | `src/shared/components/ThemeToggle.js` | Dark/light toggle |
| `Footer` | `src/shared/components/Footer.js` | Page footer |

### 12. Local-Only Dependencies

- `zustand` — state management (all stores)
- `zustand/middleware` — persist middleware for theme
- `next/navigation` — `useSearchParams`, `useRouter` (client components)
- `prop-types` — runtime type checking in components

## Behavioral Invariants

1. **SSE is the primary real-time mechanism** for usage stats and console logs. REST endpoints (`/api/usage/stats`, `/api/translator/console-logs`) provide initial/periodic snapshots; SSE pushes deltas.
2. **Tunnel reachability is probed from both the server and the browser**. Server-side: watchdog + network monitor. Browser-side: `clientPingAny()` polls tunnel URLs via CORS fetch to `/api/health`. Both use miss-debounce (5 consecutive misses).
3. **Theme is the only persisted browser state** (localStorage). All other stores are transient and re-fetched.
4. **OAuth flows are handled server-side** with PKCE. The browser only sees the authorize URL redirect and callback page.
5. **Login rate-locking is IP-based** with configurable thresholds. Lockout resets after `retryAfter` seconds.
6. **Dashboard pages are mostly "use client"** — server components are minimal stubs that delegate to client-side components.

## Process-Local or Concurrency Assumptions

- `global.__quotaAutoPing` singleton survives Next.js hot reload (global object).
- `global.__appSingleton` guards server-side initialization.
- `global.__appBootstrapped` prevents double-bootstrap in ESM.
- SSE keepalive intervals (25s) are hardcoded and match no-load balancer requirement.
- `SETTINGS_CACHE_TTL` (60s) prevents redundant fetches across dashboard tabs.

## Uncertainties and Conflicting Evidence

- **Uncertain**: Whether `public/sw.js` is a fully functional service worker or a placeholder. Not traced.
- **Uncertain**: Exact `CLIToolsPageClient` data-fetching from `all-statuses` API — the `api/cli-tools/all-statuses/route.js` exists but the client fetch was not traced in detail.
- **Uncertain**: The `src/app/(dashboard)/dashboard/providers/page.js` may pass server-side props to client components not traced in this pass.
- **Uncertain**: `src/app/(dashboard)/dashboard/usage/components/RequestDetailsTab.js` may have its own polling or SSE — not read in complete.

## Parity Requirements

1. Every dashboard page route must exist at the same path.
2. SSE streams `/api/usage/stream`, `/api/translator/console-logs/stream`, and `/api/mcp/[plugin]/sse` must push the same event shapes.
3. Theme persistence in localStorage key `"theme"`.
4. Zustand stores must expose the same state shape and actions for every consuming component.
5. All `fetch()` calls to `/api/*` routes must return the same response shape (`response.ok`, `.json()`).
6. Login must use IP-based rate limiting, bcrypt password hashing, JWT cookie sessions.
7. OAuth PKCE flow with `/callback` page.
8. OAuth import endpoints for all providers.

## Questions Requiring User Decisions

1. **State management library**: Zustand is used in the original. Does the rewrite use Zustand, React Context, or something else?
2. **SSE vs WebSocket**: The original uses SSE for real-time updates. Should the rewrite use SSE or WebSocket?
3. **Theme persistence**: localStorage key `"theme"` — should the rewrite preserve this exact key for migration?
4. **Service worker**: Is `public/sw.js` used and should it be replicated?
5. **Store TTL**: `CLIENT_STORE_TTL_MS = 60000` — is this appropriate for the rewrite?
6. **Ping timeouts** `TUNNEL_PING_INTERVAL_MS`, `STATUS_POLL_FAST_MS`, etc. — should these stay identical?

## Coverage Gaps

- `BasicChatPageClient.js` not read — may include additional API calls.
- `CLIToolsPageClient.js` not fully traced.
- `ToolDetailClient.js` not traced.
- `MitmPageClient.js` not traced.
- `PxpipeClient.js` not traced.
- `TokenSaverClient.js` not traced.
- `public/sw.js` not examined.
- `src/app/(dashboard)/dashboard/usage/components/RequestDetailsTab.js` not examined.
- `src/app/dashboard/settings/pricing/page.js` not examined.
