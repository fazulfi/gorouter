# 05 — Streaming & Translation

<!-- markdownlint-disable MD013 -->

## 1. Scope

Format detection, request/response translation, streaming SSE pipeline, terminal event rules, forced stream-to-JSON conversion, cancellation, timeouts, and per-stream usage/pricing accounting for the chat modality. Covers translators, the SSE transform stream, stream controller, stall watchdog, and abort propagation.

## 2. Audited Repository and Exact Commit

- Repository: `decolua/9router`
- Commit: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1` (v0.5.40, 2026-07-20)
- Read-only clone: `../upstream-original`

## 3. Method

Source code reading of all files under `open-sse/translator/`, `open-sse/utils/stream*.js`, `open-sse/transformer/`, `open-sse/handlers/chatCore/`, `open-sse/config/runtimeConfig.js`, and `open-sse/utils/usageTracking.js`. Test files under `tests/translator/` and `tests/unit/` for streaming termination, kiro-terminal-integrity, force-stream-config, and responses-abort-terminal. Symbol inventory via grep and AST-grep.

## 4. Confirmed Findings

### 4.1 Format Detection

**Entry point:** `open-sse/services/provider.js`, function `detectFormat(body)` (line 28).

The original detects the client's request format by inspecting the body structure, checked in this order:

1. **openai-responses**: body has `input` (array or string) but no `messages` → `"openai-responses"`.
2. **antigravity**: body has `request.contents` AND `userAgent === "antigravity"` → `"antigravity"`.
3. **gemini**: body has `contents` array → `"gemini"`.
4. **openai**: if any of `stream_options`, `response_format`, `logprobs`, `top_logprobs`, `n`, `presence_penalty`, `frequency_penalty`, `logit_bias`, `user` are present → `"openai"` (checked before Claude).
5. **claude**: if `messages` array with type-checked content blocks (Claude-specific `text`/`image`/`tool_use`/`tool_result` types) or has `system` or `anthropic_version` → `"claude"`.
6. **Default**: falls back to `"openai"`.

**Endpoint-based override:** `open-sse/translator/formats.js` function `detectFormatByEndpoint(pathname, body)` (line 22). Applied by `src/sse/handlers/chat.js` as `sourceFormatOverride`:

- `/v1/responses` → `FORMATS.OPENAI_RESPONSES`
- `/v1/messages` → `FORMATS.CLAUDE`
- `/v1/chat/completions` with `body.input` array → `FORMATS.OPENAI` (Cursor CLI sends Responses body via chat endpoint)

**Source:** `detectFormat` at `open-sse/services/provider.js:28`, `detectFormatByEndpoint` at `open-sse/translator/formats.js:22`.

### 4.2 Format Identifiers

Defined as a single source of truth in `open-sse/translator/formats.js` (line 2):

| Constant | Value |
| --- | --- |
| `FORMATS.OPENAI` | `"openai"` |
| `FORMATS.OPENAI_RESPONSES` | `"openai-responses"` |
| `FORMATS.OPENAI_RESPONSE` | `"openai-response"` |
| `FORMATS.CLAUDE` | `"claude"` |
| `FORMATS.GEMINI` | `"gemini"` |
| `FORMATS.GEMINI_CLI` | `"gemini-cli"` |
| `FORMATS.VERTEX` | `"vertex"` |
| `FORMATS.CODEX` | `"codex"` |
| `FORMATS.ANTIGRAVITY` | `"antigravity"` |
| `FORMATS.KIRO` | `"kiro"` |
| `FORMATS.CURSOR` | `"cursor"` |
| `FORMATS.OLLAMA` | `"ollama"` |
| `FORMATS.COMMANDCODE` | `"commandcode"` |

**Note:** `FORMATS.OPENAI_RESPONSE` (singular, line 5) appears to be a misspelled alias; downstream code uses `FORMATS.OPENAI_RESPONSES` (plural).

### 4.3 Translator Architecture

**Registry:** `open-sse/translator/index.js`. Translators self-register via `register(from, to, requestFn, responseFn)` (line 19) called as an import side-effect. Two Maps: `requestRegistry` (line 15) and `responseRegistry` (line 16), keyed by `"${from}:${to}"`.

**Confirmed — Three translation strategies:**

1. **Direct route** (lossless, preferred): When a translator is registered for the exact `source:target` pair, it runs directly, skipping the OpenAI pivot. Confirmed for `claude:kiro` (`open-sse/translator/request/claude-to-kiro.js`), `kiro:claude` (`open-sse/translator/response/kiro-to-claude.js`).
2. **OpenAI pivot** (generic): Step 1 converts `source → openai`; Step 2 converts `openai → target`. Each step uses the same `requestRegistry`/`responseRegistry`.
3. **Same-format passthrough**: When `sourceFormat === targetFormat`, both `translateRequest` and `translateResponse` return the input unchanged.

**Request translation flow (`translateRequest`, line 52):**

1. Strip content types per model's `strip[]` config (`stripContentTypes`, line 35).
2. Normalize thinking config (delete `body.thinking` if last message is not from user).
3. Ensure tool_call IDs (`ensureToolCallIds`).
4. Fix missing tool responses (`fixMissingToolResponses`).
5. Capture thinking intent from original body (`captureThinking`).
6. Capture session ID for logging.
7. If `sourceFormat !== targetFormat`: try direct route, else pivot through OpenAI.
8. Apply thinking config to target format (`applyThinking`).
9. Normalize to clean OpenAI format if target is OpenAI (`filterToOpenAIFormat`).
10. Prepare Claude-format request if target is Claude (`prepareClaudeRequest`).
11. Apply Claude cloaking if provider has `cloakToolsOnOAuth` quirk.

**Response translation flow (`translateResponse`, line 157):**

1. If same format, return as-is.
2. Try direct route (e.g. `kiro:claude`).
3. Else: `target → openai` then `openai → source`.
4. Attach `.openaiIntermediate` for logging if both conversions happened.

**Translator side-effect imports** at `open-sse/translator/index.js:270-292`:

- Request: `claude-to-openai`, `openai-to-claude`, `gemini-to-openai`, `openai-to-gemini`, `openai-to-vertex`, `antigravity-to-openai`, `openai-responses`, `openai-to-kiro`, `openai-to-cursor`, `openai-to-ollama`, `openai-to-commandcode`, `claude-to-kiro`.
- Response: `claude-to-openai`, `openai-to-claude`, `gemini-to-openai`, `openai-to-antigravity`, `openai-responses`, `kiro-to-openai`, `cursor-to-openai`, `ollama-to-openai`, `commandcode-to-openai`, `kiro-to-claude`.

**Key invariant:** Adding a new translator requires both the `.js` file and a new `import` line in `index.js`. Missing either silently disables the route.

### 4.4 Target Format Resolution

**`getTargetFormat(provider)`** at `open-sse/services/provider.js:128` determines the format the provider expects:

- `openai-compatible-*` prefix → `"openai"` (or `"openai-responses"` if provider name includes "responses").
- `anthropic-compatible-*` prefix → `"claude"`.
- Falls back to `PROVIDERS[provider].format` or `"openai"`.

**`resolveTransport(provider, sourceFormat)`** at `open-sse/services/provider.js:142` picks a transport from `PROVIDERS[provider].transports[]` matching the sourceFormat. This is how multi-endpoint providers (e.g. providers with both `/v1/chat/completions` and `/v1/messages` endpoints) avoid lossy translation. The matched transport sets `credentials.runtimeTransport`.

**Per-model override:** `getModelTargetFormat(alias, model)` from `open-sse/config/providerModels.js` can override per model, checked before the provider-level `getTargetFormat`.

### 4.5 Streaming SSE Pipeline

**Architecture:** The chat request lifecycle that produces streaming SSE:

1. `src/sse/handlers/chat.js` → model parse, combo expansion, account selection loop.
2. `open-sse/handlers/chatCore.js` `handleChatCore()` → format detection, translation, token savers, executor dispatch.
3. `open-sse/handlers/chatCore/streamingHandler.js` `handleStreamingResponse()` → builds transform stream, pipes provider response.
4. `open-sse/utils/stream.js` `createSSEStream()` → the actual SSE transform.

**Stream modes** (`open-sse/utils/stream.js:20`):

- **`TRANSLATE`** (`"translate"`): Full translation via `translateResponse()`. Requires `targetFormat`, `sourceFormat`, initializes translation `state` via `initState()`.
- **`PASSTHROUGH`** (`"passthrough"`): No translation. Normalizes output, extracts usage, forwards SSE. Used when same-format.

**Transform stream construction** (`streamingHandler.js:25` `buildTransformStream`):

1. **Responses-API provider** (codex etc.) with `targetFormat === FORMATS.OPENAI_RESPONSES`: translates `OPENAI_RESPONSES → client format` via `CODEX_SOURCE_TO_TARGET` mapping (line 14: `OPENAI_RESPONSES→OPENAI_RESPONSES`, `CLAUDE→CLAUDE`, `ANTIGRAVITY→ANTIGRAVITY`, `GEMINI→ANTIGRAVITY`, `GEMINI_CLI→ANTIGRAVITY`, unknown→`OPENAI`).
2. **Translation needed** (`targetFormat !== sourceFormat`): `createSSETransformStreamWithLogger` with translation.
3. **Passthrough**: `createPassthroughStreamWithLogger`.

**SSE line parsing** (`streamHelpers.js:4` `parseSSELine`):

- Ollama format: raw JSON lines without `data:` prefix.
- Standard SSE: starts with `data:`, parses JSON payload. `[DONE]` sentinel returns `{ done: true }`.
- Non-JSON data lines are skipped silently (defense against upstream HTML error pages).

**Usage extraction during streaming** (in `createSSEStream`):

- Content/thinking lengths accumulated per chunk for estimation fallback.
- `extractUsage(parsed)` called per chunk, `mergeUsage(state.usage, extracted)` aggregates.
- On finish chunk: if no valid usage, `estimateUsage(body, totalContentLength, format)` estimates.
- `addBufferToUsage(usage)` always adds 2000 buffer tokens.
- `filterUsageForFormat(usage, targetFormat)` strips fields the target doesn't expect.

### 4.6 Terminal Event Rules

**Claude SSE:** Uses `event: message_stop` or `event: message_delta` with `stop_reason`. Translated to OpenAI `finish_reason`. Done sentinel handled natively.

**OpenAI Chat Completions SSE:** Terminates with `data: [DONE]\n\n`. Intermediate chunks are JSON with `choices[0].delta`.

**OpenAI Responses API SSE** (`open-sse/utils/responsesStreamHelpers.js`):

- Terminal events set: `response.completed`, `response.done`, `response.failed`, `error` (line 6-11).
- `isOpenAIResponsesTerminalEvent()` (line 19) checks both event name and `chunk.response.status`.
- `buildAbortedResponsesTerminalBytes()` (line 29) synthesizes `response.failed` + `[DONE]` for aborted streams.
- `formatIncompleteOpenAIResponsesStreamFailure()` (line 34) builds a `response.failed` event with `stream_disconnected` error code.

**Responses same-format passthrough rules** (in `createSSEStream`):

- If `done: true` seen before terminal event → synthesizes `response.failed`.
- If stream closes (flush) without terminal event → synthesizes `response.failed`.
- `[DONE]` sentinel sent once, guarded by `openAIResponsesDoneSent` and `streamDoneSent` flags.
- `streamDoneSent` prevents duplicate `[DONE]` across transform and flush.

**Passthrough mode `[DONE]` rule** (in `flush` at line 370): Gemini-family providers (`antigravity`, `gemini`, `vertex`) reject `[DONE]` with 400 error, so it is skipped for those providers. All others get `data: [DONE]\n\n`.

**Ollama:** `done: true` is the final NDJSON chunk carrying `finish_reason`/usage — not a terminator. Explicitly excluded from the `done ⇒ skip` logic (line 224).

### 4.7 Forced Stream-to-JSON

**When it triggers:** Provider forces streaming (`PROVIDERS[provider].forceStream === true`) but client requested non-streaming. Handled via `open-sse/handlers/chatCore/sseToJsonHandler.js` `handleForcedSSEToJson()` (line 111).

**Two paths:**

1. **Responses API SSE** (Codex etc.): Uses `convertResponsesStreamToJson()` from `open-sse/transformer/streamToJsonConverter.js`. Output is translated to client format (OpenAI chat, Antigravity/Gemini, or native Responses).
2. **Standard Chat Completions SSE:** Uses `parseSSEToOpenAIResponse()` (line 41) to collect all SSE chunks and reassemble into one JSON completion.

**Output transformation:** After converting SSE → JSON, the handler translates tool calls, reasoning content, and usage from the streaming accumulator to match the format expected by the client.

**Error handling:** Returns 502 if SSE parsing fails or upstream returns an error.

### 4.8 Cancellation and Stream Controller

**`createStreamController()`** at `open-sse/utils/streamHandler.js:18`:

- Creates an `AbortController` whose `signal` is passed to the executor's `fetch()`.
- `handleDisconnect(reason)`: Sets a 500ms delay before calling `abort()`, allowing cleanup. Logs `"⚡ DISCONNECT"`.
- `handleComplete()`: Cleans up abort timeout without aborting.
- `handleError(error)`: AbortErrors are logged as `"⚡ ABORTED"`, others as `"✗ ERROR"`.
- `isConnected()`: Returns `!disconnected`.

**`pipeWithDisconnect()`** at `open-sse/utils/streamHandler.js:191`:

- Stall watchdog: tracks raw upstream byte activity (not transform output).
- Timeout configurable via `PROVIDERS[provider].stallTimeoutMs` or `STREAM_STALL_TIMEOUT_MS` env var (default 360s).
- On stall: calls `streamController.handleError(new Error("stream stall timeout"))` then `streamController.abort()`.
- Stall timer cleared on every path: complete, error, disconnect, abort, flush.
- Upstream tap (`upstreamTap` TransformStream) counts bytes and resets stall timer.

**`createDisconnectAwareStream()`** at `open-sse/utils/streamHandler.js:98`:

- Wraps transform stream output in a `ReadableStream`.
- On disconnect: emits synthesized terminal payload via `onAbortTerminal` if provided (Responses passthrough).
- On network errors (AbortError, socket hang up, ECONNRESET, ETIMEDOUT, EPIPE, UND_ERR_SOCKET): graceful close, not `controller.error()`.

**Confirmation:** The stall timeout is deliberately measured on raw upstream bytes, not transform output. Comment at line 178 explains: reasoning models (Claude thinking via Kiro) can produce zero SSE output for long stretches while partial EventStream frames keep arriving. Measuring stall on transform output caused false stalls.

### 4.9 Timeouts

Defined in `open-sse/config/runtimeConfig.js`:

| Constant | Env Override | Default | Purpose |
| --- | --- | --- | --- |
| `STREAM_STALL_TIMEOUT_MS` | `STREAM_STALL_TIMEOUT_MS` | 360,000 ms (6 min) | Inter-chunk stall, measured on raw bytes |
| `STREAM_FIRST_CHUNK_TIMEOUT_MS` | `STREAM_FIRST_CHUNK_TIMEOUT_MS` | 200,000 ms (~3.3 min) | Time-to-first-token (prompt prefill) |
| `FETCH_CONNECT_TIMEOUT_MS` | `FETCH_CONNECT_TIMEOUT_MS` | 60,000 ms (1 min) | Upstream response headers timeout |
| `GEMINI_NATIVE_TTS_FETCH_TIMEOUT_MS` | `GEMINI_NATIVE_TTS_FETCH_TIMEOUT_MS` | 45,000 ms | Google TTS-specific timeout |

Connect timeout is applied via a secondary `AbortController` inside `BaseExecutor.execute()` (line 134-136) merged with the stream controller's signal.

### 4.10 Usage and Pricing Side Effects During Streaming

**`extractUsage()`** at `open-sse/utils/usageTracking.js:236` handles these formats:

1. **Claude** `message_start`: `input_tokens`, `cache_read_input_tokens`, `cache_creation_input_tokens`.
2. **Claude** `message_delta`: `output_tokens` (cumulative, merged via `mergeUsage`).
3. **OpenAI Responses API** `response.completed`/`response.done`: `input_tokens`, `output_tokens`, `cached_tokens`, `reasoning_tokens`.
4. **OpenAI** Chat Completions: `prompt_tokens`, `completion_tokens`, `cached_tokens`, `reasoning_tokens`.
5. **Gemini/Antigravity** `usageMetadata`: `promptTokenCount`, `candidatesTokenCount`, `cachedContentTokenCount`, `thoughtsTokenCount`.
6. **Ollama** NDJSON `done=true`: `prompt_eval_count`, `eval_count`.

**`mergeUsage()`** at line 318: Field-wise `Math.max` to merge Claude's split usage events. Also guards against `NaN` with `Number.isFinite`.

**`canonicalizeUsage()`** at line 164: Normalizes to a single input-token convention:

- **Claude path** (when `cached_tokens` is undefined but `cache_read_input_tokens` or `cache_creation_input_tokens` present): folds cache into `prompt_tokens`.
- **OpenAI/Gemini path** (already canonical): passes through, preserving `cached_tokens`.

**`addBufferToUsage()`** at line 33: Adds 2000 buffer tokens to prevent context errors.

**`estimateUsage()`** at line 393: Falls back when provider doesn't return usage. Estimates input as `JSON.stringify(body).length / 4`, output as `contentLength / 4`.

**`logUsage()`** at line 404: Now a no-op hook (debug only). Console output moved to the unified `"📊 done"` line in `streamingHandler.js`.

**`buildOnStreamComplete()`** at `streamingHandler.js:113`: Creates the callback that saves request details and calls `saveUsageStats()`.

### 4.11 Byte-level SSE Formatting

**`formatSSE()`** at `open-sse/utils/streamHelpers.js:104` writes the wire format:

- `null`/`undefined` → `"data: null\n\n"`
- `{ done: true }` → `"data: [DONE]\n\n"`
- OpenAI Responses `{ event, data }` → `"event: {event}\ndata: {cleaned}\n\n"`
- Claude format (`sourceFormat === CLAUDE` and has `.type`) → `"event: {type}\ndata: {json}\n\n"`
- Default → `"data: {json}\n\n"`

**`cleanUsagePayload()`** (line 76): Strips `usage: null` and `usage.perf_metrics: null` from response payloads before serialization.

**`fixInvalidId()`** (line 65): Generates `chatcmpl-{fallback}` for generic IDs like `"chat"` or `"completion"`.

**`hasValuableContent()`** (line 37): Filters empty delta chunks for OpenAI and Claude formats before emitting.

## 5. Source Evidence

| Symbol / File | Line(s) | What It Does |
| --- | --- | --- |
| `open-sse/translator/formats.js` FORMATS | 2-16 | All format identifier constants |
| `open-sse/translator/formats.js` detectFormatByEndpoint | 22-35 | URL-path-based format detection |
| `open-sse/services/provider.js` detectFormat | 28-105 | Body-structure-based format detection |
| `open-sse/services/provider.js` getTargetFormat | 128-137 | Provider → format resolution |
| `open-sse/services/provider.js` resolveTransport | 142-147 | Multi-endpoint transport selection |
| `open-sse/translator/index.js` register | 19-29 | Translator self-registration |
| `open-sse/translator/index.js` translateRequest | 52-154 | Request translation pipeline |
| `open-sse/translator/index.js` translateResponse | 157-211 | Response translation pipeline |
| `open-sse/translator/index.js` initState | 219-263 | Per-stream translation state |
| `open-sse/translator/index.js` stripContentTypes | 35-49 | Model-config content stripping |
| `open-sse/utils/stream.js` STREAM_MODE | 20-23 | Translate/passthrough mode constants |
| `open-sse/utils/stream.js` createSSEStream | 39-465 | Main SSE transform stream |
| `open-sse/utils/stream.js` createSSETransformStreamWithLogger | 467-481 | Translate-mode stream factory |
| `open-sse/utils/stream.js` createPassthroughStreamWithLogger | 483-494 | Passthrough-mode stream factory |
| `open-sse/utils/streamHandler.js` createStreamController | 18-87 | Abort controller + disconnect handling |
| `open-sse/utils/streamHandler.js` pipeWithDisconnect | 191-253 | Stall watchdog + upstream tap |
| `open-sse/utils/streamHandler.js` createDisconnectAwareStream | 98-173 | Client-facing readable with terminal synth |
| `open-sse/utils/streamHelpers.js` parseSSELine | 4-34 | SSE line → parsed object |
| `open-sse/utils/streamHelpers.js` hasValuableContent | 37-62 | Empty delta filter |
| `open-sse/utils/streamHelpers.js` formatSSE | 104-122 | Object → SSE wire bytes |
| `open-sse/utils/streamHelpers.js` fixInvalidId | 65-74 | Malformed ID repair |
| `open-sse/utils/responsesStreamHelpers.js` isOpenAIResponsesTerminalEvent | 19-24 | Terminal event detection |
| `open-sse/utils/responsesStreamHelpers.js` buildAbortedResponsesTerminalBytes | 29-31 | Abort terminal payload |
| `open-sse/utils/responsesStreamHelpers.js` formatIncompleteOpenAIResponsesStreamFailure | 34-49 | Synthesized response.failed |
| `open-sse/handlers/chatCore.js` handleChatCore | 41-401 | Core chat orchestrator |
| `open-sse/handlers/chatCore/streamingHandler.js` handleStreamingResponse | 46-108 | Streaming response setup |
| `open-sse/handlers/chatCore/streamingHandler.js` buildTransformStream | 25-41 | Transform stream selection |
| `open-sse/handlers/chatCore/streamingHandler.js` buildOnStreamComplete | 113-144 | Stream completion + usage callback |
| `open-sse/handlers/chatCore/nonStreamingHandler.js` handleNonStreamingResponse | 201-312 | Non-streaming response handler |
| `open-sse/handlers/chatCore/nonStreamingHandler.js` translateNonStreamingResponse | 66-196 | Non-streaming response translator |
| `open-sse/handlers/chatCore/sseToJsonHandler.js` handleForcedSSEToJson | 111-249 | Forced stream→JSON (two paths) |
| `open-sse/handlers/chatCore/sseToJsonHandler.js` parseSSEToOpenAIResponse | 41-105 | SSE reassembly → completion |
| `open-sse/utils/usageTracking.js` extractUsage | 236-311 | Per-format usage extraction |
| `open-sse/utils/usageTracking.js` mergeUsage | 318-332 | Field-wise max-merge |
| `open-sse/utils/usageTracking.js` canonicalizeUsage | 164-207 | Cache-aware token normalization |
| `open-sse/utils/usageTracking.js` estimateUsage | 393-399 | Fallback estimation |
| `open-sse/utils/usageTracking.js` addBufferToUsage | 33-57 | 2000 buffer token adder |
| `open-sse/utils/usageTracking.js` filterUsageForFormat | 59-112 | Format-specific field stripping |
| `open-sse/config/runtimeConfig.js` STREAM_STALL_TIMEOUT_MS | 53 | Default 360s stall timeout |
| `open-sse/config/runtimeConfig.js` STREAM_FIRST_CHUNK_TIMEOUT_MS | 56 | Default 200s TTFT timeout |
| `open-sse/config/runtimeConfig.js` FETCH_CONNECT_TIMEOUT_MS | 59 | Default 60s connect timeout |
| `src/sse/handlers/chat.js` handleChat | 31-132 | App-level handler (combo/account routing) |
| `src/sse/handlers/chat.js` handleSingleModelChat | 137-287 | Single-model dispatch with account fallback |

### Relevant Tests

| Test File | Key Tests |
| --- | --- |
| `tests/unit/openai-responses-terminal-event.test.js` | Terminal event synthesis, Responses passthrough terminal rules, `response.failed` on abort |
| `tests/unit/kiro-terminal-integrity.test.js` | Kiro EventStream termination behavior |
| `tests/unit/responses-abort-terminal.test.js` | Aborted Responses terminal bytes |
| `tests/unit/force-stream-config.test.js` | `forceStream` provider config → SSE→JSON conversion |
| `tests/unit/base-executor-retry.test.js` | `BaseExecutor.execute` retry/fallback (affects streaming error recovery) |
| `tests/unit/cached-token-usage.test.js` | `canonicalizeUsage` cache folding |
| `tests/unit/cached-token-e2e.test.js` | Cache token E2E with streaming |
| `tests/translator/golden-response-stream.test.js` | Streaming response translation golden tests |
| `tests/translator/format-roundtrip.test.js` | Roundtrip request→response format conversions |
| `tests/translator/claude-kiro-direct.test.js` | Direct claude:kiro route (no OpenAI pivot) |
| `tests/translator/thinking-unified.test.js` | Thinking config capture/apply lifecycle |
| `tests/unit/combo-routing.test.js` | Combo rotation (affects which provider the stream hits) |

## 6. Behavioral Invariants

1. **Format detection is greedy**: `openai-responses` and `antigravity` checks run before `gemini`, which runs before `openai`, which runs before `claude`. The `claude` check requires type-annotated content blocks; plain string messages at `/v1/chat/completions` default to `openai`.
2. **Translator registry is side-effect-driven**: `register()` is called during module import. If a translator file is not imported in `index.js` lines 270-292, it does not exist.
3. **OpenAI pivot is lossy**: comment in `AGENTS.md` explicitly warns about loss of thinking, non-base64 images, tool ids, `is_error`. Direct routes preferred for fragile pairs.
4. **Stall timer tracks raw bytes**: timer reset by each upstream chunk, not by transform output. Prevents false stalls on long-thinking models.
5. **`[DONE]` sentinel is exclusive**: never sent twice (guarded by `streamDoneSent`). Never sent for Gemini-family providers.
6. **Responses passthrough synthesizes `response.failed`**: if the upstream stream closes without a terminal event, the system generates one rather than leaving the client hanging.
7. **Usage side effects are post-stream**: `logUsage` is a no-op hook — real persistence happens in `saveUsageStats` called from `buildOnStreamComplete` or `handleForcedSSEToJson`.
8. **2000 buffer tokens** are always added to usage before responding to the client.

## 7. Process-Local or Concurrency Assumptions

1. **SSE stream state is per-connection**: `initState()` creates a fresh state each time. No shared mutable state across requests.
2. **`sharedEncoder` is stateless**: comment confirms `new TextEncoder()` is safe to share.
3. **TextDecoder with `stream: true`**: Each stream gets its own decoder instance to correctly handle multi-byte characters split across chunks.
4. **AbortController per request**: Each `createStreamController` creates a new `AbortController`. Signals merged via `AbortSignal.any()` when connect timeout is also active.
5. **Disconnect 500ms delay**: `handleDisconnect` sets a 500ms timer before calling `abort()`, to allow cleanup/in-flight work.

## 8. Error and Edge-Case Behavior

| Case | Behavior | Source |
| --- | --- | --- |
| Non-JSON SSE data lines | Silently skipped, not forwarded | `streamHelpers.js:28-32` |
| Upstream returns HTML instead of SSE | Body read, `<title>` extracted, sanitized (160 chars), returned as JSON error with upstream status | `streamingHandler.js:62-80` |
| Unknown SSE line format | `parseSSELine` returns null → line ignored | `streamHelpers.js:4-34` |
| Stream stall | Controller aborts after `STALL_TIMEOUT_MS` of no upstream bytes | `streamHandler.js:203-208` |
| Disconnect during stream | 500ms delay then abort; terminal event synthesized if configured | `streamHandler.js:40-53, 98-173` |
| No valid usage in finish chunk | Estimated from body string length / content length | `stream.js:318-321` |
| Translation fails | 400 error returned; `trackPendingRequest` called with error=true | `chatCore.js:144-147` |
| Executor throws AbortError | 499 status with "Request aborted" message | `chatCore.js:319-321` |
| Executor throws other error | 502 status, error formatted via `formatProviderError` | `chatCore.js:323-327` |
| 401/403 from executor | Token refresh retry (up to 3 attempts), then re-execute | `chatCore.js:331-353` |
| Non-streaming provider returns SSE | Parsed via `parseSSEToOpenAIResponse` which reassembles chunks | `nonStreamingHandler.js:206-213` |
| Client Accept: application/json but stream:true | `stream` set to false unless provider forces streaming | `chatCore.js:101-107` |
| Empty `tool_calls: []` in streaming delta | Stripped to prevent AI SDK reasoning-tracking false positives | `stream.js:138-145` |
| Azure `prompt_filter_results` / `content_filter_results` | Stripped from passthrough chunks | `stream.js:119-131` |
| `generic` or short ID (`< 8 chars`) | Replaced with `chatcmpl-{requestId/timestamp}` | `streamHelpers.js:65-74` |

## 9. Security Implications

1. **Non-SSE upstream content sanitization**: HTML error pages from upstream are sanitized (tags stripped, 160-char limit) before being returned to the client as JSON. The comment explicitly notes: "the UI may render error.message as HTML."
2. **HTML tag stripping** done on upstream error page `<title>` extraction. Not applicable to normal streaming.
3. **No streaming content is read/forwarded without passing through the transform stream**, which normalizes format and strips unexpected fields.

## 10. Uncertainties and Conflicting Evidence

1. **`FORMATS.OPENAI_RESPONSE` (singular) vs `FORMATS.OPENAI_RESPONSES` (plural)**: Both defined. Downstream code uses `OPENAI_RESPONSES` exclusively. The singular variant appears unused but preserved.
2. **`formatIncompleteOpenAIResponsesStreamFailure` encodes `type: "response.failed"` twice**: Once as the event name, once inside the data object. This matches OpenAI's wire format but is redundant.
3. **Stream-to-JSON for forced streaming uses two completely separate code paths** (Responses API and Chat Completions) with different output construction. Both are tested, but subtle inconsistencies in tool_call handling or reasoning extraction could exist.
4. **`buildOnStreamComplete` creates a random `streamDetailId`** using `Date.now() + Math.random()`. Collisions are theoretically possible under heavy concurrency.

## 11. Parity Requirements

- Parity would require preserving or consciously replacing: format detection heuristics (body-structure + URL-path), the translator registration/dispatch pattern (direct route + OpenAI pivot), the SSE transform stream with its two modes, stall watchdog on raw bytes (not decoded output), `[DONE]` exclusion for Gemini-family, `response.failed` synthesis for Responses passthrough, forced SSE→JSON for providers with `forceStream`, and usage extraction across all supported formats.
- The 2000-token buffer on all usage values is an observed behavior that must be preserved or compensated for if estimation changes.
- The 500ms disconnect delay is a design choice that must be preserved or consciously changed.

## 12. Questions Requiring User Decisions

1. **Go implementation**: Will the translator use the same `register()` side-effect pattern, or a declarative mapping? Side-effect imports are fragile but maintain backward compatibility with the JS design.
2. **Format detection priority**: The body-inspection order is position-dependent with 8 branches. If the Go version uses a different ordering, edge cases at the boundaries (e.g., OpenAI vs Claude ambiguous bodies) may behave differently.
3. **OpenAI pivot vs direct routes**: All current direct routes should be enumerated and either preserved or consciously dropped in the Go rewrite.
4. **Stall timeout defaults**: 360s is generous. User may want different defaults in Go.

## 13. Coverage Gaps

- **Stream-to-JSON for non-standard SSE formats**: The `parseSSEToOpenAIResponse` function only handles OpenAI-style `data: {...}` lines. If a non-supporting provider emits a different SSE format, SSE→JSON conversion fails.
- **No explicit `response.completed` → `[DONE]` ordering test** for the case where `response.completed` arrives simultaneously with stream EOF.
- **`hasValuableContent` for Gemini and other formats** always returns `true` (line 62). Filtering is only implemented for OpenAI and Claude.
- **Per-modality non-streaming handlers** (image generation, TTS, STT, embeddings, search) have their own format handling not covered in this chat-focused audit.
