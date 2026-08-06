// Package parity_test hosts the management compatibility parity test
// (TestManagementRoutesEquivalence): for each preserved historical route, the
// compatibility adapter and its Administration API v1 twin must produce
// identical persisted state and response semantics.
package parity_test

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	appproviders "gorouter/internal/app/providers"
	"gorouter/internal/domain/auth"
	domcombo "gorouter/internal/domain/combo"
	domkeys "gorouter/internal/domain/keys"
	domprovider "gorouter/internal/domain/provider"
	domsettings "gorouter/internal/domain/settings"
	"gorouter/internal/transport/httpserver"
	adminapiv1 "gorouter/internal/transport/httpserver/adminapi/v1"
	"gorouter/internal/transport/httpserver/compatibility"

	"github.com/google/uuid"
)

// parityTime is the fixed clock every fake uses so both sides of an
// equivalence comparison marshal identical timestamps.
var parityTime = time.Date(2026, 1, 5, 12, 0, 0, 0, time.UTC)

// detID returns a deterministic UUID for a counter value so two freshly
// created stores produce byte-identical persisted state.
func detID(n uint64) uuid.UUID {
	var u [16]byte
	binary.BigEndian.PutUint64(u[8:], n)
	u[6] = (u[6] & 0x0f) | 0x40
	u[8] = (u[8] & 0x3f) | 0x80
	return uuid.UUID(u)
}

func parityPAT(context.Context, string) (*domkeys.PAT, error) {
	return &domkeys.PAT{ID: detID(7000), UserID: detID(7001)}, nil
}

// parityAuth satisfies the admin auth and OIDC surfaces.
type parityAuth struct{}

func (parityAuth) Login(w http.ResponseWriter, _ *http.Request)        {}
func (parityAuth) Logout(w http.ResponseWriter, _ *http.Request)       {}
func (parityAuth) Me(w http.ResponseWriter, _ *http.Request)           {}
func (parityAuth) Status(w http.ResponseWriter, _ *http.Request)       {}
func (parityAuth) OIDCStart(w http.ResponseWriter, _ *http.Request)    {}
func (parityAuth) OIDCCallback(w http.ResponseWriter, _ *http.Request) {}
func (parityAuth) OIDCTest(w http.ResponseWriter, _ *http.Request)     {}
func (parityAuth) SessionCookieName() string                           { return "gorouter_session" }
func (parityAuth) ValidateSession(context.Context, string) (*auth.Actor, error) {
	return &auth.Actor{Kind: auth.ActorKindSession, Origin: auth.ActorOriginRemote}, nil
}

// parityStore is the in-memory persisted state shared by every fake service
// of one router instance.
type parityStore struct {
	Keys           []domkeys.APIKey
	keyN           uint64
	Providers      []domprovider.Provider
	providerN      uint64
	Combos         []domcombo.Definition
	comboN         uint64
	RequireLogin   bool
	TunnelStatus   string
	RequestDetails []any
}

func newParityStore() *parityStore {
	return &parityStore{
		TunnelStatus:   "up",
		RequestDetails: []any{map[string]any{"id": "r1", "model": "gpt-4o"}},
	}
}

func stateJSON(st *parityStore) string {
	b, _ := json.Marshal(st)
	return string(b)
}

// parityKeys implements KeysService over the shared store.
type parityKeys struct{ store *parityStore }

var _ adminapiv1.KeysService = (*parityKeys)(nil)

func (s *parityKeys) List(_ context.Context, _ uuid.UUID) ([]domkeys.APIKey, error) {
	return append([]domkeys.APIKey(nil), s.store.Keys...), nil
}
func (s *parityKeys) Get(_ context.Context, id uuid.UUID) (*domkeys.APIKey, error) {
	for i := range s.store.Keys {
		if s.store.Keys[i].ID == id {
			k := s.store.Keys[i]
			return &k, nil
		}
	}
	return nil, errors.New("not found")
}
func (s *parityKeys) Create(_ context.Context, _ uuid.UUID, name string, _ *time.Time) (*domkeys.APIKey, string, error) {
	s.store.keyN++
	k := domkeys.APIKey{ID: detID(s.store.keyN), Name: name, KeyPrefix: "sk-", CreatedAt: parityTime}
	s.store.Keys = append(s.store.Keys, k)
	return &k, "sk-" + name, nil
}
func (s *parityKeys) Update(_ context.Context, id uuid.UUID, name string, _ *time.Time) (*domkeys.APIKey, error) {
	for i := range s.store.Keys {
		if s.store.Keys[i].ID == id {
			s.store.Keys[i].Name = name
			k := s.store.Keys[i]
			return &k, nil
		}
	}
	return nil, errors.New("not found")
}
func (s *parityKeys) Revoke(_ context.Context, id uuid.UUID) error {
	for i := range s.store.Keys {
		if s.store.Keys[i].ID == id {
			rv := parityTime
			s.store.Keys[i].RevokedAt = &rv
			return nil
		}
	}
	return errors.New("not found")
}

// parityProviders implements ProvidersService over the shared store.
type parityProviders struct{ store *parityStore }

var _ adminapiv1.ProvidersService = (*parityProviders)(nil)

func (s *parityProviders) List(_ context.Context) ([]domprovider.Provider, error) {
	return append([]domprovider.Provider(nil), s.store.Providers...), nil
}
func (s *parityProviders) Client(_ context.Context) ([]domprovider.Provider, error) {
	return append([]domprovider.Provider(nil), s.store.Providers...), nil
}
func (s *parityProviders) Get(_ context.Context, id uuid.UUID) (*domprovider.Provider, error) {
	for i := range s.store.Providers {
		if s.store.Providers[i].ID == id {
			p := s.store.Providers[i]
			return &p, nil
		}
	}
	return nil, errors.New("not found")
}
func (s *parityProviders) Create(_ context.Context, in *domprovider.Provider) (*domprovider.Provider, error) {
	s.store.providerN++
	in.ID = detID(s.store.providerN)
	s.store.Providers = append(s.store.Providers, *in)
	p := *in
	return &p, nil
}
func (s *parityProviders) Update(_ context.Context, in *domprovider.Provider) (*domprovider.Provider, error) {
	for i := range s.store.Providers {
		if s.store.Providers[i].ID == in.ID {
			s.store.Providers[i] = *in
			p := s.store.Providers[i]
			return &p, nil
		}
	}
	return nil, errors.New("not found")
}
func (s *parityProviders) Delete(_ context.Context, id uuid.UUID) error {
	for i := range s.store.Providers {
		if s.store.Providers[i].ID == id {
			s.store.Providers = append(s.store.Providers[:i], s.store.Providers[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}
func (s *parityProviders) KiloFreeModels(_ context.Context) ([]string, error) {
	return []string{"k1"}, nil
}
func (s *parityProviders) SuggestedModels(_ context.Context) ([]string, error) {
	return []string{"s1"}, nil
}
func (s *parityProviders) TestBatch(_ context.Context, _ []uuid.UUID, _ int) (appproviders.TestBatchResult, error) {
	return appproviders.TestBatchResult{Results: []appproviders.ProviderTestResult{
		{ProviderID: detID(1), Name: "p", OK: true},
	}}, nil
}
func (s *parityProviders) Validate(_ context.Context, _ uuid.UUID) error { return nil }

// parityCombos implements CombosService over the shared store.
type parityCombos struct{ store *parityStore }

var _ adminapiv1.CombosService = (*parityCombos)(nil)

func (s *parityCombos) List(_ context.Context) ([]domcombo.Definition, error) {
	return append([]domcombo.Definition(nil), s.store.Combos...), nil
}
func (s *parityCombos) Get(_ context.Context, id uuid.UUID) (*domcombo.Definition, error) {
	for i := range s.store.Combos {
		if s.store.Combos[i].ID == id {
			c := s.store.Combos[i]
			return &c, nil
		}
	}
	return nil, errors.New("not found")
}
func (s *parityCombos) Create(_ context.Context, _ *auth.Actor, name string, strategy domcombo.Strategy, _ json.RawMessage, _ []domcombo.Member) (*domcombo.Definition, error) {
	s.store.comboN++
	c := domcombo.Definition{ID: detID(s.store.comboN), Name: name, Strategy: strategy, IsActive: true}
	s.store.Combos = append(s.store.Combos, c)
	return &c, nil
}
func (s *parityCombos) Update(_ context.Context, _ *auth.Actor, def *domcombo.Definition) error {
	for i := range s.store.Combos {
		if s.store.Combos[i].ID == def.ID {
			s.store.Combos[i] = *def
			return nil
		}
	}
	return errors.New("not found")
}
func (s *parityCombos) Delete(_ context.Context, _ *auth.Actor, id uuid.UUID) error {
	for i := range s.store.Combos {
		if s.store.Combos[i].ID == id {
			s.store.Combos = append(s.store.Combos[:i], s.store.Combos[i+1:]...)
			return nil
		}
	}
	return errors.New("not found")
}

// paritySettings implements SettingsService over the shared store.
type paritySettings struct{ store *parityStore }

var _ adminapiv1.SettingsService = (*paritySettings)(nil)

func (s *paritySettings) Get(_ context.Context, _ *auth.Actor, key string) (*domsettings.Setting, error) {
	return &domsettings.Setting{Key: key, Value: json.RawMessage(`"v"`)}, nil
}
func (s *paritySettings) List(_ context.Context, _ *auth.Actor) ([]domsettings.Setting, error) {
	return []domsettings.Setting{{Key: "k", Value: json.RawMessage(`"v"`)}}, nil
}
func (s *paritySettings) Update(_ context.Context, _ *auth.Actor, _ map[string]json.RawMessage) error {
	return nil
}
func (s *paritySettings) ProxyTest(_ context.Context, _ *auth.Actor, _ any) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (s *paritySettings) RequireLogin(_ context.Context, _ *auth.Actor) (*domsettings.Setting, error) {
	return &domsettings.Setting{Key: "require_login", Value: json.RawMessage(fmt.Sprintf("%t", s.store.RequireLogin))}, nil
}
func (s *paritySettings) SetRequireLogin(_ context.Context, _ *auth.Actor, enabled bool) error {
	s.store.RequireLogin = enabled
	return nil
}

// parityTunnels implements TunnelsService over the shared store.
type parityTunnels struct{ store *parityStore }

var _ adminapiv1.TunnelsService = (*parityTunnels)(nil)

func (s *parityTunnels) Get(_ context.Context) (any, error) {
	return map[string]any{"tunnel": s.store.TunnelStatus}, nil
}
func (s *parityTunnels) Status(_ context.Context) (any, error) {
	return map[string]any{"status": s.store.TunnelStatus}, nil
}
func (s *parityTunnels) TailscaleCheck(_ context.Context) (any, error) {
	return map[string]any{"available": true}, nil
}
func (s *parityTunnels) Enable(_ context.Context) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (s *parityTunnels) Disable(_ context.Context) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (s *parityTunnels) TailscaleInstall(_ context.Context) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (s *parityTunnels) TailscaleEnable(_ context.Context) (any, error) {
	return map[string]any{"ok": true}, nil
}
func (s *parityTunnels) TailscaleDisable(_ context.Context) (any, error) {
	return map[string]any{"ok": true}, nil
}

// parityUsage implements UsageService over the shared store.
type parityUsage struct{ store *parityStore }

var _ adminapiv1.UsageService = (*parityUsage)(nil)

func (s *parityUsage) Stats(_ context.Context) (any, error) { return map[string]any{"total": 1}, nil }
func (s *parityUsage) History(_ context.Context) (any, error) {
	return []any{map[string]any{"day": "2026-01-05"}}, nil
}
func (s *parityUsage) Chart(_ context.Context) (any, error) {
	return []any{map[string]any{"day": "2026-01-05"}}, nil
}
func (s *parityUsage) Providers(_ context.Context) (any, error) {
	return []any{map[string]any{"provider": "openai"}}, nil
}
func (s *parityUsage) RequestDetails(_ context.Context) (any, error) {
	return s.store.RequestDetails, nil
}
func (s *parityUsage) RequestLogs(_ context.Context) (any, error) {
	return []any{map[string]any{"id": "r1"}}, nil
}
func (s *parityUsage) Logs(_ context.Context) (any, error) {
	return []any{map[string]any{"id": "r1"}}, nil
}
func (s *parityUsage) Connection(_ context.Context, connectionID string) (any, error) {
	return map[string]any{"connectionId": connectionID}, nil
}

func newParityDeps(store *parityStore) *adminapiv1.Dependencies {
	return &adminapiv1.Dependencies{
		Keys:        &parityKeys{store: store},
		Providers:   &parityProviders{store: store},
		Combos:      &parityCombos{store: store},
		Settings:    &paritySettings{store: store},
		Tunnels:     &parityTunnels{store: store},
		Usage:       &parityUsage{store: store},
		ValidatePAT: parityPAT,
	}
}

func newParityRouter(t *testing.T, store *parityStore) http.Handler {
	t.Helper()
	return httpserver.NewAdminRouter(httpserver.AdminConfig{
		Auth:      parityAuth{},
		OIDC:      parityAuth{},
		Resources: newParityDeps(store),
	})
}

func newSweepRouter(t *testing.T) http.Handler {
	t.Helper()
	return httpserver.NewAdminRouter(httpserver.AdminConfig{
		Auth: parityAuth{},
		OIDC: parityAuth{},
	})
}

type parityResult struct {
	code int
	body string
	ct   string
}

func parityCall(h http.Handler, method, path, body string, headers map[string]string, cookies []*http.Cookie) parityResult {
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader(`{}`)
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	for _, c := range cookies {
		req.AddCookie(c)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return parityResult{code: rr.Code, body: rr.Body.String(), ct: rr.Header().Get("Content-Type")}
}

func parityBearer() map[string]string {
	return map[string]string{"Authorization": "Bearer pat-1"}
}

// assertEquivalence runs one request against the twin on store A and the same
// request against the compatibility adapter on store B, then asserts the
// persisted state and the response semantics are identical.
func assertEquivalence(t *testing.T, name, method, adminPath, compatPath, body string) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		storeA := newParityStore()
		routerA := newParityRouter(t, storeA)
		storeB := newParityStore()
		routerB := newParityRouter(t, storeB)

		adminRes := parityCall(routerA, method, adminPath, body, parityBearer(), nil)
		compatRes := parityCall(routerB, method, compatPath, body, parityBearer(), nil)

		if compatRes.code != adminRes.code {
			t.Fatalf("status = %d, twin status = %d (body %s)", compatRes.code, adminRes.code, compatRes.body)
		}
		if compatRes.body != adminRes.body {
			t.Fatalf("response body diverged:\ncompat: %s\ntwin:   %s", compatRes.body, adminRes.body)
		}
		if got, want := stateJSON(storeB), stateJSON(storeA); got != want {
			t.Fatalf("persisted state diverged:\ncompat: %s\ntwin:   %s", got, want)
		}
	})
}

// parityExpandPath substitutes the chi parameters a sweep request needs.
func parityExpandPath(path string) string {
	repl := strings.NewReplacer(
		"{id}", "00000000-0000-0000-0000-000000000042",
		"{connectionId}", "c1",
		"{tool}", "kilo-settings",
		"{provider}", "anthropic",
		"{plugin}", "mp",
		"{path}", "probe",
	)
	return repl.Replace(path)
}

// TestManagementRoutesEquivalence is the management compatibility parity
// gate. It asserts (1) the complete frozen historical management surface is
// registered as thin adapters (137 rows; every compat route and its twin
// resolve, never 404), and (2) representative routes produce identical
// persisted state and response semantics through the adapter and the twin.
func TestManagementRoutesEquivalence(t *testing.T) {
	table := compatibility.RouteTable(adminapiv1.New(adminapiv1.Config{Auth: parityAuth{}, OIDC: parityAuth{}}))
	if len(table) != 137 {
		t.Fatalf("compatibility surface = %d adapters, want 137 (frozen management rows)", len(table))
	}

	sweep := newSweepRouter(t)
	sess := []*http.Cookie{{Name: "gorouter_session", Value: "raw"}}
	csrf := []*http.Cookie{{Name: "gorouter_session", Value: "raw"}, {Name: "gorouter_csrf", Value: "tok"}}
	for _, rt := range table {
		compatPath := parityExpandPath(rt.Path)
		adminPath := "/api/admin/v1" + strings.TrimPrefix(rt.Path, compatibility.MountPrefix)
		adminPath = parityExpandPath(adminPath)
		var headers map[string]string
		var cookies []*http.Cookie
		switch rt.Mode {
		case adminapiv1.AuthPublic:
		case adminapiv1.AuthSession:
			cookies = sess
			if rt.Method != http.MethodGet && rt.Method != http.MethodHead {
				cookies = csrf
				headers = map[string]string{"X-CSRF-Token": "tok"}
			}
		case adminapiv1.AuthSessionPAT:
			headers = parityBearer()
		}
		for label, p := range map[string]string{"compat": compatPath, "twin": adminPath} {
			rr := parityCall(sweep, rt.Method, p, `{"confirm":true}`, headers, cookies)
			if rr.code == http.StatusNotFound || rr.code == http.StatusMethodNotAllowed {
				t.Errorf("%s route %s %s resolved to %d (must never 404)", label, rt.Method, p, rr.code)
			}
		}
	}

	cases := []struct {
		name       string
		method     string
		adminPath  string
		compatPath string
		body       string
	}{
		{"keys create", http.MethodPost, "/api/admin/v1/keys", "/api/compat/keys", `{"name":"alpha"}`},
		{"keys list", http.MethodGet, "/api/admin/v1/keys", "/api/compat/keys", ""},
		{"combo create", http.MethodPost, "/api/admin/v1/combos", "/api/compat/combos", `{"name":"c","strategy":"roundrobin"}`},
		{"combo list", http.MethodGet, "/api/admin/v1/combos", "/api/compat/combos", ""},
		{"provider create", http.MethodPost, "/api/admin/v1/providers", "/api/compat/providers", `{"name":"p","type":"openai"}`},
		{"provider list", http.MethodGet, "/api/admin/v1/providers", "/api/compat/providers", ""},
		{"settings require-login read", http.MethodGet, "/api/admin/v1/settings/require-login", "/api/compat/settings/require-login", ""},
		{"settings require-login write", http.MethodPut, "/api/admin/v1/settings/require-login", "/api/compat/settings/require-login", `{"value":true}`},
		{"tunnel status", http.MethodGet, "/api/admin/v1/tunnel/status", "/api/compat/tunnel/status", ""},
		{"usage request-details", http.MethodGet, "/api/admin/v1/usage/request-details", "/api/compat/usage/request-details", ""},
	}
	for _, c := range cases {
		assertEquivalence(t, c.name, c.method, c.adminPath, c.compatPath, c.body)
	}
}
