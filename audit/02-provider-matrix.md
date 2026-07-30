# Provider Matrix -- Source-Backed Registry Inventory

## Scope

Complete inventory of all AI providers registered in the decolua/9router codebase at the audited commit. Includes canonical IDs, aliases, auth types, service kinds, transport configuration, executor ownership, models, capabilities, and cross-cutting behavioural invariants.

## Audited Repository and Exact Commit

- **Repository**: `decolua/9router`
- **Exact commit**: `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`
- **Tag**: `v0.5.40` (2026-07-20)
- **Read-only clone**: `../upstream-original`

## Method

1. Static registry: enumerated all 100 files in open-sse/providers/registry/ (indexed by index.js).
2. Category extraction: read the category field from each registry entry, yielding 5 categories.
3. Service-kind extraction: read the serviceKinds field (default ["llm"] per providers.js:99).
4. Executor inventory: read from open-sse/executors/index.js -- 25 specialized + 1 DefaultExecutor.
5. Transport extraction: read transport and transports fields.
6. Auth analysis: cross-referenced category, authType, authModes, hasOAuth, noAuth, oauth.
7. OAuth flow-type analysis: read src/lib/oauth/providers.js PROVIDERS map.
8. Models count: aggregated from models arrays + modelsFetcher + passthroughModels flags.
9. Generated file identification: index.js auto-generated (sequential import p0..p99).
10. Reproducible counts: all verifiable against the clone.

## Confirmed Findings

### 1. Registry Overview -- Three Layers

Layer 1 -- Static registry (open-sse/providers/registry/): 100 entry files indexed by open-sse/providers/registry/index.js (auto-generated, import p0..p99).

Layer 2 -- Derived registries (open-sse/providers/index.js lines 25-56):

- PROVIDERS: transport config keyed by entry.id
- PROVIDER_MODELS: model arrays keyed by entry.alias or entry.id
- PROVIDER_OAUTH: OAuth config keyed by entry.id
- PROVIDER_MEDIA: media configs keyed by entry.id

Layer 3 -- Frontend overlay (src/shared/constants/providers.js): AI_PROVIDERS built from REGISTRY by category.

### 2. Category Distribution

64 apikey | 16 oauth | 13 freeTier | 5 free | 2 webCookie = 100 total

Verify: for f in open-sse/providers/registry/*.js; do grep -q 'category:' "$f" && echo "$(grep 'id:' "$f" | head -1 | sed 's/.*"(.*)".*/1/'): $(grep 'category:' "$f" | head -1 | sed 's/.*"(.*)".*/1/')"; done | sort | awk -F: '{count[$2]++} END {for(c in count) print c, count[c]}'

### 3. Auth-Type by Category

- apikey: 64 entries (some explicitly authType: "apikey")
- oauth: 16 entries (some authModes: ["oauth"] or ["oauth","apikey"])
- freeTier: mixed (apikey, none, or implicit)
- free: 5 with noAuth: true (opencode, mimo-free, gemini-cli, qoder, kiro)
- webCookie: 2 with authType: "cookie" (grok-web, perplexity-web)

### 4. Service Kinds

Default ["llm"] when unspecified (providers.js:99). ~55 LLM providers. Also embedding (~10), tts (~13), stt (~5), image (~16), imageToText (~12), video (~1, xai), webSearch (~15), webFetch (~4).

### 5. Executor Map

25 specialized executors in open-sse/executors/index.js. DefaultExecutor fallback for all other providers.

Specialized: Antigravity, Azure, GeminiCLI, Github, IFlow, Qoder, Kiro, Kimchi, Codex, Cursor (+ alias cu), Vertex (vertex + vertex-partner), Qwen, OpenCode, OpenCodeGo, GrokWeb, GrokCli (+ gcli, gb), PerplexityWeb, OllamaLocal, CommandCode, XiaomiTokenplan, MimoFree (+ alias mmf), CodeBuddy.

### 6. OAuth Providers (20)

src/lib/oauth/providers.js PROVIDERS object (line 65):

| Provider | Flow Type | PKCE | Fixed Port |
| --- | --- | --- | --- |
| claude | authorization_code_pkce | S256 | - |
| codex | authorization_code_pkce | S256 | 1455 |
| openai | authorization_code_pkce | S256 | - |
| gemini-cli | authorization_code | - | - |
| antigravity | authorization_code | - | - |
| iflow | authorization_code | - | - |
| qwen | device_code | S256 | - |
| qoder | device_code (custom) | S256 | - |
| github | device_code | - | - |
| kiro | device_code (AWS SSO OIDC) | - | - |
| kimi | device_code | - | - |
| kilocode | device_code (custom) | - | - |
| codebuddy-cn | device_code (custom) | - | - |
| grok-cli | device_code | - | - |
| gitlab | authorization_code_pkce | S256 | - |
| cline | authorization_code | - | - |
| clinepass | authorization_code | - | - |
| kimchi | browser_token | - | - |
| cursor | import_token | - | - |
| xai | authorization_code_pkce | S256 | 56121 |

Fixed ports: Codex 1455 (CODEX_PORT), xAI 56121 (XAI_PROXY_PORT) in src/lib/oauth/utils/server.js.

### 7. Transport Formats

openai (default), openai-responses, claude, vertex, ollama, grok-web, perplexity-web, local-device.

Multi-endpoint via transports array: DeepSeek only.

## Source Evidence

- Registry: open-sse/providers/registry/index.js (100 entries)
- AI_PROVIDERS: src/shared/constants/providers.js line 90
- Executors: open-sse/executors/index.js lines 8-32
- OAuth handlers: src/lib/oauth/providers.js line 65
- Transport builder: open-sse/providers/index.js line 11
- Default service kinds: src/shared/constants/providers.js line 99

## Behavioral Invariants

1. Provider IDs unique, matching registry filename.
2. Category determines auth UI method.
3. DefaultExecutor fallback cached in process-local Map (unbounded).
4. transports array overrides single transport (DeepSeek only).
5. OAuth config injected into transport.
6. Model list append-only (TTS models appended).
7. Custom provider nodes dynamic, not in static registry.

## Security Implications

1. OAuth client IDs/secrets in public registry.
2. AWS SSRF guard (assertValidAwsRegion).
3. SQLite credentials -- no encryption at rest.
4. Callback origin validation against allowlist.
5. xAI token refresh lead: 5 minutes.

## Uncertainties

1. mmf double-entry (mmf.js + mimo-free.js).
2. kimi-coding alias.
3. alims-intl.js as p99.
4. Music kind declared but unused.
5. kilocode inconsistent categorization.

## Coverage Gaps

1. Model capabilities not fully enumerated.
2. Pricing data not reviewed.
3. Media TTS/STT details not fully extracted.
4. alims-intl.js status uncertain.
