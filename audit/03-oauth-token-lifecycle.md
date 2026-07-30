# OAuth and Token Lifecycle -- Source-Backed Report

## Scope

Complete OAuth and credential/token lifecycle analysis: CLI, dashboard, and server ownership; callback mechanics, port allocation, state lifetime, token refresh, deduplication, persistence fields, error handling, and edge-case behavior.

## Audited Repository and Exact Commit

- Repository: decolua/9router
- Exact commit: 79918c7830695bbca4a45c9fea4a42c3e9fd73d1
- Tag: v0.5.40 (2026-07-20)
- Read-only clone: ../upstream-original

## Method

1. OAuth handler inventory: read src/lib/oauth/providers.js PROVIDERS object (20 providers).
2. OAuth config extraction: read src/lib/oauth/constants/oauth.js for all OAuth_CONFIG objects.
3. Fixed-port proxy analysis: read src/lib/oauth/utils/server.js (Codex and xAI proxies).
4. Callback page analysis: read src/app/callback/page.js (dashboard-side callback).
5. Route analysis: read src/app/api/oauth/[provider]/[action]/route.js (API-side OAuth flow).
6. Token persistence: read src/lib/db/repos/connectionsRepo.js (create/update/delete connections).
7. Token refresh: read src/sse/services/tokenRefresh.js (shouldRefreshCredentials, checkAndRefreshToken).
8. CLI OAuth: read cli/src/cli/menus/providers.js (CLI provider menu and OAuth flows).
9. PKCE utilities: read src/lib/oauth/utils/pkce.js.
10. Schema: read src/lib/db/schema.js providerConnections table definition.

## OAuth Flow Architecture

The application has three ownership contexts for OAuth flows:

### A. Dashboard (Browser) OAuth

Flow:

1. User clicks "Connect" on dashboard provider page.
2. Modal opens, calls GET /api/oauth/[provider]/authorize (or device-code).
3. For authorization_code flows: browser popup to provider auth URL with redirect_uri.
4. Provider redirects to /callback page (Next.js route).
5. Callback page relays code/state via postMessage, BroadcastChannel, localStorage.
6. Dashboard modal receives code, calls POST /api/oauth/[provider]/exchange.
7. Server exchanges code for tokens via src/lib/oauth/providers.js exchangeTokens().
8. Tokens saved via createProviderConnection() in src/lib/db/repos/connectionsRepo.js.

Source: src/app/api/oauth/[provider]/[action]/route.js (GET action="authorize", POST action="exchange").

### B. CLI OAuth

Flow:

1. User selects provider from cli/src/cli/menus/providers.js menu.
2. CLI calls same API routes (/api/oauth/[provider]/authorize or device-code).
3. For device_code flows: CLI opens browser with verification_uri, polls POST /api/oauth/[provider]/poll.
4. For authorization_code flows: CLI starts local server (src/lib/oauth/utils/server.js startLocalServer()).
5. Token exchange and persistence handled by server-side API routes.

Source: cli/src/cli/menus/providers.js (OAUTH_PROVIDERS map, showProviderDetail).

### C. Server-Side Proxy (Codex and xAI)

For Codex and xAI, OAuth has an additional server-side proxy mode:

1. Dashboard opens popup, calls GET /api/oauth/[provider]/start-proxy with app_port/state/codeVerifier/redirectUri.
2. Server starts a local HTTP server on a fixed port (1455 for Codex, 56121 for xAI).
3. Session registered in pendingExchanges Map (keyed by state).
4. OAuth provider redirects to the fixed port.
5. Proxy auto-exchanges code, saves connection to DB, updates session status.
6. Dashboard polls GET /api/oauth/[provider]/poll-status until status="done".

Source: src/lib/oauth/utils/server.js (startCodexProxy, startXaiProxy).

## Callback Mechanics

### Standard Callbacks (Dashboard/CLI)

The callback page (src/app/callback/page.js) receives OAuth callbacks and relays data via:

1. postMessage to window.opener (popup mode) -- sent per allowlisted origin.
2. BroadcastChannel("oauth_callback") -- for same-origin tabs.
3. localStorage.setItem("oauth_callback") -- fallback.

Origin allowlist: [window.location.origin, "http://localhost:1455"].

Callback parameters relayed: code, token, state, error, error_description, fullUrl.

### Fixed-Port Proxy Callbacks (Codex and xAI)

Mode A (server-side, session registered):

- Server auto-exchanges code for tokens, saves connection, updates session status.
- Renders success/error HTML page to browser.
- Proxy shuts down after completion.

Mode B (legacy, no session):

- HTTP 302 redirect to <http://localhost:{appPort}/callback> with original query params.
- Proxy shuts down after redirect.

Source: src/lib/oauth/utils/server.js lines 135-225 (Codex), lines 286-365 (xAI).

Both use same pattern with different constants:

- Codex proxy: CODEX_PORT=1455, CODEX_PROXY_TIMEOUT_MS=300000, pendingExchanges Map.
- xAI proxy: XAI_PROXY_PORT=56121, XAI_PROXY_TIMEOUT_MS=300000, xaiPendingExchanges Map.

### Local Server (CLI-only)

The startLocalServer() function (src/lib/oauth/utils/server.js lines 1-90):

- Creates http.Server listening on 127.0.0.1.
- Fixed port or random (port 0).
- Handles /callback and /auth/callback paths only.
- Renders auto-close HTML page on success.
- Returns { server, port, close }.

## State Lifetime

### OAuth State (CSRF Protection)

Generated by generatePKCE() in src/lib/oauth/utils/pkce.js:

- crypto.randomBytes(32).toString("base64url")
- 43-character string.

State is sent in the auth URL and validated on callback. Lifetime is the duration of the browser session or proxy timeout.

### PKCE Code Verifier

Generated by generateCodeVerifier():

- Default: crypto.randomBytes(32).toString("base64url") (43 chars).
- xAI override: XAI_PKCE_VERIFIER_BYTES = 96 (128 chars).
- In src/lib/oauth/constants/xai.js.

Code verifier is stored client-side (browser memory or CLI variable). Not persisted to DB.

### Proxy Session Lifetime

- Codex proxy: 5 minute timeout (CODEX_PROXY_TIMEOUT_MS = 300000).
- xAI proxy: 5 minute timeout (XAI_PROXY_TIMEOUT_MS = 300000).
- Sessions stored in process-local Maps (pendingExchanges, xaiPendingExchanges).
- Cleared after completion or error.

## Token Refresh

### Proactive Refresh

src/sse/services/tokenRefresh.js -- checkAndRefreshToken():

1. Calls shouldRefreshCredentials() (from open-sse/services/oauthCredentialManager.js).
2. Computes remaining time: expiresAt - Date.now().
3. Compares against getRefreshLeadMs(provider) (from open-sse/services/tokenRefresh.js).
4. If remaining < lead time, calls refreshProviderCredentials().
5. Updates localDb via updateProviderCredentials().

Refresh lead times (from open-sse/services/tokenRefresh.js / REFRESH_LEAD_MS):

- codex: 432000000 ms (5 days, defined in codex registry entry oauth.refreshLeadMs).
- xai/grok-cli: 300000 ms (5 minutes, XAI_REFRESH_LEAD_SECONDS=5*60).
- Other providers: defaults vary.

### Reactive Refresh

When a 401 response is received during API call, chatCore.js and other handlers call checkAndRefreshToken() before retrying:

- src/sse/handlers/chat.js:218
- src/sse/handlers/embeddings.js:108
- src/sse/handlers/fetch.js:181
- src/sse/handlers/imageGeneration.js:108
- src/sse/handlers/search.js:171
- src/sse/handlers/videoGeneration.js:134, 196

### Provider-Specific Refresh Functions (re-exported via open-sse)

- refreshClaudeOAuthToken(refreshToken)
- refreshGoogleToken(refreshToken, clientId, clientSecret) -- for gemini-cli, antigravity
- refreshQwenToken(refreshToken)
- refreshCodexToken(refreshToken)
- refreshIflowToken(refreshToken)
- refreshGitHubToken(refreshToken)
- refreshCopilotToken(githubAccessToken) -- special: exchanges GitHub access token for Copilot token
- refreshKiroToken(refreshToken, providerSpecificData)

Source: src/sse/services/tokenRefresh.js imports from open-sse/services/tokenRefresh.js.

### GitHub Copilot Dual Refresh

Special two-phase refresh (src/sse/services/tokenRefresh.js refreshGitHubAndCopilotTokens()):

1. Refresh GitHub OAuth token via refreshGitHubToken().
2. Exchange new GitHub access token for Copilot token via refreshCopilotToken().
3. Both stored in connection.providerSpecificData (copilotToken, copilotTokenExpiresAt).

## Token Persistence

### Schema

SQLite table providerConnections (src/lib/db/schema.js):

```sql
