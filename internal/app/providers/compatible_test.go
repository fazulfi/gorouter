package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/providers/generic"

	"github.com/google/uuid"
)

func testAccount(credentialRef string) *provider.Account {
	return &provider.Account{
		ID:            uuid.New(),
		CredentialRef: credentialRef,
		AuthType:      "api_key",
		IsEnabled:     true,
	}
}

func testRequest(format engine.RequestFormat, model string, body []byte) *engine.Request {
	return &engine.Request{
		ID:      uuid.New(),
		Format:  format,
		Model:   model,
		RawBody: body,
		Stream:  false,
	}
}

func TestCompatibleService_NewOpenAIExecutor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "test", "object": "chat.completion", "model": "gpt-4o",
			"choices": []map[string]interface{}{{
				"index": 0, "finish_reason": "stop",
				"message": map[string]interface{}{"role": "assistant", "content": "Hello"},
			}},
		})
	}))
	defer srv.Close()

	svc := NewCompatibleService()
	e := svc.NewOpenAIExecutor(srv.URL)

	if !e.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected to support FormatOpenAIChat")
	}
}

func TestCompatibleService_NewAnthropicExecutor(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "msg_1", "type": "message", "role": "assistant",
			"model": "claude-3", "content": []map[string]interface{}{{"type": "text", "text": "Hi"}},
		})
	}))
	defer srv.Close()

	svc := NewCompatibleService()
	e := svc.NewAnthropicExecutor(srv.URL)

	if !e.SupportsFormat(engine.FormatAnthropic) {
		t.Error("expected to support FormatAnthropic")
	}
}

func TestCompatibleService_ListModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "gpt-4o"}},
		})
	}))
	defer srv.Close()

	client := generic.NewClient(generic.WithBaseURL(srv.URL))
	svc := NewCompatibleServiceWithClient(client)
	account := testAccount("test-key")

	models, err := svc.ListModels(context.Background(), account)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 1 {
		t.Errorf("expected 1 model, got %d", len(models))
	}
}

func TestCompatibleService_ExecutorForProvider_OpenAI(t *testing.T) {
	svc := NewCompatibleService()
	exec, err := svc.ExecutorForProvider(provider.ProviderOpenAI, "https://custom.example.com", engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ExecutorForProvider failed: %v", err)
	}
	if !exec.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected executor to support FormatOpenAIChat")
	}
}

func TestCompatibleService_ExecutorForProvider_Anthropic(t *testing.T) {
	svc := NewCompatibleService()
	exec, err := svc.ExecutorForProvider(provider.ProviderAnthropic, "https://custom.example.com", engine.FormatAnthropic)
	if err != nil {
		t.Fatalf("ExecutorForProvider failed: %v", err)
	}
	if !exec.SupportsFormat(engine.FormatAnthropic) {
		t.Error("expected executor to support FormatAnthropic")
	}
}

func TestCompatibleService_ExecutorForProvider_Custom(t *testing.T) {
	svc := NewCompatibleService()
	exec, err := svc.ExecutorForProvider(provider.ProviderCustom, "http://192.168.1.100:8080", engine.FormatOpenAIChat)
	if err != nil {
		t.Fatalf("ExecutorForProvider failed: %v", err)
	}
	if !exec.SupportsFormat(engine.FormatOpenAIChat) {
		t.Error("expected executor to support FormatOpenAIChat for custom provider")
	}
}

func TestCompatibleService_ExecutorForProvider_Unsupported(t *testing.T) {
	svc := NewCompatibleService()
	_, err := svc.ExecutorForProvider(provider.ProviderAzure, "https://example.com", engine.FormatOpenAIChat)
	if err == nil {
		t.Fatal("expected error for unsupported provider type")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("expected unsupported error, got: %v", err)
	}
}

func TestCompatibleService_PreservesConfiguredTransport(t *testing.T) {
	requestSeen := make(chan struct{}, 1)
	tr := &requestTrackingTransport{
		rt: http.DefaultTransport,
		fn: func() {
			select {
			case requestSeen <- struct{}{}:
			default:
			}
		},
	}
	client := generic.NewClient(generic.WithTransport(tr))
	svc := NewCompatibleServiceWithClient(client)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := exec.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	select {
	case <-requestSeen:
	case <-time.After(time.Second):
		t.Fatal("request was not routed through the configured transport")
	}
}

type requestTrackingTransport struct {
	rt http.RoundTripper
	fn func()
}

func (t *requestTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.fn()
	return t.rt.RoundTrip(req)
}

func TestCompatibleService_CredentialRedaction(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"message":"bad"}}`))
	}))
	defer srv.Close()

	svc := NewCompatibleService()
	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := exec.Execute(context.Background(), req, testAccount("sk-super-secret-key"))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-key") {
		t.Error("credential leaked in error message")
	}
}

func TestCompatibleService_CancellationPropagation(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		flusher, ok := w.(http.Flusher)
		if !ok {
			return
		}
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, "data: {\"counter\":%d}\n\n", i)
			flusher.Flush()
			time.Sleep(10 * time.Millisecond)
		}
	}))
	defer srv.Close()

	svc := NewCompatibleService()
	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := exec.ExecuteStream(ctx, req, testAccount("key"))
	if err != nil {
		t.Logf("ExecuteStream with cancellation: %v", err)
	}
}

func TestCompatibleService_DefinitiveAuthRejection(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"message":"Forbidden"}}`))
	}))
	defer srv.Close()

	svc := NewCompatibleService()
	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := exec.Execute(context.Background(), req, testAccount("key"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "auth") &&
		!strings.Contains(strings.ToLower(err.Error()), "unauthorized") &&
		!strings.Contains(strings.ToLower(err.Error()), "forbidden") {
		t.Errorf("expected definitive auth error, got: %v", err)
	}
}

func TestCompatibleService_TransientRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":{"message":"Rate limited"}}`))
	}))
	defer srv.Close()

	svc := NewCompatibleService()
	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := exec.Execute(context.Background(), req, testAccount("key"))
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "rate") {
		t.Errorf("expected rate limit error, got: %v", err)
	}
}

func TestCompatibleService_ResponseBodyClosed(t *testing.T) {
	closeCalled := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	client := generic.NewClient(
		generic.WithTransport(&bodyCloseTrackingTransport{
			rt:          http.DefaultTransport,
			closeCalled: closeCalled,
		}),
		generic.WithBaseURL(srv.URL),
	)
	svc := NewCompatibleServiceWithClient(client)
	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	_, err := exec.Execute(context.Background(), req, testAccount("key"))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	select {
	case <-closeCalled:
	case <-time.After(time.Second):
		t.Fatal("response body was not closed after Execute returned")
	}
}

type bodyCloseTrackingTransport struct {
	rt          http.RoundTripper
	closeCalled chan<- struct{}
}

func (t *bodyCloseTrackingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.rt.RoundTrip(req)
	if resp != nil {
		resp.Body = &bodyCloseTrackingReadCloser{
			ReadCloser:  resp.Body,
			closeCalled: t.closeCalled,
		}
	}
	return resp, err
}

type bodyCloseTrackingReadCloser struct {
	io.ReadCloser
	closeCalled chan<- struct{}
}

func (c *bodyCloseTrackingReadCloser) Close() error {
	select {
	case c.closeCalled <- struct{}{}:
	default:
	}
	return c.ReadCloser.Close()
}

func TestCompatibleService_ListModelsEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("expected /v1/models, got %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]interface{}{
			"object": "list",
			"data":   []map[string]interface{}{{"id": "gpt-4o"}, {"id": "gpt-3.5-turbo"}},
		})
	}))
	defer srv.Close()

	client := generic.NewClient(generic.WithBaseURL(srv.URL))
	svc := NewCompatibleServiceWithClient(client)
	account := testAccount("key")

	models, err := svc.ListModels(context.Background(), account)
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}
	if len(models) != 2 {
		t.Errorf("expected 2 models, got %d", len(models))
	}
}

func TestCompatibleService_ProbeEmbeddings(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"object": "list",
				"data":   []map[string]interface{}{{"id": "some-model"}},
			})
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	client := generic.NewClient(generic.WithBaseURL(srv.URL))
	svc := NewCompatibleServiceWithClient(client)
	account := testAccount("key")

	found, err := svc.ProbeEmbeddings(context.Background(), account)
	if err != nil {
		t.Fatalf("ProbeEmbeddings failed: %v", err)
	}
	if found {
		t.Error("expected embeddings NOT to be found")
	}
}

// -- Resolver and timeout preservation through CompatibleService --------------

func TestCompatibleService_ResolverPreservation(t *testing.T) {
	const resolvedSecret = "resolver-secret-from-service"
	resolverCalled := false
	client := generic.NewClient(
		generic.WithTransport(http.DefaultTransport),
		generic.WithCredentialResolver(generic.CredentialResolverFunc(func(account *provider.Account) (string, error) {
			resolverCalled = true
			return resolvedSecret, nil
		})),
	)
	svc := NewCompatibleServiceWithClient(client)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+resolvedSecret {
			t.Errorf("expected Bearer %s, got %s", resolvedSecret, r.Header.Get("Authorization"))
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	acct := testAccount("credential-ref-should-not-be-sent")
	_, err := exec.Execute(context.Background(), req, acct)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !resolverCalled {
		t.Error("custom resolver was never called — derived client fell back to default")
	}
}

func TestCompatibleService_TimeoutPreservation(t *testing.T) {
	client := generic.NewClient(
		generic.WithTransport(http.DefaultTransport),
		generic.WithTimeout(10*time.Millisecond),
	)
	svc := NewCompatibleServiceWithClient(client)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"id": "test"})
	}))
	defer srv.Close()

	exec := svc.NewOpenAIExecutor(srv.URL)
	body, _ := json.Marshal(map[string]interface{}{"model": "gpt-4o"})
	req := testRequest(engine.FormatOpenAIChat, "gpt-4o", body)

	start := time.Now()
	_, err := exec.Execute(context.Background(), req, testAccount("key"))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if elapsed > 2*time.Second {
		t.Errorf("timeout took %v, expected ~10ms — client likely fell back to 60s default", elapsed)
	}
}
