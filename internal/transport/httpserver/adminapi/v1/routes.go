package v1

import (
	"context"
	"net/http"
	"strings"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/keys"
	"gorouter/internal/transport/middleware"
)

// AuthMode classifies the actor classes a route accepts (design §6 matrix).
type AuthMode int

const (
	// AuthPublic requires no authentication (login, oidc start/callback,
	// public health, init, locale).
	AuthPublic AuthMode = iota
	// AuthSession accepts session-cookie actors only (logout, streams, P1-5).
	AuthSession
	// AuthSessionPAT accepts session-cookie actors (behind CSRF on
	// mutations) and PAT actors (CSRF-free). The local CLI performs these
	// operations through its own channel (in-process use cases or remote
	// PAT mode), which is why the CLI column is asserted as allowed here.
	AuthSessionPAT
)

// Route is one registered Admin API v1 contract route with its authz class
// and optional host-operation gate.
type Route struct {
	Method string
	Path   string
	Mode   AuthMode
	H      http.HandlerFunc
	// HostFeature is the host-operation feature flag key (closed default,
	// design §11 P2-11). Empty means the route is not host-gated. Host
	// gating is feature flag + full-access PAT identity + audit + prompt,
	// never a PAT scope (P0-1).
	HostFeature string
}

// AuthHandlers is the auth-transport surface the resource router mounts for
// the /auth group. It mirrors the httpserver.AuthEndpoints contract so the
// concrete auth handler satisfies both without an import cycle.
type AuthHandlers interface {
	Login(w http.ResponseWriter, r *http.Request)
	Logout(w http.ResponseWriter, r *http.Request)
	Me(w http.ResponseWriter, r *http.Request)
	Status(w http.ResponseWriter, r *http.Request)
	ValidateSession(ctx context.Context, rawToken string) (*auth.Actor, error)
	SessionCookieName() string
}

// OIDCHandlers is the optional OIDC surface (start/callback public, test
// behind the session+pat boundary).
type OIDCHandlers interface {
	OIDCStart(w http.ResponseWriter, r *http.Request)
	OIDCCallback(w http.ResponseWriter, r *http.Request)
	OIDCTest(w http.ResponseWriter, r *http.Request)
}

// Dependencies is the mapping seam: every application service the handlers
// delegate to. A nil service reports backend-unavailable for its routes;
// wiring is the backend-lane integration step (design §15). ValidatePAT
// feeds the CSRF-free PAT authenticator.
type Dependencies struct {
	Keys           KeysService
	PATs           PATsService
	Providers      ProvidersService
	Accounts       AccountsService
	Nodes          NodesService
	Pools          PoolsService
	Combos         CombosService
	Aliases        AliasesService
	Models         ModelsService
	Pricing        PricingService
	Usage          UsageService
	Quota          QuotaService
	Mutators       MutatorsService
	CLITools       CLIToolsService
	Media          MediaService
	Skills         SkillsService
	Console        ConsoleService
	Tunnels        TunnelsService
	Settings       SettingsService
	ConfigTransfer ConfigTransferService
	Translator     TranslatorService
	OAuth          OAuthService
	Jobs           JobsService
	HostOps        HostOpsService
	Backups        BackupsService
	Audit          AuditService

	ValidatePAT func(context.Context, string) (*keys.PAT, error)
	Auditor     Auditor
}

// Streams carries the realtime stream handlers for the four session-cookie
// only surfaces (usage, console, providers, jobs). A nil entry leaves the
// corresponding stream route resolving with backend-unavailable; the admin
// router injects the handlers assembled by the realtime package.
type Streams struct {
	Usage     http.HandlerFunc
	Console   http.HandlerFunc
	Providers http.HandlerFunc
	Jobs      http.HandlerFunc
}

// Config wires the admin resource router: the auth surface, the CSRF cookie
// policy, host-operation feature flags (closed by default), the stream
// handlers and the service dependencies.
type Config struct {
	Auth         AuthHandlers
	OIDC         OIDCHandlers
	CookieSecure bool
	HostFlags    map[string]bool
	Streams      Streams
	Resources    Dependencies
}

// Handlers is the assembled set of resource-group handler surfaces.
type Handlers struct {
	Auth      AuthHandlers
	OIDC      OIDCHandlers
	Health    healthGroup
	Keys      *keysGroup
	PATs      *patsGroup
	Providers *providersGroup
	Accounts  *accountsGroup
	Nodes     *nodesGroup
	Pools     *poolsGroup
	Combos    *combosGroup
	Aliases   *aliasesGroup
	Models    *modelsGroup
	Pricing   *pricingGroup
	Usage     *usageGroup
	Quota     *quotaGroup
	Mutators  *mutatorsGroup
	CLITools  *cliToolsGroup
	Media     *mediaGroup
	Skills    *skillsGroup
	Console   *consoleGroup
	Tunnels   *tunnelsGroup
	Settings  *settingsGroup
	OAuth     *oauthGroup
	Jobs      *jobsGroup
	HostOps   *hostOpsGroup
	Backups   *backupsGroup
	Audit     *auditGroup
}

// stubAuth is used when no auth surface is wired; its routes resolve (never
// 404) but honestly report the backend is unavailable.
type stubAuth struct{}

func (stubAuth) Login(w http.ResponseWriter, r *http.Request)  { backendUnavailable(w, r) }
func (stubAuth) Logout(w http.ResponseWriter, r *http.Request) { backendUnavailable(w, r) }
func (stubAuth) Me(w http.ResponseWriter, r *http.Request)     { backendUnavailable(w, r) }
func (stubAuth) Status(w http.ResponseWriter, r *http.Request) { backendUnavailable(w, r) }
func (stubAuth) ValidateSession(context.Context, string) (*auth.Actor, error) {
	return nil, errUnauthorized
}
func (stubAuth) SessionCookieName() string { return "gorouter_session" }

// New assembles the resource-group handlers from cfg. Groups are always
// present (nil services report backend-unavailable); the OIDC surface is
// optional and its routes are registered only when present.
func New(cfg Config) *Handlers {
	authH := cfg.Auth
	if authH == nil {
		authH = stubAuth{}
	}
	return &Handlers{
		Auth:      authH,
		OIDC:      cfg.OIDC,
		Health:    healthGroup{},
		Keys:      &keysGroup{svc: cfg.Resources.Keys},
		PATs:      &patsGroup{svc: cfg.Resources.PATs},
		Providers: &providersGroup{svc: cfg.Resources.Providers, stream: cfg.Streams.Providers},
		Accounts:  &accountsGroup{svc: cfg.Resources.Accounts},
		Nodes:     &nodesGroup{svc: cfg.Resources.Nodes},
		Pools:     &poolsGroup{svc: cfg.Resources.Pools},
		Combos:    &combosGroup{svc: cfg.Resources.Combos},
		Aliases:   &aliasesGroup{svc: cfg.Resources.Aliases},
		Models:    &modelsGroup{svc: cfg.Resources.Models},
		Pricing:   &pricingGroup{svc: cfg.Resources.Pricing},
		Usage:     &usageGroup{svc: cfg.Resources.Usage, stream: cfg.Streams.Usage},
		Quota:     &quotaGroup{svc: cfg.Resources.Quota},
		Mutators:  &mutatorsGroup{svc: cfg.Resources.Mutators},
		CLITools:  &cliToolsGroup{svc: cfg.Resources.CLITools},
		Media:     &mediaGroup{svc: cfg.Resources.Media},
		Skills:    &skillsGroup{svc: cfg.Resources.Skills},
		Console:   &consoleGroup{svc: cfg.Resources.Console, stream: cfg.Streams.Console},
		Tunnels:   &tunnelsGroup{svc: cfg.Resources.Tunnels},
		Settings: &settingsGroup{
			svc:        cfg.Resources.Settings,
			transfer:   cfg.Resources.ConfigTransfer,
			translator: cfg.Resources.Translator,
		},
		OAuth:   &oauthGroup{svc: cfg.Resources.OAuth},
		Jobs:    &jobsGroup{svc: cfg.Resources.Jobs, stream: cfg.Streams.Jobs},
		HostOps: &hostOpsGroup{svc: cfg.Resources.HostOps, auditor: cfg.Resources.Auditor},
		Backups: &backupsGroup{svc: cfg.Resources.Backups},
		Audit:   &auditGroup{svc: cfg.Resources.Audit},
	}
}

// WrapSessionPAT returns h wrapped in the CSRF-aware session/PAT
// authenticator (design §6 L127): Bearer PAT bypasses CSRF; cookie actors
// pass the double-submit CSRF check then session validation. A missing
// credential is rejected before h runs.
func WrapSessionPAT(cfg Config, h http.HandlerFunc) http.HandlerFunc {
	csrf := middleware.CSRF(middleware.CSRFConfig{Secure: cfg.CookieSecure})
	sess := middleware.SessionAuth(cfg.Auth.ValidateSession, cfg.Auth.SessionCookieName())
	pat := middleware.PATAuth(cfg.Resources.ValidatePAT)
	return func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.Header.Get("Authorization"), "Bearer ") {
			if cfg.Resources.ValidatePAT == nil {
				backendUnavailable(w, r)
				return
			}
			pat(http.HandlerFunc(h.ServeHTTP)).ServeHTTP(w, r)
			return
		}
		csrf(sess(http.HandlerFunc(h.ServeHTTP))).ServeHTTP(w, r)
	}
}

// WrapSession returns h wrapped in CSRF → SessionAuth (session-cookie-only
// routes: logout and the stream surfaces, P1-5).
func WrapSession(cfg Config, h http.HandlerFunc) http.HandlerFunc {
	csrf := middleware.CSRF(middleware.CSRFConfig{Secure: cfg.CookieSecure})
	sess := middleware.SessionAuth(cfg.Auth.ValidateSession, cfg.Auth.SessionCookieName())
	return csrf(sess(http.HandlerFunc(h.ServeHTTP))).ServeHTTP
}

// HostGate wraps a host-operation route: the feature flag is closed by
// default; when open, the caller must be a full-access PAT identity or a
// session actor, mutations require the confirm prompt, and the attempt is
// audited. Never a PAT scope (P0-1).
func HostGate(cfg Config, feature string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !cfg.HostFlags[feature] {
			featureDisabled(w, r, feature)
			return
		}
		actor := actorFrom(r)
		if actor == nil {
			writeError(w, r, errUnauthorized)
			return
		}
		if actor.Kind != auth.ActorKindPAT && actor.Kind != auth.ActorKindSession {
			writeError(w, r, errForbidden)
			return
		}
		if r.Method != http.MethodGet && !confirmPresent(r) {
			writeJSON(w, http.StatusPreconditionRequired, errorBody{
				Code:      "CONFIRM_REQUIRED",
				Message:   "this host operation requires confirmation",
				RequestID: requestID(r),
			})
			return
		}
		if cfg.Resources.Auditor != nil {
			_ = cfg.Resources.Auditor.Audit(r.Context(), actor, "host_op:"+feature, r.URL.Path, nil,
				sanitizeJSON(map[string]string{"method": r.Method, "ip": clientIP(r)}))
		}
		h.ServeHTTP(w, r)
	}
}

// RouteTable returns every registered Admin API v1 contract route (the
// frozen 119-path/153-operation contract) with its authz classes and host
// gate. TestAuthzMatrix asserts this table exactly matches the contract.
func RouteTable(h *Handlers) []Route {
	var rt []Route

	pub := func(m, p string, hnd http.HandlerFunc) {
		rt = append(rt, Route{Method: m, Path: p, Mode: AuthPublic, H: hnd})
	}
	sess := func(m, p string, hnd http.HandlerFunc) {
		rt = append(rt, Route{Method: m, Path: p, Mode: AuthSession, H: hnd})
	}
	sp := func(m, p string, hnd http.HandlerFunc) {
		rt = append(rt, Route{Method: m, Path: p, Mode: AuthSessionPAT, H: hnd})
	}
	host := func(m, p, feature string, hnd http.HandlerFunc) {
		rt = append(rt, Route{Method: m, Path: p, Mode: AuthSessionPAT, H: hnd, HostFeature: feature})
	}

	// health + meta (public health is anonymous, design §6).
	pub(http.MethodGet, "/health", h.Health.Public)
	sp(http.MethodGet, "/health/detailed", h.Health.Detailed)
	pub(http.MethodPost, "/init", handleInit)
	pub(http.MethodGet, "/locale", handleLocale)
	sp(http.MethodGet, "/tags", handleTags)
	sp(http.MethodGet, "/version", h.HostOps.Version)

	// auth group (no reset-password route; D16).
	pub(http.MethodPost, "/auth/login", h.Auth.Login)
	sess(http.MethodPost, "/auth/logout", h.Auth.Logout)
	sp(http.MethodGet, "/auth/me", h.Auth.Me)
	sp(http.MethodGet, "/auth/status", h.Auth.Status)
	if h.OIDC != nil {
		pub(http.MethodGet, "/auth/oidc/start", h.OIDC.OIDCStart)
		pub(http.MethodGet, "/auth/oidc/callback", h.OIDC.OIDCCallback)
		sp(http.MethodPost, "/auth/oidc/test", h.OIDC.OIDCTest)
	}

	// keys + pats (credential-safe projection; keys shown once, hash-only
	// after).
	sp(http.MethodGet, "/keys", h.Keys.List)
	sp(http.MethodPost, "/keys", h.Keys.Create)
	sp(http.MethodGet, "/keys/{id}", h.Keys.Get)
	sp(http.MethodPut, "/keys/{id}", h.Keys.Update)
	sp(http.MethodDelete, "/keys/{id}", h.Keys.Delete)
	sp(http.MethodGet, "/pats", h.PATs.List)
	sp(http.MethodPost, "/pats", h.PATs.Create)
	sp(http.MethodDelete, "/pats/{id}", h.PATs.Revoke)

	// providers.
	sp(http.MethodGet, "/providers", h.Providers.List)
	sp(http.MethodPost, "/providers", h.Providers.Create)
	sp(http.MethodGet, "/providers/{id}", h.Providers.Get)
	sp(http.MethodPut, "/providers/{id}", h.Providers.Update)
	sp(http.MethodDelete, "/providers/{id}", h.Providers.Delete)
	sp(http.MethodGet, "/providers/client", h.Providers.Client)
	sp(http.MethodGet, "/providers/kilo/free-models", h.Providers.KiloFreeModels)
	sp(http.MethodGet, "/providers/suggested-models", h.Providers.SuggestedModels)
	sp(http.MethodPost, "/providers/test-batch", h.Providers.TestBatch)
	sp(http.MethodPost, "/providers/validate", h.Providers.Validate)
	sess(http.MethodGet, "/providers/stream", h.Providers.Stream)

	// provider-nodes.
	sp(http.MethodGet, "/provider-nodes", h.Nodes.List)
	sp(http.MethodPost, "/provider-nodes", h.Nodes.Create)
	sp(http.MethodGet, "/provider-nodes/{id}", h.Nodes.Get)
	sp(http.MethodPut, "/provider-nodes/{id}", h.Nodes.Update)
	sp(http.MethodDelete, "/provider-nodes/{id}", h.Nodes.Delete)
	sp(http.MethodPost, "/provider-nodes/validate", h.Nodes.Validate)

	// proxy-pools + deploy helpers (host-gated, closed default).
	sp(http.MethodGet, "/proxy-pools", h.Pools.List)
	sp(http.MethodPost, "/proxy-pools", h.Pools.Create)
	sp(http.MethodGet, "/proxy-pools/{id}", h.Pools.Get)
	sp(http.MethodPut, "/proxy-pools/{id}", h.Pools.Update)
	sp(http.MethodDelete, "/proxy-pools/{id}", h.Pools.Delete)
	host(http.MethodPost, "/proxy-pools/cloudflare-deploy", "deploy", h.Pools.CloudflareDeploy)
	host(http.MethodPost, "/proxy-pools/deno-deploy", "deploy", h.Pools.DenoDeploy)
	host(http.MethodPost, "/proxy-pools/vercel-deploy", "deploy", h.Pools.VercelDeploy)

	// models + aliases + pricing.
	sp(http.MethodGet, "/models", h.Models.List)
	sp(http.MethodPut, "/models", h.Models.Update)
	sp(http.MethodGet, "/models/alias", h.Aliases.AliasGet)
	sp(http.MethodPut, "/models/alias", h.Aliases.AliasUpdate)
	sp(http.MethodDelete, "/models/alias", h.Aliases.AliasDelete)
	sp(http.MethodGet, "/models/custom", h.Models.CustomList)
	sp(http.MethodPost, "/models/custom", h.Models.CustomCreate)
	sp(http.MethodDelete, "/models/custom", h.Models.CustomDelete)
	sp(http.MethodGet, "/models/disabled", h.Models.DisabledList)
	sp(http.MethodPost, "/models/disabled", h.Models.DisabledCreate)
	sp(http.MethodDelete, "/models/disabled", h.Models.DisabledDelete)
	sp(http.MethodPost, "/models/test", h.Models.Test)
	sp(http.MethodGet, "/models/availability", h.Models.Availability)

	// combos + pricing.
	sp(http.MethodGet, "/combos", h.Combos.List)
	sp(http.MethodPost, "/combos", h.Combos.Create)
	sp(http.MethodGet, "/combos/{id}", h.Combos.Get)
	sp(http.MethodPut, "/combos/{id}", h.Combos.Update)
	sp(http.MethodDelete, "/combos/{id}", h.Combos.Delete)
	sp(http.MethodGet, "/pricing", h.Pricing.Get)
	sp(http.MethodPatch, "/pricing", h.Pricing.Update)
	sp(http.MethodDelete, "/pricing", h.Pricing.Reset)

	// usage (stream is session-cookie-only, P1-5).
	sp(http.MethodGet, "/usage/stats", h.Usage.Stats)
	sp(http.MethodGet, "/usage/history", h.Usage.History)
	sp(http.MethodGet, "/usage/chart", h.Usage.Chart)
	sp(http.MethodGet, "/usage/providers", h.Usage.Providers)
	sp(http.MethodGet, "/usage/request-details", h.Usage.RequestDetails)
	sp(http.MethodGet, "/usage/request-logs", h.Usage.RequestLogs)
	sp(http.MethodGet, "/usage/logs", h.Usage.Logs)
	sp(http.MethodGet, "/usage/{connectionId}", h.Usage.Connection)
	sess(http.MethodGet, "/usage/stream", h.Usage.Stream)

	// cli-tools.
	sp(http.MethodGet, "/cli-tools/all-statuses", h.CLITools.AllStatuses)
	sp(http.MethodGet, "/cli-tools/{tool}", h.CLITools.ToolGet)
	sp(http.MethodPost, "/cli-tools/{tool}", h.CLITools.ToolAction)
	sp(http.MethodPost, "/cli-tools/antigravity-mitm/alias", h.CLITools.AntigravityAlias)

	// media-providers.
	sp(http.MethodGet, "/media-providers/tts/voices", h.Media.TTSVoices)
	sp(http.MethodGet, "/media-providers/tts/deepgram/voices", h.Media.DeepgramVoices)
	sp(http.MethodGet, "/media-providers/tts/elevenlabs/voices", h.Media.ElevenlabsVoices)
	sp(http.MethodGet, "/media-providers/tts/inworld/voices", h.Media.InworldVoices)
	sp(http.MethodGet, "/media-providers/tts/minimax/voices", h.Media.MinimaxVoices)

	// oauth.
	sp(http.MethodGet, "/oauth/{provider}", h.OAuth.ProviderGet)
	sp(http.MethodPost, "/oauth/{provider}", h.OAuth.ProviderConnect)
	sp(http.MethodDelete, "/oauth/{provider}", h.OAuth.ProviderDisconnect)
	sp(http.MethodPost, "/oauth/codex/import-token", h.OAuth.CodexImportToken)
	sp(http.MethodPost, "/oauth/codex/bulk-import", h.OAuth.CodexBulkImport)
	sp(http.MethodPost, "/oauth/cursor/import", h.OAuth.CursorImport)
	sp(http.MethodPost, "/oauth/cursor/auto-import", h.OAuth.CursorAutoImport)
	sp(http.MethodPost, "/oauth/gitlab/pat", h.OAuth.GitlabPAT)
	sp(http.MethodPost, "/oauth/iflow/cookie", h.OAuth.IflowCookie)
	sp(http.MethodPost, "/oauth/kiro/import", h.OAuth.KiroImport)
	sp(http.MethodPost, "/oauth/kiro/auto-import", h.OAuth.KiroAutoImport)
	sp(http.MethodPost, "/oauth/kiro/import-cli-proxy", h.OAuth.KiroImportCLIProxy)
	sp(http.MethodPost, "/oauth/kiro/api-key", h.OAuth.KiroAPIKey)
	sp(http.MethodPost, "/oauth/kiro/social-authorize", h.OAuth.KiroSocialAuthorize)
	sp(http.MethodPost, "/oauth/kiro/social-exchange", h.OAuth.KiroSocialExchange)

	// tunnels + tailscale (mutations host-gated).
	sp(http.MethodGet, "/tunnel", h.Tunnels.Get)
	host(http.MethodPost, "/tunnel/enable", "tunnel", h.Tunnels.Enable)
	host(http.MethodPost, "/tunnel/disable", "tunnel", h.Tunnels.Disable)
	sp(http.MethodGet, "/tunnel/status", h.Tunnels.Status)
	sp(http.MethodGet, "/tunnel/tailscale-check", h.Tunnels.TailscaleCheck)
	host(http.MethodPost, "/tunnel/tailscale-install", "tailscale", h.Tunnels.TailscaleInstall)
	host(http.MethodPost, "/tunnel/tailscale-enable", "tailscale", h.Tunnels.TailscaleEnable)
	host(http.MethodPost, "/tunnel/tailscale-disable", "tailscale", h.Tunnels.TailscaleDisable)

	// headroom + pxpipe + mcp (host operations, closed default).
	host(http.MethodPost, "/headroom/start", "headroom", h.HostOps.HeadroomStart)
	host(http.MethodPost, "/headroom/stop", "headroom", h.HostOps.HeadroomStop)
	host(http.MethodPost, "/headroom/restart", "headroom", h.HostOps.HeadroomRestart)
	sp(http.MethodGet, "/headroom/status", h.HostOps.HeadroomStatus)
	host(http.MethodGet, "/headroom/extras", "headroom", h.HostOps.HeadroomExtras)
	host(http.MethodGet, "/headroom/proxy/{path}", "headroom", h.HostOps.HeadroomProxyGet)
	host(http.MethodPost, "/headroom/proxy/{path}", "headroom", h.HostOps.HeadroomProxyAction)
	host(http.MethodPost, "/pxpipe/install", "pxpipe", h.HostOps.PxpipeInstall)
	host(http.MethodPost, "/pxpipe/start", "pxpipe", h.HostOps.PxpipeStart)
	host(http.MethodPost, "/pxpipe/stop", "pxpipe", h.HostOps.PxpipeStop)
	host(http.MethodPost, "/pxpipe/restart", "pxpipe", h.HostOps.PxpipeRestart)
	sp(http.MethodGet, "/pxpipe/status", h.HostOps.PxpipeStatus)
	sp(http.MethodGet, "/pxpipe/health", h.HostOps.PxpipeHealth)
	host(http.MethodPost, "/pxpipe/health", "pxpipe", h.HostOps.PxpipeHealthAction)
	sp(http.MethodGet, "/pxpipe/logs", h.HostOps.PxpipeLogs)
	sp(http.MethodGet, "/pxpipe/stats", h.HostOps.PxpipeStats)
	host(http.MethodPost, "/mcp/{plugin}/message", "mcp", h.HostOps.MCPMessage)
	sess(http.MethodGet, "/mcp/{plugin}/sse", h.HostOps.MCPSse)

	// settings + translator (translator feature-gated at the service layer).
	sp(http.MethodGet, "/settings", h.Settings.Get)
	sp(http.MethodPatch, "/settings", h.Settings.Update)
	sp(http.MethodGet, "/settings/database", h.Settings.DatabaseGet)
	sp(http.MethodPost, "/settings/database", h.Settings.DatabaseImport)
	sp(http.MethodGet, "/settings/require-login", h.Settings.RequireLoginGet)
	sp(http.MethodPut, "/settings/require-login", h.Settings.RequireLoginUpdate)
	sp(http.MethodPost, "/settings/proxy-test", h.Settings.ProxyTest)
	sp(http.MethodPost, "/translator/translate", h.Settings.Translate)
	sp(http.MethodGet, "/translator/load", h.Settings.TranslatorLoad)
	sp(http.MethodPost, "/translator/save", h.Settings.TranslatorSave)
	sp(http.MethodPost, "/translator/send", h.Settings.TranslatorSend)
	sp(http.MethodGet, "/translator/console-logs", h.Settings.TranslatorConsoleLogs)
	sp(http.MethodDelete, "/translator/console-logs", h.Settings.TranslatorConsoleLogsClear)
	sess(http.MethodGet, "/translator/console-logs/stream", h.Settings.TranslatorConsoleLogsStream)

	// jobs.
	sp(http.MethodGet, "/jobs", h.Jobs.List)
	sp(http.MethodPost, "/jobs/{type}/run-now", h.Jobs.RunNow)
	sp(http.MethodGet, "/jobs/{type}/history", h.Jobs.History)
	sess(http.MethodGet, "/jobs/stream", h.Jobs.Stream)

	// console stream (session-cookie-only).
	sess(http.MethodGet, "/console/stream", h.Console.Stream)

	// backups (no restore route; restore is local CLI only, #202).
	sp(http.MethodGet, "/backups", h.Backups.List)
	sp(http.MethodGet, "/backups/{id}/download", h.Backups.Download)
	sp(http.MethodPost, "/backups/{id}/verify", h.Backups.Verify)

	// audit (read-only).
	sp(http.MethodGet, "/audit", h.Audit.List)
	sp(http.MethodGet, "/audit/{id}", h.Audit.Get)
	sp(http.MethodGet, "/audit/export", h.Audit.Export)

	// shutdown + version host operations.
	host(http.MethodPost, "/shutdown", "shutdown", h.HostOps.Shutdown)
	host(http.MethodPost, "/version/update", "updater", h.HostOps.VersionUpdate)
	host(http.MethodPost, "/version/shutdown", "shutdown", h.HostOps.VersionShutdown)

	return rt
}
