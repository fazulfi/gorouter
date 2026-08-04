package httpserver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	domauth "gorouter/internal/domain/auth"
	domkeys "gorouter/internal/domain/keys"
	authhandlers "gorouter/internal/transport/httpserver/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type adminFakeService struct {
	loginSession *domauth.Session
	loginToken   string
	loginErr     error
	me           *domauth.User
	meErr        error
	logErr       error
}

func (f *adminFakeService) Login(context.Context, string, string) (*domauth.Session, string, error) {
	if f.loginErr != nil {
		return nil, "", f.loginErr
	}
	return f.loginSession, f.loginToken, nil
}

func (f *adminFakeService) Logout(context.Context, uuid.UUID) error {
	return f.logErr
}

func (f *adminFakeService) GetCurrentUser(context.Context, uuid.UUID) (*domauth.User, error) {
	return f.me, f.meErr
}

func (f *adminFakeService) ValidateSession(context.Context, string) (*domauth.Actor, error) {
	return &domauth.Actor{UserID: f.me.ID, SessionID: f.loginSession.ID, IsAdmin: true,
		Kind: domauth.ActorKindSession, Origin: domauth.ActorOriginRemote}, nil
}

func newAdminTestHandler(t *testing.T, svc *adminFakeService) http.Handler {
	t.Helper()
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	h := authhandlers.New(svc, authhandlers.NewMemoryLockoutStore(), authhandlers.Config{}, func() time.Time { return now })
	cfg := AdminConfig{
		Auth:           h,
		CookieSecure:   false,
		TrustedProxies: nil,
	}
	return NewAdminRouter(cfg)
}

func TestMiddlewareOrder(t *testing.T) {
	uid := uuid.New()
	svc := &adminFakeService{
		loginSession: &domauth.Session{ID: uuid.New(), UserID: uid},
		loginToken:   "raw-token",
		me:           &domauth.User{ID: uid, Email: "a@example.com", IsActive: true},
	}
	r := newAdminTestHandler(t, svc)

	// TrustedProxy runs before SecurityHeaders: a client-supplied
	// X-Forwarded-Proto must be stripped by the (untrusted) proxy middleware so
	// the HSTS forwarded-proto path is never fed a spoofed value.
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if got := rr.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("HSTS present on untrusted forwarded request: %q (TrustedProxy must precede SecurityHeaders)", got)
	}
	if rr.Header().Get("X-Request-ID") == "" {
		t.Error("Correlation middleware did not emit X-Request-ID")
	}
	if got := rr.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("SecurityHeaders CSP missing: %q", got)
	}

	// AdminCORS precedes CSRF/session: an OPTIONS preflight to a session route
	// is answered by AdminCORS (204 + allow-headers incl. X-CSRF-Token).
	pre := httptest.NewRequest(http.MethodOptions, "/api/admin/v1/auth/me", nil)
	pre.Header.Set("Origin", "http://localhost:9000")
	pre.Header.Set("Access-Control-Request-Method", "GET")
	preRr := httptest.NewRecorder()
	r.ServeHTTP(preRr, pre)
	if preRr.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS preflight = %d, want 204 (AdminCORS before CSRF/session)", preRr.Code)
	}
	if got := preRr.Header().Get("Access-Control-Allow-Headers"); !strings.Contains(got, "X-CSRF-Token") {
		t.Errorf("admin allow-headers missing X-CSRF-Token: %q", got)
	}

	// SecurityHeaders → CSRF → SessionAuth on a safe GET: the CSRF safe-method
	// branch sets gorouter_csrf before SessionAuth rejects the missing cookie.
	authReq := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/status", nil)
	authRr := httptest.NewRecorder()
	r.ServeHTTP(authRr, authReq)
	if authRr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /auth/status without session = %d, want 401 (SessionAuth after CSRF)", authRr.Code)
	}
	if got := authRr.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Errorf("CSP missing on 401 path: %q", got)
	}
	setCookies := authRr.Result().Cookies()
	if findAdminCookie(setCookies, "gorouter_csrf") == nil {
		t.Error("CSRF safe-method cookie not set before SessionAuth rejected the request (CSRF must precede SessionAuth)")
	}
}

func TestSessionMutationRequiresCSRF(t *testing.T) {
	uid := uuid.New()
	svc := &adminFakeService{
		loginSession: &domauth.Session{ID: uuid.New(), UserID: uid},
		loginToken:   "raw-token",
		me:           &domauth.User{ID: uid, Email: "a@example.com", IsActive: true},
	}
	r := newAdminTestHandler(t, svc)

	loginRr := doAdminLogin(t, r)
	if loginRr.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200", loginRr.Code)
	}
	sessionCookie := findAdminCookie(loginRr.Result().Cookies(), "gorouter_session")
	csrfCookie := findAdminCookie(loginRr.Result().Cookies(), "gorouter_csrf")
	if sessionCookie == nil || csrfCookie == nil {
		t.Fatalf("login did not issue both cookies; got %v", adminCookieNames(loginRr.Result().Cookies()))
	}

	logout := func(withToken string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/logout", nil)
		req.AddCookie(sessionCookie)
		req.AddCookie(csrfCookie)
		if withToken != "" {
			req.Header.Set("X-CSRF-Token", withToken)
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	// Fail-closed: a session-cookie mutation without the CSRF echo is 403.
	if rr := logout(""); rr.Code != http.StatusForbidden {
		t.Errorf("logout without CSRF token = %d, want 403 (fail-closed preserved)", rr.Code)
	}
	// Correct double-submit echo succeeds.
	if rr := logout(csrfCookie.Value); rr.Code != http.StatusOK {
		t.Errorf("logout with CSRF token = %d, want 200", rr.Code)
	}
}

// TestPATBypassesCSRF proves the route-grouping resolution of the CSRF/PAT
// bypass (csrf.go fails closed for cookie-less mutations; design §6 L127
// requires PAT/CLI/job to bypass CSRF). PAT handlers mount in a CSRF-free
// group, so a PAT mutation succeeds without any cookie or CSRF token, while
// session-cookie mutations stay behind CSRF (see TestSessionMutationRequiresCSRF).
func TestPATBypassesCSRF(t *testing.T) {
	uid := uuid.New()
	svc := &adminFakeService{
		loginSession: &domauth.Session{ID: uuid.New(), UserID: uid},
		loginToken:   "raw-token",
		me:           &domauth.User{ID: uid, Email: "a@example.com", IsActive: true},
	}
	patValidator := func(ctx context.Context, token string) (*domkeys.PAT, error) {
		return &domkeys.PAT{ID: uuid.New(), UserID: uid}, nil
	}

	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	h := authhandlers.New(svc, authhandlers.NewMemoryLockoutStore(), authhandlers.Config{}, func() time.Time { return now })
	cfg := AdminConfig{Auth: h, TrustedProxies: nil}

	probe := chi.NewRouter()
	probe.Use(NewAdminChain(cfg)...)
	probe.Group(func(p chi.Router) {
		p.Use(PATAuthMiddleware(patValidator))
		p.Post("/auth/probe", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	})

	req := httptest.NewRequest(http.MethodPost, "/auth/probe", nil)
	req.Header.Set("Authorization", "Bearer pat-token")
	req.Header.Set("X-CSRF-Token", "spoofed-token-that-must-be-ignored")
	rr := httptest.NewRecorder()
	probe.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Errorf("PAT mutation = %d, want 204 (PAT must bypass CSRF)", rr.Code)
	}
}

// adminOIDCFake implements authhandlers.OIDCService for the mount test.
type adminOIDCFake struct{}

func (adminOIDCFake) LoginWithOIDC(context.Context, string) (*domauth.Session, string, error) {
	return &domauth.Session{ID: uuid.New(), UserID: uuid.New()}, "raw-token", nil
}

// TestAdminOIDCMount proves the admin router exposes the three OIDC routes
// with the correct auth boundary: start/callback are public (pre-session),
// test is registered behind the session-cookie group (session/PAT, config read).
func TestAdminOIDCMount(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	svc := &adminFakeService{
		loginSession: &domauth.Session{ID: uuid.New(), UserID: uuid.New()},
		me:           &domauth.User{ID: uuid.New(), Email: "a@example.com", IsActive: true},
	}
	base := authhandlers.New(svc, authhandlers.NewMemoryLockoutStore(), authhandlers.Config{}, func() time.Time { return now })
	oidc := base.WithOIDC(authhandlers.OIDCConfig{
		Issuer: "http://insecure.test", ClientID: "c", RedirectURI: "https://app.test/cb",
	}, adminOIDCFake{}, nil, nil)
	r := NewAdminRouter(AdminConfig{Auth: base, OIDC: oidc, TrustedProxies: nil})

	start := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/oidc/start", nil)
	startRr := httptest.NewRecorder()
	r.ServeHTTP(startRr, start)
	if startRr.Code != http.StatusBadRequest {
		t.Fatalf("GET /auth/oidc/start (registered, insecure issuer) = %d, want 400", startRr.Code)
	}

	cb := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/oidc/callback", nil)
	cbRr := httptest.NewRecorder()
	r.ServeHTTP(cbRr, cb)
	if cbRr.Code != http.StatusBadRequest {
		t.Fatalf("GET /auth/oidc/callback (registered) = %d, want 400", cbRr.Code)
	}

	// test is mounted in the CSRF -> SessionAuth group: a cookie-less POST is
	// fail-closed 403 at CSRF (never 404), proving the route sits behind the
	// session boundary.
	testReq := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/oidc/test", nil)
	testRr := httptest.NewRecorder()
	r.ServeHTTP(testRr, testReq)
	if testRr.Code != http.StatusForbidden {
		t.Fatalf("POST /auth/oidc/test without CSRF = %d, want 403 (session+CSRF group)", testRr.Code)
	}
}

func doAdminLogin(t *testing.T, h http.Handler) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(`{"email":"a@example.com","password":"right"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func findAdminCookie(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func adminCookieNames(cs []*http.Cookie) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}
