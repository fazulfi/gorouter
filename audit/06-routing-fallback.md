# 06 — Routing & Fallback

<!-- markdownlint-disable MD013 -->

## 1. Scope

Model string parsing, provider alias resolution, model aliases, combo (multi-model) expansion with fallback/round-robin/fusion strategies, per-account credential selection with cooldown/backoff, account fallback on error, per-model locks, rate-limit cooldowns, retry logic in executors, and the interplay between combo routing and account fallback.

## 2. Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (v0.5.40, 2026-07-20)
- Read-only clone: `../upstream-original`

## 3. Method

Source code reading of `open-sse/services/model.js`, `open-sse/services/combo.js`, `open-sse/services/accountFallback.js`, `open-sse/services/provider.js`, `open-sse/config/errorConfig.js`, `open-sse/config/runtimeConfig.js`, `open-sse/executors/base.js`, `open-sse/executors/default.js`, `open-sse/executors/index.js`, `open-sse/providers/capabilities.js`, `src/sse/handlers/chat.js`, and `src/sse/services/model.js`. Tests under `tests/unit/` for combo-routing, combo-autoswitch, combo-fusion, base-executor-retry, model-routing, and related.

## 4. Confirmed Findings

### 4.1 Model String Parsing

**`parseModel(modelStr)`** at `open-sse/services/model.js:34`.

The model string `"provider/model"` (or `"alias/model"`) is parsed at the slash boundary:

- `providerOrAlias = modelStr.slice(0, firstSlash)`
- `model = modelStr.slice(firstSlash + 1)`
- `provider = resolveProviderAlias(providerOrAlias)`

If no slash is present, the entire string is treated as a model alias (not a provider alias):

- `{ provider: null, model: modelStr, isAlias: true, providerAlias: null }`

**`resolveProviderAlias(aliasOrId)`** at `open-sse/services/model.js:27`:

Built from the provider registry (`open-sse/providers/registry/index.js`). Each registry entry's `id`, `alias`, and `aliases[]` are mapped to the canonical `entry.id`:

```js
ALIAS_TO_PROVIDER_ID[entry.id] = entry.id
ALIAS_TO_PROVIDER_ID[entry.alias] = entry.id
for each alias in entry.aliases: ALIAS_TO_PROVIDER_ID[alias] = entry.id
```

Media-only providers (no registry transport entry) have explicit hardcoded aliases in `MEDIA_ONLY_ALIASES` (line 5): `el → elevenlabs`, `jina → jina-ai`, `polly → aws-polly`, etc.

**`getModelInfoCore(modelStr, aliasesOrGetter)`** at `open-sse/services/model.js:94`:

1. Calls `parseModel(modelStr)`.
2. If `!isAlias`, returns `{ provider, model }` directly.
3. If alias, resolves via `resolveModelAliasFromMap` from the user's alias DB and/or `BUILTIN_MODEL_ALIASES`.
4. Fallback: `inferProviderFromModelName(parsed.model)` — matches model name prefix against `MODEL_PREFIX_PROVIDERS` (line 126): `claude- → anthropic`, `gemini- → gemini`, `gpt- → openai`, `o[134] → openai`, `deepseek- → openrouter`. Final fallback: `"openai"`.

**Built-in model aliases** (line 20):

- `grok-build` → `gcli/grok-build`

### 4.2 Provider Registry and Alias Resolution

**Registry source:** `open-sse/providers/registry/index.js` — auto-generated static import list. Each registry file declares:

```js
export default {
  id: "anthropic",
  alias: "claude",
  aliases: ["cc", "anthropic"],
  // ...
}
```

The alias-to-provider mapping is built once at module load time in `open-sse/services/model.js:14`.

**App-level resolution:** `src/sse/services/model.js` wraps `getModelInfoCore` with the user's alias database retrieved via `getAliases()`.

**Confirmed:** The provider string before the slash is resolved through all aliases transitively: user writes `cc/claude-opus-4-6` → `parseModel` extracts `cc` → `resolveProviderAlias("cc")` → `"claude"` (because registry for "claude" has alias "cc").

### 4.3 Combo Expansion

**`getComboModels(modelStr)`** at `src/sse/services/model.js`: Queries the `combosRepo` for a combo with `name === modelStr`. Returns `combo.models` (array of `"provider/model"` strings) or null.

**Combo detection** in `src/sse/handlers/chat.js:90` and `:142`:

1. First check at top-level `handleChat()`: if `getComboModels(modelStr)` returns non-null, invoke combo handler.
2. Second check inside `handleSingleModelChat()`: if `modelInfo.provider` is null (model string had no slash but also no matching alias), check combos again.

**Combo strategies** (`src/sse/handlers/chat.js:95-127`):

Three strategies, set via `settings.comboStrategy` (global) or `settings.comboStrategies[comboName].fallbackStrategy` (per-combo):

| Strategy | Handler | Behavior |
| --- | --- | --- |
| `"fallback"` (default) | `handleComboChat` with sequential iteration | Try models in order; break on success |
| `"round-robin"` | `handleComboChat` with rotated order | `getRotatedModels` changes the starting model per request |
| `"fusion"` | `handleFusionChat` | Fan-out panel + judge synthesis |

### 4.4 Combo Fallback (`handleComboChat`)

**File:** `open-sse/services/combo.js:229`

**Flow:**

1. Apply rotation if strategy is `"round-robin"` via `getRotatedModels()` (line 158).
2. Apply auto-switch: if `autoSwitch` is true, detect required capabilities from body via `detectRequiredCapabilities()` and reorder models via `reorderByCapabilities()` (line 234).
3. Iterate through `rotatedModels` sequentially.
4. On success (`result.ok`): return immediately.
5. On failure: extract error text, check `checkFallbackError()` for should-fallback decision.
6. For transient errors (502/503/504) with cooldown ≤ 5s: wait before falling through (line 294).
7. If `shouldFallback` is false: return the error immediately (no fallback).
8. If all models fail: return 503 with "All combo models unavailable".

**Transient cooldown wait:** Only applied for status 503, 502, 504 when cooldownMs is between 1 and 5000ms. This gives a briefly-overloaded provider a recovery window (line 294-298).

**All-models-failed response:** Uses 503, not 406. Comment at line 315: "406 implies the request itself is invalid, but here the providers are simply unavailable."

### 4.5 Combo Round-Robin Rotation

**`getRotatedModels()`** at `open-sse/services/combo.js:157`:

- Process-local state via `comboRotationState` Map (line 88), keyed by combo name.
- Tracks `index` (current model position) and `consecutiveUseCount`.
- `stickyLimit` (default 1) controls how many consecutive requests stick to the same model before rotating.
- Only rotates when strategy is `"round-robin"`; fallback strategy returns models unchanged.
- `resetComboRotation(comboName)` clears the state (called when combo/settings change).

### 4.6 Combo Fusion (`handleFusionChat`)

**File:** `open-sse/services/combo.js:496`

**Flow:**

1. If panel has 1 model → direct answer (no fusion).
2. Fan-out: all panel models called in parallel with `stream: false` and tools stripped.
3. `collectPanel()` (line 444) uses quorum-grace collection: as soon as `minPanel` (default 2) answers arrive, start an 8s grace window for stragglers. Absolute hard timeout: 90s.
4. Collect successful answers via `extractPanelText()` (line 339).
5. If 0 answers → 503. If exactly 1 → return that answer directly.
6. Judge: synthesizes a prompt with all anonymized panel responses plus analysis instructions, then calls the judge model (defaults to `panel[0]`).

**Fusion tuning** overridable per-combo via `settings.comboStrategies[name].fusionTuning`:

```js
FUSION_DEFAULTS = { minPanel: 2, stragglerGraceMs: 8000, panelHardTimeoutMs: 90000 }
```

### 4.7 Auto-Switch (Capability-Based Reordering)

**`detectRequiredCapabilities(body)`** at `open-sse/services/combo.js:105`:

Scans only the **trailing user turn** (messages after the last assistant/model message) for modality requirements. Detected capabilities: `"vision"`, `"pdf"`. The `"search"` capability is registered but commented out ("not wired yet").

**`reorderByCapabilities(models, required)`** at `open-sse/services/combo.js:63`:

- Tier 0: satisfies all hard + all soft capabilities.
- Tier 1: satisfies all hard capabilities only.
- Tier 2: rest.
- Stable sort preserves original order within tiers.
- Only modifies order when capabilities are actually required.

**Capabilities lookup:** `getCapabilitiesForModel(provider, model)` from `open-sse/providers/capabilities.js`.

### 4.8 Account Fallback

**App-level entry point:** `src/sse/handlers/chat.js`, inside `handleSingleModelChat()` (line 137).

The outer `while (true)` loop (line 198):

1. Calls `getProviderCredentials(provider, excludeConnectionIds, model)` to get the next available account.
2. If all rate-limited: return `unavailableResponse` with `retryAfter` timing.
3. If no credentials at all and it was the first attempt: return 404 "No active credentials".
4. Run token refresh check via `checkAndRefreshToken()`.
5. Ensure project ID for Antigravity/Gemini-CLI.
6. Call `handleChatCore()`.
7. If `result.success`: return response (and `clearAccountError` via `onRequestSuccess`).
8. Else: call `markAccountUnavailable(connectionId, status, error, provider, model, resetsAtMs)` — this calculates cooldown with exponential backoff.
9. If `shouldFallback`: add to `excludeConnectionIds` and `continue` loop.
10. If not shouldFallback: return the error without fallback.

### 4.9 Account Cooldown and Error Classification

**`checkFallbackError(status, errorText, backoffLevel)`** at `open-sse/services/accountFallback.js:23`:

Checked against `ERROR_RULES` from `open-sse/config/errorConfig.js:59`:

```text
Text rules (priority order):
  "no credentials"            → 2 min cooldown
  "request not allowed"       → 5s cooldown
  "improperly formed request" → 2 min
  "rate limit"               → exponential backoff
  "too many requests"        → exponential backoff
  "quota exceeded"           → exponential backoff
  "capacity"                 → exponential backoff
  "overloaded"               → exponential backoff

Status rules (fallback):
  401 → 2 min
  402 → 2 min
  403 → 2 min
  404 → 2 min
  429 → exponential backoff

Default: 30s transient cooldown
```

**Exponential backoff:** `getQuotaCooldown(level)` (line 9): `base * 2^(level-1)`, clamped to 5 min max. `base = 2s`, `maxLevel = 15`.

**`applyErrorState(account, status, errorText)`** at line 202: sets `rateLimitedUntil`, increments `backoffLevel`, records `lastError`, sets `status: "error"`.

**`resetAccountState(account)`** at line 184: clears cooldown, resets backoff to 0, sets status to "active".

**`isAccountUnavailable(unavailableUntil)`** at line 55: compares `new Date(unavailableUntil).getTime() > Date.now()`.

**`filterAvailableAccounts(accounts, excludeId)`** at line 166: filters by `rateLimitedUntil` expiry and explicit exclusion.

### 4.10 Per-Model Locks

**`accountFallback.js:106-161`** implements model-level cooldown locks stored as flat fields on the connection record:

- `MODEL_LOCK_PREFIX = "modelLock_"`
- `MODEL_LOCK_ALL = "modelLock___all"` (for account-level lock when model is unknown)
- `getModelLockKey(model)`: builds `modelLock_${model}` or `modelLock___all`.

**`isModelLockActive(connection, model)`** at line 120: checks both `modelLock_${model}` and `modelLock___all`.

**`buildModelLockUpdate(model, cooldownMs)`** at line 147: creates update object `{ modelLock_${model}: ISO timestamp }`.

**`buildClearModelLocksUpdate(connection)`** at line 155: sets all `modelLock_*` fields to null.

### 4.11 Executor-Level Retry

**`BaseExecutor.execute()`** at `open-sse/executors/base.js:99` has a nested loop:

- Outer loop: URL fallback (iterates `getBaseUrls()` array).
- Inner per-URL: configurable retry by status code.

**Default retry config** from `open-sse/config/runtimeConfig.js:78`:

| Status | attempts | delayMs |
| --- | --- | --- |
| 429 | 0 | 0 |
| 502 | 3 | 3000 |
| 503 | 3 | 2000 |
| 504 | 2 | 3000 |

**`tryRetry(urlIndex, statusKey, reason, response)`** (line 110):

- Reads `retryConfig[statusKey]` which is `{ attempts, delayMs }`.
- If `attempts <= 0` or attempts exhausted, skip.
- Subclass hook: `computeRetryDelay(response, attemptNum, defaultDelayMs)` can return a dynamic delay (e.g. from Retry-After header). If it returns `false`, retry is vetoed.
- Sleeps `delayMs` then returns `true` (caller does `urlIndex--; continue`).

**`shouldRetry(status, urlIndex)`** (line 82): returns true for `429` when another URL is available in the baseUrls list (URL fallback, not retry).

**Network error handling** (line 163): network exceptions (ECONNRESET etc.) are mapped to 502 retry config. Connect timeout (`FETCH_CONNECT_TIMEOUT_MS` default 60s) is caught via secondary `AbortController`, converted to retryable network error (not propagated as AbortError).

### 4.12 Provider-Specific Retry Hooks

**`computeRetryDelay` override** is implemented in specialized executors:

- **AntigravityExecutor** (`open-sse/executors/antigravity.js`): Reads `Retry-After` header from 429/503 responses for dynamic delay.
- **KiroExecutor** (`open-sse/executors/kiro.js`): May implement Kiro-specific rate-limit parsing.
- **CodexExecutor** (`open-sse/executors/codex.js`): Handles Codex rate-limit window headers.

### 4.13 Token Refresh as Fallback

Inside `handleChatCore()` at `open-sse/handlers/chatCore.js:331-353`:

When executor returns 401/403:

1. Call `refreshWithRetry(executor.refreshCredentials, 3, log)`.
2. If new credentials obtained (has `accessToken` or `copilotToken`):
   - Notify `onCredentialsRefreshed` callback.
   - Re-execute the request with updated credentials.
3. If refresh fails: log warning, continue with original 401/403 result for account fallback.

**Executor refresh implementations** in `DefaultExecutor.refreshCredentials()` (`open-sse/executors/default.js:219`): dispatches to provider-specific methods for claude, codex, qwen, iflow, gemini, kiro, cline, clinepass, kimi, kimi-coding, kilocode.

### 4.14 Provider Credential Selection

**`getProviderCredentials(provider, excludeConnectionIds, model)`** at `src/sse/services/auth.js`:

- Selects the next available account for the given provider, respecting:
  - Excluded connection IDs (previously failed in current request).
  - Account-level `rateLimitedUntil`.
  - Model-level locks (`modelLock_*` fields).
- Returns credentials object with `connectionId`, `accessToken`, `apiKey`, `refreshToken`, `connectionName`, `providerSpecificData`, `rateLimitedUntil`, `retryAfter`, `retryAfterHuman`, `allRateLimited`, `lastError`, `lastErrorCode`.

### 4.15 Provider-Level Thinking Config Override

In `src/sse/handlers/chat.js:232`: `providerThinking` is read from `settings.providerThinking[provider]` and passed to `handleChatCore`.

In `handleChatCore()` at `chatCore.js:71-81`:

- Mode `"on"` → injects `thinking: { type: "enabled", budget_tokens: 10000 }`
- Mode `"off"` → injects `thinking: { type: "disabled" }`
- Any other mode (e.g. `"low"`, `"medium"`, `"high"`) → sets `body.reasoning_effort`
- Only applies if client hasn't already set these fields.

### 4.16 Provider Thinking Normalization

**`normalizeThinkingConfig(body)`** at `open-sse/services/provider.js:164`:

Deletes `body.thinking` if the last message is not a user message (prevents sending thinking config on tool-result turns).

**`applyThinking(targetFormat, model, result, provider, thinkingIntent)`** at `open-sse/translator/concerns/thinkingUnified.js`:

Maps the captured thinking intent from the original body to the target provider-native format. For Kiro's GenerateAssistantResponse endpoint, the thinking intent is mapped to `systemPrompt`/`additionalModelRequestFields` instead of the generic top-level `thinking` field.

### 4.17 Native Passthrough Detection

**`detectClientTool(headers, body)`** at `open-sse/utils/clientDetector.js` and **`isNativePassthrough(clientTool, provider)`** at the same file:

When the client tool (e.g. "claude") matches the provider ecosystem (e.g. "anthropic"), all translation is skipped — only the model name and Bearer token are swapped. This avoids the double-hop loss for same-ecosystem routes.

Applied in `handleChatCore()` at line 117.

### 4.18 Process-Local Round-Robin and Fusion State

- `comboRotationState` (Map) in `open-sse/services/combo.js:88`: per-combo rotation index + sticky count. Survives for the process lifetime.
- `collectPanel()` (line 444) uses local `setTimeout`/`clearTimeout`. No cross-process state.
- Fusion panel calls and judge call happen within the same request handler.

## 5. Source Evidence

| Symbol / File | Line(s) | What It Does |
| --- | --- | --- |
| `open-sse/services/model.js` parseModel | 34-55 | Model string → provider/model parsing |
| `open-sse/services/model.js` resolveProviderAlias | 27-29 | Alias → canonical provider ID |
| `open-sse/services/model.js` getModelInfoCore | 94-123 | Full model resolution (parse + alias + inference) |
| `open-sse/services/model.js` resolveModelAliasFromMap | 61-87 | User alias map lookup |
| `open-sse/services/model.js` inferProviderFromModelName | 138-142 | Prefix-based provider fallback |
| `open-sse/services/model.js` BUILTIN_MODEL_ALIASES | 20-22 | `grok-build → gcli/grok-build` |
| `open-sse/services/model.js` ALIAS_TO_PROVIDER_ID | 13-18 | Registry-derived alias map |
| `open-sse/services/combo.js` getRotatedModels | 157-186 | Round-robin rotation with sticky limit |
| `open-sse/services/combo.js` comboRotationState | 88 | Process-local rotation state map |
| `open-sse/services/combo.js` resetComboRotation | 192-195 | State reset on config change |
| `open-sse/services/combo.js` getComboModelsFromData | 203-215 | Combo name → model list lookup |
| `open-sse/services/combo.js` handleComboChat | 229-331 | Fallback combo iteration |
| `open-sse/services/combo.js` handleFusionChat | 496-571 | Fusion fan-out + judge |
| `open-sse/services/combo.js` collectPanel | 444-471 | Quorum-grace collection |
| `open-sse/services/combo.js` detectRequiredCapabilities | 105-133 | Vision/pdf requirement detection |
| `open-sse/services/combo.js` reorderByCapabilities | 63-82 | Capability-aware model reordering |
| `open-sse/services/combo.js` flattenToolHistory | 21-59 | Tool turns → prose for panel models |
| `open-sse/services/combo.js` buildJudgePrompt | 400-418 | Fusion judge instruction builder |
| `open-sse/services/combo.js` FUSION_DEFAULTS | 421-425 | Fusion timing defaults |
| `open-sse/services/accountFallback.js` checkFallbackError | 23-49 | Error → cooldown/backoff decision |
| `open-sse/services/accountFallback.js` getQuotaCooldown | 9-13 | Exponential backoff calculation |
| `open-sse/services/accountFallback.js` applyErrorState | 202-215 | Account error state application |
| `open-sse/services/accountFallback.js` resetAccountState | 184-193 | Account error state reset |
| `open-sse/services/accountFallback.js` isAccountUnavailable | 55-58 | Cooldown expiry check |
| `open-sse/services/accountFallback.js` filterAvailableAccounts | 166-176 | Active account filtering |
| `open-sse/services/accountFallback.js` isModelLockActive | 120-125 | Per-model cooldown check |
| `open-sse/services/accountFallback.js` buildModelLockUpdate | 147-150 | Model lock field builder |
| `open-sse/services/accountFallback.js` getEarliestRateLimitedUntil | 72-83 | Earliest reset time |
| `open-sse/config/errorConfig.js` ERROR_RULES | 59-76 | Error classification rules |
| `open-sse/config/errorConfig.js` BACKOFF_CONFIG | 32-36 | Exponential backoff params |
| `open-sse/config/errorConfig.js` TRANSIENT_COOLDOWN_MS | 39 | Default 30s transient cooldown |
| `open-sse/config/errorConfig.js` MAX_RATE_LIMIT_COOLDOWN_MS | 42 | Hard cap 30 min |
| `open-sse/config/runtimeConfig.js` DEFAULT_RETRY_CONFIG | 78-83 | Per-status retry attempts/delays |
| `open-sse/config/runtimeConfig.js` resolveRetryEntry | 86-93 | Retry config normalization |
| `open-sse/executors/base.js` BaseExecutor.execute | 99-183 | Executor retry + URL fallback |
| `open-sse/executors/base.js` tryRetry | 110-124 | Status-based retry with subclass hook |
| `open-sse/executors/base.js` shouldRetry | 82-84 | 429 → URL fallback |
| `open-sse/executors/default.js` DefaultExecutor.refreshCredentials | 219-247 | Provider-specific OAuth refresh dispatch |
| `open-sse/executors/index.js` getExecutor | 57-61 | Provider → executor resolution |
| `open-sse/executors/index.js` executors map | 25-53 | Specialized executor instances |
| `open-sse/services/provider.js` normalizeThinkingConfig | 164-169 | Thinking config suppression |
| `open-sse/providers/capabilities.js` getCapabilitiesForModel | — | Model capability lookup |
| `open-sse/handlers/chatCore.js` handleChatCore (thinking override) | 71-81 | Provider-level thinking injection |
| `open-sse/handlers/chatCore.js` handleChatCore (token refresh) | 331-353 | 401/403 → token refresh → retry |
| `src/sse/handlers/chat.js` handleChat | 31-132 | App-level combo/account dispatch |
| `src/sse/handlers/chat.js` handleSingleModelChat | 137-287 | Account loop + credentials |
| `src/sse/handlers/chat.js` getComboModels | 90 | Combo DB lookup |
| `src/sse/services/auth.js` getProviderCredentials | — | Account selection with cooldown/model-locks |
| `src/sse/services/model.js` getModelInfo | — | Wraps getModelInfoCore with user aliases |

### Relevant Tests

| Test File | Key Tests |
| --- | --- |
| `tests/unit/combo-routing.test.js` | Round-robin rotation, sticky limit, per-combo isolation, fallback no-rotation |
| `tests/unit/combo-autoswitch.test.js` | Capability-based reordering by modality |
| `tests/unit/combo-fusion.test.js` | Fusion fan-out, judge synthesis, straggler timeout, single-model fallback |
| `tests/unit/base-executor-retry.test.js` | Status-based retry (502, 429), URL fallback, network error → 502 retry mapping, retry exhaustion |
| `tests/unit/model-routing.test.js` | Model string parsing, alias resolution, provider inference |
| `tests/unit/model-test-routing.test.js` | Test model routing edge cases |
| `tests/unit/hf-model-routing.test.js` | HuggingFace model routing |
| `tests/unit/github-responses-routing.test.js` | GitHub Copilot Responses API routing |
| `tests/unit/provider-thinking-config.test.js` | Provider-level thinking override injection |
| `tests/unit/force-stream-config.test.js` | Provider `forceStream` interaction with routing |
| `tests/unit/kiro-nonstream-error.test.js` | Kiro-specific error handling for non-streaming |
| `tests/unit/antigravity-retry-hook.test.js` | Antigravity Retry-After dynamic delay hook |
| `tests/unit/token-refresh-dispatch.test.js` | Token refresh dispatch logic |
| `tests/unit/codex-refresh-token.test.js` | Codex OAuth token refresh |
| `tests/unit/file-block-routing.test.js` | File/block modality routing |
| `tests/unit/provider-custom-models.test.js` | Custom model resolution |

## 6. Behavioral Invariants

1. **Model string resolution priority**: `provider/model` (with slash) → exact registry alias lookup. No-slash → model alias DB → built-in aliases → prefix inference → `"openai"` fallback.
2. **Combo vs model check order**: In `handleChat`, combo is checked first (line 90). If the model string matches a combo name, the combo handler runs regardless of whether it also looks like a model path.
3. **Account fallback is strictly sequential**: The `while(true)` loop tries one account at a time, excluding failed ones. No parallel credential probing.
4. **Cooldown rules are priority-ordered**: Text-based rules are checked before status-based rules (line 28-46 of `checkFallbackError`).
5. **Exponential backoff applies only to rate-limit-like errors**: Errors matching `rate limit`, `too many requests`, `quota exceeded`, `capacity`, `overloaded` or status 429 get `backoff: true`.
6. **URL fallback vs retry**: `shouldRetry` (429 + another URL available) switches URLs. `tryRetry` (status-matching retry config) retries the same URL. Both can apply to different statuses.
7. **Network errors are retryable by default**: Mapped to 502 retry config (3 attempts, 3s delay).
8. **Token refresh happens before account fallback**: 401/403 triggers refresh first; only if refresh fails does the account get marked for fallback.
9. **Fusion panel models never see tools**: Tools and tool_choice are stripped, tool turns flattened to prose.
10. **Auto-switch only inspects the trailing user turn**: Older turns with media do not pin the combo to a vision model.

## 7. Process-Local or Concurrency Assumptions

1. **`comboRotationState` is a process-global Map**: persists across requests in the same Node.js process. Reset on combo config change. Not shared across processes or machines.
2. **No distributed locking**: Account cooldowns are persisted to the DB (rate-limited-until timestamps). Model locks are flat DB fields. Both rely on DB writes for cross-process consistency.
3. **In-memory backoff level**: `backoffLevel` lives on the connection object returned from DB. Each request reads the current level, applies error state, and writes back. No mutex on increment.
4. **Fusion `withTimeout()`** uses `setTimeout`/`clearTimeout` per panel call. The hung fetch continues running but is ignored after timeout.

## 8. Error and Edge-Case Behavior

| Case | Behavior | Source |
| --- | --- | --- |
| Model string has no slash and no matching alias | `inferProviderFromModelName` → prefix guess or "openai" | `model.js:118-122` |
| No credentials for provider at all | 404 "No active credentials for provider" + warning log | `chat.js:210-212` |
| All accounts rate-limited | `unavailableResponse` with earliest retry-after timestamp | `chat.js:203-207` |
| Model is combo + strategy fusion | Fan-out to panel → collect → judge | `combo.js:496-571` |
| Model is combo + strategy fallback | Sequential try until success | `combo.js:229-331` |
| Combo model returns error without shouldFallback | Error returned immediately (no fallthrough) | `combo.js:286-289` |
| Transient 503/502/504 in combo | Wait cooldownMs (≤5s) before next model | `combo.js:294-298` |
| All combo models fail | 503 with "All combo models unavailable" | `combo.js:316-330` |
| Fusion panel returns 0 answers | 503 "All fusion panel models failed" | `combo.js:555-560` |
| Fusion panel returns 1 answer | Return that answer directly (no judge) | `combo.js:562-565` |
| Executor network timeout | `tryRetry` maps to 502 retry config; if exhausted, throws network error | `base.js:163-178` |
| Executor retry for 429 with no fallback URLs | Current URL is the only one → returns 429 response to caller | `base.js:156-160` |
| ComputeRetryDelay returns `false` | Retry vetoed entirely (fallback URL may still work) | `base.js:116-117` |
| Account already in cooldown mid-request | `getProviderCredentials` excludes it automatically | `chat.js:198-215` loop |
| Model lock active but account not locked | Account is available for different models | `accountFallback.js:120-125` |
| Request includes image but first combo model lacks vision | `reorderByCapabilities` promotes a vision-capable model | `combo.js:63-82` |

## 9. Security Implications

1. **No credential enumeration protection**: The account fallback loop sequentially tries all accounts for a provider. A malicious user could infer account health by timing/order-of-errors, though only for accounts they have access rights to.
2. **Provider thinking config override** allows per-provider forced thinking. If a provider override is set and the client sends `thinking: { type: "disabled" }`, the override takes precedence only when the client hasn't set it. This is opt-in, not override.
3. **Model name inference fallback** to `"openai"` for unrecognized model strings could route requests to unexpected providers.

## 10. Uncertainties and Conflicting Evidence

1. **The exact `getProviderCredentials` implementation** is in `src/sse/services/auth.js` which was not fully audited. Account selection order (priority, round-robin) is determined there.
2. **`computeRetryDelay` hook** exists conceptually in `BaseExecutor` (line 115) but only AntigravityExecutor is confirmed to implement it. Other executors may have implementations not yet traced.
3. **`autoSwitch` default**: `handleComboChat` passes `autoSwitch = true` but the call from `chat.js` does not pass this explicitly (line 119-127). The default parameter `true` in the function signature (line 229) confirms it's enabled by default.
4. **Fusion judge model defaults to `panel[0]`**, which may not be the best judge. The per-combo `judgeModel` override from settings is optional.

## 11. Parity Requirements

- Parity would require preserving or consciously replacing: `parseModel` slash-boundary parsing, the alias-to-provider-id map built from registry (auto-generated), combo expansion with three strategies (fallback, round-robin, fusion), sequential account iteration with cooldown/backoff, per-model locks as separate dimensions from account-level cooldowns, the error classification rule set (text-based priority over status-based), exponential backoff parameters (base 2s, max 5 min, maxLevel 15), executor-level retry tables per status code, URL fallback for 429, and process-local rotation state for round-robin combos.
- The process-local `comboRotationState` Map means a Go rewrite must decide whether to keep per-process state, move it to a shared store, or accept a different distribution pattern.

## 12. Questions Requiring User Decisions

1. **Process-local rotation state**: Go typically uses multiple OS processes. Will combo rotation state be shared via a database/Redis, or is per-process rotation acceptable?
2. **Fusion implementation**: The panel+judge pattern requires executing N+1 provider calls within a single request. In Go, this maps cleanly to goroutines, but timeout/context propagation needs explicit design.
3. **Error rule configurability**: The `ERROR_RULES` in `errorConfig.js` are hardcoded. Should the Go version make them configurable at runtime?
4. **Per-model locks**: The flat-field storage pattern (`modelLock_${model}`) is a SQLite-specific design. A different DB schema would need equivalent locking semantics.

## 13. Coverage Gaps

- **The exact `getProviderCredentials` implementation** in `src/sse/services/auth.js` was not fully audited. The account selection order, priority rules, and model-lock reading logic need their own audit.
- **Auto-switch for `"search"` capability** is registered in `detectRequiredCapabilities` (commented out) but not wired. The code path exists but is inactive.
- **Combo-specific `judgeModel` and `fusionTuning`** settings are read from `comboStrategies[modelStr]` but only tested minimally.
- **Kilocode refresh** returns `null` (line 334 of `default.js`), meaning Kilocode accounts never get token refresh. This may be intentional (device-code flow).
