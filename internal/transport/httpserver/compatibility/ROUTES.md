# Compatibility Route Mapping

**Authoritative mapping table: historical external routes → Administration API v1 resource → application use case.**

This document freezes the compatibility contract for clients that were written
against the original 9Router management surface and model-compatible endpoints.
Every row below is a *compatibility route*: a historical external path that a
thin compatibility adapter may preserve (same path, method, body, status, and
error semantics) while delegating to the same application use case as its
Administration API v1 twin. A compatibility adapter is never written without a
mapping row in this table and its matching machine-checkable fixture
(`tests/fixtures/upstream/compat/*`); that invariant is enforced by
`tests/fixtures/upstream/compat/compat_fixtures_test.go`.

- **Pinned upstream baseline**: `decolua/9router` at commit
  `79918c7830695bbca4a45c9fea4a42c3e9fd73d1`. All pinned-source citations in
  this document resolve against the permalink base
  `https://github.com/decolua/9router/blob/79918c7830695bbca4a45c9fea4a42c3e9fd73d1/`.
- **Upstream route map**: the route inventory of the pinned baseline, section
  3.2 of the upstream route map (hereafter "route map §3.2"); the external
  rewrite layer and auth classification follow the HTTP contracts audit
  (`audit/01`, §4, §6).
- **Administration API v1**: the frozen contract in `api/admin-v1.openapi.yaml`
  and its route registry `internal/transport/httpserver/adminapi/v1/routes.go`
  (`RouteTable`). Compatibility rows whose disposition is `PASS` reference a
  twin that exists in that registry.
- **Public contract**: this document is public-facing and uses product terms
  ("compatibility route", "Administration API", "external client", "model
  endpoint"). Disposition and semantic notes below are part of the contract.

Row identifiers (`CMP-…`) are stable anchors: the same identifier appears as the
first column of the tables below and in the fixture rows under
`tests/fixtures/upstream/compat/`, so the mapping table and the fixtures are
machine-checkable against each other and against the Administration API route
registry.

Semantics referenced by the "Contract notes" column are defined in
[Frozen contract semantics](#frozen-contract-semantics).

---

## 1. Management compatibility routes (target: administration)

Historical external management paths (upstream spelling) mapped to their
Administration API v1 twin. `PASS` = an adapter may preserve the route by
delegating to the twin's application use case. `EXCLUDE` = the historical path
is explicitly excluded from the compatibility surface (reason in the row;
reconciliation outcomes are itemized in
[Excluded historical paths](#3-excluded-historical-paths)).

External auth tier legend (historical, from the pinned baseline): `public-api`
= public path list; `session` = dashboard session cookie; `session+pat` =
dashboard session or PAT; `local-only` = loopback plus machine-id CLI token
(replaced locally by a PAT/session boundary with a host-operation gate; the
machine-id token mechanism itself is prohibited).

Columns: **ID**, **Method**, **Historical external path**, **External auth
tier**, **Disposition**, **Administration API v1 twin**, **App use case**,
**Request body**, **Response envelope / success status**, **Error semantics**,
**CORS / headers**, **Source**.

| ID | Method | Historical external path | External auth tier | Disposition | Administration API v1 twin | App use case | Request body | Response envelope / success status | Error semantics | CORS / headers | Source |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `CMP-AUTH-01` | POST | `/auth/login` | public-api | PASS | `/auth/login` POST (public) | Dashboard sign-in | `{password}` | `{ok:true}` / 200 | 401 invalid; 429 + `Retry-After` on lockout | same-origin; Browser session sets cookie | route map §3.2 auth; pinned `src/app/api/auth/login/route.js` |
| `CMP-AUTH-02` | POST | `/auth/logout` | session | PASS | `/auth/logout` POST (session) | Sign out, revoke session | none | `{ok:true}` / 200 | 401 if unauthenticated | same-origin; session cookie cleared | route map §3.2 auth; pinned `src/app/api/auth/logout/route.js` |
| `CMP-AUTH-03` | GET | `/auth/status` | public-api | PASS | `/auth/status` GET (session+pat) | Session/actor status | none | actor object / 200 | 401 if unauthenticated | same-origin; cookie or PAT | route map §3.2 auth; pinned `src/app/api/auth/status/route.js` |
| `CMP-AUTH-04` | GET | `/auth/oidc/start` | public-api | PASS | `/auth/oidc/start` GET (public) | Begin OIDC flow | none | redirect / 302 | 400 invalid issuer (HTTPS-only) | same-origin; single-use state cookie | route map §3.2 auth; pinned `src/app/api/auth/oidc/start/route.js` |
| `CMP-AUTH-05` | GET | `/auth/oidc/callback` | public-api | PASS | `/auth/oidc/callback` GET (public) | OIDC callback | none | redirect / 302 | 400 on PKCE/nonce/replay rejection | same-origin; state cookie | route map §3.2 auth; pinned `src/app/api/auth/oidc/callback/route.js` |
| `CMP-AUTH-06` | POST | `/auth/oidc/test` | session+pat | PASS | `/auth/oidc/test` POST (session+pat) | OIDC probe | OIDC config | probe result / 200 | 400 invalid config | same-origin; cookie or PAT | route map §3.2 auth; pinned `src/app/api/auth/oidc/test/route.js` |
| `CMP-KEY-01` | GET | `/keys` | session+pat | PASS | `/keys` GET (session+pat) | List API keys | none | array of masked keys / 200 | 401 unauthenticated; 500 storage | same-origin; cookie or PAT | route map §3.2 keys; pinned `src/app/api/keys/route.js` (GET/POST) |
| `CMP-KEY-02` | POST | `/keys` | session+pat | PASS | `/keys` POST (session+pat) | Create API key | `{name}` | `{key, ...}` full value once / 201 | 400 invalid name; 409 duplicate | same-origin; cookie or PAT | route map §3.2 keys; pinned `src/app/api/keys/route.js` (GET/POST, machineId) |
| `CMP-KEY-03` | GET | `/keys/[id]` | session+pat | PASS | `/keys/{id}` GET (session+pat) | API key detail | none | key object (prefix-only) / 200 | 404 not found; 403 wrong actor | same-origin; cookie or PAT | route map §3.2 keys/[id]; pinned `src/app/api/keys/[id]/route.js` (GET/PUT/DELETE) |
| `CMP-KEY-04` | PUT | `/keys/[id]` | session+pat | PASS | `/keys/{id}` PUT (session+pat) | Update API key | `{name, enabled}` | updated key / 200 | 404 not found; 400 invalid | same-origin; cookie or PAT | route map §3.2 keys/[id]; pinned `src/app/api/keys/[id]/route.js` |
| `CMP-KEY-05` | DELETE | `/keys/[id]` | session+pat | PASS | `/keys/{id}` DELETE (session+pat) | Revoke API key | none | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 keys/[id]; pinned `src/app/api/keys/[id]/route.js` |
| `CMP-PROV-01` | GET | `/providers` | session+pat | PASS | `/providers` GET (session+pat) | List providers | none | array of providers / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 providers; pinned `src/app/api/providers/route.js` |
| `CMP-PROV-02` | POST | `/providers` | session+pat | PASS | `/providers` POST (session+pat) | Create provider connection | provider config (credential-safe) | created provider / 201 | 400 validation; 422 unsupported | same-origin; cookie or PAT | route map §3.2 providers; pinned `src/app/api/providers/route.js` |
| `CMP-PROV-03` | GET | `/providers/[id]` | session+pat | PASS | `/providers/{id}` GET (session+pat) | Provider detail | none | provider (has_credentials, never key value) / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 providers [id]; pinned `src/app/api/providers/[id]/route.js` |
| `CMP-PROV-04` | PUT | `/providers/[id]` | session+pat | PASS | `/providers/{id}` PUT (session+pat) | Update provider | partial provider config | updated provider / 200 | 404 not found; 400 invalid | same-origin; cookie or PAT | route map §3.2 providers [id]; pinned `src/app/api/providers/[id]/route.js` |
| `CMP-PROV-05` | DELETE | `/providers/[id]` | session+pat | PASS | `/providers/{id}` DELETE (session+pat) | Remove provider | none | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 providers [id]; pinned `src/app/api/providers/[id]/route.js` |
| `CMP-PROV-06` | GET | `/providers/client` | session+pat | PASS | `/providers/client` GET (session+pat) | Safe-field client projection | none | client-safe provider list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 providers client; pinned `src/app/api/providers/client/route.js` (SAFE_FIELDS) |
| `CMP-PROV-07` | GET | `/providers/kilo/free-models` | session+pat | PASS | `/providers/kilo/free-models` GET (session+pat) | Kilo free models catalog | none | model list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 providers kilo; pinned `src/app/api/providers/kilo/` |
| `CMP-PROV-08` | GET | `/providers/suggested-models` | session+pat | PASS | `/providers/suggested-models` GET (session+pat) | Suggested models catalog | none | model list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 providers suggested-models; pinned `src/app/api/providers/suggested-models/` |
| `CMP-PROV-09` | POST | `/providers/test-batch` | session+pat | PASS | `/providers/test-batch` POST (session+pat) | Test all providers (bounded concurrency) | `{}` scope filter | per-provider test results / 200 | 422 on unsupported; partial results with per-row status | same-origin; cookie or PAT | route map §3.2 providers test-batch; pinned `src/app/api/providers/test-batch/` |
| `CMP-PROV-10` | POST | `/providers/validate` | session+pat | PASS | `/providers/validate` POST (session+pat) | Validate provider credential | provider config | validation result / 200 | 400 invalid config; 422 credential rejected | same-origin; cookie or PAT | route map §3.2 providers validate; pinned `src/app/api/providers/validate/` |
| `CMP-NODE-01` | GET | `/provider-nodes` | session+pat | PASS | `/provider-nodes` GET (session+pat) | List provider nodes | none | array of nodes / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 provider-nodes; pinned `src/app/api/provider-nodes/route.js` |
| `CMP-NODE-02` | POST | `/provider-nodes` | session+pat | PASS | `/provider-nodes` POST (session+pat) | Create provider node | node config | created node / 201 | 400 validation; 422 unsupported | same-origin; cookie or PAT | route map §3.2 provider-nodes; pinned `src/app/api/provider-nodes/route.js` |
| `CMP-NODE-03` | GET | `/provider-nodes/[id]` | session+pat | PASS | `/provider-nodes/{id}` GET (session+pat) | Node detail | none | node object / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 provider-nodes [id]; pinned `src/app/api/provider-nodes/[id]/route.js` |
| `CMP-NODE-04` | PUT | `/provider-nodes/[id]` | session+pat | PASS | `/provider-nodes/{id}` PUT (session+pat) | Update node | partial node config | updated node / 200 | 404 not found; 400 invalid | same-origin; cookie or PAT | route map §3.2 provider-nodes [id]; pinned `src/app/api/provider-nodes/[id]/route.js` |
| `CMP-NODE-05` | DELETE | `/provider-nodes/[id]` | session+pat | PASS | `/provider-nodes/{id}` DELETE (session+pat) | Remove node | none | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 provider-nodes [id]; pinned `src/app/api/provider-nodes/[id]/route.js` |
| `CMP-NODE-06` | POST | `/provider-nodes/validate` | session+pat | PASS | `/provider-nodes/validate` POST (session+pat) | Validate node config | node config | validation result / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 provider-nodes validate; pinned `src/app/api/provider-nodes/validate/` |
| `CMP-POOL-01` | GET | `/proxy-pools` | session+pat | PASS | `/proxy-pools` GET (session+pat) | List proxy pools | none | array of pools / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 proxy-pools; pinned `src/app/api/proxy-pools/route.js` |
| `CMP-POOL-02` | POST | `/proxy-pools` | session+pat | PASS | `/proxy-pools` POST (session+pat) | Create proxy pool | pool config | created pool / 201 | 400 validation | same-origin; cookie or PAT | route map §3.2 proxy-pools; pinned `src/app/api/proxy-pools/route.js` |
| `CMP-POOL-03` | GET | `/proxy-pools/[id]` | session+pat | PASS | `/proxy-pools/{id}` GET (session+pat) | Pool detail | none | pool object / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 proxy-pools [id]; pinned `src/app/api/proxy-pools/[id]/route.js` |
| `CMP-POOL-04` | PUT | `/proxy-pools/[id]` | session+pat | PASS | `/proxy-pools/{id}` PUT (session+pat) | Update pool | partial pool config | updated pool / 200 | 404 not found; 400 invalid | same-origin; cookie or PAT | route map §3.2 proxy-pools [id]; pinned `src/app/api/proxy-pools/[id]/route.js` |
| `CMP-POOL-05` | DELETE | `/proxy-pools/[id]` | session+pat | PASS | `/proxy-pools/{id}` DELETE (session+pat) | Remove pool | none | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 proxy-pools [id]; pinned `src/app/api/proxy-pools/[id]/route.js` |
| `CMP-POOL-06` | POST | `/proxy-pools/cloudflare-deploy` | session+pat | PASS | `/proxy-pools/cloudflare-deploy` POST (session+pat, host-gated) | Deploy helper: Cloudflare | pool id / config | deployment config / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 proxy-pools cloudflare-deploy; pinned `src/app/api/proxy-pools/cloudflare-deploy/` |
| `CMP-POOL-07` | POST | `/proxy-pools/deno-deploy` | session+pat | PASS | `/proxy-pools/deno-deploy` POST (session+pat, host-gated) | Deploy helper: Deno | pool id / config | deployment config / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 proxy-pools deno-deploy; pinned `src/app/api/proxy-pools/deno-deploy/` |
| `CMP-POOL-08` | POST | `/proxy-pools/vercel-deploy` | session+pat | PASS | `/proxy-pools/vercel-deploy` POST (session+pat, host-gated) | Deploy helper: Vercel | pool id / config | deployment config / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 proxy-pools vercel-deploy; pinned `src/app/api/proxy-pools/vercel-deploy/` |
| `CMP-MOD-01` | GET | `/models` | session+pat | PASS | `/models` GET (session+pat) | List models | none | array of models / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 models; pinned `src/app/api/models/route.js` (GET/PUT) |
| `CMP-MOD-02` | PUT | `/models` | session+pat | PASS | `/models` PUT (session+pat) | Apply model configuration | model config | updated models / 200 | 400 validation | same-origin; cookie or PAT | route map §3.2 models; pinned `src/app/api/models/route.js` (GET/PUT) |
| `CMP-MOD-03` | GET | `/models/alias` | session+pat | PASS | `/models/alias` GET (session+pat) | List model aliases | none | array of aliases / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 models alias; pinned `src/app/api/models/alias/` (GET/PUT/DELETE) |
| `CMP-MOD-04` | PUT | `/models/alias` | session+pat | PASS | `/models/alias` PUT (session+pat) | Upsert model alias | alias map | updated aliases / 200 | 400 invalid | same-origin; cookie or PAT | route map §3.2 models alias; pinned `src/app/api/models/alias/` |
| `CMP-MOD-05` | DELETE | `/models/alias` | session+pat | PASS | `/models/alias` DELETE (session+pat) | Remove model alias | alias key | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 models alias; pinned `src/app/api/models/alias/` |
| `CMP-MOD-06` | GET | `/models/custom` | session+pat | PASS | `/models/custom` GET (session+pat) | List custom models | none | array of custom models / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 models custom; pinned `src/app/api/models/custom/` (GET/POST/DELETE) |
| `CMP-MOD-07` | POST | `/models/custom` | session+pat | PASS | `/models/custom` POST (session+pat) | Create custom model | model config | created model / 201 | 400 validation | same-origin; cookie or PAT | route map §3.2 models custom; pinned `src/app/api/models/custom/` |
| `CMP-MOD-08` | DELETE | `/models/custom` | session+pat | PASS | `/models/custom` DELETE (session+pat) | Remove custom model | model id | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 models custom; pinned `src/app/api/models/custom/` |
| `CMP-MOD-09` | GET | `/models/disabled` | session+pat | PASS | `/models/disabled` GET (session+pat) | List disabled models | none | array of disabled models / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 models disabled; pinned `src/app/api/models/disabled/` (GET/POST/DELETE) |
| `CMP-MOD-10` | POST | `/models/disabled` | session+pat | PASS | `/models/disabled` POST (session+pat) | Disable a model | model id | `{ok:true}` / 200 | 400 invalid; 404 not found | same-origin; cookie or PAT | route map §3.2 models disabled; pinned `src/app/api/models/disabled/` |
| `CMP-MOD-11` | DELETE | `/models/disabled` | session+pat | PASS | `/models/disabled` DELETE (session+pat) | Re-enable a model | model id | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 models disabled; pinned `src/app/api/models/disabled/` |
| `CMP-MOD-12` | POST | `/models/test` | session+pat | PASS | `/models/test` POST (session+pat) | Probe a model | model id / prompt | probe result / 200 | 422 provider unavailable | same-origin; cookie or PAT | route map §3.2 models test; pinned `src/app/api/models/test/` |
| `CMP-MOD-13` | GET | `/models/availability` | session+pat | PASS | `/models/availability` GET (session+pat) | Model availability | none | availability map / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 models availability; pinned `src/app/api/models/availability/` |
| `CMP-COMBO-01` | GET | `/combos` | session+pat | PASS | `/combos` GET (session+pat) | List combos | none | array of combos / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 combos; pinned `src/app/api/combos/route.js` |
| `CMP-COMBO-02` | POST | `/combos` | session+pat | PASS | `/combos` POST (session+pat) | Create combo | combo config | created combo / 201 | 400 validation; 422 no valid combo | same-origin; cookie or PAT | route map §3.2 combos; pinned `src/app/api/combos/route.js` |
| `CMP-COMBO-03` | GET | `/combos/[id]` | session+pat | PASS | `/combos/{id}` GET (session+pat) | Combo detail | none | combo object / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 combos [id]; pinned `src/app/api/combos/[id]/route.js` |
| `CMP-COMBO-04` | PUT | `/combos/[id]` | session+pat | PASS | `/combos/{id}` PUT (session+pat) | Update combo | partial combo config | updated combo / 200 | 404 not found; 400 invalid | same-origin; cookie or PAT | route map §3.2 combos [id]; pinned `src/app/api/combos/[id]/route.js` |
| `CMP-COMBO-05` | DELETE | `/combos/[id]` | session+pat | PASS | `/combos/{id}` DELETE (session+pat) | Remove combo | none | `{ok:true}` / 200 | 404 not found | same-origin; cookie or PAT | route map §3.2 combos [id]; pinned `src/app/api/combos/[id]/route.js` |
| `CMP-PRICE-01` | GET | `/pricing` | session+pat | PASS | `/pricing` GET (session+pat) | Read pricing overrides | none | pricing config / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 pricing; pinned `src/app/api/pricing/route.js` (GET/PATCH/DELETE) |
| `CMP-PRICE-02` | PATCH | `/pricing` | session+pat | PASS | `/pricing` PATCH (session+pat) | Apply pricing override | pricing override | updated pricing / 200 | 400 invalid; audited | same-origin; cookie or PAT | route map §3.2 pricing; pinned `src/app/api/pricing/route.js` |
| `CMP-PRICE-03` | DELETE | `/pricing` | session+pat | PASS | `/pricing` DELETE (session+pat) | Reset pricing overrides | none | `{ok:true}` / 200 | audited | same-origin; cookie or PAT | route map §3.2 pricing; pinned `src/app/api/pricing/route.js` |
| `CMP-USE-01` | GET | `/usage/stats` | session+pat | PASS | `/usage/stats` GET (session+pat) | Usage statistics | none | stats object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage stats; pinned `src/app/api/usage/stats/` |
| `CMP-USE-02` | GET | `/usage/history` | session+pat | PASS | `/usage/history` GET (session+pat) | Usage history | none | series / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage history; pinned `src/app/api/usage/history/` |
| `CMP-USE-03` | GET | `/usage/chart` | session+pat | PASS | `/usage/chart` GET (session+pat) | Usage chart series | none | chart series / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage chart; pinned `src/app/api/usage/chart/` |
| `CMP-USE-04` | GET | `/usage/providers` | session+pat | PASS | `/usage/providers` GET (session+pat) | Usage by provider | none | per-provider usage / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage providers; pinned `src/app/api/usage/providers/` |
| `CMP-USE-05` | GET | `/usage/request-details` | session+pat | PASS | `/usage/request-details` GET (session+pat) | Request detail records | none | request records (metadata; debug opt-in) / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage request-details; pinned `src/app/api/usage/request-details/` |
| `CMP-USE-06` | GET | `/usage/request-logs` | session+pat | PASS | `/usage/request-logs` GET (session+pat) | Request log records | none | log records / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage request-logs; pinned `src/app/api/usage/request-logs/` |
| `CMP-USE-07` | GET | `/usage/logs` | session+pat | PASS | `/usage/logs` GET (session+pat) | Usage log records | none | log records / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 usage logs; pinned `src/app/api/usage/logs/` |
| `CMP-USE-08` | GET | `/usage/[connectionId]` | session+pat | PASS | `/usage/{connectionId}` GET (session+pat) | Per-connection usage | none | connection usage / 200 | 404 unknown connection | same-origin; cookie or PAT | route map §3.2 usage [connectionId]; pinned `src/app/api/usage/[connectionId]/route.js` |
| `CMP-USE-09` | GET | `/usage/stream` | session | PASS | `/usage/stream` GET (session) | Realtime usage stream | none | `text/event-stream`; stats push + light push; 25 s keepalive | stream abort on disconnect; 401 unauthenticated | stream: EventSource; session-cookie-only; `Cache-Control: no-cache` | route map §3.2 usage stream; pinned `src/app/api/usage/stream/route.js` (L12-69) |
| `CMP-CLI-01` | GET | `/cli-tools/all-statuses` | session+pat | PASS | `/cli-tools/all-statuses` GET (session+pat) | All CLI tools status | none | per-tool status map / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 cli-tools all-statuses; pinned `src/app/api/cli-tools/all-statuses/` |
| `CMP-CLI-02` | GET | `/cli-tools/[tool]` | session+pat | PASS | `/cli-tools/{tool}` GET (session+pat) | Tool settings (16 exact tool routes) | none | tool settings / 200 | 404 unknown tool (`{tool}` enum, 16 names) | same-origin; cookie or PAT | route map §3.2 cli-tools; pinned `src/app/api/cli-tools/<tool>/route.js` (16 tool dirs) |
| `CMP-CLI-03` | POST | `/cli-tools/[tool]` | session+pat | PASS | `/cli-tools/{tool}` POST (session+pat) | Tool action | action payload | action result / 200 | 404 unknown tool; 400 invalid action | same-origin; cookie or PAT | route map §3.2 cli-tools; pinned `src/app/api/cli-tools/<tool>/route.js` |
| `CMP-CLI-04` | POST | `/cli-tools/antigravity-mitm/alias` | session+pat | PASS | `/cli-tools/antigravity-mitm/alias` POST (session+pat) | Antigravity MITM alias | alias payload | `{ok:true}` / 200 | 400 invalid | same-origin; cookie or PAT | route map §3.2 cli-tools antigravity-mitm; pinned `src/app/api/cli-tools/antigravity-mitm/alias/` |
| `CMP-MEDIA-01` | GET | `/media-providers/tts/voices` | session+pat | PASS | `/media-providers/tts/voices` GET (session+pat) | TTS voices | none | voice list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 media-providers tts; pinned `src/app/api/media-providers/tts/voices/` |
| `CMP-MEDIA-02` | GET | `/media-providers/tts/deepgram/voices` | session+pat | PASS | `/media-providers/tts/deepgram/voices` GET (session+pat) | Deepgram voices | none | voice list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 media-providers deepgram; pinned `src/app/api/media-providers/tts/deepgram/voices/` |
| `CMP-MEDIA-03` | GET | `/media-providers/tts/elevenlabs/voices` | session+pat | PASS | `/media-providers/tts/elevenlabs/voices` GET (session+pat) | ElevenLabs voices | none | voice list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 media-providers elevenlabs; pinned `src/app/api/media-providers/tts/elevenlabs/voices/` |
| `CMP-MEDIA-04` | GET | `/media-providers/tts/inworld/voices` | session+pat | PASS | `/media-providers/tts/inworld/voices` GET (session+pat) | Inworld voices | none | voice list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 media-providers inworld; pinned `src/app/api/media-providers/tts/inworld/voices/` |
| `CMP-MEDIA-05` | GET | `/media-providers/tts/minimax/voices` | session+pat | PASS | `/media-providers/tts/minimax/voices` GET (session+pat) | MiniMax voices | none | voice list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 media-providers minimax; pinned `src/app/api/media-providers/tts/minimax/voices/` |
| `CMP-OAUTH-01` | POST | `/oauth/codex/import-token` | session+pat | PASS | `/oauth/codex/import-token` POST (session+pat) | Import Codex token | token payload | `{ok:true}` / 200 | 400 invalid token; 422 credential rejected | same-origin; cookie or PAT | route map §3.2 oauth codex; pinned `src/app/api/oauth/codex/import-token/` |
| `CMP-OAUTH-02` | POST | `/oauth/codex/bulk-import` | session+pat | PASS | `/oauth/codex/bulk-import` POST (session+pat) | Bulk import Codex tokens | token array | per-row results / 200 | partial results with per-row status | same-origin; cookie or PAT | route map §3.2 oauth codex; pinned `src/app/api/oauth/codex/bulk-import/` |
| `CMP-OAUTH-03` | POST | `/oauth/cursor/import` | session+pat | PASS | `/oauth/cursor/import` POST (session+pat) | Import Cursor session | session payload | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth cursor; pinned `src/app/api/oauth/cursor/import/` |
| `CMP-OAUTH-04` | POST | `/oauth/cursor/auto-import` | local-only | PASS | `/oauth/cursor/auto-import` POST (session+pat) | Auto-import Cursor session | path/scan payload | `{ok:true}` / 200 | local-only replaced by session+pat boundary | same-origin; cookie or PAT | route map §3.2 oauth cursor; pinned `src/app/api/oauth/cursor/auto-import/` |
| `CMP-OAUTH-05` | POST | `/oauth/gitlab/pat` | session+pat | PASS | `/oauth/gitlab/pat` POST (session+pat) | Import GitLab PAT | PAT payload | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth gitlab; pinned `src/app/api/oauth/gitlab/pat/` |
| `CMP-OAUTH-06` | POST | `/oauth/iflow/cookie` | session+pat | PASS | `/oauth/iflow/cookie` POST (session+pat) | Import IFlow cookie | cookie payload | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth iflow; pinned `src/app/api/oauth/iflow/cookie/` |
| `CMP-OAUTH-07` | POST | `/oauth/kiro/import` | session+pat | PASS | `/oauth/kiro/import` POST (session+pat) | Import Kiro session | session payload | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth kiro; pinned `src/app/api/oauth/kiro/import/` |
| `CMP-OAUTH-08` | POST | `/oauth/kiro/auto-import` | local-only | PASS | `/oauth/kiro/auto-import` POST (session+pat) | Auto-import Kiro session | path/scan payload | `{ok:true}` / 200 | local-only replaced by session+pat boundary | same-origin; cookie or PAT | route map §3.2 oauth kiro; pinned `src/app/api/oauth/kiro/auto-import/` |
| `CMP-OAUTH-09` | POST | `/oauth/kiro/import-cli-proxy` | session+pat | PASS | `/oauth/kiro/import-cli-proxy` POST (session+pat) | Kiro CLI proxy import | proxy payload | `{ok:true}` / 200 | 400 invalid | same-origin; cookie or PAT | route map §3.2 oauth kiro; pinned `src/app/api/oauth/kiro/import-cli-proxy/` |
| `CMP-OAUTH-10` | POST | `/oauth/kiro/api-key` | session+pat | PASS | `/oauth/kiro/api-key` POST (session+pat) | Import Kiro API key | key payload | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth kiro; pinned `src/app/api/oauth/kiro/api-key/` |
| `CMP-OAUTH-11` | POST | `/oauth/kiro/social-authorize` | session+pat | PASS | `/oauth/kiro/social-authorize` POST (session+pat) | Kiro social authorize | authorize payload | `{ok:true}` / 200 | 400 invalid | same-origin; cookie or PAT | route map §3.2 oauth kiro; pinned `src/app/api/oauth/kiro/social-authorize/` |
| `CMP-OAUTH-12` | POST | `/oauth/kiro/social-exchange` | session+pat | PASS | `/oauth/kiro/social-exchange` POST (session+pat) | Kiro social token exchange | exchange payload | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth kiro; pinned `src/app/api/oauth/kiro/social-exchange/` |
| `CMP-OAUTH-13` | GET | `/oauth/[provider]` | session+pat | PASS | `/oauth/{provider}` GET (session+pat) | OAuth provider status | none | provider status / 200 | 404 unknown provider | same-origin; cookie or PAT | route map §3.2 oauth [provider] (local admin surface); pinned `src/app/api/oauth/[provider]/route.js` |
| `CMP-OAUTH-14` | POST | `/oauth/[provider]` | session+pat | PASS | `/oauth/{provider}` POST (session+pat) | Connect OAuth provider | provider scope/config | `{ok:true}` / 200 | 400 invalid; 422 rejected | same-origin; cookie or PAT | route map §3.2 oauth [provider]; pinned `src/app/api/oauth/[provider]/route.js` |
| `CMP-OAUTH-15` | DELETE | `/oauth/[provider]` | session+pat | PASS | `/oauth/{provider}` DELETE (session+pat) | Disconnect OAuth provider | none | `{ok:true}` / 200 | 404 unknown provider | same-origin; cookie or PAT | route map §3.2 oauth [provider]; pinned `src/app/api/oauth/[provider]/route.js` |
| `CMP-TUNNEL-01` | GET | `/tunnel` | session+pat | PASS | `/tunnel` GET (session+pat) | Tunnel configuration | none | tunnel config / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 tunnel; pinned `src/lib/tunnel/cloudflare` + `src/app/api/tunnel/` |
| `CMP-TUNNEL-02` | POST | `/tunnel/enable` | local-only | PASS | `/tunnel/enable` POST (session+pat, host-gated) | Enable tunnel | config | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 tunnel enable; pinned `src/app/api/tunnel/enable/` |
| `CMP-TUNNEL-03` | POST | `/tunnel/disable` | local-only | PASS | `/tunnel/disable` POST (session+pat, host-gated) | Disable tunnel | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 tunnel disable; pinned `src/app/api/tunnel/disable/` |
| `CMP-TUNNEL-04` | GET | `/tunnel/status` | session+pat | PASS | `/tunnel/status` GET (session+pat) | Tunnel status | none | status object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 tunnel status; pinned `src/app/api/tunnel/status/` |
| `CMP-TUNNEL-05` | GET | `/tunnel/tailscale-check` | local-only | PASS | `/tunnel/tailscale-check` GET (session+pat) | Tailscale availability check | none | check result / 200 | authenticated as session+pat (local-only replaced) | same-origin; cookie or PAT | route map §3.2 tunnel tailscale-check; pinned `src/app/api/tunnel/tailscale-check/` |
| `CMP-TUNNEL-06` | POST | `/tunnel/tailscale-install` | local-only | PASS | `/tunnel/tailscale-install` POST (session+pat, host-gated) | Install Tailscale | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 tunnel tailscale-install; pinned `src/app/api/tunnel/tailscale-install/` |
| `CMP-TUNNEL-07` | POST | `/tunnel/tailscale-enable` | local-only | PASS | `/tunnel/tailscale-enable` POST (session+pat, host-gated) | Enable Tailscale funnel | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 tunnel tailscale-enable; pinned `src/app/api/tunnel/tailscale-enable/` |
| `CMP-TUNNEL-08` | POST | `/tunnel/tailscale-disable` | local-only | PASS | `/tunnel/tailscale-disable` POST (session+pat, host-gated) | Disable Tailscale funnel | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 tunnel tailscale-disable; pinned `src/app/api/tunnel/tailscale-disable/` |
| `CMP-HR-01` | POST | `/headroom/start` | local-only | PASS | `/headroom/start` POST (session+pat, host-gated) | Start Headroom | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.7 headroom; pinned `src/app/api/headroom/start/` |
| `CMP-HR-02` | POST | `/headroom/stop` | local-only | PASS | `/headroom/stop` POST (session+pat, host-gated) | Stop Headroom | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 headroom stop; pinned `src/app/api/headroom/stop/` |
| `CMP-HR-03` | POST | `/headroom/restart` | local-only | PASS | `/headroom/restart` POST (session+pat, host-gated) | Restart Headroom | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 headroom restart; pinned `src/app/api/headroom/restart/` |
| `CMP-HR-04` | GET | `/headroom/status` | session+pat | PASS | `/headroom/status` GET (session+pat) | Headroom status | none | status object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 headroom status; pinned `src/app/api/headroom/status/` |
| `CMP-HR-05` | GET | `/headroom/extras` | local-only | PASS | `/headroom/extras` GET (session+pat, host-gated) | Headroom extras | none | extras / 200 | 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 headroom extras; pinned `src/app/api/headroom/extras/` |
| `CMP-HR-06` | GET | `/headroom/proxy/[...path]` | local-only | PASS | `/headroom/proxy/{path}` GET (session+pat, host-gated) | Headroom proxy (multi-segment) | none | proxied response / 200 | 503 feature disabled; audited; multi-segment upstream catch-all mapped to single-segment twin | same-origin; cookie or PAT | route map §3.2 headroom proxy; pinned `src/app/api/headroom/proxy/[...path]/` |
| `CMP-HR-07` | POST | `/headroom/proxy/[...path]` | local-only | PASS | `/headroom/proxy/{path}` POST (session+pat, host-gated) | Headroom proxy action | proxied payload | proxied response / 200 | 503 feature disabled; audited; multi-segment upstream catch-all | same-origin; cookie or PAT | route map §3.2 headroom proxy; pinned `src/app/api/headroom/proxy/[...path]/` |
| `CMP-PXP-01` | POST | `/pxpipe/install` | local-only | PASS | `/pxpipe/install` POST (session+pat, host-gated) | Install Pxpipe | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 pxpipe install; pinned `src/lib/pxpipe/` + `src/app/api/pxpipe/` |
| `CMP-PXP-02` | POST | `/pxpipe/start` | local-only | PASS | `/pxpipe/start` POST (session+pat, host-gated) | Start Pxpipe | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 pxpipe start; pinned `src/app/api/pxpipe/start/` |
| `CMP-PXP-03` | POST | `/pxpipe/stop` | local-only | PASS | `/pxpipe/stop` POST (session+pat, host-gated) | Stop Pxpipe | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 pxpipe stop; pinned `src/app/api/pxpipe/stop/` |
| `CMP-PXP-04` | POST | `/pxpipe/restart` | local-only | PASS | `/pxpipe/restart` POST (session+pat, host-gated) | Restart Pxpipe | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 pxpipe restart; pinned `src/app/api/pxpipe/restart/` |
| `CMP-PXP-05` | GET | `/pxpipe/status` | session+pat | PASS | `/pxpipe/status` GET (session+pat) | Pxpipe status | none | status object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 pxpipe status; pinned `src/app/api/pxpipe/status/` |
| `CMP-PXP-06` | GET | `/pxpipe/health` | session+pat | PASS | `/pxpipe/health` GET (session+pat) | Pxpipe health probe | none | health object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 pxpipe health; pinned `src/app/api/pxpipe/health/` |
| `CMP-PXP-07` | POST | `/pxpipe/health` | local-only | PASS | `/pxpipe/health` POST (session+pat, host-gated) | Pxpipe health action | probe payload | health result / 200 | 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.2 pxpipe health (GET+POST); pinned `src/app/api/pxpipe/health/` |
| `CMP-PXP-08` | GET | `/pxpipe/logs` | session+pat | PASS | `/pxpipe/logs` GET (session+pat) | Pxpipe logs | none | log records / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 pxpipe logs; pinned `src/app/api/pxpipe/logs/` |
| `CMP-PXP-09` | GET | `/pxpipe/stats` | session+pat | PASS | `/pxpipe/stats` GET (session+pat) | Pxpipe statistics | none | stats object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 pxpipe stats; pinned `src/app/api/pxpipe/stats/` |
| `CMP-MCP-01` | POST | `/mcp/[plugin]/message` | local-only | PASS | `/mcp/{plugin}/message` POST (session+pat, host-gated) | MCP plugin message (JSON-RPC) | JSON-RPC payload | JSON-RPC response / 200 | 428 confirm-required; 503 feature disabled; audited; labelled stream, not chat | same-origin; cookie or PAT | route map §3.2 mcp message; pinned `src/lib/mcp/stdioSseBridge.js` |
| `CMP-MCP-02` | GET | `/mcp/[plugin]/sse` | local-only | PASS | `/mcp/{plugin}/sse` GET (session) | MCP plugin SSE stream | none | `text/event-stream` / 200 | stream abort on disconnect; 401 unauthenticated | stream: session-cookie-only; `Cache-Control: no-cache` | route map §3.2 mcp sse; pinned `src/app/api/mcp/[plugin]/sse/` |
| `CMP-SET-01` | GET | `/settings` | session+pat | PASS | `/settings` GET (session+pat) | Read settings | none | settings object / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 settings route; pinned `src/app/api/settings/route.js` (GET/PATCH) |
| `CMP-SET-02` | PATCH | `/settings` | session+pat | PASS | `/settings` PATCH (session+pat) | Update settings | partial settings | updated settings / 200 | 400 invalid; audited | same-origin; cookie or PAT | route map §3.2 settings route; pinned `src/app/api/settings/route.js` |
| `CMP-SET-03` | GET | `/settings/database` | session+pat | PASS | `/settings/database` GET (session+pat) | Read database settings / config-transfer export | none | config-transfer payload / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 settings database; pinned `src/app/api/settings/database/` |
| `CMP-SET-04` | POST | `/settings/database` | session+pat | PASS | `/settings/database` POST (session+pat) | Config-transfer import (manual, partial/destructive, confirmed) | config-transfer payload | `{ok:true}` / 200 | 428 confirm-required; 422 invalid payload; audited | same-origin; cookie or PAT | route map §3.2 settings database; pinned `src/app/api/settings/database/` |
| `CMP-SET-05` | GET | `/settings/require-login` | session+pat | PASS | `/settings/require-login` GET (session+pat) | Read require-login policy | none | `{requireLogin:bool}` / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 settings require-login; pinned `src/app/api/settings/require-login/` (GET only upstream; admin adds PUT) |
| `CMP-SET-06` | PUT | `/settings/require-login` | session+pat | PASS | `/settings/require-login` PUT (session+pat) | Set require-login policy | `{requireLogin:bool}` | `{ok:true}` / 200 | write surface added by admin (upstream GET only) | same-origin; cookie or PAT | route map §3.2 settings require-login; admin twin adds write surface |
| `CMP-SET-07` | POST | `/settings/proxy-test` | session+pat | PASS | `/settings/proxy-test` POST (session+pat) | Proxy connectivity test | test config | test result / 200 | 422 unreachable; 400 invalid | same-origin; cookie or PAT | route map §3.2 settings proxy-test; pinned `src/app/api/settings/proxy-test/` |
| `CMP-TRANS-01` | POST | `/translator/translate` | session+pat | PASS | `/translator/translate` POST (session+pat) | Translate payload | translation request | translation result / 200 | 422 provider unavailable; feature-gated | same-origin; cookie or PAT | route map §3.2 translator translate; pinned `src/app/api/translator/translate/` |
| `CMP-TRANS-02` | GET | `/translator/load` | session+pat | PASS | `/translator/load` GET (session+pat) | Load translator config | none | translator config / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 translator load; pinned `src/app/api/translator/load/` |
| `CMP-TRANS-03` | POST | `/translator/save` | session+pat | PASS | `/translator/save` POST (session+pat) | Save translator config | translator config | `{ok:true}` / 200 | 400 invalid | same-origin; cookie or PAT | route map §3.2 translator save; pinned `src/app/api/translator/save/` |
| `CMP-TRANS-04` | POST | `/translator/send` | session+pat | PASS | `/translator/send` POST (session+pat) | Send translator request | request payload | result / 200 | 422 provider unavailable | same-origin; cookie or PAT | route map §3.2 translator send; pinned `src/app/api/translator/send/` |
| `CMP-TRANS-05` | GET | `/translator/console-logs` | session+pat | PASS | `/translator/console-logs` GET (session+pat) | Read translator console logs | none | log records / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 translator console-logs; pinned `src/app/api/translator/console-logs/` |
| `CMP-TRANS-06` | DELETE | `/translator/console-logs` | session+pat | PASS | `/translator/console-logs` DELETE (session+pat) | Clear translator console logs | none | `{ok:true}` / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 translator console-logs; pinned `src/app/api/translator/console-logs/` |
| `CMP-TRANS-07` | GET | `/translator/console-logs/stream` | session | PASS | `/translator/console-logs/stream` GET (session) | Translator console SSE stream (init replay + line events) | none | `text/event-stream`; init + line events; 25 s keepalive | stream abort on disconnect; 401 unauthenticated | stream: session-cookie-only; `Cache-Control: no-cache` | route map §3.2 translator console-logs stream; pinned `src/app/api/translator/console-logs/stream/route.js` (L12-40) |
| `CMP-SHUT-01` | POST | `/shutdown` | local-only | PASS | `/shutdown` POST (session+pat, host-gated) | Graceful process shutdown | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited; never silent/kill | same-origin; cookie or PAT | route map §3.7 shutdown; pinned `src/app/api/shutdown/route.js` (SHUTDOWN_SECRET→gate, L1-25) |
| `CMP-VER-01` | GET | `/version` | session+pat | PASS | `/version` GET (session+pat) | Version and update status | none | version object (hasUpdate) / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 version; pinned `src/app/api/version/route.js` (L56-60) |
| `CMP-VER-02` | POST | `/version/update` | local-only | PASS | `/version/update` POST (session+pat, host-gated) | Apply update | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited; detached updater | same-origin; cookie or PAT | route map §3.7 version/update; pinned `src/app/api/version/update/route.js` (L1-30) |
| `CMP-VER-03` | POST | `/version/shutdown` | local-only | PASS | `/version/shutdown` POST (session+pat, host-gated) | Shutdown for manual update | none | `{ok:true}` / 200 | 428 confirm-required; 503 feature disabled; audited | same-origin; cookie or PAT | route map §3.7 version/shutdown; pinned `src/app/api/version/shutdown/route.js` (L1-18) |
| `CMP-META-01` | POST | `/init` | public-api | PASS | `/init` POST (public) | First-run bootstrap | `{password}` | `{ok:true}` / 200 | 400 if already initialized | same-origin; public | route map §3.2 init; pinned `src/app/api/init/route.js` |
| `CMP-META-02` | POST | `/locale` | public-api | PASS | `/locale` GET (public) | Locale catalog (GET admin surface) | none | locale object / 200 | 401 unauthenticated (GET) | same-origin; public | route map §3.2 locale; pinned `src/app/api/locale/route.js` (POST upstream, GET admin surface) |
| `CMP-META-03` | GET | `/tags` | session+pat | PASS | `/tags` GET (session+pat) | Tags catalog | none | tag list / 200 | 401 unauthenticated | same-origin; cookie or PAT | route map §3.2 tags; pinned `src/app/api/tags/route.js` (GET) |
| `CMP-META-04` | GET | `/health` | public-api | PASS | `/health` GET (public) | Minimal health probe | none | `{ok:true}` / 200 | 401 only on detailed variants; public minimal | same-origin; public | route map §3.2 health; pinned `src/app/api/health/route.js` (L1-15) |

---

## 2. Model-API compatibility rewrites (external model endpoints)

These are external LLM/model-endpoint rewrites applied by the upstream rewrite
layer before a request reaches a route handler (`next.config.mjs` lines 37–71;
also `audit/01` §4, lines 58–84). They target the model-compatible engine
surface, **not** the management Administration API, so their disposition is
`MODEL`: preserved by the existing model-compatible surface (the local
`/v1*` engine prefix), never by a management adapter, and never mapped to an
Administration API resource. `ANY` means every HTTP method is accepted and
forwarded to the destination.

| ID | Rewrite source (external pattern) | Method | Rewrite destination | Disposition | App use case | Auth tier | Source |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `CMP-MODEL-01` | `/v1/:path*` | ANY | `/api/v1/:path*` | MODEL | Primary LLM API prefix | model-api-key | `next.config.mjs` L68; audit/01 §4 L74 |
| `CMP-MODEL-02` | `/v1` | ANY | `/api/v1` | MODEL | Primary LLM API root | model-api-key | `next.config.mjs` L75; audit/01 §4 L75 |
| `CMP-MODEL-03` | `/v1/v1/:path*` | ANY | `/api/v1/:path*` | MODEL | Double-`/v1` normalization | model-api-key | `next.config.mjs` L37-40; audit/01 §4 L68 |
| `CMP-MODEL-04` | `/v1/v1` | ANY | `/api/v1` | MODEL | Double-`/v1` root normalization | model-api-key | `next.config.mjs` L41-44; audit/01 §4 L69 |
| `CMP-MODEL-05` | `/codex/:path*` | ANY | `/api/v1/responses` | MODEL | Codex SDK compatibility (always → Responses handler; sub-path discarded) | model-api-key | `next.config.mjs` L45-48; audit/01 §4 L70, L81-84 |
| `CMP-MODEL-06` | `/responses` | ANY | `/api/v1/responses` | MODEL | Standalone Responses path | model-api-key | `next.config.mjs` L49-52; audit/01 §4 L71 |
| `CMP-MODEL-07` | `/v1beta/:path*` | ANY | `/api/v1beta/:path*` | MODEL | Gemini v1beta compatibility | model-api-key | `next.config.mjs` L53-56; audit/01 §4 L72 |
| `CMP-MODEL-08` | `/v1beta` | ANY | `/api/v1beta` | MODEL | Gemini v1beta root | model-api-key | `next.config.mjs` L57-60; audit/01 §4 L73 |

Model-endpoint auth tier (`model-api-key`): the external LLM endpoints require a
valid API key (derived from `Authorization: Bearer`, `x-api-key`,
`x-goog-api-key`, or the `key` query parameter), a loopback request, or the CLI
token (audit/01 §6.2). Locally the machine-id CLI token mechanism is replaced;
model endpoints use the model-key auth surface.

---

## 3. Excluded historical paths

Historical paths present in the pinned baseline that are **explicitly excluded**
from the compatibility surface (disposition `EXCLUDE`): no adapter is written
for them and they have no Administration API twin. Reconciliation outcomes for
the contract inventory flags follow, with the pinned path each row derives from.
These rows carry the same management-table columns for machine-checking.

| ID | Method | Historical external path | External auth tier | Disposition | Administration API v1 twin | App use case | Request body | Response envelope / success status | Error semantics | CORS / headers | Source |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `CMP-XCL-01` | GET | `/providers/[id]/models` | session+pat | EXCLUDE | n/a | Per-provider model list (not in contract) | n/a | n/a | not registered; contract fixture must grow before adoption | n/a | route map §3.2 providers; pinned `src/app/api/providers/[id]/models/route.js` (GET) |
| `CMP-XCL-02` | POST | `/providers/[id]/test` | session+pat | EXCLUDE | n/a | Single-provider test (only test-batch in contract) | n/a | n/a | not registered; use `test-batch` | n/a | route map §3.2 providers; pinned `src/app/api/providers/[id]/test/route.js` (POST) |
| `CMP-XCL-03` | POST | `/providers/[id]/test-models` | session+pat | EXCLUDE | n/a | Per-provider test-models (not in contract) | n/a | n/a | not registered; contract fixture must grow before adoption | n/a | route map §3.2 providers; pinned `src/app/api/providers/[id]/test-models/route.js` (POST) |
| `CMP-XCL-04` | POST | `/proxy-pools/[id]/test` | session+pat | EXCLUDE | n/a | Per-pool test (not in contract) | n/a | n/a | not registered; contract fixture must grow before adoption | n/a | route map §3.2 proxy-pools; pinned `src/app/api/proxy-pools/[id]/test/route.js` (POST) |
| `CMP-XCL-05` | GET | `/usage/[connectionId]/codex-reset-credits` | session+pat | EXCLUDE | n/a | Codex cooldown reset (out of contract scope; global reset beyond original scope) | n/a | n/a | not registered; no global cooldown reset | n/a | route map §3.2 usage [connectionId]; pinned `src/app/api/usage/[connectionId]/codex-reset-credits/route.js` (GET) |
| `CMP-XCL-06` | POST | `/usage/[connectionId]/codex-reset-credits` | session+pat | EXCLUDE | n/a | Codex cooldown reset | n/a | n/a | not registered; no global cooldown reset | n/a | route map §3.2 usage [connectionId]; pinned `src/app/api/usage/[connectionId]/codex-reset-credits/route.js` (POST) |
| `CMP-XCL-07` | POST | `/auth/reset-password` | local-only | EXCLUDE | n/a | Password reset (local CLI only; no HTTP route) | n/a | n/a | prohibited HTTP surface; reset is local CLI only | n/a | route map §3.2 auth; audit/01 §6.7 L199; pinned `src/app/api/auth/reset-password/` |
| `CMP-XCL-08` | POST | `/backups/restore` | n/a | EXCLUDE | n/a | Backup restore (local CLI only; no HTTP route) | n/a | n/a | prohibited HTTP surface; restore is local CLI only | n/a | design backup; restore local CLI only (no upstream route) |

---

## Frozen contract semantics

- **JSON envelope (`admin-json`)**: successful responses carry domain-specific
  JSON. Error responses use the Administration API envelope `{code, message,
  requestId}` with the documented status; the compatibility adapter preserves
  the twin's status and error body for the equivalent failure.
- **`sse`**: `text/event-stream`, `Cache-Control: no-cache`; per-domain event
  names and payloads per the Administration API realtime surface. Session-cookie
  only (P1-5); PAT consumers use the JSON export endpoints, never the stream.
- **Tier transitions (historical → twin)**: `local-only` historical routes are
  served through the twin's `session+pat` boundary with a host-operation gate
  (feature flag closed by default, full-access PAT or session actor, confirm
  prompt on mutations, audited). The machine-id CLI token mechanism is
  prohibited.
- **Method adaptation**: `/locale` is `POST` upstream but `GET` as the
  Administration API surface; `/settings/require-login` is read-only upstream
  and gains a `PUT` write surface in the Administration API; `headroom/proxy`
  upstream catch-all (`[...path]`, multi-segment) maps to the single-segment
  `{path}` twin. The adapter preserves the upstream behavior (path, method,
  body, status, error) and delegates to the same application use case.
- **CORS**: historical `/api/*` management routes are protected prefixes that
  tolerate same-origin browser sessions and PAT/token callers; they do not
  emit a permissive public CORS policy. Health/tags historically used CORS `*`
  (audit/01 §7); locally these are deliberately hardened/split (no `*`).
- **Streams**: `/usage/stream`, `/translator/console-logs/stream`, `/console/stream`,
  `/jobs/stream`, `/mcp/{plugin}/sse` and `/providers/stream` are session-cookie
  only and never accept a PAT bearer.

---

*End of compatibility mapping table. This document is the mapping/deliverable
for the compatibility contract; the machine-checkable fixtures under
`tests/fixtures/upstream/compat/` encode the same rows, and the validation test
enforces each row maps to an existing Administration API registry route.*