// Package contract holds machine-checkable contract tests for the
// Admin API v1 OpenAPI document (api/admin-v1.openapi.yaml).
//
// The route fixture below is the authoritative inventory for the admin
// contract. It is derived from the pinned upstream route map
// (.sisyphus/evidence/phase-4-upstream-map.md §3.2) and the Phase 4
// design (§7, .sisyphus/plans/phase-4-design.md), with the exact
// upstream path spellings and HTTP methods verified against the pinned
// decolua/9router clone at 79918c7830695bbca4a45c9fea4a42c3e9fd73d1.
// Local additions (pats, auth/me, health/detailed, jobs, backups,
// audit, console/stream) are marked local:true and come from design §7
// and §9 only.
//
// Binding rules enforced by this suite:
//   - every contract route has exactly one /api/admin/v1 mapping with
//     the expected method and auth tier;
//   - no reset-password route and no backup restore route exist;
//   - no PAT scopes exist anywhere (PAT is always referenced with an
//     empty scope list);
//   - every stream/sse surface is session-cookie-only (P1-5);
//   - the MCP message/sse surface is labelled stream, never chat
//     (product axis 6);
//   - every operation declares Actor and Scope in its description;
//   - credential-safe projection: response schemas never carry full
//     credential values except the one-time create responses.
package contract

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// authTier is the expected security tier of a contract route.
type authTier int

const (
	// tierPublic requires no security (login, oidc entry, health, init, locale).
	tierPublic authTier = iota
	// tierSession requires the SessionCookie only (also used by SSE streams,
	// which must never accept a PAT bearer per P1-5).
	tierSession
	// tierSessionPAT accepts either the SessionCookie or a PAT bearer.
	tierSessionPAT
)

func (t authTier) String() string {
	switch t {
	case tierPublic:
		return "public"
	case tierSession:
		return "session"
	case tierSessionPAT:
		return "session+pat"
	default:
		return "unknown"
	}
}

// contractRoute is one row of the admin v1 contract fixture.
type contractRoute struct {
	// id is a stable slug used in failure messages.
	id string
	// method is the HTTP method of the mapping.
	method string
	// path is the /api/admin/v1-relative path template, e.g. /keys/{id}.
	path string
	// tier is the expected security tier.
	tier authTier
	// hostOp marks design §6 host operations (policy/flag gated, audited).
	hostOp bool
	// local marks a local approved addition that is not an upstream route.
	local bool
}

// adminV1Routes is the fixture: the complete Admin API v1 contract.
//
// Upstream sources are cited as phase-4-upstream-map.md §3.2 domains;
// exact upstream path spellings were verified in the pinned clone
// (src/app/api/**). Local rows cite design §7 (pats, auth/me,
// health/detailed) and §9 (console/stream) or the approved local
// additions (jobs, backups, audit, §7 contract list).
var adminV1Routes = []contractRoute{
	// auth — upstream §3.2 "auth"; NO reset-password route (design §3 D16).
	{id: "auth:login", method: "POST", path: "/auth/login", tier: tierPublic},
	{id: "auth:logout", method: "POST", path: "/auth/logout", tier: tierSession},
	{id: "auth:me", method: "GET", path: "/auth/me", tier: tierSessionPAT, local: true}, // design §7
	{id: "auth:status", method: "GET", path: "/auth/status", tier: tierSessionPAT},
	{id: "auth:oidc:start", method: "GET", path: "/auth/oidc/start", tier: tierPublic},
	{id: "auth:oidc:callback", method: "GET", path: "/auth/oidc/callback", tier: tierPublic},
	{id: "auth:oidc:test", method: "POST", path: "/auth/oidc/test", tier: tierSessionPAT},

	// keys — upstream §3.2 "keys, keys/[id]".
	{id: "keys:list", method: "GET", path: "/keys", tier: tierSessionPAT},
	{id: "keys:create", method: "POST", path: "/keys", tier: tierSessionPAT},
	{id: "keys:get", method: "GET", path: "/keys/{id}", tier: tierSessionPAT},
	{id: "keys:update", method: "PUT", path: "/keys/{id}", tier: tierSessionPAT},
	{id: "keys:delete", method: "DELETE", path: "/keys/{id}", tier: tierSessionPAT},

	// pats — local addition (design §7; D63-69 replace machine-id CLI token).
	{id: "pats:list", method: "GET", path: "/pats", tier: tierSessionPAT, local: true},
	{id: "pats:create", method: "POST", path: "/pats", tier: tierSessionPAT, local: true},
	{id: "pats:revoke", method: "DELETE", path: "/pats/{id}", tier: tierSessionPAT, local: true},

	// providers — upstream §3.2 "providers: [id], client, kilo,
	// suggested-models, test-batch, validate".
	{id: "providers:list", method: "GET", path: "/providers", tier: tierSessionPAT},
	{id: "providers:create", method: "POST", path: "/providers", tier: tierSessionPAT},
	{id: "providers:get", method: "GET", path: "/providers/{id}", tier: tierSessionPAT},
	{id: "providers:update", method: "PUT", path: "/providers/{id}", tier: tierSessionPAT},
	{id: "providers:delete", method: "DELETE", path: "/providers/{id}", tier: tierSessionPAT},
	{id: "providers:client", method: "GET", path: "/providers/client", tier: tierSessionPAT}, // safe-field projection
	{id: "providers:kilo", method: "GET", path: "/providers/kilo/free-models", tier: tierSessionPAT},
	{id: "providers:suggested-models", method: "GET", path: "/providers/suggested-models", tier: tierSessionPAT},
	{id: "providers:test-batch", method: "POST", path: "/providers/test-batch", tier: tierSessionPAT},
	{id: "providers:validate", method: "POST", path: "/providers/validate", tier: tierSessionPAT},
	{id: "providers:stream", method: "GET", path: "/providers/stream", tier: tierSession, local: true}, // design §9 quiet status/cooldown refresh

	// provider-nodes — upstream §3.2.
	{id: "provider-nodes:list", method: "GET", path: "/provider-nodes", tier: tierSessionPAT},
	{id: "provider-nodes:create", method: "POST", path: "/provider-nodes", tier: tierSessionPAT},
	{id: "provider-nodes:get", method: "GET", path: "/provider-nodes/{id}", tier: tierSessionPAT},
	{id: "provider-nodes:update", method: "PUT", path: "/provider-nodes/{id}", tier: tierSessionPAT},
	{id: "provider-nodes:delete", method: "DELETE", path: "/provider-nodes/{id}", tier: tierSessionPAT},
	{id: "provider-nodes:validate", method: "POST", path: "/provider-nodes/validate", tier: tierSessionPAT},

	// proxy-pools — upstream §3.2 incl. cloudflare/deno/vercel deploy helpers.
	{id: "proxy-pools:list", method: "GET", path: "/proxy-pools", tier: tierSessionPAT},
	{id: "proxy-pools:create", method: "POST", path: "/proxy-pools", tier: tierSessionPAT},
	{id: "proxy-pools:get", method: "GET", path: "/proxy-pools/{id}", tier: tierSessionPAT},
	{id: "proxy-pools:update", method: "PUT", path: "/proxy-pools/{id}", tier: tierSessionPAT},
	{id: "proxy-pools:delete", method: "DELETE", path: "/proxy-pools/{id}", tier: tierSessionPAT},
	{id: "proxy-pools:cloudflare-deploy", method: "POST", path: "/proxy-pools/cloudflare-deploy", tier: tierSessionPAT, hostOp: true},
	{id: "proxy-pools:deno-deploy", method: "POST", path: "/proxy-pools/deno-deploy", tier: tierSessionPAT, hostOp: true},
	{id: "proxy-pools:vercel-deploy", method: "POST", path: "/proxy-pools/vercel-deploy", tier: tierSessionPAT, hostOp: true},

	// models — upstream §3.2 "models: alias, custom, disabled, test, availability".
	{id: "models:list", method: "GET", path: "/models", tier: tierSessionPAT},
	{id: "models:update", method: "PUT", path: "/models", tier: tierSessionPAT},
	{id: "models:alias:get", method: "GET", path: "/models/alias", tier: tierSessionPAT},
	{id: "models:alias:update", method: "PUT", path: "/models/alias", tier: tierSessionPAT},
	{id: "models:alias:delete", method: "DELETE", path: "/models/alias", tier: tierSessionPAT},
	{id: "models:custom:list", method: "GET", path: "/models/custom", tier: tierSessionPAT},
	{id: "models:custom:create", method: "POST", path: "/models/custom", tier: tierSessionPAT},
	{id: "models:custom:delete", method: "DELETE", path: "/models/custom", tier: tierSessionPAT},
	{id: "models:disabled:list", method: "GET", path: "/models/disabled", tier: tierSessionPAT},
	{id: "models:disabled:create", method: "POST", path: "/models/disabled", tier: tierSessionPAT},
	{id: "models:disabled:delete", method: "DELETE", path: "/models/disabled", tier: tierSessionPAT},
	{id: "models:test", method: "POST", path: "/models/test", tier: tierSessionPAT},
	{id: "models:availability", method: "GET", path: "/models/availability", tier: tierSessionPAT},

	// combos + pricing — upstream §3.2.
	{id: "combos:list", method: "GET", path: "/combos", tier: tierSessionPAT},
	{id: "combos:create", method: "POST", path: "/combos", tier: tierSessionPAT},
	{id: "combos:get", method: "GET", path: "/combos/{id}", tier: tierSessionPAT},
	{id: "combos:update", method: "PUT", path: "/combos/{id}", tier: tierSessionPAT},
	{id: "combos:delete", method: "DELETE", path: "/combos/{id}", tier: tierSessionPAT},
	{id: "pricing:get", method: "GET", path: "/pricing", tier: tierSessionPAT},
	{id: "pricing:update", method: "PATCH", path: "/pricing", tier: tierSessionPAT},
	{id: "pricing:reset", method: "DELETE", path: "/pricing", tier: tierSessionPAT},

	// usage — upstream §3.2 "usage: stats, history, chart, providers,
	// request-details, request-logs, logs, stream, [connectionId]".
	{id: "usage:stats", method: "GET", path: "/usage/stats", tier: tierSessionPAT},
	{id: "usage:history", method: "GET", path: "/usage/history", tier: tierSessionPAT},
	{id: "usage:chart", method: "GET", path: "/usage/chart", tier: tierSessionPAT},
	{id: "usage:providers", method: "GET", path: "/usage/providers", tier: tierSessionPAT},
	{id: "usage:request-details", method: "GET", path: "/usage/request-details", tier: tierSessionPAT},
	{id: "usage:request-logs", method: "GET", path: "/usage/request-logs", tier: tierSessionPAT},
	{id: "usage:logs", method: "GET", path: "/usage/logs", tier: tierSessionPAT},
	{id: "usage:connection", method: "GET", path: "/usage/{connectionId}", tier: tierSessionPAT},
	{id: "usage:stream", method: "GET", path: "/usage/stream", tier: tierSession},

	// cli-tools — upstream §3.2 "cli-tools: all-statuses + 15 tool
	// settings routes (...) + antigravity-mitm". The 16 exact tool
	// route names were verified in the pinned clone (cowork×3 =
	// cowork-mcp-registry, cowork-mcp-tools, cowork-settings).
	{id: "cli-tools:all-statuses", method: "GET", path: "/cli-tools/all-statuses", tier: tierSessionPAT},
	{id: "cli-tools:tool:get", method: "GET", path: "/cli-tools/{tool}", tier: tierSessionPAT},
	{id: "cli-tools:tool:action", method: "POST", path: "/cli-tools/{tool}", tier: tierSessionPAT},
	{id: "cli-tools:antigravity-alias", method: "POST", path: "/cli-tools/antigravity-mitm/alias", tier: tierSessionPAT},

	// media-providers — upstream §3.2 "media-providers: tts, voices,
	// deepgram, elevenlabs, inworld, minimax". Exact upstream paths are
	// nested under /media-providers/tts/ (verified in the pinned clone).
	{id: "media:tts-voices", method: "GET", path: "/media-providers/tts/voices", tier: tierSessionPAT},
	{id: "media:tts-deepgram-voices", method: "GET", path: "/media-providers/tts/deepgram/voices", tier: tierSessionPAT},
	{id: "media:tts-elevenlabs-voices", method: "GET", path: "/media-providers/tts/elevenlabs/voices", tier: tierSessionPAT},
	{id: "media:tts-inworld-voices", method: "GET", path: "/media-providers/tts/inworld/voices", tier: tierSessionPAT},
	{id: "media:tts-minimax-voices", method: "GET", path: "/media-providers/tts/minimax/voices", tier: tierSessionPAT},

	// oauth — upstream §3.2 "oauth: [provider], codex (import-token,
	// bulk-import), cursor (import, auto-import), gitlab, iflow, kiro
	// (import, auto-import, import-cli-proxy)". The full exact flow set
	// was verified in the pinned clone. {provider} status/connect/
	// disconnect is the local admin surface (design §7 "oauth per
	// provider").
	{id: "oauth:provider:get", method: "GET", path: "/oauth/{provider}", tier: tierSessionPAT},
	{id: "oauth:provider:connect", method: "POST", path: "/oauth/{provider}", tier: tierSessionPAT},
	{id: "oauth:provider:disconnect", method: "DELETE", path: "/oauth/{provider}", tier: tierSessionPAT},
	{id: "oauth:codex:import-token", method: "POST", path: "/oauth/codex/import-token", tier: tierSessionPAT},
	{id: "oauth:codex:bulk-import", method: "POST", path: "/oauth/codex/bulk-import", tier: tierSessionPAT},
	{id: "oauth:cursor:import", method: "POST", path: "/oauth/cursor/import", tier: tierSessionPAT},
	{id: "oauth:cursor:auto-import", method: "POST", path: "/oauth/cursor/auto-import", tier: tierSessionPAT},
	{id: "oauth:gitlab:pat", method: "POST", path: "/oauth/gitlab/pat", tier: tierSessionPAT},
	{id: "oauth:iflow:cookie", method: "POST", path: "/oauth/iflow/cookie", tier: tierSessionPAT},
	{id: "oauth:kiro:import", method: "POST", path: "/oauth/kiro/import", tier: tierSessionPAT},
	{id: "oauth:kiro:auto-import", method: "POST", path: "/oauth/kiro/auto-import", tier: tierSessionPAT},
	{id: "oauth:kiro:import-cli-proxy", method: "POST", path: "/oauth/kiro/import-cli-proxy", tier: tierSessionPAT},
	{id: "oauth:kiro:api-key", method: "POST", path: "/oauth/kiro/api-key", tier: tierSessionPAT},
	{id: "oauth:kiro:social-authorize", method: "POST", path: "/oauth/kiro/social-authorize", tier: tierSessionPAT},
	{id: "oauth:kiro:social-exchange", method: "POST", path: "/oauth/kiro/social-exchange", tier: tierSessionPAT},

	// tunnel + tailscale-* — upstream §3.2.
	{id: "tunnel:get", method: "GET", path: "/tunnel", tier: tierSessionPAT},
	{id: "tunnel:enable", method: "POST", path: "/tunnel/enable", tier: tierSessionPAT, hostOp: true},
	{id: "tunnel:disable", method: "POST", path: "/tunnel/disable", tier: tierSessionPAT, hostOp: true},
	{id: "tunnel:status", method: "GET", path: "/tunnel/status", tier: tierSessionPAT},
	{id: "tunnel:tailscale-check", method: "GET", path: "/tunnel/tailscale-check", tier: tierSessionPAT},
	{id: "tunnel:tailscale-install", method: "POST", path: "/tunnel/tailscale-install", tier: tierSessionPAT, hostOp: true},
	{id: "tunnel:tailscale-enable", method: "POST", path: "/tunnel/tailscale-enable", tier: tierSessionPAT, hostOp: true},
	{id: "tunnel:tailscale-disable", method: "POST", path: "/tunnel/tailscale-disable", tier: tierSessionPAT, hostOp: true},

	// headroom — upstream §3.2 "headroom: start, stop, restart, status,
	// proxy, extras".
	{id: "headroom:start", method: "POST", path: "/headroom/start", tier: tierSessionPAT, hostOp: true},
	{id: "headroom:stop", method: "POST", path: "/headroom/stop", tier: tierSessionPAT, hostOp: true},
	{id: "headroom:restart", method: "POST", path: "/headroom/restart", tier: tierSessionPAT, hostOp: true},
	{id: "headroom:status", method: "GET", path: "/headroom/status", tier: tierSessionPAT},
	{id: "headroom:extras", method: "GET", path: "/headroom/extras", tier: tierSessionPAT, hostOp: true},
	{id: "headroom:proxy:get", method: "GET", path: "/headroom/proxy/{path}", tier: tierSessionPAT, hostOp: true},
	{id: "headroom:proxy:action", method: "POST", path: "/headroom/proxy/{path}", tier: tierSessionPAT, hostOp: true},

	// pxpipe — upstream §3.2 "pxpipe: install, start, stop, restart,
	// status, health, logs, stats".
	{id: "pxpipe:install", method: "POST", path: "/pxpipe/install", tier: tierSessionPAT, hostOp: true},
	{id: "pxpipe:start", method: "POST", path: "/pxpipe/start", tier: tierSessionPAT, hostOp: true},
	{id: "pxpipe:stop", method: "POST", path: "/pxpipe/stop", tier: tierSessionPAT, hostOp: true},
	{id: "pxpipe:restart", method: "POST", path: "/pxpipe/restart", tier: tierSessionPAT, hostOp: true},
	{id: "pxpipe:status", method: "GET", path: "/pxpipe/status", tier: tierSessionPAT},
	{id: "pxpipe:health", method: "GET", path: "/pxpipe/health", tier: tierSessionPAT},
	{id: "pxpipe:health-action", method: "POST", path: "/pxpipe/health", tier: tierSessionPAT, hostOp: true},
	{id: "pxpipe:logs", method: "GET", path: "/pxpipe/logs", tier: tierSessionPAT},
	{id: "pxpipe:stats", method: "GET", path: "/pxpipe/stats", tier: tierSessionPAT},

	// mcp — upstream §3.2 "mcp: [plugin]/message, [plugin]/sse".
	// Realtime stream surface, labelled stream not chat (product axis 6).
	{id: "mcp:message", method: "POST", path: "/mcp/{plugin}/message", tier: tierSessionPAT, hostOp: true},
	{id: "mcp:sse", method: "GET", path: "/mcp/{plugin}/sse", tier: tierSession},

	// settings — upstream §3.2 "settings: route, database,
	// require-login, proxy-test".
	{id: "settings:get", method: "GET", path: "/settings", tier: tierSessionPAT},
	{id: "settings:update", method: "PATCH", path: "/settings", tier: tierSessionPAT},
	{id: "settings:database:get", method: "GET", path: "/settings/database", tier: tierSessionPAT},
	{id: "settings:database:import", method: "POST", path: "/settings/database", tier: tierSessionPAT}, // manual, partial/destructive, confirmed
	{id: "settings:require-login:get", method: "GET", path: "/settings/require-login", tier: tierSessionPAT},
	{id: "settings:require-login:update", method: "PUT", path: "/settings/require-login", tier: tierSessionPAT},
	{id: "settings:proxy-test", method: "POST", path: "/settings/proxy-test", tier: tierSessionPAT},

	// translator — upstream §3.2 "translator: translate, load, save,
	// send, console-logs (+stream)".
	{id: "translator:translate", method: "POST", path: "/translator/translate", tier: tierSessionPAT},
	{id: "translator:load", method: "GET", path: "/translator/load", tier: tierSessionPAT},
	{id: "translator:save", method: "POST", path: "/translator/save", tier: tierSessionPAT},
	{id: "translator:send", method: "POST", path: "/translator/send", tier: tierSessionPAT},
	{id: "translator:console-logs", method: "GET", path: "/translator/console-logs", tier: tierSessionPAT},
	{id: "translator:console-logs-clear", method: "DELETE", path: "/translator/console-logs", tier: tierSessionPAT},
	{id: "translator:console-logs-stream", method: "GET", path: "/translator/console-logs/stream", tier: tierSession},

	// jobs — local addition (design §7; D81-87/366-368 central scheduler).
	{id: "jobs:list", method: "GET", path: "/jobs", tier: tierSessionPAT, local: true},
	{id: "jobs:run-now", method: "POST", path: "/jobs/{type}/run-now", tier: tierSessionPAT, local: true},
	{id: "jobs:history", method: "GET", path: "/jobs/{type}/history", tier: tierSessionPAT, local: true},
	{id: "jobs:stream", method: "GET", path: "/jobs/stream", tier: tierSession, local: true},

	// console — local addition (design §9 console stream).
	{id: "console:stream", method: "GET", path: "/console/stream", tier: tierSession, local: true},

	// backups — local addition (design §7; list, download, verify only;
	// NO restore route, restore is local CLI only per DECISIONS #202).
	{id: "backups:list", method: "GET", path: "/backups", tier: tierSessionPAT, local: true},
	{id: "backups:download", method: "GET", path: "/backups/{id}/download", tier: tierSessionPAT, local: true},
	{id: "backups:verify", method: "POST", path: "/backups/{id}/verify", tier: tierSessionPAT, local: true},

	// audit — local addition, read-only (design §6/§7).
	{id: "audit:list", method: "GET", path: "/audit", tier: tierSessionPAT, local: true},
	{id: "audit:get", method: "GET", path: "/audit/{id}", tier: tierSessionPAT, local: true},
	{id: "audit:export", method: "GET", path: "/audit/export", tier: tierSessionPAT, local: true},

	// shutdown — upstream §3.2; host operation, policy/flag gated.
	{id: "shutdown", method: "POST", path: "/shutdown", tier: tierSessionPAT, hostOp: true},

	// version — upstream §3.2 "version (+update, shutdown)".
	{id: "version:get", method: "GET", path: "/version", tier: tierSessionPAT},
	{id: "version:update", method: "POST", path: "/version/update", tier: tierSessionPAT, hostOp: true},
	{id: "version:shutdown", method: "POST", path: "/version/shutdown", tier: tierSessionPAT, hostOp: true},

	// health — upstream §3.2 "health"; detailed is a local addition
	// (design §6 public health minimal #199).
	{id: "health", method: "GET", path: "/health", tier: tierPublic},
	{id: "health:detailed", method: "GET", path: "/health/detailed", tier: tierSessionPAT, local: true},

	// init — upstream §3.2 "init" (first-run bootstrap).
	{id: "init", method: "POST", path: "/init", tier: tierPublic},

	// locale — upstream §3.2 "locale".
	{id: "locale", method: "GET", path: "/locale", tier: tierPublic},

	// tags — upstream §3.2 "tags".
	{id: "tags", method: "GET", path: "/tags", tier: tierSessionPAT},
}

// cliToolRoutes are the exact cli-tools tool route names from the pinned
// upstream clone (src/app/api/cli-tools/*), the "15 tool settings
// routes + antigravity-mitm" of upstream-map §3.2.
var cliToolRoutes = []string{
	"antigravity-mitm",
	"claude-settings",
	"cline-settings",
	"codex-settings",
	"copilot-settings",
	"cowork-mcp-registry",
	"cowork-mcp-tools",
	"cowork-settings",
	"deepseek-tui-settings",
	"droid-settings",
	"grok-build-settings",
	"hermes-settings",
	"jcode-settings",
	"kilo-settings",
	"openclaw-settings",
	"opencode-settings",
}

// ---- OpenAPI document model (subset) ----

type openAPIDoc struct {
	OpenAPI string `yaml:"openapi"`
	Info    struct {
		Title   string `yaml:"title"`
		Version string `yaml:"version"`
	} `yaml:"info"`
	Servers []struct {
		URL string `yaml:"url"`
	} `yaml:"servers"`
	Components struct {
		SecuritySchemes map[string]any `yaml:"securitySchemes"`
		Schemas         map[string]any `yaml:"schemas"`
	} `yaml:"components"`
	Paths map[string]*pathItem `yaml:"paths"`
}

type pathItem struct {
	Get    *operation `yaml:"get"`
	Post   *operation `yaml:"post"`
	Put    *operation `yaml:"put"`
	Patch  *operation `yaml:"patch"`
	Delete *operation `yaml:"delete"`
}

type operation struct {
	OperationID string                `yaml:"operationId"`
	Summary     string                `yaml:"summary"`
	Description string                `yaml:"description"`
	Tags        []string              `yaml:"tags"`
	Security    []map[string][]string `yaml:"security"`
	Parameters  []param               `yaml:"parameters"`
	Responses   map[string]response   `yaml:"responses"`
}

type param struct {
	Name   string `yaml:"name"`
	In     string `yaml:"in"`
	Schema struct {
		Enum []string `yaml:"enum"`
	} `yaml:"schema"`
}

type response struct {
	Content map[string]struct {
		Schema map[string]any `yaml:"schema"`
	} `yaml:"content"`
}

func rootDir(elem ...string) string {
	return filepath.Join(append([]string{"..", ".."}, elem...)...)
}

func loadOpenAPI(t *testing.T) *openAPIDoc {
	t.Helper()
	data, err := os.ReadFile(rootDir("api", "admin-v1.openapi.yaml"))
	if err != nil {
		t.Fatalf("read api/admin-v1.openapi.yaml: %v", err)
	}
	var doc openAPIDoc
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse api/admin-v1.openapi.yaml: %v", err)
	}
	return &doc
}

func (pi *pathItem) operation(method string) *operation {
	switch method {
	case "GET":
		return pi.Get
	case "POST":
		return pi.Post
	case "PUT":
		return pi.Put
	case "PATCH":
		return pi.Patch
	case "DELETE":
		return pi.Delete
	}
	return nil
}

func (pi *pathItem) declaredMethods() []string {
	var out []string
	if pi.Get != nil {
		out = append(out, "GET")
	}
	if pi.Post != nil {
		out = append(out, "POST")
	}
	if pi.Put != nil {
		out = append(out, "PUT")
	}
	if pi.Patch != nil {
		out = append(out, "PATCH")
	}
	if pi.Delete != nil {
		out = append(out, "DELETE")
	}
	return out
}

// expectedSecurity returns the expected security requirements for a tier.
func expectedSecurity(tier authTier) []map[string][]string {
	switch tier {
	case tierPublic:
		return nil
	case tierSession:
		return []map[string][]string{{"SessionCookie": {}}}
	case tierSessionPAT:
		return []map[string][]string{
			{"SessionCookie": {}},
			{"PAT": {}},
		}
	}
	return nil
}

// securityKeys returns the sorted set of scheme names referenced by an
// operation, and reports whether any reference carries non-empty scopes.
func securityKeys(op *operation) (keys []string, hasScopes bool) {
	seen := map[string]bool{}
	for _, req := range op.Security {
		for scheme, scopes := range req {
			if len(scopes) > 0 {
				hasScopes = true
			}
			if !seen[scheme] {
				seen[scheme] = true
				keys = append(keys, scheme)
			}
		}
	}
	sort.Strings(keys)
	return keys, hasScopes
}

func securityEqual(got, want []map[string][]string) bool {
	if len(got) != len(want) {
		return false
	}
	gotSet := map[string]bool{}
	wantSet := map[string]bool{}
	for _, req := range got {
		for scheme, scopes := range req {
			if len(scopes) > 0 {
				return false
			}
			gotSet[scheme] = true
		}
	}
	for _, req := range want {
		for scheme, scopes := range req {
			if len(scopes) > 0 {
				return false
			}
			wantSet[scheme] = true
		}
	}
	if len(gotSet) != len(wantSet) {
		return false
	}
	for s := range gotSet {
		if !wantSet[s] {
			return false
		}
	}
	return true
}

// TestAdminV1ContractCompleteness is the primary machine-checkable
// contract test (plan API-01): every fixture route has exactly one
// /api/admin/v1 mapping with the expected method and auth tier, and no
// unexpected extra paths or methods exist.
func TestAdminV1ContractCompleteness(t *testing.T) {
	doc := loadOpenAPI(t)

	expectedMethods := map[string]map[string]bool{}
	fixture := map[string]bool{}
	for _, r := range adminV1Routes {
		fixture[r.path] = true
		if expectedMethods[r.path] == nil {
			expectedMethods[r.path] = map[string]bool{}
		}
		expectedMethods[r.path][r.method] = true
	}

	// Every fixture route must map exactly once with the expected tier.
	var failures []string
	for _, r := range adminV1Routes {
		pi, ok := doc.Paths[r.path]
		if !ok {
			failures = append(failures, fmt.Sprintf("missing path %s (%s, %s)", r.path, r.method, r.id))
			continue
		}
		op := pi.operation(r.method)
		if op == nil {
			failures = append(failures, fmt.Sprintf("missing method %s on %s (%s)", r.method, r.path, r.id))
			continue
		}
		if !securityEqual(op.Security, expectedSecurity(r.tier)) {
			keys, _ := securityKeys(op)
			failures = append(failures, fmt.Sprintf("auth tier mismatch on %s %s: want %s, got security %v (schemes %v)",
				r.method, r.path, r.tier, op.Security, keys))
		}
	}

	// Reverse direction: declared paths and methods must exactly equal
	// the fixture (single mapping, no drift).
	var paths []string
	for p := range doc.Paths {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if !fixture[p] {
			failures = append(failures, fmt.Sprintf("path %s is declared but has no contract fixture row", p))
			continue
		}
		declared := map[string]bool{}
		for _, m := range doc.Paths[p].declaredMethods() {
			declared[m] = true
		}
		var want []string
		for m := range expectedMethods[p] {
			want = append(want, m)
		}
		var have []string
		for m := range declared {
			have = append(have, m)
		}
		sort.Strings(want)
		sort.Strings(have)
		if strings.Join(want, ",") != strings.Join(have, ",") {
			failures = append(failures, fmt.Sprintf("method set mismatch on %s: want [%s], declared [%s]",
				p, strings.Join(want, ","), strings.Join(have, ",")))
		}
	}

	if len(failures) > 0 {
		t.Errorf("contract incomplete (%d failures):\n%s", len(failures), strings.Join(failures, "\n"))
	}
}

// TestAdminV1CliToolsInventory asserts the cli-tools tool parameter
// enumerates every exact upstream tool route name and covers the 15
// tool settings routes plus antigravity-mitm of upstream-map §3.2.
func TestAdminV1CliToolsInventory(t *testing.T) {
	doc := loadOpenAPI(t)
	pi, ok := doc.Paths["/cli-tools/{tool}"]
	if !ok {
		t.Fatal("path /cli-tools/{tool} is missing")
	}
	op := pi.Get
	if op == nil {
		t.Fatal("GET /cli-tools/{tool} is missing")
	}
	var enum []string
	for _, p := range op.Parameters {
		if p.Name == "tool" && p.In == "path" {
			enum = p.Schema.Enum
		}
	}
	if len(enum) == 0 {
		t.Fatal("path parameter tool on /cli-tools/{tool} has no enum")
	}
	set := map[string]bool{}
	var dup []string
	for _, name := range enum {
		if set[name] {
			dup = append(dup, name)
		}
		set[name] = true
	}
	if len(dup) > 0 {
		t.Errorf("duplicate tool names in enum: %v", dup)
	}
	for _, want := range cliToolRoutes {
		if !set[want] {
			t.Errorf("cli-tools enum is missing upstream tool route %q", want)
		}
	}
	if len(enum) < len(cliToolRoutes) {
		t.Errorf("cli-tools enum has %d entries, want at least %d (all-statuses + 15 tools + antigravity-mitm)", len(enum), len(cliToolRoutes))
	}
}

// TestAdminV1ForbiddenSurfaces asserts the two prohibited surfaces are
// absent: reset-password (design §3 D16, #74/#129) and backup restore
// (DECISIONS #202, local CLI only).
func TestAdminV1ForbiddenSurfaces(t *testing.T) {
	doc := loadOpenAPI(t)
	for p := range doc.Paths {
		lower := strings.ToLower(p)
		if strings.Contains(lower, "reset-password") || strings.Contains(lower, "reset_password") {
			t.Errorf("forbidden route present: %s (reset-password is local CLI only)", p)
		}
		if strings.Contains(lower, "/restore") {
			t.Errorf("forbidden route present: %s (backup restore is local CLI only)", p)
		}
	}
	// Also guard against near-miss spellings.
	raw, err := os.ReadFile(rootDir("api", "admin-v1.openapi.yaml"))
	if err != nil {
		t.Fatalf("read contract: %v", err)
	}
	text := strings.ToLower(string(raw))
	for _, forbidden := range []string{"reset-password", "reset_password", "password/reset"} {
		if strings.Contains(text, forbidden) {
			t.Errorf("contract text mentions forbidden surface %q", forbidden)
		}
	}
}

// TestAdminV1NoPATScopes asserts design item 17: the PAT scheme is
// never referenced with scopes anywhere in the contract.
func TestAdminV1NoPATScopes(t *testing.T) {
	doc := loadOpenAPI(t)
	if _, ok := doc.Components.SecuritySchemes["PAT"]; !ok {
		t.Error("PAT security scheme is missing")
	}
	var failures []string
	for p, pi := range doc.Paths {
		for _, m := range pi.declaredMethods() {
			op := pi.operation(m)
			for _, req := range op.Security {
				if scopes, ok := req["PAT"]; ok && len(scopes) > 0 {
					failures = append(failures, fmt.Sprintf("%s %s references PAT with scopes %v", m, p, scopes))
				}
			}
		}
	}
	if len(failures) > 0 {
		t.Errorf("PAT scopes present (%d):\n%s", len(failures), strings.Join(failures, "\n"))
	}
}

// TestAdminV1SecuritySchemes asserts the three contract security
// schemes stay SessionCookie, PAT, APIKey and are declared.
func TestAdminV1SecuritySchemes(t *testing.T) {
	doc := loadOpenAPI(t)
	for _, want := range []string{"SessionCookie", "PAT", "APIKey"} {
		if _, ok := doc.Components.SecuritySchemes[want]; !ok {
			t.Errorf("security scheme %s is not declared", want)
		}
	}
	if len(doc.Components.SecuritySchemes) != 3 {
		t.Errorf("security schemes = %d, want exactly 3 (SessionCookie, PAT, APIKey)", len(doc.Components.SecuritySchemes))
	}
}

// TestAdminV1StreamsSessionOnly asserts P1-5: every stream/sse surface
// is session-cookie-only and never accepts a PAT bearer.
func TestAdminV1StreamsSessionOnly(t *testing.T) {
	doc := loadOpenAPI(t)
	var failures []string
	for p, pi := range doc.Paths {
		if !(strings.HasSuffix(p, "/stream") || strings.HasSuffix(p, "/sse")) {
			continue
		}
		for _, m := range pi.declaredMethods() {
			op := pi.operation(m)
			if !securityEqual(op.Security, expectedSecurity(tierSession)) {
				failures = append(failures, fmt.Sprintf("%s %s must be session-cookie-only, got %v", m, p, op.Security))
			}
		}
	}
	if len(failures) > 0 {
		t.Errorf("stream/sse surfaces must be session-cookie-only (P1-5):\n%s", strings.Join(failures, "\n"))
	}
}

// TestAdminV1EveryRouteDeclaresActorAndScope asserts every operation
// description declares its actor classes and scope (design §7 "every
// route declares actor and scope").
func TestAdminV1EveryRouteDeclaresActorAndScope(t *testing.T) {
	doc := loadOpenAPI(t)
	var failures []string
	for p, pi := range doc.Paths {
		for _, m := range pi.declaredMethods() {
			op := pi.operation(m)
			if !strings.Contains(op.Description, "Actor:") {
				failures = append(failures, fmt.Sprintf("%s %s description does not declare Actor", m, p))
			}
			if !strings.Contains(op.Description, "Scope:") {
				failures = append(failures, fmt.Sprintf("%s %s description does not declare Scope", m, p))
			}
		}
	}
	if len(failures) > 0 {
		t.Errorf("actor/scope declarations missing (%d):\n%s", len(failures), strings.Join(failures, "\n"))
	}
}

// TestAdminV1McpSurfaceIsStream asserts the MCP message/sse surface is
// labelled stream (realtime), never chat (product axis 6).
func TestAdminV1McpSurfaceIsStream(t *testing.T) {
	doc := loadOpenAPI(t)
	for _, p := range []string{"/mcp/{plugin}/message", "/mcp/{plugin}/sse"} {
		pi, ok := doc.Paths[p]
		if !ok {
			t.Errorf("path %s is missing", p)
			continue
		}
		for _, m := range pi.declaredMethods() {
			op := pi.operation(m)
			text := strings.ToLower(op.Summary + "\n" + op.Description + "\n" + strings.Join(op.Tags, ","))
			if !strings.Contains(text, "stream") && !strings.Contains(text, "realtime") {
				t.Errorf("%s %s is not labelled as a stream/realtime surface", m, p)
			}
			if strings.Contains(text, "chat") {
				t.Errorf("%s %s is labelled chat; the MCP surface must be labelled stream, not chat (product axis 6)", m, p)
			}
		}
	}
}

// TestAdminV1SafeFieldProjection asserts credential-safe projection:
// response schemas never expose full credential values (api keys, PATs,
// secrets, passwords), except the one-time create responses whose
// description marks the value as shown only once.
func TestAdminV1SafeFieldProjection(t *testing.T) {
	doc := loadOpenAPI(t)

	// Collect every schema reachable from any operation response body.
	reachable := map[string]bool{}
	var seed []string
	for _, pi := range doc.Paths {
		for _, m := range pi.declaredMethods() {
			op := pi.operation(m)
			for _, resp := range op.Responses {
				for _, c := range resp.Content {
					seed = append(seed, refName(c.Schema)...)
				}
			}
		}
	}
	var queue []string
	for _, s := range seed {
		if s != "" && !reachable[s] {
			reachable[s] = true
			queue = append(queue, s)
		}
	}
	for len(queue) > 0 {
		name := queue[0]
		queue = queue[1:]
		schema, ok := doc.Components.Schemas[name].(map[string]any)
		if !ok {
			continue
		}
		for _, ref := range schemaRefs(schema) {
			if !reachable[ref] {
				reachable[ref] = true
				queue = append(queue, ref)
			}
		}
	}

	// One-time create responses may carry the full value once.
	allowed := map[string]bool{
		"CreateAPIKeyResponse": true,
		"CreatePATResponse":    true,
	}
	forbiddenProps := []string{"api_key", "api_key_value", "key", "token", "secret", "client_secret", "password", "pat"}

	var failures []string
	for name := range reachable {
		if allowed[name] {
			continue
		}
		schema, ok := doc.Components.Schemas[name].(map[string]any)
		if !ok {
			continue
		}
		for _, prop := range responsePropertyNames(schema) {
			pl := strings.ToLower(prop)
			for _, f := range forbiddenProps {
				if pl == f {
					failures = append(failures, fmt.Sprintf("schema %s exposes full credential property %q in a response", name, prop))
				}
			}
		}
	}
	if len(failures) > 0 {
		t.Errorf("credential-safe projection violated (%d):\n%s", len(failures), strings.Join(failures, "\n"))
	}
}

// refName returns the schema name referenced by a response schema node,
// handling direct $ref, arrays with $ref items, and allOf members.
func refName(node map[string]any) []string {
	var out []string
	if ref, ok := node["$ref"].(string); ok {
		if name := refBase(ref); name != "" {
			out = append(out, name)
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		out = append(out, refName(items)...)
	}
	if allOf, ok := node["allOf"].([]any); ok {
		for _, m := range allOf {
			if mm, ok := m.(map[string]any); ok {
				out = append(out, refName(mm)...)
			}
		}
	}
	return out
}

// schemaRefs returns the $ref targets inside a schema node (properties,
// items, allOf), used for the reachability walk.
func schemaRefs(node map[string]any) []string {
	var out []string
	if props, ok := node["properties"].(map[string]any); ok {
		for _, v := range props {
			if m, ok := v.(map[string]any); ok {
				out = append(out, refName(m)...)
			}
		}
	}
	if items, ok := node["items"].(map[string]any); ok {
		out = append(out, refName(items)...)
	}
	if allOf, ok := node["allOf"].([]any); ok {
		for _, m := range allOf {
			if mm, ok := m.(map[string]any); ok {
				out = append(out, refName(mm)...)
			}
		}
	}
	return out
}

// responsePropertyNames returns the property names of a schema node,
// following allOf members.
func responsePropertyNames(node map[string]any) []string {
	var out []string
	if props, ok := node["properties"].(map[string]any); ok {
		for name := range props {
			out = append(out, name)
		}
	}
	if allOf, ok := node["allOf"].([]any); ok {
		for _, m := range allOf {
			if mm, ok := m.(map[string]any); ok {
				out = append(out, responsePropertyNames(mm)...)
			}
		}
	}
	return out
}

func refBase(ref string) string {
	const prefix = "#/components/schemas/"
	if strings.HasPrefix(ref, prefix) {
		return strings.TrimPrefix(ref, prefix)
	}
	return ""
}
