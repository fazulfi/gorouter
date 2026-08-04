package httpserver

import (
	"context"
	"net/http"

	domauth "gorouter/internal/domain/auth"
	domkeys "gorouter/internal/domain/keys"
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
	ValidateSession(ctx context.Context, rawToken string) (*domauth.Actor, error)
	SessionCookieName() string
}

// AdminConfig wires the admin mount group: the auth endpoints, the cookie
// policy, and the trusted-proxy CIDRs for real-IP normalization.
type AdminConfig struct {
	Auth           AuthEndpoints
	CookieSecure   bool
	TrustedProxies []string
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
func PATAuthMiddleware(validate func(context.Context, string) (*domkeys.PAT, error)) func(http.Handler) http.Handler {
	return middleware.PATAuth(validate)
}

// NewAdminRouter assembles the concrete admin mount group under /api/admin/v1
// with the P2-9 middleware order and the CSRF/PAT route-grouping resolution:
//   - /auth/login is public (no session or CSRF cookie exists yet; the handler
//     issues both on success) and is therefore mounted outside CSRF.
//   - session-cookie routes (/auth/me, /auth/status, /auth/logout) live behind
//     CSRF → SessionAuth; the logout mutation is fail-closed without the CSRF
//     double-submit echo.
//   - PAT mutations bypass CSRF by mounting in a separate CSRF-free PAT group
//     (see PATAuthMiddleware); none exist in the auth surface itself.
func NewAdminRouter(cfg AdminConfig) http.Handler {
	r := chi.NewRouter()
	r.Route("/api/admin/v1", func(admin chi.Router) {
		admin.Use(NewAdminChain(cfg)...)

		admin.Post("/auth/login", cfg.Auth.Login)

		admin.Group(func(s chi.Router) {
			s.Use(
				middleware.CSRF(middleware.CSRFConfig{Secure: cfg.CookieSecure}),
				middleware.SessionAuth(cfg.Auth.ValidateSession, cfg.Auth.SessionCookieName()),
			)
			s.Get("/auth/me", cfg.Auth.Me)
			s.Get("/auth/status", cfg.Auth.Status)
			s.Post("/auth/logout", cfg.Auth.Logout)
		})
	})
	return r
}
