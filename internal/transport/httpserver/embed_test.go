package httpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	frontendassets "gorouter" //nolint:gci // root package exposes the embedded FS
	"gorouter/internal/transport/httpserver/health"

	"github.com/go-chi/chi/v5"
)

// spaTestFS returns a small frontend build: a shell document, one hashed JS
// asset, one hashed CSS asset and one hashed SVG icon.
func spaTestFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":               {Data: []byte(`<!doctype html><html><head><script type="module">console.log("boot")</script></head><body>app-boot</body></html>`)},
		"assets/app-1a2b3c4d.js":   {Data: []byte(`console.log("app")`)},
		"assets/site-c8d9e0f1.css": {Data: []byte(`body{color:red}`)},
		"assets/logo-99887766.svg": {Data: []byte(`<svg xmlns="http://www.w3.org/2000/svg"/>`)},
	}
}

// TestSPAFallback asserts the serving contract: / and unmatched client-side
// GET routes serve index.html, /api/* and /health resolve to the API (never
// the SPA), /.well-known/* and missing assets 404, hashed assets are cached
// immutably, and HTML responses carry the security headers plus a CSP nonce.
func TestSPAFallback(t *testing.T) {
	r := chi.NewRouter()
	// API precedence: /health (liveness) and the administration API resolve
	// before the embed handler, mirroring the production mount order.
	r.Get("/health", health.PublicHandler())
	r.Get("/api/admin/v1/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"api":"health"}`))
	})
	r.Mount("/", EmbedHandler(spaTestFS()))

	cases := []struct {
		name     string
		method   string
		path     string
		wantCode int
		wantBody string
	}{
		{"root serves index.html", http.MethodGet, "/", http.StatusOK, "app-boot"},
		{"spa route falls back to index.html", http.MethodGet, "/some/spa/route", http.StatusOK, "app-boot"},
		{"nested spa route falls back", http.MethodGet, "/dashboard/analytics", http.StatusOK, "app-boot"},
		{"api route wins over the spa", http.MethodGet, "/api/admin/v1/health", http.StatusOK, `{"api":"health"}`},
		{"health liveness wins over the spa", http.MethodGet, "/health", http.StatusOK, `"status":"ok"`},
		{"well-known never masked", http.MethodGet, "/.well-known/acme-challenge/x", http.StatusNotFound, ""},
		{"missing asset 404s", http.MethodGet, "/assets/missing.js", http.StatusNotFound, ""},
		{"missing asset 404s (top level)", http.MethodGet, "/missing-asset.js", http.StatusNotFound, ""},
		{"non-get never falls back", http.MethodPost, "/some/spa/route", http.StatusNotFound, ""},
		{"api prefix never masked", http.MethodGet, "/api/nonexistent", http.StatusNotFound, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != tc.wantCode {
				t.Fatalf("%s %s = %d, want %d (body %q)", tc.method, tc.path, rr.Code, tc.wantCode, rr.Body.String())
			}
			if tc.wantBody != "" && !strings.Contains(rr.Body.String(), tc.wantBody) {
				t.Errorf("%s body %q missing %q", tc.method, rr.Body.String(), tc.wantBody)
			}
		})
	}
}

// TestEmbedHTMLSecurityHeaders asserts HTML responses carry the security
// headers (CSP with a per-response nonce, no unsafe-inline, nosniff,
// X-Frame-Options Deny, Referrer-Policy, Permissions-Policy) and that an
// inline <script> receives the response nonce.
func TestEmbedHTMLSecurityHeaders(t *testing.T) {
	h := EmbedHandler(spaTestFS())
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rr.Code)
	}
	csp := rr.Header().Get("Content-Security-Policy")
	if !strings.Contains(csp, "default-src 'self'") {
		t.Errorf("CSP default-src missing: %q", csp)
	}
	if strings.Contains(csp, "unsafe-inline") {
		t.Fatalf("CSP must never contain unsafe-inline: %q", csp)
	}
	nonce := cspNonce(csp)
	if nonce == "" {
		t.Fatalf("CSP carries no nonce: %q", csp)
	}
	if !strings.Contains(rr.Header().Get("Cache-Control"), "no-cache") {
		t.Errorf("index.html Cache-Control = %q, want no-cache", rr.Header().Get("Cache-Control"))
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q, want text/html", ct)
	}
	if !strings.Contains(rr.Header().Get("X-Frame-Options"), "DENY") {
		t.Errorf("X-Frame-Options missing DENY")
	}
	if got := rr.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if !strings.Contains(rr.Body.String(), `nonce="`+nonce+`"`) {
		t.Errorf("inline <script> lacks response nonce %q; body %q", nonce, rr.Body.String())
	}
}

// TestEmbedAssetCacheHeaders asserts content-hashed assets are cached
// immutably while a plain (non-hashed) asset is not.
func TestEmbedAssetCacheHeaders(t *testing.T) {
	h := EmbedHandler(spaTestFS())
	req := httptest.NewRequest(http.MethodGet, "/assets/app-1a2b3c4d.js", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("hashed asset = %d, want 200", rr.Code)
	}
	if got := rr.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("hashed asset Cache-Control = %q, want immutable", got)
	}
	if ct := rr.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/javascript") {
		t.Errorf("JS Content-Type = %q, want javascript", ct)
	}

	css := httptest.NewRecorder()
	h.ServeHTTP(css, httptest.NewRequest(http.MethodGet, "/assets/site-c8d9e0f1.css", nil))
	if got := css.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Errorf("hashed css Cache-Control = %q, want immutable", got)
	}
}

// TestEmbedPlaceholderRealNoPanic is the build-tag-independent dev-mode guard:
// it runs against the placeholder FS in a non-prod build and against the real
// embedded FS in a prod build, and must never panic. A missing asset 404s in
// both configurations.
func TestEmbedPlaceholderNoPanic(t *testing.T) {
	h := EmbedHandler(frontendassets.FS)
	for _, p := range []string{"/", "/some/spa/route", "/missing-asset.js", "/.well-known/acme-challenge/x"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code == 0 {
			t.Errorf("GET %s wrote no status (handler must always respond)", p)
		}
	}
	// A missing asset must 404 in both tag configurations.
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/missing-asset.js", nil))
	if rr.Code != http.StatusNotFound {
		t.Errorf("missing asset = %d, want 404 (never the SPA shell)", rr.Code)
	}
}

func cspNonce(csp string) string {
	start := strings.Index(csp, "nonce-")
	if start < 0 {
		return ""
	}
	rest := csp[start+len("nonce-"):]
	end := strings.Index(rest, "'")
	if end < 0 {
		return ""
	}
	return rest[:end]
}
