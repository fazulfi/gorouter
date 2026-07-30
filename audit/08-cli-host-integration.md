# Audit 08: CLI/Host Integration — Commands, Menus, Launcher, Tray, Autostart, Update, Packaging, Child Processes

## Scope

Complete inventory of the CLI launcher package (`cli/`): entry point, argument parser, subcommands, interactive menus, terminal UI, system tray (Windows/macOS/Linux), autostart, update checker, child process management (server spawn, MITM, tunnels), SQLite runtime self-heal, packaging, and API client.

## Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (tag `v0.5.40`, 2026-07-20)
- Package: `cli/` (npm package `9router`, published independently)
- Read-only clone: `C:\Users\faizz\9router-go\upstream-original`

## Method

- Read every file under `cli/` (25 files total).
- Traced entry point `cli.js` (870 lines) for argument parsing, server spawn, tray lifecycle, update, process management.
- Traced all menus (`providers.js`, `apiKeys.js`, `combos.js`, `settings.js`, `cliTools.js`).
- Traced `terminalUI.js` (121 lines) for interactive TUI.
- Traced `api/client.js` (556 lines) for full HTTP API surface.
- Traced `tray/tray.js`, `tray/trayWin.js`, `tray/autostart.js`.
- Traced commands `commands/xaiVideo.js` (300 lines).
- Traced hooks `sqliteRuntime.js`, `trayRuntime.js`, `postinstall.js`.
- Traced `utils/` directory (7 files).
- Read `scripts/build-cli.js` and `scripts/buildMitm.js`.

## Confirmed Findings — Complete File Inventory

### 1. CLI File Inventory (25 files total)

**Entry point:**
- `cli/cli.js` (870 lines) — executable entry point, argument parser, server lifecycle

**Package metadata:**
- `cli/package.json` — npm package `9router` v0.5.40
- `cli/README.md` — published readme
- `cli/LICENSE` — MIT license
- `cli/.gitignore`
- `cli/.npmignore`

**Build scripts:**
- `cli/scripts/build-cli.js` — esbuild bundler config for standalone build
- `cli/scripts/buildMitm.js` — MITM server build

**Hooks (runtime self-heal):**
- `cli/hooks/postinstall.js` (22 lines) — postinstall warm-up script
- `cli/hooks/sqliteRuntime.js` (156 lines) — SQLite runtime dependency manager
- `cli/hooks/trayRuntime.js` (107 lines) — systray binary dependency manager

**Command implementation:**
- `cli/src/cli/commands/xaiVideo.js` (300 lines) — `9router xai video` subcommand

**API client:**
- `cli/src/cli/api/client.js` (556 lines) — full HTTP API client to local gateway

**Interactive menus:**
- `cli/src/cli/menus/providers.js` (847 lines) — provider list, add, test, delete, models
- `cli/src/cli/menus/apiKeys.js` (233 lines) — API key CRUD
- `cli/src/cli/menus/combos.js` (477 lines) — combo CRUD
- `cli/src/cli/menus/settings.js` (204 lines) — tunnel, RTK, Headroom, auth, password reset
- `cli/src/cli/menus/cliTools.js` (618 lines) — CLI tool config (Claude Code, Codex, Droid, OpenClaw etc.)

**Terminal UI:**
- `cli/src/cli/terminalUI.js` (121 lines) — interactive TUI with header refresh + menu navigation

**System tray:**
- `cli/src/cli/tray/tray.js` (322 lines) — cross-platform tray controller
- `cli/src/cli/tray/trayWin.js` (89 lines) — Windows PowerShell NotifyIcon
- `cli/src/cli/tray/tray.ps1` — PowerShell tray script
- `cli/src/cli/tray/autostart.js` (306 lines) — OS autostart (macOS launchd, Windows Startup, Linux .desktop)
- `cli/src/cli/tray/icon.ico` — Windows tray icon
- `cli/src/cli/tray/icon.png` — macOS/Linux tray icon

**Utility files:**
- `cli/src/cli/utils/clipboard.js` — clipboard copy
- `cli/src/cli/utils/display.js` — screen clear, status, header formatting
- `cli/src/cli/utils/endpoint.js` — tunnel-aware endpoint URL resolution
- `cli/src/cli/utils/format.js` — date formatting, key masking
- `cli/src/cli/utils/input.js` — prompts, confirm, select menu (uses `enquirer`)
- `cli/src/cli/utils/menuHelper.js` — reusable menu navigation with breadcrumbs
- `cli/src/cli/utils/modelSelector.js` — model list selection

### 2. CLI `package.json` Details

```json
{
  "name": "9router",
  "version": "0.5.40",
  "bin": { "9router": "./cli.js" },
  "files": ["cli.js", "src", "hooks", "app", "README.md", "LICENSE"],
  "scripts": {
    "dev": "nodemon -I --watch cli.js --watch src --watch hooks --ext js,json cli.js",
    "build": "node scripts/build-cli.js",
    "pack:cli": "npm run build && npm pack --pack-destination ../..",
    "publish:cli": "npm run build && npm publish",
    "postinstall": "node hooks/postinstall.js",
    "prepublishOnly": "npm run build"
  },
  "dependencies": {
    "enquirer": "^2.4.1",
    "node-forge": "^1.3.3",
    "node-machine-id": "^1.1.12",
    "react": "19.2.1",
    "react-dom": "19.2.1"
  },
  "devDependencies": {
    "esbuild": "^0.25.12",
    "nodemon": "^3.1.14"
  },
  "engines": { "node": ">=18.0.0" }
}
```

**Key design decisions (from package.json comments):**
- `sql.js` + `better-sqlite3` are NOT bundled. Installed into `~/.9router/runtime/node_modules` by postinstall.js and self-healed at runtime (avoids Windows EBUSY on global update).
- `systray2` is NOT bundled. Lazy-installed into `~/.9router/runtime/node_modules` on macOS/Linux only. Windows uses PowerShell NotifyIcon (zero binary, avoids Kaspersky false positives).

### 3. CLI Arguments and Subcommands

**Source**: `cli/cli.js` lines 68-166

**Flags** (standard startup mode):

| Flag | Alias | Default | Description |
|------|-------|---------|-------------|
| `--port` | `-p` | `20128` | Port to bind server |
| `--host` | `-H` | `0.0.0.0` | Host to bind |
| `--no-browser` | `-n` | `false` | Skip browser auto-open |
| `--log` | `-l` | `false` | Show server logs (stderr passthrough) |
| `--skip-update` | — | `false` | Skip npm update check |
| `--tray` | `-t` | `false` | Run in system tray (background, no TUI) |
| `--help` | `-h` | — | Show help text |
| `--version` | `-v` | — | Show version |

**Subcommands** (bypass launcher, run against running gateway):

| Command | File | Description |
|---------|------|-------------|
| `xai video --prompt "..." --output video.mp4 [options]` | `commands/xaiVideo.js` | Generate Grok Imagine video via gateway |

**Subcommand-specific flags** for `xai video`:
`--prompt`, `--output`, `--model` (default `xai/grok-imagine-video`), `--duration`, `--aspect-ratio`, `--resolution`, `--image`, `--timeout` (600s), `--port`, `--host`, `--api-key`, `--poll-interval-ms`

**Auto-detection**: If `--skip-update` is set but no TTY and not `--tray`, auto-enables tray mode (line 169-172).

### 4. Launcher Lifecycle

**Startup sequence** (`cli.js` lines 542-545):
1. Check for update (`checkForUpdate()`) — HTTPS GET to `registry.npmjs.org/9router/latest`, 8s timeout, non-blocking
2. Kill existing app processes (`killAllAppProcesses(port)`)
3. Kill process on port (`killProcessOnPort(port)`)
4. Start server (`startServer(updatePromise)`)

**Server spawn** (`cli.js` lines 612-634):
```js
spawn(RUNTIME, ["--dns-result-order=ipv4first", "--max-old-space-size=6144", serverPath], {
  cwd: standaloneDir,
  stdio: showLog ? "inherit" : ["ignore", "ignore", "pipe"],
  detached: true,
  windowsHide: true,
  env: { ...buildEnvWithRuntime(process.env), PORT, HOSTNAME }
});
```
- RUNTIME = `process.execPath` (Node.js, always absolute path)
- `serverPath` = `standaloneDir/custom-server.js` (preferred) or `standaloneDir/server.js`
- `standaloneDir` = `cli/app/` (bundled Next.js standalone build)
- `--max-old-space-size=6144` = 6GB heap
- `--dns-result-order=ipv4first` = prefer IPv4 DNS
- Stderr captured to `crashLog[]` (last 50 lines) when `--log` is off

**Interface selection menu** (`cli.js` lines 548-590, `showInterfaceMenu()`):
After server ready, shows:
1. Update to vX (if available) → exits to terminal, user runs `npm i -g 9router@latest`
2. Web UI (Open in Browser) → `openBrowser(url)` + pause
3. Terminal UI (Interactive CLI) → `startTerminalUI(port)`
4. Hide to Tray (Background) → spawns detached bgProcess or on macOS keeps current process
5. Exit

**Server crash recovery** (`cli.js` lines 833-867):
- `MAX_RESTARTS = 2` within 30s window
- Exponential backoff: `min(1000 * restartCount, 10000)` ms
- After 2 crashes: disables MITM via `db.json` edit, spawns fresh server, resets counter
- Crash log (last 50 stderr lines) printed on each restart

**Auto-restart after update** (line 169): When `--skip-update` and no TTY, auto-enables tray mode.

### 5. System Tray

**Cross-platform controller**: `src/cli/tray/tray.js`

**Supported platforms**: macOS, Windows, Linux (with DISPLAY).

**Menu items** (shared across platforms):
1. `9Router (Port ${port})` — disabled label
2. `Open Dashboard` — opens browser
3. `Enable Auto-start` / `✓ Auto-start Enabled` — toggles autostart
4. `Quit` — kills server + tray + exit

**Windows tray** (`trayWin.js`):
- Spawns `powershell.exe -NoProfile -ExecutionPolicy Bypass -WindowStyle Hidden -File tray.ps1 -IconPath <path> -Tooltip <text>`
- Communication via JSON over stdin/stdout
- Menu item add/update via stdin JSON commands
- Returns controller with `updateItem()`, `setTooltip()`, `kill()`

**macOS/Linux tray** (`tray.js` lines 188-245):
- Uses `systray2` (preferred) or `systray` (legacy, broken on macOS 14+)
- Binary installed to `~/.9router/runtime/node_modules/` by `trayRuntime.js`
- `chmodTrayBin()` ensures +x on binary (EACCES workaround)
- `isTemplateIcon: false` to avoid white square on macOS
- Graceful shutdown via `kill(true)` to avoid NSStatusItem ghost

**Tray lifecycle** (`tray.js` lines 252-298, `killTray()`):
- Windows: immediate `instance.kill()`
- Unix: graceful IPC exit → 800ms SIGTERM → 1600ms SIGKILL → 3000ms poll fallback
- Critical for macOS: must release NSStatusItem before re-spawn

### 6. Autostart

**Source**: `src/cli/tray/autostart.js`

**macOS** (`enableMacOS`, lines 141-215):
- Writes `~/Library/LaunchAgents/com.9router.autostart.plist`
- launchd agent: runs `node <cli.js> --tray --skip-update`
- Sets `PATH` to include node bin dir for child processes
- Handles `isAgentSelfMacOS()` detection to avoid SIGTERM-self on toggle
- Skips `launchctl load/unload` when current process is the agent (prevents tray icon loss)
- Logs to `/tmp/9router.log` and `/tmp/9router.error.log`

**Windows** (`enableWindows`, lines 239-256):
- Writes `%APPDATA%\Microsoft\Windows\Start Menu\Programs\Startup\9router.vbs`
- VBScript runs `node <cli.js> --tray --skip-update` via `Wscript.Shell.Run` (hidden, 0)

**Linux** (`enableLinux`, lines 268-292):
- Writes `~/.config/autostart/9router.desktop`
- `Exec=<node> <cli.js> --tray --skip-update`

**Detection** (`isAutoStartEnabled`, lines 84-109):
- macOS: plist exists AND `launchctl list` succeeds
- Windows: startup VBS exists
- Linux: .desktop autostart file exists

**CLI path resolution** (`getCliJsPath`, lines 26-40):
- Explicit `cliPath` argument > `process.argv[1]` > computed from `__dirname`

### 7. Update Checker

**Source**: `cli/cli.js` lines 459-505

- HTTPS GET to `https://registry.npmjs.org/${pkg.name}/latest` with 3s request timeout
- 8s safety timeout total
- Compares versions with `compareVersions()` (semver, 3 parts)
- Runs in parallel with server start (non-blocking)
- When update found: shown in interface menu as first option
- Update action: logs `npm i -g 9router@latest --prefer-online`, exits (user runs manually)
- `--skip-update` flag bypasses entirely

### 8. Child Process Management

**Server process** (main child):
- `spawn(RUNTIME, [...args, serverPath], { detached: true, ... })`
- PID tracked via `server.pid`
- Cleanup on exit: `cleanup()` kills server, MITM, tunnels, tray
- `uncaughtException` suppressed during shutdown

**Process finding and killing** (`cli.js` lines 195-440):

| Function | Purpose | Method |
|----------|---------|--------|
| `killByPidFile(pidFile)` | Kill by PID file | Windows: `taskkill /F /T /PID`; Unix: `SIGKILL` |
| `killTunnelByPidFile()` | Kill cloudflared/tailscale pid files | Reads `tunnel/cloudflared.pid`, `tunnel/tailscale.pid` |
| `killCloudflaredByAppPort(port)` | Kill cloudflared by matching port | Windows: WMI query; Unix: `ps -eo pid,command` |
| `killAllAppProcesses(port)` | Kill all 9router/next-server processes | Windows: WMI `Win32_Process` filter on `node.*9router.*cli.js\|next-server`; Unix: `ps aux` + filter |
| `killProxyByPidFile()` | Kill MITM server (privileged) | SIGTERM → 1500ms → SIGKILL, with PowerShell/`sudo -n` fallback |
| `killProcessOnPort(port)` | Kill any process on port | Windows: `netstat -ano \| findstr :port` + `taskkill`; Unix: `lsof -ti:port` + `kill -9` |

**Process identifiers for safe killing** (lines 114-116):
```js
const PROCESS_IDENTIFIERS = ['9router']; // Only package name - avoid killing other apps
```

**Precautions**: Whitelist-based process matching avoids killing editors/grep/cursor that incidentally have "9router" in command line.

### 9. SQLite Runtime Self-Heal

**Source**: `cli/hooks/sqliteRuntime.js`

- Ensures `sql.js` + `better-sqlite3` in `~/.9router/runtime/node_modules`
- `better-sqlite3` version: `12.6.2`, `sql.js` version: `1.14.1`
- Binary validation for `better_sqlite3.node`
- Auto-installs via `npm install` in runtime dir
- Exports `buildEnvWithRuntime()` that prepends runtime node_modules to `NODE_PATH`
- Called at startup: `cli.js` line 86 (`ensureSqliteRuntime({ silent: true })`)
- Also called during `postinstall.js`

### 10. Tray Runtime Self-Heal

**Source**: `cli/hooks/trayRuntime.js`

- Installs `systray2` v2.1.4 to `~/.9router/runtime/node_modules/`
- Windows only: skipped (uses PowerShell NotifyIcon)
- Cleans up legacy `systray` package from runtime and global install dirs
- `chmodSystrayBin()` fixes missing +x on Go binary
- Called at startup: `cli.js` line 89
- Also called during `postinstall.js`

### 11. Terminal UI

**Source**: `src/cli/terminalUI.js`

- Main menu: Providers, API Keys, Combos, CLI Tools, Settings, Back
- Header shows endpoint URL, tunnel status, first API key
- SWR pattern: background header refresh via `refreshHeaderBg()` (calls `api.getApiKeys()` + `api.getTunnelStatus()`)
- All menu sub-navigation via `showMenuWithBack()` with breadcrumb tracking

### 12. Interactive Menus

| Menu | File | Exported Function | Description |
|------|------|-------------------|-------------|
| Providers | `menus/providers.js` | `showProvidersMenu` | List/add/test/delete providers, view models, OAuth connect |
| API Keys | `menus/apiKeys.js` | `showApiKeysMenu` | List/create/view/copy/delete API keys |
| Combos | `menus/combos.js` | `showCombosMenu` | List/create/edit/delete model combos |
| Settings | `menus/settings.js` | `showSettingsMenu` | Tunnel ON/OFF, RTK toggle, Headroom toggle, password reset |
| CLI Tools | `menus/cliTools.js` | `showCliToolsMenu` | Per-tool config (Claude Code, Codex, Droid, OpenClaw) with quick setup, model select, MCP |

### 13. API Client

**Source**: `src/cli/api/client.js`

**Authentication**: Machine-ID based CLI token via `x-9r-cli-token` header:
```js
const CLI_TOKEN_HEADER = "x-9r-cli-token";
const CLI_TOKEN_SALT = "9r-cli-auth";
// SHA256(machineId + salt + cliSecret).substring(0, 16)
```
- Machine ID from `~/.9router/machine-id` (shared file written by server) or `node-machine-id`
- CLI secret from `~/.9router/auth/cli-secret` (random 32-byte hex, generated on first use)
- Token cached in process memory

**HTTP layer**:
- Uses Node.js `http`/`https` modules (no fetch API)
- JSON request/response
- 30s timeout
- `{ success, data, error, statusCode }` response shape

**API surface** (all methods mapped):

| Category | Function | Method | Path |
|----------|----------|--------|------|
| Providers | `getProviders` | GET | `/api/providers` |
| | `getProviderById` | GET | `/api/providers/:id` |
| | `testProvider` | POST | `/api/providers/:id/test` |
| | `deleteProvider` | DELETE | `/api/providers/:id` |
| | `getProviderModels` | GET | `/api/providers/:id/models` |
| | `updateConnection` | PUT | `/api/providers/:id` |
| OAuth | `getOAuthAuthUrl` | GET | `/api/oauth/:provider/authorize?redirect_uri=...` |
| | `exchangeOAuthCode` | POST | `/api/oauth/:provider/exchange` |
| | `getOAuthDeviceCode` | GET | `/api/oauth/:provider/device-code` |
| | `pollOAuthToken` | POST | `/api/oauth/:provider/poll` |
| | `createApiKeyProvider` | POST | `/api/providers` |
| API Keys | `getApiKeys` | GET | `/api/keys` |
| | `createApiKey` | POST | `/api/keys` |
| | `deleteApiKey` | DELETE | `/api/keys/:id` |
| Combos | `getCombos` | GET | `/api/combos` |
| | `getComboById` | GET | `/api/combos/:id` |
| | `createCombo` | POST | `/api/combos` |
| | `updateCombo` | PUT | `/api/combos/:id` |
| | `deleteCombo` | DELETE | `/api/combos/:id` |
| CLI Tools | `getCliToolSettings` | GET | `/api/cli-tools/:tool-settings` |
| | `applyCliToolSettings` | POST | `/api/cli-tools/:tool-settings` |
| | `resetCliToolSettings` | DELETE | `/api/cli-tools/:tool-settings` |
| Settings | `getSettings` | GET | `/api/settings` |
| | `updateSettings` | PATCH | `/api/settings` |
| | `resetPassword` | POST | `/api/auth/reset-password` |
| Tunnel | `getTunnelStatus` | GET | `/api/tunnel/status` |
| | `enableTunnel` | POST | `/api/tunnel/enable` |
| | `disableTunnel` | POST | `/api/tunnel/disable` |
| Models | `getModels` | GET | `/api/models` |
| | `getAvailableModels` | GET | `/v1/models` |
| Provider Nodes | `getProviderNodes` | GET | `/api/provider-nodes` |
| | `createProviderNode` | POST | `/api/provider-nodes` |
| | `updateProviderNode` | PUT | `/api/provider-nodes/:id` |
| | `deleteProviderNode` | DELETE | `/api/provider-nodes/:id` |
| | `validateProviderNode` | POST | `/api/provider-nodes/validate` |

### 14. Packaging

**Build process** (`scripts/build-cli.js`):
- Uses `esbuild` to bundle the Next.js standalone output into `cli/app/`
- Produces `cli/app/server.js` and `cli/app/custom-server.js`
- Published to npm as `9router`

**npm distribution**:
- Published via `npm publish` from `cli/` directory
- Build required before publish (`prepublishOnly: "npm run build"`)
- Pack available via `npm run pack:cli` → `npm pack --pack-destination ../..`

**MITM build** (`scripts/buildMitm.js`):
- Builds MITM proxy server
- Output to `cli/app/`

**Published files** (from package.json `"files"`):
`cli.js`, `src/`, `hooks/`, `app/`, `README.md`, `LICENSE`

### 15. Utility Functions

| File | Functions | Description |
|------|-----------|-------------|
| `clipboard.js` | `copyToClipboard(text)` | Cross-platform clipboard copy |
| `display.js` | `clearScreen()`, `showStatus(msg, type)`, `showHeader(title)` | Terminal display helpers |
| `endpoint.js` | `getEndpoint(port)` | Resolves tunnel-aware endpoint URL via API |
| `format.js` | `formatDate()`, `getRelativeTime()`, `maskKey()` | Date formatting, key masking |
| `input.js` | `prompt()`, `confirm()`, `pause()`, `selectMenu()` | Interactive input (wraps `enquirer`) |
| `menuHelper.js` | `showMenuWithBack()` | Reusable menu with breadcrumbs |
| `modelSelector.js` | `selectModelFromList()` | Interactive model list selection |

### 16. Process-Local or Concurrency Assumptions

1. **Single-instance**: Always kills existing processes on startup (`killAllAppProcesses` + `killProcessOnPort`).
2. **Detached server**: Server child process is `detached: true` — survives parent exit.
3. **Tray icon**: Only one tray instance per process. macOS re-spawn requires killing old NSStatusItem first.
4. **Update check**: Non-blocking, runs in parallel with server start. 8s ceiling.
5. **Runtime self-heal**: Best-effort; failures logged to stderr, never block startup.
6. **No IPC between parent and child**: Server started as detached process, CLI communicates via HTTP to `/api/*`.

## Behavioral Invariants

1. **CLI always kills first, spawns second** — prevents port conflict and orphan accumulation.
2. **SQLite deps self-heal at startup** — `sql.js` required, `better-sqlite3` optional.
3. **Tray deps self-heal on macOS/Linux** — silent fail if unable to install.
4. **Crash recovery is limited** — 2 restarts within 30s window. After that, MITM disabled.
5. **macOS tray requires graceful IPC shutdown** — to avoid ghost NSStatusItem.
6. **Windows tray uses PowerShell** — zero native binaries, no antivirus false positives.
7. **Autostart always runs with `--tray --skip-update`** — all platforms.
8. **CLI token is HMAC of machineId + salt + random secret** — not reusable across machines.
9. **`xai video` subcommand polls via HTTP** — 5s default interval, 600s timeout, terminal status detection.
10. **All API client calls use 30s timeout and `{ success, data, error }` response contract**.

## Uncertainties and Conflicting Evidence

- **Uncertain**: Whether `cli/app/` (standalone build) is present in the git checkout or only built before publish. The CLI check `if (!fs.existsSync(serverPath))` suggests it's expected after build.
- **Uncertain**: The exact `buildMitm.js` output structure — the MITM server binary path derived from manager.js is not fully traced.
- **Uncertain**: `tray.ps1` content not read — the PowerShell NotifyIcon implementation details are assumed.
- **Uncertain**: `display.js`, `format.js`, `clipboard.js` exact implementations not traced in detail.

## Parity Requirements

1. Same `9router` npm package name and bin entry point.
2. Same CLI flags: `--port`, `--host`, `--no-browser`, `--log`, `--skip-update`, `--tray`, `--help`, `--version`.
3. Same subcommand architecture: `xai video` (and extensible for future subcommands).
4. Same server spawn contract: Node.js executable, `--dns-result-order=ipv4first`, `--max-old-space-size=6144`, standalone dir `app/`.
5. Same process management: kill-before-spawn, crash recovery (2 restarts/30s), MITM disable after repeated crash.
6. Same tray lifecycle: system tray with Open Dashboard, Enable Autostart, Quit menu items.
7. Same autostart mechanism: macOS launchd plist, Windows Startup VBS, Linux .desktop.
8. Same runtime dir `~/.9router/runtime/node_modules` (or `%APPDATA%/9router/runtime/node_modules` on Windows).
9. Same API client authentication: `x-9r-cli-token` header derived from `machine-id` + `cli-secret`.
10. Same interactive menus structure: Providers, API Keys, Combos, CLI Tools, Settings.

## Questions Requiring User Decisions

1. **Node.js dependency**: The CLI requires Node.js runtime. Does the rewrite also require Node.js, or use a self-contained binary?
2. **esbuild vs native bundling**: Original uses esbuild for standalone build. What build tool for rewrite?
3. **npm publish**: Package published to npm. Does the rewrite publish to npm or use a different distribution channel?
4. **Tray implementation**: Windows uses PowerShell (zero binary deps). Does the rewrite accept PowerShell dependency, or use a different Windows tray approach?
5. **Autostart files**: Does the rewrite write the same OS-specific autostart files?
6. **`cli-secret` file-based auth**: Token derived from machine-id file + random secret in `~/.9router/auth/cli-secret`. Preserve?
7. **Runtime self-heal**: SQLite and systray deps installed on demand. Acceptable for rewrite or bundle all deps?
8. **Update mechanism**: Currently just shows message + exits. Does the rewrite auto-update?

## Coverage Gaps

- `tray.ps1` content not read.
- `scripts/build-cli.js` exact esbuild config not read.
- `scripts/buildMitm.js` exact output not read.
- `utils/display.js`, `utils/format.js`, `utils/clipboard.js` not read in full.
- MITM server child process (src/mitm/server.js) not part of this audit (belongs in separate MITM audit).
