# 11 — Runtime Background Jobs

## Scope

All initialization sequences, timers, interval listeners, signal
handlers, caches, watchers, state machines, process-local mutable
state, and lifecycle cleanup at the audited commit.

## Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Exact commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- Read-only clone: `upstream-original/`

## Method

Manual source inspection of all files that register timers, intervals,
signal handlers, or global mutable state at module load time or during
initialization.

## Confirmed Findings

### 1. Application Bootstrap (Entry Point)

**File:** `src/shared/services/bootstrap.js` (12 lines)

```javascript
if (typeof window === "undefined" && !isBuildPhase && !global.__appBootstrapped) {
  global.__appBootstrapped = true;
  initializeApp().catch((e) => console.error("[Bootstrap] init failed:", e.message));
}
```

- Guarded against Next.js build phases (`phase-production-build`,
  `phase-export`, `phase-static`).
- Singleton via `global.__appBootstrapped` to survive HMR re-imports.
- Called as a module side-effect (imported by Next.js during server
  startup).

### 2. Core Initialization Sequence

**File:** `src/shared/services/initializeApp.js` (330 lines)

#### 2a. Global Singleton State

```javascript
const g = global.__appSingleton ??= {
  signalHandlersRegistered: false,
  watchdogInterval: null,
  networkMonitorInterval: null,
  lastNetworkFingerprint: null,
  lastWatchdogTick: Date.now(),
  lastOnline: null,
  mitmStartInProgress: false,
  tunnelAutoResumed: false,
  tailscaleAutoResumed: false,
};
```

This object lives on `global.__appSingleton` and survives Next.js HMR.
All background jobs read from and write to this shared mutable state.

#### 2b. Signal Handlers (lines 56–67)

Registered once per process:

| Signal | Action |
| -------- | -------- |
| `SIGINT` | `removeAllDNSEntriesSync()`, `killAllBridges()`, `killCloudflared()`, `process.exit()` |
| `SIGTERM` | Same as SIGINT |
| `exit` | `removeAllDNSEntriesSync()` (best-effort) |

The cleanup function is constructed as a closure and registered via
`process.on()`. `killAllBridges()` from `src/lib/mcp/stdioSseBridge.js`
is called on SIGINT/SIGTERM but not on plain `exit`.

#### 2c. Tunnel Unexpected-Exit Callback (line 69–71)

```javascript
setTunnelUnexpectedExitCallback(() => {
  safeRestartTunnel("unexpected-exit").catch(() => {});
});
```

This wires a per-process restart trigger from the cloudflared/tailscale
management layer.

#### 2d. Deferred Heavy Startup (line 74–76)

```javascript
setTimeout(() => {
  runHeavyStartup().catch(...)
}, STARTUP_DEFER_MS);  // 3000ms
```

All non-critical startup is deferred by 3 seconds so the first HTTP
request is not blocked.

#### 2e. `runHeavyStartup()` — Deferred Work (lines 82–115)

Executed after 3s delay:

1. `cleanupProviderConnections()` — DB cleanup for stale connection
   states.
2. Auto-resume tunnel if previously enabled (once per process via
   `g.tunnelAutoResumed` flag).
3. Auto-resume tailscale if previously enabled (once per process via
   `g.tailscaleAutoResumed` flag).
4. `ensureCloudflared()` — download cloudflared binary if missing.
5. Auto-start MITM if previously enabled:
   - `syncMitmAliasCache()` — sync DB → JSON for standalone MITM server.
   - `autoStartMitm(settings)` — with mutex via `g.mitmStartInProgress`.
6. `configureTunnelMonitoring(settings)` — starts watchdog and network
   monitor.
7. Start quota auto-ping if any provider has auto-ping enabled.

### 3. Watchdog Timer

**File:** `src/shared/services/initializeApp.js`, function `startWatchdog()` (lines 235–248)

```javascript
g.watchdogInterval = setInterval(() => {
  safeRestartTunnel("watchdog").catch(() => {});
  safeRestartTailscale("watchdog").catch(() => {});
}, WATCHDOG_INTERVAL_MS);
```

- `WATCHDOG_INTERVAL_MS` is imported from `src/lib/tunnel/shared/watchdogConfig.js`
  (not inspected — likely 60,000ms).
- Calls `unref()` so it does **not** keep the process alive.
- Started only when tunnel or tailscale is enabled.
- Multiple calls to `startWatchdog()` are idempotent via the
  `g.watchdogInterval` null check.

### 4. Network Monitor Interval

**File:** `src/shared/services/initializeApp.js`, function `startNetworkMonitor()` (lines 267–318)

```javascript
g.networkMonitorInterval = setInterval(async () => {
  // ...fingerprint check, sleep detection, internet check...
}, NETWORK_CHECK_INTERVAL_MS);
```

`NETWORK_CHECK_INTERVAL_MS` from `watchdogConfig.js`.

**What it does every tick:**

1. Compute network fingerprint via `os.networkInterfaces()` — non-internal
   IPv4 addresses joined as a string.
2. Detect sleep: if elapsed time > 6× interval, treat as wake.
3. Detect network change: if fingerprint differs from last.
4. Real reachability check: TCP connect to `1.1.1.1:443`
   (`checkInternet()`).
5. If online and (network changed or woke or came back online):
   - Wait `NETWORK_SETTLE_MS` for DHCP/DNS.
   - `safeRestartTunnel(reason)` and `safeRestartTailscale(reason)`.

**Reason strings passed to restart functions:**

- `"sleep+netchange"` — woke plus different network.
- `"sleep"` — woke on same network.
- `"online"` — was offline, now online.
- `"netchange"` — network changed while awake.

### 5. Tunnel Safe-Restart

**File:** `src/shared/services/initializeApp.js`, function `safeRestartTunnel()` (lines 161–190)

Four guards:

1. `svc.cancelToken.cancelled` — tunnel service was permanently cancelled.
2. `svc.spawnInProgress` — already spawning.
3. `isCloudflaredRunning()` — if cloudflared process is alive, trust it
   (self-reconnects via `--retries 99`).
4. Cooldown: `RESTART_COOLDOWN_MS` elapsed since last restart (bypassed
   for force reasons: `startup`, `netchange`, `sleep`, `sleep+netchange`,
   `online`, `unexpected-exit`).
5. `checkInternet()` — no internet, skip.

### 6. Tailscale Safe-Restart

**File:** `src/shared/services/initializeApp.js`, function `safeRestartTailscale()` (lines 192–231)

Same guard pattern as tunnel but:

- Uses `isTailscaleRunning()` vs `isTailscaleRunningStrict()` depending
  on reason (strict for startup, cached for runtime).
- Checks `isDaemonAlive()` + `svc.activeLocalPort` for funnel-only
  recovery (no full restart).
- Tailscale daemon is OS-level with built-in reconnect.

### 7. Quota Auto-Ping Scheduler

**File:** `src/shared/services/quotaAutoPing.js` (313 lines)

#### 7a. Global State

```javascript
const g = (global.__quotaAutoPing ??= {
  interval: null,
  running: false,
  resetCache: {},
  failureCache: {},
});
```

Also survives HMR.

#### 7b. Interval

`startQuotaAutoPing()` (line 292–298):

```javascript
g.interval = setInterval(() => { runQuotaAutoPingTick().catch(() => {}); }, C.tickIntervalMs);
```

- `tickIntervalMs` = 60,000ms (from `QUOTA_AUTOPING_CONFIG`).
- `unref()` called so it does not keep the process alive.
- Idempotent via `g.interval` null check.

#### 7c. Per-Tick Behavior (`runQuotaAutoPingTick`, line 261–289)

1. Mutex via `g.running` flag.
2. For each configured provider (claude, codex):
   - Filter connections where `authType === "oauth"` and auto-ping enabled.
   - For each connection:
     - Skip if reset time hasn't arrived (with `refreshAheadMs` tolerance).
     - Skip if a recent failure is in cooldown (`failureCooldownMs` = 15min).
     - Refresh credentials, fetch usage, check quota exhaustion.
     - Send a tiny request (1-token for Claude, "hi" for Codex).
     - Update `lastPingedResetAt` in DB on success.
3. Failure caching prevents replay for 15 minutes.

#### 7d. Configuration

From `QUOTA_AUTOPING_CONFIG`:

| Setting | Value |
| --------- | ------- |
| `tickIntervalMs` | 60,000 |
| `pingLeadMs` | 5,000 |
| `refreshAheadMs` | 300,000 |
| `failureCooldownMs` | 900,000 |
| Claude ping model | `claude-haiku-4-5-20251001` |
| Claude ping text | `"hi"` |
| Codex ping model | `gpt-5.5` |
| Codex ping text | `"hi"` |
| Codex min ping interval | 600,000 |

### 8. Console Log Capture

**File:** `src/lib/consoleLogBuffer.js` (108 lines)

#### 8a. Global State

```javascript
global._consoleLogBufferState = {
  logs: [],
  patched: false,
  originals: {},
  emitter: new EventEmitter(),
};
```

#### 8b. Monkey-Patch

`initConsoleLogCapture()` replaces `console.log`, `console.info`,
`console.warn`, `console.error`, `console.debug` with wrapped versions
that write to the in-memory buffer and emit events.

#### 8c. Flush Timer

```javascript
const FLUSH_INTERVAL_MS = 100;
const MAX_BATCH_LINES = 50;
```

Every 100ms (or when 50 lines accumulate), pending lines are emitted
via the EventEmitter so SSE streams can forward them to the dashboard.

**Bound:** 200 lines maximum stored (`CONSOLE_LOG_CONFIG.maxLines`).

### 9. Outbound Proxy Initialization

**File:** `src/lib/network/initOutboundProxy.js` (25 lines)

```javascript
setImmediate(() => {
  ensureOutboundProxyInitialized().catch(console.log);
});
```

- Called as a module side-effect.
- Reads proxy settings from DB and sets `HTTP_PROXY`, `HTTPS_PROXY`,
  `ALL_PROXY` env vars.
- Guarded by `initialized` flag (process-local, survives HMR).

### 10. SSE Auth Service Mutex

**File:** `src/sse/services/auth.js`, variable `selectionMutex` (line 9)

```javascript
let selectionMutex = Promise.resolve();
```

Serializes `getProviderCredentials()` calls to prevent race conditions
during account selection. The mutex is **process-local** — it provides
no cross-process coordination.

### 11. MITM Manager Process-Local State

**File:** `src/mitm/manager.js` (883 lines, partial inspection)

Key state variables:

```javascript
let mitmRestartCount = 0;
let mitmLastStartTime = 0;
let mitmIsRestarting = false;
```

Restart schedule: up to 5 retries at 5s, 10s, 20s, 30s, 60s delays.
Resets after 60s of successful running.

Lock file at `${MITM_DIR}/.mitm.lock` using O_EXCL for atomic
startup coordination.

### 12. Updater (Standalone Process)

**File:** `src/lib/updater/updater.js` (235 lines)

Spawned as a **detached child process** by `src/lib/appUpdater.js`.

#### 12a. Lifecycle

1. Spawner (`spawnUpdaterAndExit`) creates a detached Node.js process
   running `updater.js`, then calls `process.exit(0)` after 500ms delay.
2. Updater starts an HTTP status server on `127.0.0.1:20129` (no auth).
3. Waits for app port (`:20128`) to become free (polls every 500ms for
   3–15s).
4. Runs `npm i -g 9router --prefer-online` (up to 3 retries).
5. On success: relaunches via npx (if `UPDATER_RELAUNCH=1`), waits for
   new app to be ready, opens browser.
6. Lingers for 30s (`UPDATER_LINGER_MS`) so browser can poll final
   status, then exits.

#### 12b. Kill-Ap Processes (appUpdater.js)

`killAppProcesses()` collects PIDs of all 9router-related processes
using `ps aux` (Unix) or `Get-WmiObject Win32_Process` (Windows).
Kills MITM by PID file, kills cloudflared and tray binaries on Windows.

**Critical:** On Unix, uses `sudo -n kill -9` for the MITM PID, with
fallback to `process.kill(pid, "SIGKILL")`. On Windows, uses `taskkill
/F` with PowerShell `Stop-Process -Force` as fallback.

### 13. MCP Bridge Cleanup

**File:** `src/lib/mcp/stdioSseBridge.js` (referenced by `initializeApp.js`)

`killAllBridges()` is called during signal cleanup. The full bridge
implementation was not inspected in detail.

### 14. DNS Entry Cleanup

**File:** `src/mitm/dns/dnsConfig.js` (referenced)

`removeAllDNSEntriesSync()` is called synchronously on:

- SIGINT
- SIGTERM
- `process.on("exit")`

This is a synchronous filesystem/DNS operation during shutdown —
critical for MITM DNS entries to not persist after the process dies.

## Summary of Intervals and Timers

| Timer | Interval | Creates | File | Cleanup |
| ------- | ---------- | --------- | ------ | --------- |
| Watchdog | `WATCHDOG_INTERVAL_MS` (~60s) | `setInterval` | `initializeApp.js` | `stopWatchdog()`, `clearInterval` |
| Network monitor | `NETWORK_CHECK_INTERVAL_MS` | `setInterval` | `initializeApp.js` | `stopNetworkMonitor()`, `clearInterval` |
| Quota auto-ping | 60,000ms | `setInterval` | `quotaAutoPing.js` | `stopQuotaAutoPing()`, `clearInterval` |
| Console flush | 100ms | `setTimeout` (self-scheduling) | `consoleLogBuffer.js` | None explicit (timer cleared on flush) |
| Deferred startup | 3,000ms | `setTimeout` (one-shot) | `initializeApp.js` | None needed |
| Outbound proxy | Immediate | `setImmediate` (one-shot) | `initOutboundProxy.js` | None needed |
| Updater linger | 30,000ms | `setTimeout` (one-shot) | `updater.js` | Killed by process exit |

## Summary of Global Mutable State

| Key | Module | Content |
| ----- | -------- | --------- |
| `global.__appSingleton` | `initializeApp.js` | Watchdog/network state, signal flags |
| `global.__quotaAutoPing` | `quotaAutoPing.js` | Interval, running flag, caches |
| `global._consoleLogBufferState` | `consoleLogBuffer.js` | Log buffer, patch state, emitter |
| `global.__appBootstrapped` | `bootstrap.js` | Init-once guard |
| Module-level `initialized` | `initOutboundProxy.js` | Proxy init guard |
| Module-level `selectionMutex` | `auth.js` (SSE) | Auth selection serialization |
| Module-level mitm state | `manager.js` | Restart count, in-progress flag |
| Module-level `cachedCliToken` | `dashboardGuard.js` | Cached machine-ID-derived token |

## Uncertainties and Conflicting Evidence

1. **`WATCHDOG_INTERVAL_MS` and `NETWORK_CHECK_INTERVAL_MS` values**
   were not confirmed by inspecting `watchdogConfig.js`. The import
   in `initializeApp.js` line 14 imports them, but the values were
   not verified.

2. **`RESTART_COOLDOWN_MS` and `NETWORK_SETTLE_MS` values** also come
   from `watchdogConfig.js` and were not verified.

3. **The `consoleLogBuffer.js`** `setTimeout` for flushing does **not**
   call `unref()`. This timer will keep the Node.js event loop alive
   if no other async operations are pending.

4. **The `selectionMutex`** in `auth.js` uses promise-chaining for
   mutual exclusion. If any `await` in the critical section throws,
   the mutex chain could break, leaving subsequent calls permanently
   waiting. The `try/finally` that resolves the mutex in
   `getProviderCredentials` mitigates this, but the mutex resolution
   relies on closure capture of `resolveMutex`, which is set before
   `await currentMutex`.

5. **The updater status server** (`127.0.0.1:20129`) has no auth and
   responds to any request on `/update/status` or `/` with full state
   including the `error` field and install log tail. It listens only
   on loopback, but any process on the same host can read update
   progress.

## Parity Requirements

- All intervals and timers must either `unref()` or be explicitly
  cleaned up on shutdown.
- Deferred initialization (3s delay) should be preserved or consciously
  replaced.
- Signal handlers for SIGINT/SIGTERM with DNS removal + MITM bridge
  cleanup + cloudflared kill must be present.
- The process-local `__appSingleton` pattern must be replaced with
  explicit lifecycle management.
- The quota auto-ping scheduler must have equivalent behavior with
  failure cooldown and reset tracking.
- The console log capture monkey-patch must be preserved for dashboard
  real-time log display.
- The updater detached-child-process pattern must be preserved or
  replaced with an equivalent mechanism.
- MITM restart guard with O_EXCL lock file must be preserved.
- The network monitor sleep/wake detection must be preserved for tunnel
  resilience.

## Coverage Gaps

- No automated test for `initializeApp.js` (no test file exists).
- No test for the network monitor fingerprint or sleep detection.
- No test for the tunnel/tailscale restart cooldown or guards.
- No test for the console log capture patch or flush timer.
- No test for the mutex behavior in `getProviderCredentials`.
- No test for the updater status server response or lifecycle.
- No test for `killAppProcesses` PID collection or edge cases.
- No test for the `outboundProxy` `setImmediate` initialization.
- No test for `configureTunnelMonitoring` settings-based
  enable/disable.
