package auth

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	domauth "gorouter/internal/domain/auth"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakeOIDCService is the OIDCSessionService surface the transport maps onto;
// the concrete app service is wired at bootstrap (api-06 pattern: fakes drive
// the transport tests).
type fakeOIDCService struct {
	session  *domauth.Session
	token    string
	err      error
	gotEmail string
}

func (f *fakeOIDCService) LoginWithOIDC(ctx context.Context, email string) (*domauth.Session, string, error) {
	f.gotEmail = email
	if f.err != nil {
		return nil, "", f.err
	}
	return f.session, f.token, nil
}

// codeParams describes how a fake authorization code must be redeemed.
type codeParams struct {
	challenge string
	nonce     string
	aud       string
	iss       string
	exp       int64
	iat       int64
	email     string
}

// fakeOIDCIssuer is a loopback-only mock OIDC identity provider serving
// discovery, JWKS and the token endpoint. All requests from the handler are
// redirected onto it by oidcRewriteTransport, so no real external calls escape.
type fakeOIDCIssuer struct {
	t      *testing.T
	srv    *httptest.Server
	issuer string

	mu              sync.Mutex
	signKey         *rsa.PrivateKey
	signKid         string
	codes           map[string]codeParams
	discoveryStatus int
}

func newFakeOIDCIssuer(t *testing.T) *fakeOIDCIssuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate test signing key: %v", err)
	}
	f := &fakeOIDCIssuer{
		t:               t,
		issuer:          "https://oidc.test",
		signKey:         key,
		signKid:         "key-1",
		codes:           map[string]codeParams{},
		discoveryStatus: http.StatusOK,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", f.handleDiscovery)
	mux.HandleFunc("/jwks", f.handleJWKS)
	mux.HandleFunc("/token", f.handleToken)
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeOIDCIssuer) handleDiscovery(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	st := f.discoveryStatus
	f.mu.Unlock()
	if st != http.StatusOK {
		w.WriteHeader(st)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"issuer":                 f.issuer,
		"authorization_endpoint": f.issuer + "/authorize",
		"token_endpoint":         f.issuer + "/token",
		"jwks_uri":               f.issuer + "/jwks",
	})
}

func (f *fakeOIDCIssuer) handleJWKS(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	pub := f.signKey.Public().(*rsa.PublicKey)
	jwk := map[string]string{
		"kty": "RSA", "kid": f.signKid, "use": "sig", "alg": "RS256",
		"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
		"e": base64.RawURLEncoding.EncodeToString(bigEndianBytes(pub.E)),
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"keys": []any{jwk}})
}

func (f *fakeOIDCIssuer) handleToken(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	code := r.Form.Get("code")
	verifier := r.Form.Get("code_verifier")
	f.mu.Lock()
	p, ok := f.codes[code]
	f.mu.Unlock()
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// Enforce PKCE S256 on the loopback issuer: the verifier must derive the
	// challenge that start issued.
	sum := sha256.Sum256([]byte(verifier))
	if p.challenge != "" && base64.RawURLEncoding.EncodeToString(sum[:]) != p.challenge {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"access_token": "at-" + code, "token_type": "Bearer", "expires_in": 3600,
		"id_token": f.signToken(p),
	})
}

func (f *fakeOIDCIssuer) signToken(p codeParams) string {
	header := map[string]any{"alg": "RS256", "kid": f.signKid, "typ": "JWT"}
	hb, _ := json.Marshal(header)
	pb, _ := json.Marshal(map[string]any{
		"iss": p.iss, "sub": "subj-123", "aud": p.aud, "exp": p.exp, "iat": p.iat,
		"nonce": p.nonce, "email": p.email,
	})
	msg := base64.RawURLEncoding.EncodeToString(hb) + "." + base64.RawURLEncoding.EncodeToString(pb)
	sum := sha256.Sum256([]byte(msg))
	f.mu.Lock()
	key := f.signKey
	f.mu.Unlock()
	sig, err := rsa.SignPKCS1v15(rand.Reader, key, crypto.SHA256, sum[:])
	if err != nil {
		f.t.Fatalf("sign id token: %v", err)
	}
	return msg + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func (f *fakeOIDCIssuer) issueCode(code string, p codeParams) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.codes[code] = p
}

func (f *fakeOIDCIssuer) rotate() {
	f.mu.Lock()
	defer f.mu.Unlock()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		f.t.Fatalf("rotate signing key: %v", err)
	}
	f.signKey = key
	f.signKid = "key-2"
}

func (f *fakeOIDCIssuer) client() *http.Client {
	return &http.Client{Transport: &oidcRewriteTransport{host: "oidc.test", target: f.srv.URL}}
}

func bigEndianBytes(v int) []byte {
	if v == 0 {
		return []byte{0}
	}
	var out []byte
	for v > 0 {
		out = append([]byte{byte(v & 0xff)}, out...)
		v >>= 8
	}
	return out
}

// oidcRewriteTransport redirects requests for the fake https://oidc.test host
// onto the loopback httptest server (the only network the handler ever hits).
type oidcRewriteTransport struct {
	host   string
	target string // e.g. http://127.0.0.1:PORT
}

func (t *oidcRewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Host != t.host {
		return http.DefaultTransport.RoundTrip(req)
	}
	u := *req.URL
	u.Scheme = "http"
	u.Host = strings.TrimPrefix(t.target, "http://")
	req2 := req.Clone(req.Context())
	req2.URL = &u
	return http.DefaultTransport.RoundTrip(req2)
}

func TestOIDCCallbackReplay(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	iss := newFakeOIDCIssuer(t)
	h := newOIDCTestHandler(t, iss, now, Config{})
	r := chi.NewRouter()
	h.Routes(r)

	t.Run("same code cannot be redeemed twice", func(t *testing.T) {
		state, nonce, challenge, cookie := startOIDCFlow(t, h, r)
		iss.issueCode("code-1", codeParams{
			challenge: challenge, nonce: nonce, aud: "client-id", iss: "https://oidc.test",
			exp: now.Add(time.Hour).Unix(), iat: now.Unix(), email: "user@example.com",
		})

		rr := redeemOIDCCallback(t, h, r, "code-1", state, cookie)
		if rr.Code != http.StatusFound {
			t.Fatalf("first redemption = %d, want 302 (successful callback redirect)", rr.Code)
		}
		if findCookie(rr.Result().Cookies(), sessionCookieName) == nil {
			t.Error("successful callback must issue the session cookie")
		}

		rr2 := redeemOIDCCallback(t, h, r, "code-1", state, cookie)
		if rr2.Code != http.StatusBadRequest {
			t.Fatalf("replayed callback = %d, want 400 (replay must be blocked)", rr2.Code)
		}
	})

	t.Run("nonce mismatch fails", func(t *testing.T) {
		state, _, challenge, cookie := startOIDCFlow(t, h, r)
		iss.issueCode("code-2", codeParams{
			challenge: challenge, nonce: "attacker-controlled-nonce", aud: "client-id", iss: "https://oidc.test",
			exp: now.Add(time.Hour).Unix(), iat: now.Unix(), email: "user@example.com",
		})
		rr := redeemOIDCCallback(t, h, r, "code-2", state, cookie)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("nonce mismatch = %d, want 400 (nonce must be enforced)", rr.Code)
		}
		if findCookie(rr.Result().Cookies(), sessionCookieName) != nil {
			t.Error("a failed callback must not issue a session cookie")
		}
	})
}

func TestOIDCStartContract(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	t.Run("https-only issuer is rejected", func(t *testing.T) {
		iss := newFakeOIDCIssuer(t)
		h := newOIDCTestHandler(t, iss, now, Config{})
		h.oidcCfg.Issuer = "http://oidc.test"
		r := chi.NewRouter()
		h.Routes(r)
		req := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("http issuer start = %d, want 400 (HTTPS-only enforced)", rr.Code)
		}
		if loc := rr.Header().Get("Location"); loc != "" {
			t.Errorf("http issuer must not redirect: Location = %q", loc)
		}
	})

	t.Run("state cookie attributes under Secure config", func(t *testing.T) {
		iss := newFakeOIDCIssuer(t)
		h := newOIDCTestHandler(t, iss, now, Config{Secure: true})
		r := chi.NewRouter()
		h.Routes(r)
		_, _, _, cookie := startOIDCFlow(t, h, r)
		if !cookie.Secure {
			t.Error("state cookie Secure = false, want true")
		}
		if !cookie.HttpOnly {
			t.Error("state cookie HttpOnly = false, want true")
		}
		if cookie.SameSite != http.SameSiteLaxMode {
			t.Errorf("state cookie SameSite = %v, want Lax", cookie.SameSite)
		}
		if cookie.Path != "/" {
			t.Errorf("state cookie Path = %q, want /", cookie.Path)
		}
		if cookie.MaxAge > 600 {
			t.Errorf("state cookie MaxAge = %d, want <= 600 (10 minute TTL cap)", cookie.MaxAge)
		}
	})

	t.Run("state ttl clamped to ten minutes", func(t *testing.T) {
		iss := newFakeOIDCIssuer(t)
		h := newOIDCTestHandler(t, iss, now, Config{})
		h.oidcCfg.StateTTL = 30 * time.Minute
		r := chi.NewRouter()
		h.Routes(r)
		req := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		cookie := findCookie(rr.Result().Cookies(), oidcStateCookieName)
		if cookie == nil {
			t.Fatal("state cookie not set")
		}
		if cookie.MaxAge > 600 {
			t.Errorf("StateTTL 30m must be clamped: MaxAge = %d, want <= 600", cookie.MaxAge)
		}
	})
}

func TestOIDCInvalidCallbackInputs(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	iss := newFakeOIDCIssuer(t)
	h := newOIDCTestHandler(t, iss, now, Config{})
	r := chi.NewRouter()
	h.Routes(r)

	cases := []struct {
		name    string
		query   string
		cookies []*http.Cookie
	}{
		{"missing code and state", "", nil},
		{"missing cookie", "code=c&state=s", nil},
		{"state cookie mismatch", "code=c&state=s", []*http.Cookie{{Name: oidcStateCookieName, Value: "other"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/auth/oidc/callback", nil)
			if tc.query != "" {
				req = httptest.NewRequest(http.MethodGet, "/auth/oidc/callback?"+tc.query, nil)
			}
			for _, c := range tc.cookies {
				req.AddCookie(c)
			}
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("callback = %d, want 400", rr.Code)
			}
		})
	}
}

func TestOIDCJWTValidation(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	iss := newFakeOIDCIssuer(t)
	h := newOIDCTestHandler(t, iss, now, Config{})
	r := chi.NewRouter()
	h.Routes(r)

	redeem := func(t *testing.T, p codeParams) int {
		t.Helper()
		state, nonce, challenge, cookie := startOIDCFlow(t, h, r)
		if p.nonce == "" {
			p.nonce = nonce
		}
		if p.challenge == "" {
			p.challenge = challenge
		}
		code := "code-" + state
		iss.issueCode(code, p)
		return redeemOIDCCallback(t, h, r, code, state, cookie).Code
	}

	valid := codeParams{aud: "client-id", iss: "https://oidc.test",
		exp: now.Add(time.Hour).Unix(), iat: now.Unix(), email: "user@example.com"}

	t.Run("valid token redeems", func(t *testing.T) {
		if got := redeem(t, valid); got != http.StatusFound {
			t.Fatalf("valid callback = %d, want 302", got)
		}
	})
	t.Run("audience must match", func(t *testing.T) {
		p := valid
		p.aud = "some-other-client"
		if got := redeem(t, p); got != http.StatusBadRequest {
			t.Errorf("wrong aud = %d, want 400", got)
		}
	})
	t.Run("issuer must match", func(t *testing.T) {
		p := valid
		p.iss = "https://evil.test"
		if got := redeem(t, p); got != http.StatusBadRequest {
			t.Errorf("wrong iss = %d, want 400", got)
		}
	})
	t.Run("expired token is rejected", func(t *testing.T) {
		p := valid
		p.exp = now.Add(-time.Hour).Unix()
		if got := redeem(t, p); got != http.StatusBadRequest {
			t.Errorf("expired token = %d, want 400", got)
		}
	})
	t.Run("missing email is rejected", func(t *testing.T) {
		p := valid
		p.email = ""
		if got := redeem(t, p); got != http.StatusBadRequest {
			t.Errorf("missing email = %d, want 400", got)
		}
	})
}

func TestOIDCJWKSRotation(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	iss := newFakeOIDCIssuer(t)
	h := newOIDCTestHandler(t, iss, now, Config{})
	r := chi.NewRouter()
	h.Routes(r)

	state, nonce, challenge, cookie := startOIDCFlow(t, h, r)
	iss.issueCode("code-a", codeParams{challenge: challenge, nonce: nonce, aud: "client-id", iss: "https://oidc.test",
		exp: now.Add(time.Hour).Unix(), iat: now.Unix(), email: "user@example.com"})
	if rr := redeemOIDCCallback(t, h, r, "code-a", state, cookie); rr.Code != http.StatusFound {
		t.Fatalf("pre-rotation redemption = %d, want 302", rr.Code)
	}

	iss.rotate() // new key and kid; the old key drops out of JWKS
	state2, nonce2, challenge2, cookie2 := startOIDCFlow(t, h, r)
	iss.issueCode("code-b", codeParams{challenge: challenge2, nonce: nonce2, aud: "client-id", iss: "https://oidc.test",
		exp: now.Add(time.Hour).Unix(), iat: now.Unix(), email: "user@example.com"})
	rr := redeemOIDCCallback(t, h, r, "code-b", state2, cookie2)
	if rr.Code != http.StatusFound {
		t.Fatalf("post-rotation redemption = %d, want 302 (unknown kid must re-fetch JWKS)", rr.Code)
	}
}

func TestOIDCStateTTLAndPurge(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)
	ctx := context.Background()

	t.Run("expired state is rejected on consume", func(t *testing.T) {
		store := newMemoryOIDCStateStore(func() time.Time { return now })
		_ = store.Create(ctx, &OIDCState{State: "expired", Nonce: "n", CodeVerifier: "v", ExpiresAt: now.Add(-time.Minute)}, time.Minute)
		if _, err := store.Consume(ctx, "expired"); !errors.Is(err, ErrOIDCStateExpired) {
			t.Fatalf("consume = %v, want ErrOIDCStateExpired", err)
		}
	})

	t.Run("purge removes used and expired entries on a bounded window", func(t *testing.T) {
		store := newMemoryOIDCStateStore(func() time.Time { return now })
		_ = store.Create(ctx, &OIDCState{State: "used", Nonce: "n", CodeVerifier: "v", ExpiresAt: now.Add(5 * time.Minute), Used: true}, time.Minute)
		_ = store.Create(ctx, &OIDCState{State: "stale", Nonce: "n", CodeVerifier: "v", ExpiresAt: now.Add(-time.Minute)}, time.Minute)
		_ = store.Create(ctx, &OIDCState{State: "live", Nonce: "n", CodeVerifier: "v", ExpiresAt: now.Add(5 * time.Minute)}, time.Minute)
		if err := store.PurgeExpired(ctx, now); err != nil {
			t.Fatalf("purge: %v", err)
		}
		if _, err := store.Consume(ctx, "live"); err != nil {
			t.Errorf("live state must survive purge: %v", err)
		}
		if _, err := store.Consume(ctx, "used"); !errors.Is(err, ErrOIDCStateNotFound) {
			t.Errorf("used state must be purged: got %v, want ErrOIDCStateNotFound", err)
		}
		if _, err := store.Consume(ctx, "stale"); !errors.Is(err, ErrOIDCStateNotFound) {
			t.Errorf("expired state must be purged: got %v, want ErrOIDCStateNotFound", err)
		}
	})
}

func TestOIDCTestHandler(t *testing.T) {
	now := time.Date(2026, 8, 5, 12, 0, 0, 0, time.UTC)

	t.Run("valid issuer reports ok", func(t *testing.T) {
		iss := newFakeOIDCIssuer(t)
		h := newOIDCTestHandler(t, iss, now, Config{})
		r := chi.NewRouter()
		h.Routes(r)
		req := httptest.NewRequest(http.MethodPost, "/auth/oidc/test", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("test = %d, want 200", rr.Code)
		}
	})

	t.Run("discovery failure is 400", func(t *testing.T) {
		iss := newFakeOIDCIssuer(t)
		iss.mu.Lock()
		iss.discoveryStatus = http.StatusInternalServerError
		iss.mu.Unlock()
		h := newOIDCTestHandler(t, iss, now, Config{})
		r := chi.NewRouter()
		h.Routes(r)
		req := httptest.NewRequest(http.MethodPost, "/auth/oidc/test", nil)
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("test with failing discovery = %d, want 400", rr.Code)
		}
	})
}

// --- helpers ----------------------------------------------------------------

func newOIDCTestHandler(t *testing.T, iss *fakeOIDCIssuer, now time.Time, cfg Config) *Handler {
	t.Helper()
	base := New(&fakeAuthService{}, NewMemoryLockoutStore(), cfg, func() time.Time { return now })
	oidcCfg := OIDCConfig{
		Issuer:      iss.issuer,
		ClientID:    "client-id",
		RedirectURI: "https://app.test/api/admin/v1/auth/oidc/callback",
	}
	oidcSvc := &fakeOIDCService{
		session: &domauth.Session{ID: uuid.New(), UserID: uuid.New()},
		token:   "raw-oidc-session",
	}
	return base.WithOIDC(oidcCfg, oidcSvc, newMemoryOIDCStateStore(func() time.Time { return now }), iss.client())
}

func startOIDCFlow(t *testing.T, h *Handler, r chi.Router) (state, nonce, challenge string, cookie *http.Cookie) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/auth/oidc/start", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != http.StatusFound {
		t.Fatalf("start = %d, want 302", rr.Code)
	}
	loc, err := url.Parse(rr.Header().Get("Location"))
	if err != nil {
		t.Fatalf("invalid Location %q: %v", rr.Header().Get("Location"), err)
	}
	q := loc.Query()
	if q.Get("response_type") != "code" {
		t.Errorf("response_type = %q, want code", q.Get("response_type"))
	}
	if q.Get("code_challenge_method") != "S256" {
		t.Errorf("code_challenge_method = %q, want S256", q.Get("code_challenge_method"))
	}
	if q.Get("client_id") != "client-id" {
		t.Errorf("client_id = %q, want client-id", q.Get("client_id"))
	}
	if q.Get("scope") != "openid" {
		t.Errorf("scope = %q, want openid", q.Get("scope"))
	}
	state = q.Get("state")
	nonce = q.Get("nonce")
	challenge = q.Get("code_challenge")
	if state == "" || nonce == "" || challenge == "" {
		t.Fatalf("auth URL must include state/nonce/code_challenge, got state=%q nonce=%q challenge=%q", state, nonce, challenge)
	}
	cookie = findCookie(rr.Result().Cookies(), oidcStateCookieName)
	if cookie == nil {
		t.Fatal("start must issue the gorouter_oidc_state cookie")
	}
	return state, nonce, challenge, cookie
}

func redeemOIDCCallback(t *testing.T, h *Handler, r chi.Router, code, state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	u := "/auth/oidc/callback?code=" + url.QueryEscape(code) + "&state=" + url.QueryEscape(state)
	req := httptest.NewRequest(http.MethodGet, u, nil)
	req.AddCookie(cookie)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}
