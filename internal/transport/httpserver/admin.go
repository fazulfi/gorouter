package httpserver

import (
	"context"
	"net/http"

	appauth "gorouter/internal/app/auth"
	appkeys "gorouter/internal/app/keys"
	adminapiv1 "gorouter/internal/transport/httpserver/adminapi/v1"
	"gorouter/internal/transport/httpserver/compatibility"
	"gorouter/internal/transport/httpserver/realtime"
	"gorouter/internal/transport/middleware"

	"github.com/go-chi/chi/v5"
)

// AuthEndpoints is the auth-transport surface the admin router mounts. It
// keeps the mount assembly decoupled from the concrete handler package.
type AuthEndpoints interface {
	Login(w http.ResponseWriter, r *http.Request)
	Logout(w http.ResponseWriter, r *http.Request)
	Me(w http.ResponseWriter, r *http.Request)
	Status(w http.ResponseWriter, r *http.Request)
	ValidateSession(ctx context.Context, rawToken string) (*appauth.Actor, error)
	SessionCookieName() string
}

// OIDCEndpoints is the OIDC auth surface the admin router mounts. start and
// callback are public (no session exists pre-sign-in); test sits behind the
// session-cookie group (openapi: SessionCookie/PAT, scope config read).
type OIDCEndpoints interface {
	OIDCStart(w http.ResponseWriter, r *http.Request)
	OIDCCallback(w http.ResponseWriter, r *http.Request)
	OIDCTest(w http.ResponseWriter, r *http.Request)
}

// AdminConfig wires the admin mount group: the auth endpoints, the cookie
// policy, the trusted-proxy CIDRs for real-IP normalization, and the
// adminapi/v1 service dependencies (nil-safe; unwired services resolve with
// backend-unavailable).
type AdminConfig struct {
	Auth           AuthEndpoints
	OIDC           OIDCEndpoints
	CookieSecure   bool
	TrustedProxies []string
	Resources      *adminapiv1.Dependencies
	// HostFlags enables host-operation features (closed by default; design
	// §11 P2-11 keys enable_tunnel, enable_tailscale, ...).
	HostFlags map[string]bool
	// Realtime wires the session-cookie-only SSE stream sources (usage,
	// console, providers, jobs). A nil field leaves the corresponding
	// stream route resolving with backend-unavailable.
	Realtime *realtime.Config
}

// NewAdminChain returns the common P2-9 middleware chain applied to every
// admin route group: TrustedProxy → Recovery → Correlation → AdminCORS →
// SecurityHeaders. TrustedProxy precedes SecurityHeaders so the HSTS
// X-Forwarded-Proto path only trusts proxy-normalized values.
func NewAdminChain(cfg AdminConfig) []func(http.Handler) http.Handler {
	return []func(http.Handler) http.Handler{
		middleware.TrustedProxy(middleware.TrustedProxyConfig{TrustedProxies: cfg.TrustedProxies}),
		middleware.Recovery,
		middleware.Correlation,
		middleware.AdminCORS(middleware.CORSConfig{}),
		middleware.SecurityHeaders(),
	}
}

// PATAuthMiddleware returns the PATAuth middleware for CSRF-free PAT route
// groups. Design §6 L127: PAT/CLI/job paths bypass CSRF (no cookie), so
// resource-group handlers (API-05) mount PAT mutations in a PAT group rather
// than inside the session group's CSRF.
func PATAuthMiddleware(validate func(context.Context, string) (*appkeys.PAT, error)) func(http.Handler) http.Handler {
	return middleware.PATAuth(validate)
}

// NewAdminRouter assembles the concrete admin mount group under /api/admin/v1
// with the P2-9 middleware order and the CSRF/PAT route-grouping resolution:
// every route in the adminapi/v1 registry is mounted with its declared authz
// class; host-operation routes ride the HostGate (feature flag closed by
// default, full-access PAT identity, audit, prompt, P0-1); session-cookie
// mutations require the double-submit CSRF echo while PAT actors bypass CSRF.
func NewAdminRouter(cfg AdminConfig) http.Handler {
	resources := adminapiv1.Dependencies{}
	if cfg.Resources != nil {
		resources = *cfg.Resources
	}
	vcfg := adminapiv1.Config{
		Auth:         cfg.Auth,
		OIDC:         cfg.OIDC,
		CookieSecure: cfg.CookieSecure,
		HostFlags:    cfg.HostFlags,
		Resources:    resources,
	}
	if cfg.Realtime != nil {
		ka := cfg.Realtime.Keepalive
		if s := cfg.Realtime.UsageSource; s != nil {
			vcfg.Streams.Usage = realtime.NewUsageStream(s, ka)
		}
		if s := cfg.Realtime.ConsoleSource; s != nil {
			vcfg.Streams.Console = realtime.NewConsoleStream(s, ka)
		}
		if s := cfg.Realtime.ProvidersSource; s != nil {
			vcfg.Streams.Providers = realtime.NewProvidersStream(s, ka)
		}
		if s := cfg.Realtime.JobsSource; s != nil {
			vcfg.Streams.Jobs = realtime.NewJobsStream(s, ka)
		}
	}
	h := adminapiv1.New(vcfg)

	r := chi.NewRouter()
	r.Route("/api/admin/v1", func(admin chi.Router) {
		admin.Use(NewAdminChain(cfg)...)
		for _, rt := range adminapiv1.RouteTable(h) {
			handler := rt.H
			if rt.HostFeature != "" {
				handler = adminapiv1.HostGate(vcfg, rt.HostFeature, handler)
			}
			switch rt.Mode {
			case adminapiv1.AuthPublic:
				admin.Method(rt.Method, rt.Path, handler)
			case adminapiv1.AuthSession:
				admin.Method(rt.Method, rt.Path, adminapiv1.WrapSession(vcfg, handler))
			case adminapiv1.AuthSessionPAT:
				admin.Method(rt.Method, rt.Path, adminapiv1.WrapSessionPAT(vcfg, handler))
			}
		}
	})

	// Compatibility surface: thin adapters preserve the historical management
	// routes under /api/compat/*, delegating to the same application use cases
	// and authz classes as their Administration API v1 twins. Mounted after the
	// admin group so admin routes are never shadowed.
	r.Group(func(compat chi.Router) {
		compat.Use(NewAdminChain(cfg)...)
		for _, rt := range compatibility.RouteTable(h) {
			handler := rt.H
			if rt.HostFeature != "" {
				handler = adminapiv1.HostGate(vcfg, rt.HostFeature, handler)
			}
			switch rt.Mode {
			case adminapiv1.AuthPublic:
				compat.Method(rt.Method, rt.Path, handler)
			case adminapiv1.AuthSession:
				compat.Method(rt.Method, rt.Path, adminapiv1.WrapSession(vcfg, handler))
			case adminapiv1.AuthSessionPAT:
				compat.Method(rt.Method, rt.Path, adminapiv1.WrapSessionPAT(vcfg, handler))
			}
		}
	})
	return r
}
