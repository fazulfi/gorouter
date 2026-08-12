package bootstrap

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestServerModeAdminWiring pins the admin-API wiring contract of
// server-mode bootstrap: the handler built by buildServerHandler (the exact
// handler dispatchServer serves) must mount the administration surface that
// NewAdminRouter registers under /api/admin/v1, and the legacy /api/compat
// surface it carries, at the paths the adminapi/v1 contract declares.
//
// Before the wiring fix dispatchServer only mounted /health, the model API
// group and the SPA embed; every /api/* path fell through to the embed,
// whose explicit allow-list 404s them (embed.go blockedPath), so the whole
// probe table below was 404 — the live-VPS validation gap this test locks.
//
// The probes never touch the database: public routes (health, login with an
// empty body — rejected by the handler before any service call) and the
// session/PAT routes, which are refused by the CSRF/session chain with a
// missing cookie. A 401/400/200 is a wired surface; a 404 is an unwired one.
func TestServerModeAdminWiring(t *testing.T) {
	app := newServerTestApp(t)
	handler, err := buildServerHandler(app, nil)
	if err != nil {
		t.Fatalf("buildServerHandler: %v", err)
	}

	type probe struct {
		name   string
		method string
		path   string
		want   int
	}
	probes := []probe{
		// Public admin surface.
		{"admin health public", http.MethodGet, "/api/admin/v1/health", http.StatusOK},

		// Session/PAT routes fail closed with a missing credential (never 404).
		{"admin health detailed", http.MethodGet, "/api/admin/v1/health/detailed", http.StatusUnauthorized},
		{"auth login malformed body", http.MethodPost, "/api/admin/v1/auth/login", http.StatusBadRequest},
		{"auth me", http.MethodGet, "/api/admin/v1/auth/me", http.StatusUnauthorized},
		{"auth status", http.MethodGet, "/api/admin/v1/auth/status", http.StatusUnauthorized},
		{"version", http.MethodGet, "/api/admin/v1/version", http.StatusUnauthorized},
		{"providers", http.MethodGet, "/api/admin/v1/providers", http.StatusUnauthorized},
		{"providers stream", http.MethodGet, "/api/admin/v1/providers/stream", http.StatusUnauthorized},
		{"keys", http.MethodGet, "/api/admin/v1/keys", http.StatusUnauthorized},
		{"pats", http.MethodGet, "/api/admin/v1/pats", http.StatusUnauthorized},
		{"combos", http.MethodGet, "/api/admin/v1/combos", http.StatusUnauthorized},
		{"settings", http.MethodGet, "/api/admin/v1/settings", http.StatusUnauthorized},
		{"usage stats", http.MethodGet, "/api/admin/v1/usage/stats", http.StatusUnauthorized},
		{"jobs", http.MethodGet, "/api/admin/v1/jobs", http.StatusUnauthorized},
		{"backups", http.MethodGet, "/api/admin/v1/backups", http.StatusUnauthorized},
		{"console stream", http.MethodGet, "/api/admin/v1/console/stream", http.StatusUnauthorized},

		// Legacy compatibility surface carried by NewAdminRouter
		// (MountPrefix + twin path, compatibility/registry.go).
		{"compat health", http.MethodGet, "/api/compat/health", http.StatusOK},

		// Controls: pre-existing surfaces must keep resolving.
		{"public health", http.MethodGet, "/health", http.StatusOK},
		{"model api key gate", http.MethodPost, "/v1/chat/completions", http.StatusUnauthorized},

		// Doc-drift controls: paths docs once claimed that the code never
		// registers; the router must keep 404ing them (docs fixed instead).
		{"doc-drift healthz", http.MethodGet, "/healthz", http.StatusNotFound},
		{"doc-drift api v1 healthz", http.MethodGet, "/api/v1/healthz", http.StatusNotFound},
	}

	for _, p := range probes {
		t.Run(p.name, func(t *testing.T) {
			req := httptest.NewRequest(p.method, p.path, nil)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code == http.StatusNotFound && p.want != http.StatusNotFound {
				t.Fatalf("%s %s = 404 (admin surface not wired); want %d", p.method, p.path, p.want)
			}
			if rr.Code != p.want {
				t.Errorf("%s %s = %d, want %d", p.method, p.path, rr.Code, p.want)
			}
		})
	}
}
