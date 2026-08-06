package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	appauth "gorouter/internal/app/auth"
	domauth "gorouter/internal/domain/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

type fakeAuthService struct {
	loginSession *domauth.Session
	loginToken   string
	loginErr     error

	me       *domauth.User
	meErr    error
	logErr   error
	valid    *domauth.Actor
	validErr error
}

func (f *fakeAuthService) Login(context.Context, string, string) (*domauth.Session, string, error) {
	if f.loginErr != nil {
		return nil, "", f.loginErr
	}
	return f.loginSession, f.loginToken, nil
}

func (f *fakeAuthService) Logout(context.Context, uuid.UUID) error {
	return f.logErr
}

func (f *fakeAuthService) GetCurrentUser(context.Context, uuid.UUID) (*domauth.User, error) {
	return f.me, f.meErr
}

func (f *fakeAuthService) ValidateSession(context.Context, string) (*domauth.Actor, error) {
	return f.valid, f.validErr
}

// durableKV plays the role of gorouter_runtime_state for the labeled
// restart-survival deviation test: it outlives a single handler instance.
type durableKV struct {
	m map[string]json.RawMessage
}

func (d *durableKV) Get(ctx context.Context, key string) (json.RawMessage, error) {
	v, ok := d.m[key]
	if !ok {
		return nil, nil
	}
	return v, nil
}

func (d *durableKV) Set(ctx context.Context, key string, value json.RawMessage, ttl time.Duration) error {
	d.m[key] = value
	return nil
}

func TestLockoutBands(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	advance := func(d time.Duration) { now = now.Add(d) }

	svc := &fakeAuthService{loginErr: appauth.ErrInvalidCredentials}
	store := NewMemoryLockoutStore()
	h := newTestHandler(svc, store, &now)

	fails := func(h *Handler, n int) {
		for i := 0; i < n; i++ {
			rr := doLogin(t, h, "a@example.com", "wrong")
			if rr.Code != http.StatusUnauthorized {
				t.Fatalf("failed attempt %d: got %d, want 401", i+1, rr.Code)
			}
		}
	}
	trigger := func(h *Handler, expectRetryAfter time.Duration) {
		fails(h, 5)
		rr := doLogin(t, h, "a@example.com", "wrong")
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("locked attempt: got %d, want 429", rr.Code)
		}
		if got := rr.Header().Get("Retry-After"); got != strconv.FormatInt(int64(expectRetryAfter/time.Second), 10) {
			t.Errorf("Retry-After = %q, want %s", got, strconv.FormatInt(int64(expectRetryAfter/time.Second), 10))
		}
		advance(expectRetryAfter + time.Second)
	}

	t.Run("progressive bands 30s/2m/10m/30m with last reused", func(t *testing.T) {
		trigger(h, 30*time.Second)
		trigger(h, 2*time.Minute)
		trigger(h, 10*time.Minute)
		trigger(h, 30*time.Minute)
		trigger(h, 30*time.Minute)
	})

	t.Run("success resets lockout state", func(t *testing.T) {
		store := NewMemoryLockoutStore()
		hBad := newTestHandler(&fakeAuthService{loginErr: appauth.ErrInvalidCredentials}, store, &now)
		fails(hBad, 5)
		advance(31 * time.Second)
		good := &fakeAuthService{
			loginSession: &domauth.Session{ID: uuid.New(), UserID: uuid.New()},
			loginToken:   "raw-token",
			me:           &domauth.User{ID: uuid.New(), Email: "a@example.com", IsActive: true},
		}
		hGood := newTestHandler(good, store, &now)
		if rr := doLogin(t, hGood, "a@example.com", "right"); rr.Code != http.StatusOK {
			t.Fatalf("successful login: got %d, want 200", rr.Code)
		}
		// After the reset, a fresh block starts from the base 30s band again.
		fails(hBad, 5)
		rr := doLogin(t, hBad, "a@example.com", "wrong")
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("post-reset escalation: got %d, want 429", rr.Code)
		}
		if got := rr.Header().Get("Retry-After"); got != "30" {
			t.Errorf("Retry-After = %q, want 30 (success must reset to the base band)", got)
		}
	})

	t.Run("one-hour idle window resets escalation", func(t *testing.T) {
		store := NewMemoryLockoutStore()
		h := newTestHandler(svc, store, &now)
		fails(h, 5)
		advance(2 * time.Minute)
		fails(h, 5)
		advance(61 * time.Minute)
		fails(h, 5)
		rr := doLogin(t, h, "a@example.com", "wrong")
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("post-idle lock: got %d, want 429", rr.Code)
		}
		if got := rr.Header().Get("Retry-After"); got != "30" {
			t.Errorf("Retry-After = %q, want 30 (band reset to 30s after idle)", got)
		}
	})

	// Persisting the active lockout through gorouter_runtime_state is a
	// documented hardening deviation from the pinned upstream reset-on-restart
	// behavior (design D17, decision #22) — never an upstream-parity claim.
	t.Run("restart-survival via gorouter_runtime_state (labeled deviation)", func(t *testing.T) {
		kv := &durableKV{m: map[string]json.RawMessage{}}
		store := newRuntimeStateLockoutStore(kv)
		h1 := newTestHandler(svc, store, &now)
		fails(h1, 5)
		rr := doLogin(t, h1, "a@example.com", "wrong")
		if rr.Code != http.StatusTooManyRequests {
			t.Fatalf("pre-restart lock: got %d, want 429", rr.Code)
		}
		h2 := newTestHandler(&fakeAuthService{loginErr: appauth.ErrInvalidCredentials}, store, &now)
		rr2 := doLogin(t, h2, "a@example.com", "wrong")
		if rr2.Code != http.StatusTooManyRequests {
			t.Errorf("restart-survival: got %d, want 429 (lock must persist)", rr2.Code)
		}
	})
}

func TestSessionCookieAttributes(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	good := &fakeAuthService{
		loginSession: &domauth.Session{ID: uuid.New(), UserID: userID},
		loginToken:   "raw-session-token",
		me:           &domauth.User{ID: userID, Email: "a@example.com", IsActive: true},
	}

	t.Run("loopback cookie: gorouter_session httpOnly lax, csrf readable", func(t *testing.T) {
		store := NewMemoryLockoutStore()
		h := newTestHandler(good, store, &now)
		rr := doLogin(t, h, "a@example.com", "right")
		if rr.Code != http.StatusOK {
			t.Fatalf("login: got %d, want 200", rr.Code)
		}
		cookies := rr.Result().Cookies()

		sess := findCookie(cookies, "gorouter_session")
		if sess == nil {
			t.Fatalf("session cookie %q not set; got %v", "gorouter_session", cookieNames(cookies))
		}
		if sess.HttpOnly != true {
			t.Errorf("session cookie HttpOnly = %v, want true", sess.HttpOnly)
		}
		if sess.SameSite != http.SameSiteLaxMode {
			t.Errorf("session cookie SameSite = %v, want Lax", sess.SameSite)
		}
		if sess.Path != "/" {
			t.Errorf("session cookie Path = %q, want /", sess.Path)
		}
		if sess.Secure {
			t.Errorf("loopback session cookie Secure = true, want false")
		}

		csrf := findCookie(cookies, "gorouter_csrf")
		if csrf == nil {
			t.Fatalf("csrf cookie %q not set; got %v", "gorouter_csrf", cookieNames(cookies))
		}
		if csrf.HttpOnly {
			t.Errorf("csrf cookie HttpOnly = true, want false (readable by client)")
		}
		if csrf.SameSite != http.SameSiteLaxMode {
			t.Errorf("csrf cookie SameSite = %v, want Lax", csrf.SameSite)
		}
	})

	t.Run("secure cookie: __Host- prefix, Secure, httpOnly", func(t *testing.T) {
		store := NewMemoryLockoutStore()
		h := New(good, store, Config{Secure: true}, func() time.Time { return now })
		rr := doLogin(t, h, "a@example.com", "right")
		if rr.Code != http.StatusOK {
			t.Fatalf("login: got %d, want 200", rr.Code)
		}
		cookies := rr.Result().Cookies()

		sess := findCookie(cookies, "__Host-gorouter_session")
		if sess == nil {
			t.Fatalf("secure session cookie %q not set; got %v", "__Host-gorouter_session", cookieNames(cookies))
		}
		if !sess.Secure {
			t.Errorf("secure session cookie Secure = false, want true")
		}
		if !sess.HttpOnly {
			t.Errorf("secure session cookie HttpOnly = false, want true")
		}
		if sess.Path != "/" {
			t.Errorf("secure session cookie Path = %q, want /", sess.Path)
		}
		if sess.SameSite != http.SameSiteLaxMode {
			t.Errorf("secure session cookie SameSite = %v, want Lax", sess.SameSite)
		}
		if findCookie(cookies, "gorouter_session") != nil {
			t.Errorf("loopback session cookie must not be issued under Secure config")
		}
	})
}

func TestNoResetRoute(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	h := newTestHandler(&fakeAuthService{}, NewMemoryLockoutStore(), &now)
	r := chi.NewRouter()
	h.Routes(r)

	// Password reset is a local CLI use case only (design D16); no HTTP route
	// or handler may exist for it.
	for _, p := range []string{
		"/auth/reset-password",
		"/auth/reset",
		"/auth/password-reset",
		"/auth/password",
	} {
		for _, m := range []string{http.MethodGet, http.MethodPost} {
			req := httptest.NewRequest(m, p, nil)
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != http.StatusNotFound {
				t.Errorf("%s %s = %d, want 404 (reset-password must not exist)", m, p, rr.Code)
			}
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/auth/me", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("GET /auth/me (unauthenticated, plain Routes) = %d, want 401 (registered)", rr.Code)
	}
}

func TestMeProjectionIsCredentialSafe(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	svc := &fakeAuthService{
		me: &domauth.User{
			ID:           userID,
			Email:        "a@example.com",
			PasswordHash: "should-never-leak",
			IsAdmin:      true,
			IsActive:     true,
		},
	}
	h := New(svc, NewMemoryLockoutStore(), Config{}, func() time.Time { return now })
	actor := &domauth.Actor{UserID: userID, SessionID: uuid.New(), IsAdmin: true,
		Kind: domauth.ActorKindSession, Origin: domauth.ActorOriginRemote}
	ctx := domauth.ContextWithActor(context.Background(), actor)
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/me", nil).WithContext(ctx)
	rr := httptest.NewRecorder()
	h.Me(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /auth/me = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("me body invalid JSON: %v", err)
	}
	if s := strings.ToLower(rr.Body.String()); strings.Contains(s, "password") || strings.Contains(s, "should-never-leak") {
		t.Errorf("me projection leaked credential material: %s", rr.Body.String())
	}
	if body["email"] != "a@example.com" {
		t.Errorf("me email = %v, want a@example.com", body["email"])
	}
}

func TestLogoutClearsCookies(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	sid := uuid.New()
	svc := &fakeAuthService{}
	h := New(svc, NewMemoryLockoutStore(), Config{}, func() time.Time { return now })
	actor := &domauth.Actor{UserID: uuid.New(), SessionID: sid, IsAdmin: true,
		Kind: domauth.ActorKindSession, Origin: domauth.ActorOriginRemote}

	t.Run("clears session and csrf cookies", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/logout", nil)
		req = req.WithContext(domauth.ContextWithActor(req.Context(), actor))
		rr := httptest.NewRecorder()
		h.Logout(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("logout = %d, want 200", rr.Code)
		}
		cleared := 0
		for _, c := range rr.Result().Cookies() {
			if c.Name == "gorouter_session" || c.Name == "gorouter_csrf" {
				if c.MaxAge >= 0 {
					t.Errorf("cookie %s not cleared (MaxAge=%d)", c.Name, c.MaxAge)
				}
				cleared++
			}
		}
		if cleared != 2 {
			t.Errorf("expected 2 cleared cookies, got %d", cleared)
		}
	})

	t.Run("revoke error is a 500", func(t *testing.T) {
		h := New(&fakeAuthService{logErr: errors.New("revoke failed")}, NewMemoryLockoutStore(), Config{}, func() time.Time { return now })
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/logout", nil)
		req = req.WithContext(domauth.ContextWithActor(req.Context(), actor))
		rr := httptest.NewRecorder()
		h.Logout(rr, req)
		if rr.Code != http.StatusInternalServerError {
			t.Errorf("logout revoke failure = %d, want 500", rr.Code)
		}
	})

	t.Run("unauthenticated is 401", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/logout", nil)
		rr := httptest.NewRecorder()
		h.Logout(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("logout without actor = %d, want 401", rr.Code)
		}
	})
}

func TestStatusReturnsSessionInfo(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	userID := uuid.New()
	display := "Alice"
	svc := &fakeAuthService{me: &domauth.User{ID: userID, Email: "a@example.com", IsAdmin: true, DisplayName: &display}}
	h := New(svc, NewMemoryLockoutStore(), Config{}, func() time.Time { return now })
	actor := &domauth.Actor{UserID: userID, SessionID: uuid.New(), IsAdmin: true,
		Kind: domauth.ActorKindSession, Origin: domauth.ActorOriginRemote}

	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/status", nil)
	req = req.WithContext(domauth.ContextWithActor(req.Context(), actor))
	req.AddCookie(&http.Cookie{Name: "gorouter_csrf", Value: "csrf-echo-value"})
	rr := httptest.NewRecorder()
	h.Status(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var body map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &body); err != nil {
		t.Fatalf("status body invalid JSON: %v", err)
	}
	if body["authenticated"] != true {
		t.Errorf("authenticated = %v, want true", body["authenticated"])
	}
	if body["session_id"] != actor.SessionID.String() {
		t.Errorf("session_id = %v, want %s", body["session_id"], actor.SessionID.String())
	}
	if body["csrf_token"] != "csrf-echo-value" {
		t.Errorf("csrf_token = %v, want csrf-echo-value", body["csrf_token"])
	}

	unauthed := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/status", nil)
	urr := httptest.NewRecorder()
	h.Status(urr, unauthed)
	if urr.Code != http.StatusUnauthorized {
		t.Errorf("status without actor = %d, want 401", urr.Code)
	}
}

func TestMeUnauthenticatedIs401(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	h := New(&fakeAuthService{}, NewMemoryLockoutStore(), Config{}, func() time.Time { return now })
	req := httptest.NewRequest(http.MethodGet, "/api/admin/v1/auth/me", nil)
	rr := httptest.NewRecorder()
	h.Me(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Errorf("me without actor = %d, want 401", rr.Code)
	}
}

func TestLoginErrorPaths(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	t.Run("invalid body is 400", func(t *testing.T) {
		h := newTestHandler(&fakeAuthService{}, NewMemoryLockoutStore(), &now)
		req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader("{not-json"))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		h.Login(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Errorf("malformed login body = %d, want 400", rr.Code)
		}
	})

	t.Run("service error is 500 and does not count the failure", func(t *testing.T) {
		store := NewMemoryLockoutStore()
		h := newTestHandler(&fakeAuthService{loginErr: errors.New("db down")}, store, &now)
		rr := doLogin(t, h, "a@example.com", "wrong")
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("service error = %d, want 500", rr.Code)
		}
		// A non-credential error must not count toward lockout.
		rr2 := doLogin(t, h, "a@example.com", "wrong")
		if rr2.Code != http.StatusInternalServerError {
			t.Errorf("second service error = %d, want 500 (no lockout accrual)", rr2.Code)
		}
	})

	t.Run("credential failure is 401 and counts toward lockout", func(t *testing.T) {
		h := newTestHandler(&fakeAuthService{loginErr: appauth.ErrInvalidCredentials}, NewMemoryLockoutStore(), &now)
		rr := doLogin(t, h, "a@example.com", "wrong")
		if rr.Code != http.StatusUnauthorized {
			t.Errorf("invalid credentials = %d, want 401", rr.Code)
		}
	})
}

func TestValidateSessionPassthrough(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	actor := &domauth.Actor{UserID: uuid.New(), Kind: domauth.ActorKindSession}
	svc := &fakeAuthService{valid: actor}
	h := New(svc, NewMemoryLockoutStore(), Config{}, func() time.Time { return now })
	got, err := h.ValidateSession(context.Background(), "raw-token")
	if err != nil {
		t.Fatalf("ValidateSession error: %v", err)
	}
	if got != actor {
		t.Errorf("ValidateSession returned %v, want the service actor", got)
	}
}

func TestNewDefaults(t *testing.T) {
	svc := &fakeAuthService{}
	h := New(svc, nil, Config{}, nil)
	if h.store == nil {
		t.Error("nil store must default to an in-memory store")
	}
	if h.SessionCookieName() != "gorouter_session" {
		t.Errorf("SessionCookieName = %q, want gorouter_session", h.SessionCookieName())
	}
	hSecure := New(svc, nil, Config{Secure: true}, nil)
	if hSecure.SessionCookieName() != "__Host-gorouter_session" {
		t.Errorf("secure SessionCookieName = %q, want __Host-gorouter_session", hSecure.SessionCookieName())
	}
}

// --- helpers ----------------------------------------------------------------

func newTestHandler(svc AuthService, store LockoutStore, clock *time.Time) *Handler {
	return New(svc, store, Config{}, func() time.Time { return *clock })
}

func doLogin(t *testing.T, h *Handler, email, password string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"email":%q,"password":%q}`, email, password)
	req := httptest.NewRequest(http.MethodPost, "/api/admin/v1/auth/login", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	h.Login(rr, req)
	return rr
}

func findCookie(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}

func cookieNames(cs []*http.Cookie) []string {
	out := make([]string, 0, len(cs))
	for _, c := range cs {
		out = append(out, c.Name)
	}
	return out
}
