package auth

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	appauth "gorouter/internal/app/auth"
	engineoauth "gorouter/internal/engine/oauth"

	"github.com/go-chi/chi/v5"
)

const (
	oidcStateCookieName   = "gorouter_oidc_state"
	oidcStateKeyPrefix    = "oidc:state:"
	oidcDefaultStateTTL   = 10 * time.Minute
	oidcMaxStateTTL       = 10 * time.Minute
	oidcDiscoveryPath     = "/.well-known/openid-configuration"
	oidcJWKSCacheTTL      = 5 * time.Minute
	oidcClockSkew         = 30 * time.Second
	oidcCallbackRedirect  = "/callback/oidc"
	oidcAuthResponseScope = "openid"
	oidcAuthResponseType  = "code"
	oidcChallengeMethod   = "S256"
)

var (
	ErrOIDCNotConfigured  = errors.New("oidc: not configured")
	ErrOIDCInvalidConfig  = errors.New("oidc: invalid configuration")
	ErrOIDCInsecureIssuer = errors.New("oidc: issuer must use https")
	ErrOIDCStateNotFound  = errors.New("oidc: state not found")
	ErrOIDCStateReplay    = errors.New("oidc: state replay blocked")
	ErrOIDCStateExpired   = errors.New("oidc: state expired")
	ErrOIDCUnknownKey     = errors.New("oidc: unknown signing key")
	ErrOIDCInvalidToken   = errors.New("oidc: invalid id token")
	ErrOIDCExchangeFailed = errors.New("oidc: token exchange failed")
)

// OIDCConfig is the transport's OIDC issuer configuration (security P1-8).
// The issuer must be HTTPS-only; StateTTL is bounded to 10 minutes.
type OIDCConfig struct {
	Issuer      string
	ClientID    string
	RedirectURI string
	Audiences   []string
	StateTTL    time.Duration
}

func (c OIDCConfig) validate() error {
	if c.Issuer == "" || c.ClientID == "" || c.RedirectURI == "" {
		return ErrOIDCInvalidConfig
	}
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return ErrOIDCInsecureIssuer
	}
	return nil
}

func (c OIDCConfig) issuer() string { return strings.TrimSuffix(c.Issuer, "/") }

// stateTTL clamps the configured TTL to the <= 10 minute contract.
func (c OIDCConfig) stateTTL() time.Duration {
	if c.StateTTL <= 0 {
		return oidcDefaultStateTTL
	}
	if c.StateTTL > oidcMaxStateTTL {
		return oidcMaxStateTTL
	}
	return c.StateTTL
}

func (c OIDCConfig) audiences() []string {
	if len(c.Audiences) == 0 {
		return []string{c.ClientID}
	}
	return append([]string(nil), c.Audiences...)
}

// OIDCService is the application-layer surface the OIDC handlers map onto: it
// creates a session for a verified OIDC identity and returns the raw session
// token for cookie issuance. The concrete app service is wired at bootstrap;
// fakes drive the transport tests.
type OIDCService interface {
	LoginWithOIDC(ctx context.Context, email string) (*appauth.Session, string, error)
}

// OIDCState is a single-use authorization-state record bound to one sign-in
// attempt: the PKCE verifier and the OIDC nonce the callback must satisfy.
type OIDCState struct {
	State        string    `json:"state"`
	Nonce        string    `json:"nonce"`
	CodeVerifier string    `json:"code_verifier"`
	ExpiresAt    time.Time `json:"expires_at"`
	Used         bool      `json:"used"`
}

// OIDCStateStore persists single-use OIDC state with a bounded TTL. A state
// can be consumed exactly once (replay-blocked); expired and used entries are
// purged on a bounded window (pending OAuth state, per #346).
type OIDCStateStore interface {
	Create(ctx context.Context, st *OIDCState, ttl time.Duration) error
	Consume(ctx context.Context, state string) (*OIDCState, error)
	PurgeExpired(ctx context.Context, now time.Time) error
}

// NewMemoryOIDCStateStore returns the in-process single-use state store (the
// default and test seam). Production wiring uses the gorouter_runtime_state
// backed store via newRuntimeStateOIDCStateStore.
func NewMemoryOIDCStateStore() OIDCStateStore {
	return newMemoryOIDCStateStore(time.Now)
}

type memoryOIDCStateStore struct {
	mu  sync.Mutex
	now func() time.Time
	m   map[string]*OIDCState
}

func newMemoryOIDCStateStore(now func() time.Time) *memoryOIDCStateStore {
	return &memoryOIDCStateStore{now: now, m: map[string]*OIDCState{}}
}

func (s *memoryOIDCStateStore) Create(_ context.Context, st *OIDCState, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(s.now())
	cp := *st
	if cp.ExpiresAt.IsZero() {
		cp.ExpiresAt = s.now().Add(ttl)
	}
	s.m[cp.State] = &cp
	return nil
}

func (s *memoryOIDCStateStore) Consume(_ context.Context, state string) (*OIDCState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.m[state]
	if !ok {
		return nil, ErrOIDCStateNotFound
	}
	if st.Used {
		return nil, ErrOIDCStateReplay
	}
	if s.now().After(st.ExpiresAt) {
		delete(s.m, state)
		return nil, ErrOIDCStateExpired
	}
	st.Used = true
	cp := *st
	return &cp, nil
}

func (s *memoryOIDCStateStore) PurgeExpired(_ context.Context, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.purgeLocked(now)
	return nil
}

// purgeLocked drops used and expired entries on a bounded window: only states
// past their TTL (or already consumed) are ever removed. This is the
// transport-level realization of the pending-OAuth purge (#346).
func (s *memoryOIDCStateStore) purgeLocked(now time.Time) {
	for k, v := range s.m {
		if v.Used || now.After(v.ExpiresAt) {
			delete(s.m, k)
		}
	}
}

// runtimeStateOIDCStateStore persists state through gorouter_runtime_state via
// the same structural kvStore seam as the lockout store, honoring the approved
// single-row TTL (combo.RuntimeState Get/Set).
type runtimeStateOIDCStateStore struct {
	kv  kvStore
	now func() time.Time
}

func newRuntimeStateOIDCStateStore(kv kvStore, now func() time.Time) OIDCStateStore {
	return &runtimeStateOIDCStateStore{kv: kv, now: now}
}

func (s *runtimeStateOIDCStateStore) Create(ctx context.Context, st *OIDCState, ttl time.Duration) error {
	raw, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return s.kv.Set(ctx, oidcStateKeyPrefix+st.State, raw, ttl)
}

func (s *runtimeStateOIDCStateStore) Consume(ctx context.Context, state string) (*OIDCState, error) {
	raw, err := s.kv.Get(ctx, oidcStateKeyPrefix+state)
	if err != nil || raw == nil {
		return nil, ErrOIDCStateNotFound
	}
	var st OIDCState
	if err := json.Unmarshal(raw, &st); err != nil {
		return nil, err
	}
	now := s.now()
	if st.Used {
		return nil, ErrOIDCStateReplay
	}
	if now.After(st.ExpiresAt) {
		return nil, ErrOIDCStateExpired
	}
	st.Used = true
	remain := st.ExpiresAt.Sub(now)
	marked, err := json.Marshal(&st)
	if err != nil {
		return nil, err
	}
	if err := s.kv.Set(ctx, oidcStateKeyPrefix+state, marked, remain); err != nil {
		return nil, err
	}
	return &st, nil
}

func (s *runtimeStateOIDCStateStore) PurgeExpired(context.Context, time.Time) error {
	// Expiry is enforced by the gorouter_runtime_state row TTL on Set and by
	// the explicit ExpiresAt check on Consume; Get shadows expired rows. The
	// periodic scheduler cleanup completes the sweep.
	return nil
}

// oidcHTTPClient is the loopback/mocked HTTP seam the handlers fetch discovery,
// JWKS and the token endpoint through.
type oidcHTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

type oidcMetadata struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	JWKSURI               string `json:"jwks_uri"`
}

type oidcJWKS struct {
	Keys []oidcJWK `json:"keys"`
}

type oidcJWK struct {
	Kty string `json:"kty"`
	Kid string `json:"kid"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	N   string `json:"n"`
	E   string `json:"e"`
	Crv string `json:"crv"`
	X   string `json:"x"`
	Y   string `json:"y"`
}

// oidcJWKSCache memoizes the issuer's signing keys. Rotation is honored by
// invalidating on an unknown kid so the next lookup re-fetches.
type oidcJWKSCache struct {
	mu      sync.Mutex
	uri     string
	fetched time.Time
	keys    map[string]oidcJWK
}

func (c *oidcJWKSCache) load(ctx context.Context, client oidcHTTPClient, now func() time.Time, uri string) (map[string]oidcJWK, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.keys != nil && c.uri == uri && now().Sub(c.fetched) < oidcJWKSCacheTTL {
		return c.keys, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, uri, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrOIDCInvalidConfig
	}
	var ks oidcJWKS
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&ks); err != nil {
		return nil, err
	}
	keys := make(map[string]oidcJWK, len(ks.Keys))
	for _, k := range ks.Keys {
		if k.Kid != "" {
			keys[k.Kid] = k
		}
	}
	c.uri = uri
	c.fetched = now()
	c.keys = keys
	return keys, nil
}

func (c *oidcJWKSCache) invalidate() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.keys = nil
	c.uri = ""
}

func (h *Handler) oidcMetadata(ctx context.Context, cfg OIDCConfig) (*oidcMetadata, error) {
	u := cfg.issuer() + oidcDiscoveryPath
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.oidcHTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrOIDCInvalidConfig
	}
	var m oidcMetadata
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&m); err != nil {
		return nil, err
	}
	if m.Issuer == "" || m.AuthorizationEndpoint == "" || m.TokenEndpoint == "" || m.JWKSURI == "" {
		return nil, ErrOIDCInvalidConfig
	}
	if m.Issuer != cfg.issuer() {
		return nil, ErrOIDCInvalidConfig
	}
	return &m, nil
}

// OIDCRoutes registers the OIDC surface (security P1-8). start and callback
// are public; test requires a session and is mounted behind the session-cookie
// group by the admin router (openapi: SessionCookie/PAT, scope config read).
func (h *Handler) OIDCRoutes(r chi.Router) {
	r.Get("/auth/oidc/start", h.OIDCStart)
	r.Get("/auth/oidc/callback", h.OIDCCallback)
	r.Post("/auth/oidc/test", h.OIDCTest)
}

// WithOIDC returns a copy of h extended with the OIDC surface. states and
// httpClient default to the in-memory store and http.DefaultClient when nil.
func (h *Handler) WithOIDC(cfg OIDCConfig, svc OIDCService, states OIDCStateStore, httpClient oidcHTTPClient) *Handler {
	cp := *h
	cp.oidcCfg = cfg
	cp.oidcSvc = svc
	if states == nil {
		states = NewMemoryOIDCStateStore()
	}
	cp.oidcSt = states
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	cp.oidcHTTP = httpClient
	cp.oidcKeys = &oidcJWKSCache{}
	return &cp
}

// OIDCStart begins a sign-in: HTTPS-only issuer, single-use state cookie, and
// an S256 PKCE + nonce authorization redirect.
func (h *Handler) OIDCStart(w http.ResponseWriter, r *http.Request) {
	if h.oidcCfg.validate() != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_oidc_config", "invalid or insecure issuer configuration")
		return
	}
	if h.oidcSt == nil || h.oidcHTTP == nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "oidc not ready")
		return
	}

	ttl := h.oidcCfg.stateTTL()
	pkce, err := engineoauth.GeneratePKCE()
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to generate pkce")
		return
	}
	stateValue, err := engineoauth.GenerateState()
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to generate state")
		return
	}
	nonce, err := generateRandomHex(32)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to generate nonce")
		return
	}

	st := &OIDCState{
		State:        stateValue,
		Nonce:        nonce,
		CodeVerifier: pkce.CodeVerifier,
		ExpiresAt:    h.now().Add(ttl),
	}
	if err := h.oidcSt.Create(r.Context(), st, ttl); err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to persist state")
		return
	}

	meta, err := h.oidcMetadata(r.Context(), h.oidcCfg)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_oidc_config", "invalid or insecure issuer configuration")
		return
	}

	authURL := buildAuthURL(meta.AuthorizationEndpoint, h.oidcCfg, stateValue, nonce, pkce)

	http.SetCookie(w, &http.Cookie{ // #nosec G124 -- Secure configurable for loopback HTTP (P2-10 precedent)
		Name:     oidcStateCookieName,
		Value:    stateValue,
		Path:     "/",
		HttpOnly: true,
		Secure:   h.cfg.Secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(ttl.Seconds()),
		Expires:  h.now().Add(ttl),
	})
	http.Redirect(w, r, authURL, http.StatusFound)
}

// OIDCCallback completes a sign-in: consumes the single-use state, enforces
// PKCE + nonce + aud/iss/exp via JWKS verification, then issues the session
// cookie and redirects to the dashboard. A replayed or nonce-mismatched
// callback is rejected.
func (h *Handler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	if h.oidcCfg.validate() != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_oidc_config", "invalid or insecure issuer configuration")
		return
	}
	if h.oidcSt == nil || h.oidcHTTP == nil || h.oidcSvc == nil || h.oidcKeys == nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "oidc not ready")
		return
	}

	q := r.URL.Query()
	code := q.Get("code")
	stateParam := q.Get("state")
	if code == "" || stateParam == "" {
		h.writeOIDCReject(w)
		return
	}
	cookie, err := r.Cookie(oidcStateCookieName)
	if err != nil || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(stateParam)) != 1 {
		h.writeOIDCReject(w)
		return
	}

	st, err := h.oidcSt.Consume(r.Context(), stateParam)
	if err != nil {
		h.writeOIDCReject(w)
		return
	}

	meta, err := h.oidcMetadata(r.Context(), h.oidcCfg)
	if err != nil {
		h.writeOIDCReject(w)
		return
	}

	token, err := h.exchangeCode(r.Context(), meta.TokenEndpoint, code, st.CodeVerifier)
	if err != nil {
		h.writeOIDCReject(w)
		return
	}

	claims, err := h.verifyIDToken(r.Context(), token.IDToken, meta.JWKSURI, st.Nonce)
	if err != nil {
		h.writeOIDCReject(w)
		return
	}

	_, rawToken, err := h.oidcSvc.LoginWithOIDC(r.Context(), claims.Email)
	if err != nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "failed to create session")
		return
	}

	h.setSessionCookie(w, rawToken)
	clearCookie(w, oidcStateCookieName, h.cfg.Secure)
	http.Redirect(w, r, oidcCallbackRedirect, http.StatusFound)
}

// OIDCTest validates the issuer configuration end to end: HTTPS, reachable
// discovery, and a fetchable JWKS.
func (h *Handler) OIDCTest(w http.ResponseWriter, r *http.Request) {
	if h.oidcCfg.validate() != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_oidc_config", "invalid or insecure issuer configuration")
		return
	}
	if h.oidcHTTP == nil || h.oidcKeys == nil {
		h.writeJSONError(w, http.StatusInternalServerError, "internal_error", "oidc not ready")
		return
	}
	meta, err := h.oidcMetadata(r.Context(), h.oidcCfg)
	if err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_oidc_config", "invalid or insecure issuer configuration")
		return
	}
	if _, err := h.oidcKeys.load(r.Context(), h.oidcHTTP, h.now, meta.JWKSURI); err != nil {
		h.writeJSONError(w, http.StatusBadRequest, "invalid_oidc_config", "invalid or insecure issuer configuration")
		return
	}
	h.writeJSON(w, http.StatusOK, map[string]any{"ok": true, "issuer": h.oidcCfg.Issuer})
}

type oidcTokenResponse struct {
	AccessToken string `json:"access_token"`
	IDToken     string `json:"id_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int64  `json:"expires_in"`
}

func (h *Handler) exchangeCode(ctx context.Context, tokenEndpoint, code, codeVerifier string) (*oidcTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", h.oidcCfg.RedirectURI)
	form.Set("client_id", h.oidcCfg.ClientID)
	form.Set("code_verifier", codeVerifier)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := h.oidcHTTP.Do(req)
	if err != nil {
		return nil, ErrOIDCExchangeFailed
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, ErrOIDCExchangeFailed
	}
	var tr oidcTokenResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tr); err != nil {
		return nil, ErrOIDCExchangeFailed
	}
	if tr.IDToken == "" {
		return nil, ErrOIDCExchangeFailed
	}
	return &tr, nil
}

type oidcJWTHeader struct {
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	Typ string `json:"typ"`
}

type oidcIDTokenClaims struct {
	Iss   string `json:"iss"`
	Sub   string `json:"sub"`
	Aud   any    `json:"aud"`
	Exp   int64  `json:"exp"`
	Iat   int64  `json:"iat"`
	Nonce string `json:"nonce"`
	Email string `json:"email"`
}

func (c *oidcIDTokenClaims) audienceSet() map[string]struct{} {
	switch a := c.Aud.(type) {
	case string:
		return map[string]struct{}{a: {}}
	case []any:
		out := make(map[string]struct{}, len(a))
		for _, v := range a {
			if s, ok := v.(string); ok {
				out[s] = struct{}{}
			}
		}
		return out
	default:
		return nil
	}
}

// verifyIDToken enforces signature (RS256/ES256 only via JWKS, rotation
// honored), then iss / aud / exp / nonce. alg=none and mismatched key types
// are rejected.
func (h *Handler) verifyIDToken(ctx context.Context, raw, jwksURI, expectedNonce string) (*oidcIDTokenClaims, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return nil, ErrOIDCInvalidToken
	}
	headerJSON, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return nil, ErrOIDCInvalidToken
	}
	payloadJSON, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, ErrOIDCInvalidToken
	}
	var header oidcJWTHeader
	if err := json.Unmarshal(headerJSON, &header); err != nil {
		return nil, ErrOIDCInvalidToken
	}
	switch header.Alg {
	case "RS256", "ES256":
	default:
		return nil, ErrOIDCInvalidToken
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return nil, ErrOIDCInvalidToken
	}

	signing := []byte(parts[0] + "." + parts[1])
	keys, err := h.oidcKeys.load(ctx, h.oidcHTTP, h.now, jwksURI)
	if err != nil {
		return nil, ErrOIDCInvalidToken
	}
	key, ok := keys[header.Kid]
	if !ok {
		h.oidcKeys.invalidate()
		keys, err = h.oidcKeys.load(ctx, h.oidcHTTP, h.now, jwksURI)
		if err != nil {
			return nil, ErrOIDCUnknownKey
		}
		key, ok = keys[header.Kid]
		if !ok {
			return nil, ErrOIDCUnknownKey
		}
	}
	if !verifySignature(header.Alg, key, signing, sig) {
		return nil, ErrOIDCInvalidToken
	}

	var claims oidcIDTokenClaims
	if err := json.Unmarshal(payloadJSON, &claims); err != nil {
		return nil, ErrOIDCInvalidToken
	}
	if claims.Iss != h.oidcCfg.issuer() {
		return nil, ErrOIDCInvalidToken
	}
	if !claimsContainsAudience(claims.audienceSet(), h.oidcCfg.audiences()) {
		return nil, ErrOIDCInvalidToken
	}
	if claims.Exp <= 0 || h.now().Add(oidcClockSkew).After(time.Unix(claims.Exp, 0)) {
		return nil, ErrOIDCInvalidToken
	}
	if claims.Sub == "" || claims.Email == "" {
		return nil, ErrOIDCInvalidToken
	}
	if subtle.ConstantTimeCompare([]byte(claims.Nonce), []byte(expectedNonce)) != 1 {
		return nil, ErrOIDCInvalidToken
	}
	return &claims, nil
}

func claimsContainsAudience(got map[string]struct{}, want []string) bool {
	for _, w := range want {
		if _, ok := got[w]; ok {
			return true
		}
	}
	return false
}

func verifySignature(alg string, key oidcJWK, signing, sig []byte) bool {
	switch alg {
	case "RS256":
		if key.Kty != "RSA" || key.N == "" || key.E == "" {
			return false
		}
		n := decodeBig(key.N)
		e := decodeBig(key.E)
		if n == nil || e == nil || !e.IsInt64() {
			return false
		}
		pub := &rsa.PublicKey{N: n, E: int(e.Int64())}
		digest := sha256.Sum256(signing)
		return rsa.VerifyPKCS1v15(pub, crypto.SHA256, digest[:], sig) == nil
	case "ES256":
		if key.Kty != "EC" || key.Crv != "P-256" || key.X == "" || key.Y == "" {
			return false
		}
		x := decodeBig(key.X)
		y := decodeBig(key.Y)
		if x == nil || y == nil || !elliptic.P256().IsOnCurve(x, y) {
			return false
		}
		pub := &ecdsa.PublicKey{Curve: elliptic.P256(), X: x, Y: y}
		digest := sha256.Sum256(signing)
		return ecdsa.VerifyASN1(pub, digest[:], sig)
	default:
		return false
	}
}

func decodeBig(enc string) *big.Int {
	b, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return nil
	}
	return new(big.Int).SetBytes(b)
}

func buildAuthURL(endpoint string, cfg OIDCConfig, state, nonce string, pkce *engineoauth.PKCEParams) string {
	v := url.Values{}
	v.Set("response_type", oidcAuthResponseType)
	v.Set("client_id", cfg.ClientID)
	v.Set("redirect_uri", cfg.RedirectURI)
	v.Set("scope", oidcAuthResponseScope)
	v.Set("state", state)
	v.Set("nonce", nonce)
	v.Set("code_challenge", pkce.CodeChallenge)
	v.Set("code_challenge_method", oidcChallengeMethod)
	if strings.Contains(endpoint, "?") {
		return endpoint + "&" + v.Encode()
	}
	return endpoint + "?" + v.Encode()
}

func generateRandomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func (h *Handler) writeOIDCReject(w http.ResponseWriter) {
	h.writeJSONError(w, http.StatusBadRequest, "oidc_callback_failed", "oidc callback validation failed")
}
