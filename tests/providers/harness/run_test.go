package harness_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/tests/providers/harness"
)

func fixtureManifest(id, authType, envVar string, timeout time.Duration, retries int) *harness.Manifest {
	return &harness.Manifest{
		ProviderID:       id,
		ProviderType:     id,
		AuthType:         authType,
		CredentialEnvVar: envVar,
		Timeout:          timeout,
		Retries:          retries,
		MockFixtures: map[string]string{
			"FormatOpenAIChat": "testdata/fixtures/openai_chat.json",
		},
	}
}

func goldenFixture(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/fixtures/openai_chat.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	return data
}

func jsonServer(t *testing.T, status int, body []byte, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		w.Write(body)
	}))
}

func TestRunProviderMockGoldenPass(t *testing.T) {
	fixture := goldenFixture(t)
	srv := jsonServer(t, http.StatusOK, fixture, 0)
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 1)
	red := harness.NewRedactor([]string{harness.PlaceholderCredential("openai")}, nil)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{
		Mode:     harness.ModeMock,
		BaseURL:  srv.URL,
		BaseDir:  ".",
		Redactor: red,
	})
	if err != nil {
		t.Fatalf("mock golden run must pass: %v", err)
	}
	if res.Status != harness.ResultPass {
		t.Fatalf("status = %s, want pass", res.Status)
	}
	if res.RoundTrips != 1 {
		t.Errorf("round trips = %d, want 1", res.RoundTrips)
	}
	if res.Duration <= 0 {
		t.Errorf("duration must be positive, got %v", res.Duration)
	}
	if err := harness.CompareArtifacts(res.Artifact, fixture); err != nil {
		t.Errorf("artifact must equal fixture: %v", err)
	}
}

func TestRunProviderMockGoldenMismatch(t *testing.T) {
	other := []byte(`{"choices":[{"message":{"content":"different"}}]}`)
	srv := jsonServer(t, http.StatusOK, other, 0)
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 0)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock, BaseURL: srv.URL})
	if err == nil {
		t.Fatal("artifact mismatch must fail the run")
	}
	if res.Status != harness.ResultFail {
		t.Errorf("status = %s, want fail", res.Status)
	}
}

func TestRunProviderMockRequiresBaseURL(t *testing.T) {
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 0)
	if _, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock}); err == nil {
		t.Error("mock mode without BaseURL must error")
	}
}

func TestRunProviderInvalidManifest(t *testing.T) {
	m := &harness.Manifest{ProviderID: "openai", ProviderType: "openai", AuthType: "apikey", Timeout: 0}
	if _, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock}); err == nil {
		t.Error("invalid manifest must error before any request")
	}
}

func TestRunProviderTimeout(t *testing.T) {
	srv := jsonServer(t, http.StatusOK, []byte(`{}`), 2*time.Second)
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 150*time.Millisecond, 0)
	start := time.Now()
	res, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock, BaseURL: srv.URL})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("slow provider must time out")
	}
	if res.Status != harness.ResultFail {
		t.Errorf("status = %s, want fail", res.Status)
	}
	if res.RoundTrips != 1 {
		t.Errorf("round trips = %d, want 1 (timeout is not retried)", res.RoundTrips)
	}
	if elapsed > time.Second {
		t.Errorf("timeout enforcement too slow: %v", elapsed)
	}
}

func TestRunProviderRetrySucceeds(t *testing.T) {
	fixture := goldenFixture(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	}))
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 2)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock, BaseURL: srv.URL})
	if err != nil {
		t.Fatalf("retry must eventually pass: %v", err)
	}
	if res.RoundTrips != 3 {
		t.Errorf("round trips = %d, want 3 (two failures then success)", res.RoundTrips)
	}
}

func TestRunProviderRetryExhausted(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"overloaded"}}`, http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 2)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock, BaseURL: srv.URL})
	if err == nil {
		t.Fatal("exhausted retries must fail")
	}
	if res.RoundTrips != 3 {
		t.Errorf("round trips = %d, want 3", res.RoundTrips)
	}
	if strings.Contains(err.Error(), "overloaded") {
		t.Error("error must not embed upstream body content")
	}
}

func TestRunProviderNoRetryOnAuthError(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
	}))
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 3)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeMock, BaseURL: srv.URL})
	if err == nil {
		t.Fatal("401 must fail the run")
	}
	if res.RoundTrips != 1 {
		t.Errorf("401 must not be retried, round trips = %d", res.RoundTrips)
	}
}

func TestRunProviderProviderIsolation(t *testing.T) {
	credA := harness.PlaceholderCredential("openai")
	credB := harness.PlaceholderCredential("anthropic")
	fixture := goldenFixture(t)
	handlerFor := func(want string) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if got := r.Header.Get("Authorization"); got != "Bearer "+want {
				http.Error(w, `{"error":{"message":"unauthorized"}}`, http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Write(fixture)
		})
	}
	srvA := httptest.NewServer(handlerFor(credA))
	defer srvA.Close()
	srvB := httptest.NewServer(handlerFor(credB))
	defer srvB.Close()

	mA := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 0)
	mB := fixtureManifest("anthropic", "apikey", "GOROUTER_LIVE_ANTHROPIC_KEY", 5*time.Second, 0)
	secrets := []string{credA, credB}
	red := harness.NewRedactor(secrets, nil)

	optA := harness.Options{Mode: harness.ModeMock, BaseURL: srvA.URL, Redactor: red}
	optB := harness.Options{Mode: harness.ModeMock, BaseURL: srvB.URL, Redactor: red}

	resA, errA := harness.RunProvider(context.Background(), mA, optA)
	if errA != nil {
		t.Fatalf("provider A against its own server must pass: %v", errA)
	}
	if _, err := harness.RunProvider(context.Background(), mA, optB); err == nil {
		t.Error("provider A credential must be rejected by provider B's server")
	}
	resB, errB := harness.RunProvider(context.Background(), mB, optB)
	if errB != nil {
		t.Fatalf("provider B run must be unaffected by A's failure: %v", errB)
	}
	for _, s := range secrets {
		if strings.Contains(string(resA.Artifact), s) {
			t.Errorf("artifact of A leaks secret %q", s)
		}
		if strings.Contains(string(resB.Artifact), s) {
			t.Errorf("artifact of B leaks secret %q", s)
		}
	}
}

func TestRunProviderSanitisedArtifact(t *testing.T) {
	cred := harness.PlaceholderCredential("openai")
	body := []byte(`{"choices":[{"message":{"role":"assistant","content":"echo ` + cred + `"},"finish_reason":"stop"}]}`)
	srv := jsonServer(t, http.StatusOK, body, 0)
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 0)
	red := harness.NewRedactor([]string{cred}, nil)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{
		Mode:     harness.ModeLive,
		Creds:    harness.MemSource{"GOROUTER_LIVE_OPENAI_KEY": cred},
		BaseURL:  srv.URL,
		Redactor: red,
	})
	if err != nil {
		t.Fatalf("run must pass: %v", err)
	}
	if strings.Contains(string(res.Artifact), cred) {
		t.Fatalf("artifact must be sanitised, leaked %q", cred)
	}
	if !strings.Contains(string(res.Artifact), harness.RedactedMarker) {
		t.Error("artifact must carry the redaction marker")
	}
}

func TestRunProviderDryRunAlwaysSkips(t *testing.T) {
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 0)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{Mode: harness.ModeDryRun})
	if err != nil {
		t.Fatalf("dry-run must not error: %v", err)
	}
	if res.Status != harness.ResultSkip {
		t.Errorf("dry-run status = %s, want skip", res.Status)
	}
	if res.SkipReason == "" {
		t.Error("dry-run skip must carry an explicit reason")
	}
	if res.RoundTrips != 0 {
		t.Errorf("dry-run must not execute requests, round trips = %d", res.RoundTrips)
	}
}

func TestRunProviderLiveSkipsWhenCredentialUnavailable(t *testing.T) {
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 0)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{
		Mode:  harness.ModeLive,
		Creds: harness.MemSource{},
	})
	if err != nil {
		t.Fatalf("missing credential must skip, not error: %v", err)
	}
	if res.Status != harness.ResultSkip {
		t.Errorf("status = %s, want skip (never false pass)", res.Status)
	}
	if !strings.Contains(res.SkipReason, "GOROUTER_LIVE_OPENAI_KEY") {
		t.Errorf("skip reason must name the credential env var, got %q", res.SkipReason)
	}
	if res.RoundTrips != 0 {
		t.Errorf("live skip must not execute requests, round trips = %d", res.RoundTrips)
	}
}

func TestRunProviderLiveExecutesWhenCredentialPresent(t *testing.T) {
	fixture := []byte(`{"choices":[{"message":{"content":"pong"},"finish_reason":"stop"}]}`)
	srv := jsonServer(t, http.StatusOK, fixture, 0)
	defer srv.Close()
	m := fixtureManifest("openai", "apikey", "GOROUTER_LIVE_OPENAI_KEY", 5*time.Second, 1)
	res, err := harness.RunProvider(context.Background(), m, harness.Options{
		Mode:    harness.ModeLive,
		Creds:   harness.MemSource{"GOROUTER_LIVE_OPENAI_KEY": "lab-credential"},
		BaseURL: srv.URL,
	})
	if err != nil {
		t.Fatalf("live run with credential must pass: %v", err)
	}
	if res.Status != harness.ResultPass {
		t.Errorf("status = %s, want pass", res.Status)
	}
	if strings.Contains(string(res.Artifact), "lab-credential") {
		t.Error("live artifact must be sanitised")
	}
}

func TestRunProviderLiveShapeValidation(t *testing.T) {
	m := fixtureManifest("gemini", "apikey", "GOROUTER_LIVE_GEMINI_KEY", 5*time.Second, 0)
	m.MockFixtures = map[string]string{"FormatGemini": "testdata/fixtures/gemini.json"}
	srv := jsonServer(t, http.StatusOK, []byte(`{"foo":1}`), 0)
	defer srv.Close()
	res, err := harness.RunProvider(context.Background(), m, harness.Options{
		Mode:    harness.ModeLive,
		Creds:   harness.MemSource{"GOROUTER_LIVE_GEMINI_KEY": "lab-credential"},
		BaseURL: srv.URL,
	})
	if err == nil {
		t.Fatal("live artifact without the format envelope must fail shape validation")
	}
	if res.Status != harness.ResultFail {
		t.Errorf("status = %s, want fail", res.Status)
	}
}

func TestAuthHeaders(t *testing.T) {
	cases := []struct {
		name       string
		m          *harness.Manifest
		wantHeader map[string]string
	}{
		{"apikey-bearer", fixtureManifest("openai", "apikey", "", 0, 0), map[string]string{"Authorization": "Bearer sk-abc"}},
		{"none", fixtureManifest("searxng", "none", "", 0, 0), map[string]string{}},
		{"cookie", fixtureManifest("grok-web", "cookie", "", 0, 0), map[string]string{"Cookie": "session=sk-abc"}},
	}
	anthropic := fixtureManifest("anthropic", "apikey", "", 0, 0)
	anthropic.MockFixtures = map[string]string{"FormatAnthropic": "x.json"}
	gemini := fixtureManifest("gemini", "apikey", "", 0, 0)
	gemini.MockFixtures = map[string]string{"FormatGemini": "x.json"}
	cases = append(cases,
		[]struct {
			name       string
			m          *harness.Manifest
			wantHeader map[string]string
		}{
			{"anthropic-x-api-key", anthropic, map[string]string{"X-Api-Key": "sk-abc"}},
			{"gemini-x-goog-api-key", gemini, map[string]string{"X-Goog-Api-Key": "sk-abc"}},
		}...)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := harness.AuthHeaders(c.m, "sk-abc")
			if len(got) != len(c.wantHeader) {
				t.Fatalf("AuthHeaders = %v, want %v", got, c.wantHeader)
			}
			for k, v := range c.wantHeader {
				if got[k] != v {
					t.Errorf("AuthHeaders[%q] = %q, want %q", k, got[k], v)
				}
			}
		})
	}
}

func TestProbeRequest(t *testing.T) {
	formats := []engine.RequestFormat{
		engine.FormatOpenAIChat,
		engine.FormatOpenAICompat,
		engine.FormatCodexResponses,
		engine.FormatAnthropic,
		engine.FormatGemini,
	}
	for _, f := range formats {
		body, err := harness.ProbeRequest(f)
		if err != nil {
			t.Fatalf("ProbeRequest(%s): %v", f, err)
		}
		var doc map[string]any
		if err := json.Unmarshal(body, &doc); err != nil {
			t.Errorf("ProbeRequest(%s) must return JSON: %v", f, err)
		}
		if len(doc) == 0 {
			t.Errorf("ProbeRequest(%s) must return a non-empty body", f)
		}
	}
	if _, err := harness.ProbeRequest(engine.RequestFormat("bogus")); err == nil {
		t.Error("ProbeRequest must reject unknown formats")
	}
}
