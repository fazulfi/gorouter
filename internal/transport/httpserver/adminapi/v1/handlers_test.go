package v1

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/keys"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// fakePAT returns a valid PAT for every token.
func fakePAT(context.Context, string) (*keys.PAT, error) {
	return &keys.PAT{ID: uuid.New(), UserID: uuid.New()}, nil
}

// testRouter mounts the route table exactly as the admin router does
// (authn wrappers + host gate), using the supplied config.
func testRouter(cfg Config) http.Handler {
	h := New(cfg)
	r := chi.NewRouter()
	r.Route("/api/admin/v1", func(admin chi.Router) {
		for _, rt := range RouteTable(h) {
			handler := rt.H
			if rt.HostFeature != "" {
				handler = HostGate(cfg, rt.HostFeature, handler)
			}
			switch rt.Mode {
			case AuthPublic:
				admin.Method(rt.Method, rt.Path, handler)
			case AuthSession:
				admin.Method(rt.Method, rt.Path, WrapSession(cfg, handler))
			case AuthSessionPAT:
				admin.Method(rt.Method, rt.Path, WrapSessionPAT(cfg, handler))
			}
		}
	})
	return r
}

func doJSON(t *testing.T, h http.Handler, method, path string, cookies []*http.Cookie, headers map[string]string, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader(`{}`)
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	return rr
}

func bearer(token string) map[string]string {
	return map[string]string{"Authorization": "Bearer " + token}
}

// TestSessionPATAuth asserts the CSRF/PAT grouping resolution (design §6
// L127): PAT actors bypass CSRF, cookie actors must echo the CSRF token on
// mutations, and missing credentials are rejected.
func TestSessionPATAuth(t *testing.T) {
	h := testRouter(Config{Auth: authFake{}, Resources: Dependencies{ValidatePAT: fakePAT}})

	// PAT mutation with a spoofed CSRF token must succeed (CSRF-free).
	if rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/keys", nil,
		bearer("pat-1"), `{"name":"k"}`); rr.Code != http.StatusServiceUnavailable && rr.Code != http.StatusCreated {
		t.Fatalf("PAT mutation = %d, want 503 (unwired svc) or 201", rr.Code)
	}

	// No credential on a session+pat route → 401.
	if rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/keys", nil, nil, ""); rr.Code != http.StatusUnauthorized {
		t.Fatalf("GET /keys without credential = %d, want 401", rr.Code)
	}

	// Session cookie + missing CSRF on a mutation → 403 (fail-closed).
	sess := &http.Cookie{Name: "gorouter_session", Value: "raw-session"}
	if rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/keys", []*http.Cookie{sess}, nil, `{"name":"k"}`); rr.Code != http.StatusForbidden {
		t.Fatalf("session mutation without CSRF = %d, want 403", rr.Code)
	}

	// Session safe GET sets the CSRF cookie even without a session (CSRF
	// precedes SessionAuth).
	rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/keys", nil, nil, "")
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("safe GET without session = %d, want 401", rr.Code)
	}
	if findCookie(rr.Result().Cookies(), "gorouter_csrf") == nil {
		t.Fatal("CSRF cookie not set on safe GET (CSRF must precede SessionAuth)")
	}
}

// TestHostGate asserts the P0-1 host-operation gate: feature flag closed by
// default, full-access PAT identity, confirm prompt on mutations, audit.
func TestHostGate(t *testing.T) {
	auditor := &recordingAuditor{}
	flags := map[string]bool{"shutdown": true}
	cfg := Config{
		Auth:      authFake{},
		HostFlags: flags,
		Resources: Dependencies{ValidatePAT: fakePAT, Auditor: auditor},
	}
	h := testRouter(cfg)

	// Closed default: a host route without its flag → 403 FEATURE_DISABLED.
	cfgClosed := Config{Auth: authFake{}, Resources: Dependencies{ValidatePAT: fakePAT}}
	hClosed := testRouter(cfgClosed)
	if rr := doJSON(t, hClosed, http.MethodPost, "/api/admin/v1/shutdown", nil, bearer("pat-1"), `{"confirm":true}`); rr.Code != http.StatusForbidden {
		t.Fatalf("host route with closed flag = %d, want 403", rr.Code)
	}

	// Open flag + PAT, missing confirm → 428.
	if rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/shutdown", nil, bearer("pat-1"), `{}`); rr.Code != http.StatusPreconditionRequired {
		t.Fatalf("host mutation without confirm = %d, want 428", rr.Code)
	}
	// Open flag + PAT + confirm → resolves to the handler (503 unwired svc).
	if rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/shutdown", nil, bearer("pat-1"), `{"confirm":true}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("host mutation with confirm = %d, want 503 (unwired svc)", rr.Code)
	}
	if len(auditor.entries) == 0 {
		t.Fatal("host operation was not audited")
	}
	e := auditor.entries[0]
	if e.Action != "host_op:shutdown" || e.Target != "/api/admin/v1/shutdown" {
		t.Fatalf("audit entry = action %q target %q", e.Action, e.Target)
	}
	if e.Actor == nil || e.Actor.Kind != auth.ActorKindPAT {
		t.Fatalf("audit entry actor kind = %v, want pat", e.Actor.Kind)
	}
}

// TestCredentialSafeProjection asserts keys and PATs are shown once: the
// one-time create responses carry the full value; list responses expose only
// prefixes and never hashes or full values.
func TestCredentialSafeProjection(t *testing.T) {
	keySvc := &fakeKeysService{}
	cfg := Config{
		Auth: authFake{},
		Resources: Dependencies{
			ValidatePAT: fakePAT,
			Keys:        keySvc,
		},
	}
	h := testRouter(cfg)
	headers := bearer("pat-1")

	rr := doJSON(t, h, http.MethodPost, "/api/admin/v1/keys", nil, headers, `{"name":"alpha"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create key = %d, want 201", rr.Code)
	}
	var created map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &created)
	if created["key"] != "sk-test-secret" {
		t.Fatalf("one-time create response must carry the full key, got %v", created["key"])
	}

	rr = doJSON(t, h, http.MethodGet, "/api/admin/v1/keys", nil, headers, "")
	if rr.Code != http.StatusOK {
		t.Fatalf("list keys = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	if strings.Contains(body, "sk-test-secret") || strings.Contains(body, "key_hash") || strings.Contains(body, "TokenHash") {
		t.Fatalf("list response leaked a credential or hash: %s", body)
	}
	if !strings.Contains(body, `"key_prefix":"sk-"`) {
		t.Fatalf("list response must carry key_prefix only: %s", body)
	}

	patSvc := &fakePATsService{}
	cfg2 := Config{Auth: authFake{}, Resources: Dependencies{ValidatePAT: fakePAT, PATs: patSvc}}
	h2 := testRouter(cfg2)
	rr = doJSON(t, h2, http.MethodPost, "/api/admin/v1/pats", nil, headers, `{"description":"d"}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create pat = %d, want 201", rr.Code)
	}
	var patCreated map[string]any
	_ = json.Unmarshal(rr.Body.Bytes(), &patCreated)
	if patCreated["token"] != "pat-test-secret" {
		t.Fatalf("one-time create response must carry the full token, got %v", patCreated["token"])
	}
	rr = doJSON(t, h2, http.MethodGet, "/api/admin/v1/pats", nil, headers, "")
	if strings.Contains(rr.Body.String(), "pat-test-secret") || strings.Contains(rr.Body.String(), "TokenHash") {
		t.Fatalf("pat list leaked a credential or hash: %s", rr.Body.String())
	}
}

// TestRouteSweepResolvesNo404 walks every registered route and asserts it
// resolves to a real handler: never 404 (and never 405). Host routes ride
// the closed gate (403); unwired services report 503; the auth surface
// answers from its own handlers.
func TestRouteSweepResolvesNo404(t *testing.T) {
	h := testRouter(Config{
		Auth:      authFake{},
		OIDC:      authFake{},
		Resources: Dependencies{ValidatePAT: fakePAT},
	})
	sess := []*http.Cookie{{Name: "gorouter_session", Value: "raw"}}
	csrf := []*http.Cookie{{Name: "gorouter_session", Value: "raw"}, {Name: "gorouter_csrf", Value: "tok"}}

	for _, rt := range RouteTable(New(Config{Auth: authFake{}, OIDC: authFake{}})) {
		path := "/api/admin/v1" + rt.Path
		var rr *httptest.ResponseRecorder
		headers := map[string]string{}
		switch rt.Mode {
		case AuthPublic:
			rr = doJSON(t, h, rt.Method, path, nil, headers, `{"confirm":true}`)
		case AuthSession:
			cookies := sess
			if rt.Method != http.MethodGet && rt.Method != http.MethodHead {
				cookies = csrf
				headers["X-CSRF-Token"] = "tok"
			}
			rr = doJSON(t, h, rt.Method, path, cookies, headers, `{"confirm":true}`)
		case AuthSessionPAT:
			rr = doJSON(t, h, rt.Method, path, nil, bearer("pat-1"), `{"confirm":true}`)
		}
		if rr.Code == http.StatusNotFound || rr.Code == http.StatusMethodNotAllowed {
			t.Errorf("route %s %s resolved to %d (must never 404)", rt.Method, rt.Path, rr.Code)
		}
	}
}

// TestBackendUnavailableAndDelegation exercises the delegation seams with
// fakes: services receive the actor and return projected responses.
func TestBackendUnavailableAndDelegation(t *testing.T) {
	h := testRouter(Config{Auth: authFake{}, Resources: Dependencies{ValidatePAT: fakePAT}})
	headers := bearer("pat-1")

	// Unwired group → 503, never 404.
	if rr := doJSON(t, h, http.MethodGet, "/api/admin/v1/providers", nil, headers, ""); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("unwired providers list = %d, want 503", rr.Code)
	}

	// Wired jobs service receives the actor on run-now.
	jobsSvc := &fakeJobsService{runNowActor: nil}
	cfg := Config{Auth: authFake{}, Resources: Dependencies{ValidatePAT: fakePAT, Jobs: jobsSvc}}
	hj := testRouter(cfg)
	if rr := doJSON(t, hj, http.MethodPost, "/api/admin/v1/jobs/refresh/run-now", nil, headers, `{}`); rr.Code != http.StatusAccepted {
		t.Fatalf("run-now = %d, want 202", rr.Code)
	}
	if jobsSvc.runNowActor == nil || jobsSvc.runNowActor.Kind != auth.ActorKindPAT {
		t.Fatalf("run-now actor was not resolved and passed: %+v", jobsSvc.runNowActor)
	}
	if jobsSvc.runNowType != "refresh" {
		t.Fatalf("run-now type = %q, want refresh", jobsSvc.runNowType)
	}
}

func findCookie(cs []*http.Cookie, name string) *http.Cookie {
	for _, c := range cs {
		if c.Name == name {
			return c
		}
	}
	return nil
}
