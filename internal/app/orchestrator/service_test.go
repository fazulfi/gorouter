package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"

	"gorouter/internal/app/retry"
	"gorouter/internal/app/translate"
	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/engine/stream"
	"gorouter/internal/domain/modelref"
	"gorouter/internal/domain/provider"
)

// ---------------------------------------------------------------------------
// Mock implementations
// ---------------------------------------------------------------------------

type mockResolver struct {
	resolveFn func(ctx context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error)
}

func (m *mockResolver) Resolve(ctx context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
	return m.resolveFn(ctx, ref)
}

type mockAccountSelector struct {
	selectFn func(ctx context.Context, providerID uuid.UUID, model string) (*provider.Account, []provider.Account, error)
}

func (m *mockAccountSelector) SelectAccount(ctx context.Context, providerID uuid.UUID, model string) (*provider.Account, []provider.Account, error) {
	return m.selectFn(ctx, providerID, model)
}

type mockAccountRepo struct {
	findByIDFn func(ctx context.Context, id uuid.UUID) (*provider.Account, error)
}

func (m *mockAccountRepo) FindByID(ctx context.Context, id uuid.UUID) (*provider.Account, error) {
	return m.findByIDFn(ctx, id)
}

func (m *mockAccountRepo) FindByProviderID(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
	return nil, nil
}

func (m *mockAccountRepo) Create(ctx context.Context, account *provider.Account) error {
	return nil
}

func (m *mockAccountRepo) Update(ctx context.Context, account *provider.Account) error {
	return nil
}

func (m *mockAccountRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return nil
}

type mockExecutor struct {
	executeFn       func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error)
	executeStreamFn func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error)
	providerType    provider.ProviderType
}

func (m *mockExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return m.executeFn(ctx, req, account)
}

func (m *mockExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return m.executeStreamFn(ctx, req, account)
}

func (m *mockExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return true
}

func (m *mockExecutor) ProviderType() provider.ProviderType {
	return m.providerType
}

type mockCooldown struct {
	mu            sync.Mutex
	onCooldown    map[uuid.UUID]bool
	failCount     map[uuid.UUID]int
	successCount  map[uuid.UUID]int
}

func newMockCooldown() *mockCooldown {
	return &mockCooldown{
		onCooldown:   make(map[uuid.UUID]bool),
		failCount:    make(map[uuid.UUID]int),
		successCount: make(map[uuid.UUID]int),
	}
}

func (m *mockCooldown) IsOnCooldown(_ context.Context, accountID uuid.UUID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onCooldown[accountID]
}

func (m *mockCooldown) RecordFailure(_ context.Context, accountID uuid.UUID, _ error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.failCount[accountID]++
	m.onCooldown[accountID] = true
}

func (m *mockCooldown) RecordSuccess(_ context.Context, accountID uuid.UUID) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.successCount[accountID]++
	m.onCooldown[accountID] = false
}

// noopLogger returns a logger that discards all output.
func noopLogger() zerolog.Logger {
	return zerolog.New(nil).Level(zerolog.Disabled)
}

// ---------------------------------------------------------------------------
// Test fixtures
// ---------------------------------------------------------------------------

func testAccount(idOffset byte) *provider.Account {
	return &provider.Account{
		ID: uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-%012d", idOffset)),
		ProviderID: uuid.MustParse("10000000-0000-0000-0000-000000000000"),
		Label:      "test",
		AuthType:   "api_key",
		Priority:   0,
		IsEnabled:  true,
	}
}

func testProvider() *provider.Provider {
	return &provider.Provider{
		ID:        uuid.MustParse("10000000-0000-0000-0000-000000000000"),
		Name:      "test-provider",
		Type:      provider.ProviderOpenAI,
		IsEnabled: true,
	}
}

func testRequest() *engine.Request {
	return &engine.Request{
		ID:     uuid.New(),
		Model:  "gpt-4",
		RawBody: []byte(`{"model":"gpt-4","messages":[{"role":"user","content":"hi"}]}`),
		Stream: false,
	}
}

func testOrchestrator() *Orchestrator {
	execFact := &mockExecutorFactory{
		getFn: func(_ provider.ProviderType, _ engine.RequestFormat) (engine.Executor, bool) {
			return &mockExecutor{
				providerType: provider.ProviderOpenAI,
				executeFn: func(_ context.Context, _ *engine.Request, _ *provider.Account) (*engine.Response, error) {
					return &engine.Response{
						RequestID:  uuid.New(),
						Body:       []byte(`ok`),
						Model:      "test",
						StatusCode: 200,
					}, nil
				},
			}, true
		},
	}
	svc := translate.NewService()

	logger := noopLogger()

	retryCfg := retry.Config{
		MaxAttempts: 1,
		BaseDelay:   1 * time.Millisecond,
		MaxDelay:    5 * time.Millisecond,
	}
	return New(DefaultConfig(), nil, svc, execFact, nil, nil, nil, retryCfg, nil, logger)
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestExecuteRequest_Success(t *testing.T) {
	acc := testAccount(1)
	prov := testProvider()

	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountSel := &mockAccountSelector{
		selectFn: func(_ context.Context, _ uuid.UUID, _ string) (*provider.Account, []provider.Account, error) {
			return acc, []provider.Account{*acc}, nil
		},
	}

	execFact := &mockExecutorFactory{
		getFn: func(_ provider.ProviderType, _ engine.RequestFormat) (engine.Executor, bool) {
			return &mockExecutor{
				providerType: provider.ProviderOpenAI,
				executeFn: func(_ context.Context, _ *engine.Request, _ *provider.Account) (*engine.Response, error) {
					return &engine.Response{
						RequestID:  uuid.New(),
						Body:       []byte(`{"choices":[{"message":{"content":"hello"}}]}`),
						Model:      "gpt-4",
						StatusCode: 200,
					}, nil
				},
			}, true
		},
	}

	cd := newMockCooldown()

	o := New(
		DefaultConfig(),
		resolver,
		translate.NewService(),
		execFact,
		accountSel,
		&mockAccountRepo{
			findByIDFn: func(_ context.Context, _ uuid.UUID) (*provider.Account, error) {
				return nil, errors.New("should not be called")
			},
		},
		cd,
		retry.DefaultConfig(),
		nil,
		noopLogger(),
	)

	req := testRequest()
	resp, err := o.ExecuteRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestExecuteRequest_ModelResolutionFails(t *testing.T) {
	resolver := &mockResolver{
		resolveFn: func(_ context.Context, _ modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return nil, modelref.ErrProviderNotFound
		},
	}

	o := testOrchestrator()
	o.resolver = resolver
	o.accountRepo = &mockAccountRepo{}
	o.accountSel = &mockAccountSelector{}
	o.cooldown = newMockCooldown()

	req := testRequest()
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrModelResolutionFailed) {
		t.Fatalf("expected ErrModelResolutionFailed, got: %v", err)
	}
}

func TestExecuteRequest_FormatDetectionFails(t *testing.T) {
	prov := testProvider()
	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	o := testOrchestrator()
	o.resolver = resolver
	o.accountRepo = &mockAccountRepo{}
	o.accountSel = &mockAccountSelector{}
	o.cooldown = newMockCooldown()

	req := testRequest()
	req.RawBody = []byte(`{"unknown":"data"}`)
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrFormatDetectionFailed) && !errors.Is(err, ErrTranslationFailed) {
		t.Fatalf("expected format/translation error, got: %v", err)
	}
}

func TestExecuteRequest_AccountSelectionFails(t *testing.T) {
	prov := testProvider()
	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountSel := &mockAccountSelector{
		selectFn: func(_ context.Context, _ uuid.UUID, _ string) (*provider.Account, []provider.Account, error) {
			return nil, nil, errors.New("no accounts available")
		},
	}

	o := testOrchestrator()
	o.resolver = resolver
	o.accountSel = accountSel
	o.accountRepo = &mockAccountRepo{}
	o.cooldown = newMockCooldown()

	req := testRequest()
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAccountSelection) {
		t.Fatalf("expected ErrAccountSelection, got: %v", err)
	}
}

func TestExecuteRequest_ExplicitAccount(t *testing.T) {
	acc := testAccount(1)
	accID := acc.ID
	prov := testProvider()

	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				AccountID:  &accID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountRepo := &mockAccountRepo{
		findByIDFn: func(_ context.Context, id uuid.UUID) (*provider.Account, error) {
			if id == accID {
				return acc, nil
			}
			return nil, fmt.Errorf("account %s not found", id.String())
		},
	}

	execFact := &mockExecutorFactory{
		getFn: func(_ provider.ProviderType, _ engine.RequestFormat) (engine.Executor, bool) {
			return &mockExecutor{
				providerType: provider.ProviderOpenAI,
				executeFn: func(_ context.Context, _ *engine.Request, _ *provider.Account) (*engine.Response, error) {
					return &engine.Response{
						RequestID:  uuid.New(),
						Body:       []byte(`ok`),
						Model:      "gpt-4",
						StatusCode: 200,
					}, nil
				},
			}, true
		},
	}

	o := New(
		DefaultConfig(),
		resolver,
		translate.NewService(),
		execFact,
		&mockAccountSelector{
			selectFn: func(_ context.Context, _ uuid.UUID, _ string) (*provider.Account, []provider.Account, error) {
				return nil, nil, errors.New("should not be called")
			},
		},
		accountRepo,
		newMockCooldown(),
		retry.DefaultConfig(),
		nil,
		noopLogger(),
	)

	req := testRequest()
	resp, err := o.ExecuteRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestExecuteRequest_ExplicitAccountNotFound(t *testing.T) {
	accID := uuid.New()
	prov := testProvider()

	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				AccountID:  &accID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountRepo := &mockAccountRepo{
		findByIDFn: func(_ context.Context, _ uuid.UUID) (*provider.Account, error) {
			return nil, nil
		},
	}

	o := testOrchestrator()
	o.resolver = resolver
	o.accountRepo = accountRepo
	o.cooldown = newMockCooldown()

	req := testRequest()
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAccountSelection) {
		t.Fatalf("expected ErrAccountSelection, got: %v", err)
	}
}

func TestExecuteRequest_Streaming(t *testing.T) {
	acc := testAccount(1)
	prov := testProvider()

	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountSel := &mockAccountSelector{
		selectFn: func(_ context.Context, _ uuid.UUID, _ string) (*provider.Account, []provider.Account, error) {
			return acc, []provider.Account{*acc}, nil
		},
	}

	// Build an executor factory, then replace the OpenAI chat executor with
	// a mock that returns a stream response.
	mockExec := &mockExecutor{
		providerType: provider.ProviderOpenAI,
		executeStreamFn: func(_ context.Context, _ *engine.Request, _ *provider.Account) (*engine.Response, error) {
			s := stream.NewStream(context.Background(), 10)
			s.Push(stream.Chunk{Data: []byte("hello"), IsFinal: true})
			// Keep the stream open so the test can Peek.
			return &engine.Response{
				RequestID: uuid.New(),
				Stream:   s,
				Model:    "gpt-4",
			}, nil
		},
	}

	execFact := &mockExecutorFactory{
		getFn: func(ptype provider.ProviderType, format engine.RequestFormat) (engine.Executor, bool) {
			return mockExec, true
		},
	}

	cd := newMockCooldown()

	o := New(
		DefaultConfig(),
		resolver,
		translate.NewService(),
		execFact,
		accountSel,
		&mockAccountRepo{},
		cd,
		retry.DefaultConfig(),
		nil,
		noopLogger(),
	)

	req := testRequest()
	req.Stream = true

	resp, err := o.ExecuteRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
	if resp.Stream == nil {
		t.Fatal("expected non-nil stream in response")
	}

	// Verify the stream produces the expected chunk.
	s := resp.Stream.(*stream.Stream)
	chunk, err := s.Peek(context.Background())
	if err != nil {
		t.Fatalf("unexpected peek error: %v", err)
	}
	if string(chunk.Data) != "hello" {
		t.Fatalf("expected 'hello', got '%s'", string(chunk.Data))
	}
}

func TestCancelStream_Success(t *testing.T) {
	o := testOrchestrator()
	reqID := uuid.New()
	s := stream.NewStream(context.Background(), 10)
	o.mu.Lock()
	o.streams[reqID] = s
	o.mu.Unlock()

	err := o.CancelStream(context.Background(), reqID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if s.State() != stream.StateCancelled {
		t.Fatalf("expected StateCancelled, got %v", s.State())
	}

	// Verify the stream is removed from the map.
	o.mu.RLock()
	_, exists := o.streams[reqID]
	o.mu.RUnlock()
	if exists {
		t.Fatal("expected stream to be removed from map")
	}
}

func TestCancelStream_NotFound(t *testing.T) {
	o := testOrchestrator()
	err := o.CancelStream(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrStreamNotFound) {
		t.Fatalf("expected ErrStreamNotFound, got: %v", err)
	}
}

func TestCancelStream_MultipleCalls(t *testing.T) {
	o := testOrchestrator()
	reqID := uuid.New()
	s := stream.NewStream(context.Background(), 10)
	o.mu.Lock()
	o.streams[reqID] = s
	o.mu.Unlock()

	// First call succeeds.
	if err := o.CancelStream(context.Background(), reqID); err != nil {
		t.Fatalf("first cancel: %v", err)
	}

	// Second call should return ErrStreamNotFound (removed after first).
	if err := o.CancelStream(context.Background(), reqID); !errors.Is(err, ErrStreamNotFound) {
		t.Fatalf("expected ErrStreamNotFound on second call, got: %v", err)
	}
}

func TestExecuteRequest_ContextTimeout(t *testing.T) {
	acc := testAccount(1)
	prov := testProvider()

	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountSel := &mockAccountSelector{
		selectFn: func(_ context.Context, _ uuid.UUID, _ string) (*provider.Account, []provider.Account, error) {
			return acc, []provider.Account{*acc}, nil
		},
	}

	mockExec := &mockExecutor{
		providerType: provider.ProviderOpenAI,
		executeFn: func(ctx context.Context, _ *engine.Request, _ *provider.Account) (*engine.Response, error) {
			// Block until context is cancelled.
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}

	execFact := &mockExecutorFactory{
		getFn: func(ptype provider.ProviderType, format engine.RequestFormat) (engine.Executor, bool) {
			return mockExec, true
		},
	}

	o := New(
		Config{DefaultMaxTokens: 4096, RequestTimeout: 50 * time.Millisecond},
		resolver,
		translate.NewService(),
		execFact,
		accountSel,
		&mockAccountRepo{},
		newMockCooldown(),
		retry.Config{MaxAttempts: 1},
		nil,
		noopLogger(),
	)

	req := testRequest()
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}

func TestPipelineError_Wrapping(t *testing.T) {
	o := testOrchestrator()

	// Empty model string should produce an invalid model ref error.
	req := testRequest()
	req.Model = ""
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrModelResolutionFailed) {
		t.Fatalf("expected ErrModelResolutionFailed, got: %v", err)
	}
}

func TestExecuteRequest_CodexFormat(t *testing.T) {
	acc := testAccount(1)
	prov := testProvider()
	prov.Type = "codex"

	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	accountSel := &mockAccountSelector{
		selectFn: func(_ context.Context, _ uuid.UUID, _ string) (*provider.Account, []provider.Account, error) {
			return acc, []provider.Account{*acc}, nil
		},
	}

	execFact := &mockExecutorFactory{
		getFn: func(_ provider.ProviderType, _ engine.RequestFormat) (engine.Executor, bool) {
			return &mockExecutor{
				providerType: "codex",
				executeFn: func(_ context.Context, _ *engine.Request, _ *provider.Account) (*engine.Response, error) {
					return &engine.Response{
						RequestID:  uuid.New(),
						Body:       []byte(`ok`),
						Model:      "gpt-4",
						StatusCode: 200,
					}, nil
				},
			}, true
		},
	}
	cd := newMockCooldown()

	o := New(
		DefaultConfig(),
		resolver,
		translate.NewService(),
		execFact,
		accountSel,
		&mockAccountRepo{},
		cd,
		retry.DefaultConfig(),
		nil,
		noopLogger(),
	)

	req := testRequest()
	req.RawBody = []byte(`{"model":"gpt-4","input":"hello world"}`)

	resp, err := o.ExecuteRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

func TestExecuteRequest_EmptyBody(t *testing.T) {
	prov := testProvider()
	resolver := &mockResolver{
		resolveFn: func(_ context.Context, ref modelref.ModelRef) (*modelref.ResolvedRef, error) {
			return &modelref.ResolvedRef{
				Ref:        ref,
				ProviderID: prov.ID,
				ModelName:  "gpt-4",
				Provider:   prov,
			}, nil
		},
	}

	o := testOrchestrator()
	o.resolver = resolver
	o.accountRepo = &mockAccountRepo{}
	o.accountSel = &mockAccountSelector{}
	o.cooldown = newMockCooldown()

	req := testRequest()
	req.RawBody = []byte{}
	_, err := o.ExecuteRequest(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()
	if cfg.DefaultMaxTokens != 4096 {
		t.Errorf("expected DefaultMaxTokens=4096, got %d", cfg.DefaultMaxTokens)
	}
	if cfg.RequestTimeout != 120*time.Second {
		t.Errorf("expected RequestTimeout=120s, got %v", cfg.RequestTimeout)
	}
}

func TestMapTranslateFormat(t *testing.T) {
	tests := []struct {
		input    engine.RequestFormat
		expected engine.RequestFormat
	}{
		{engine.FormatOpenAIChat, engine.FormatOpenAIChat},
		{engine.FormatCodexResponses, engine.FormatCodexResponses},
		{engine.FormatOpenAICompat, engine.FormatOpenAICompat},
	}
	for _, tt := range tests {
		got := mapTranslateFormat(tt.input)
		if got != tt.expected {
			t.Errorf("mapTranslateFormat(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

// ---------------------------------------------------------------------------
// Helper: mockExecutorFactory
// ---------------------------------------------------------------------------

type mockExecutorFactory struct {
	getFn func(ptype provider.ProviderType, format engine.RequestFormat) (engine.Executor, bool)
}

func (m *mockExecutorFactory) GetExecutor(ptype provider.ProviderType, format engine.RequestFormat) (engine.Executor, bool) {
	return m.getFn(ptype, format)
}
