package retry

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

// ---------------------------------------------------------------------------
// mockInnerExecutor implements engine.Executor for testing the retry wrapper.
// ---------------------------------------------------------------------------

type mockInnerExecutor struct {
	executeFn       func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error)
	executeStreamFn func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error)
}

func (m *mockInnerExecutor) Execute(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return m.executeFn(ctx, req, account)
}

func (m *mockInnerExecutor) ExecuteStream(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
	return m.executeStreamFn(ctx, req, account)
}

func (m *mockInnerExecutor) SupportsFormat(format engine.RequestFormat) bool {
	return true
}

func (m *mockInnerExecutor) ProviderType() provider.ProviderType {
	return provider.ProviderOpenAI
}

func retryableError() error {
	return &shared.AppError{
		Code:       ErrCodeUpstream,
		Message:    "upstream API returned error",
		HTTPStatus: 503,
	}
}

func terminalError() error {
	return &shared.AppError{
		Code:       shared.ErrValidation,
		Message:    "invalid request",
		HTTPStatus: 400,
	}
}

func newTestAccount() *provider.Account {
	return &provider.Account{
		ID:            uuid.New(),
		CredentialRef: "test-key",
		AuthType:      "api_key",
		IsEnabled:     true,
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRetryExecutor_Execute_SuccessFirstAttempt(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`ok`),
				StatusCode: 200,
			}, nil
		},
	}

	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call, got %d", callCount.Load())
	}
}

func TestRetryExecutor_Execute_RetryThenSuccess(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			count := callCount.Add(1)
			if count == 1 {
				// First attempt fails with retryable error.
				return nil, retryableError()
			}
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`ok`),
				StatusCode: 200,
			}, nil
		},
	}

	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond // speed up test
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 success), got %d", callCount.Load())
	}
}

func TestRetryExecutor_Execute_AllRetriesExhausted(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return nil, retryableError()
		},
	}

	cfg := DefaultConfig()
	cfg.MaxAttempts = 2
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	_, err := exec.Execute(context.Background(), req, account)
	if err == nil {
		t.Fatal("expected error after exhausting retries, got nil")
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls (max attempts), got %d", callCount.Load())
	}
}

func TestRetryExecutor_Execute_NonRetryableError(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return nil, terminalError()
		},
	}

	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	_, err := exec.Execute(context.Background(), req, account)
	if err == nil {
		t.Fatal("expected error for non-retryable error, got nil")
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call (no retry), got %d", callCount.Load())
	}
}

func TestRetryExecutor_Execute_ContextCancelled(t *testing.T) {
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return nil, retryableError()
		},
	}

	cfg := DefaultConfig()
	cfg.BaseDelay = 100 * time.Millisecond // enough time for cancel to take effect
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	_, err := exec.Execute(ctx, req, account)
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestRetryExecutor_ExecuteStream_SuccessFirstAttempt(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`stream data`),
				StatusCode: 200,
			}, nil
		},
	}

	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	resp, err := exec.ExecuteStream(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if callCount.Load() != 1 {
		t.Errorf("expected 1 call, got %d", callCount.Load())
	}
}

func TestRetryExecutor_ExecuteStream_RetryThenSuccess(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			count := callCount.Add(1)
			if count == 1 {
				return nil, retryableError()
			}
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`stream ok`),
				StatusCode: 200,
			}, nil
		},
	}

	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	resp, err := exec.ExecuteStream(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success after retry, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls (1 fail + 1 success), got %d", callCount.Load())
	}
}

func TestRetryExecutor_ExecuteStream_FailsOnRetry(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return nil, retryableError()
		},
	}

	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	_, err := exec.ExecuteStream(context.Background(), req, account)
	if err == nil {
		t.Fatal("expected error after stream retry failure, got nil")
	}
	// Stream retries only once.
	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls (stream retry limit), got %d", callCount.Load())
	}
}

func TestRetryExecutor_ExecuteStream_NonRetryable(t *testing.T) {
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return nil, terminalError()
		},
	}

	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil)

	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	_, err := exec.ExecuteStream(context.Background(), req, account)
	if err == nil {
		t.Fatal("expected error for non-retryable stream error, got nil")
	}
}

func TestRetryExecutor_NewExecutor(t *testing.T) {
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return &engine.Response{StatusCode: 200}, nil
		},
	}
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil)

	if exec == nil {
		t.Fatal("expected non-nil Executor")
	}
}

func TestRetryExecutor_Execute_NilPolicy(t *testing.T) {
	// A nil policy would cause a nil pointer dereference but NewExecutor
	// doesn't validate. We test that valid input works.
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return &engine.Response{StatusCode: 200}, nil
		},
	}
	exec := NewExecutor(inner, nil, nil)
	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRetryExecutor_Execute_WithFallbackAndCooldown(t *testing.T) {
	// Test that a fallback selector with cooldown registry is used correctly.
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`ok`),
				StatusCode: 200,
			}, nil
		},
	}

	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, sel)

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	// Success should have been recorded.
	if len(cooldown.successes) != 1 {
		t.Errorf("expected 1 success record, got %d", len(cooldown.successes))
	}
}

func TestRetryExecutor_Execute_WithFallbackRetryableFailure(t *testing.T) {
	// Test that fallback is queried after repeated failures.
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return nil, retryableError()
		},
	}

	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.MaxAttempts = 3
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel)

	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	_, err := exec.Execute(context.Background(), req, account)
	if err == nil {
		t.Fatal("expected error after exhausting retries with fallback, got nil")
	}
	// Failures should have been recorded.
	if len(cooldown.failures) == 0 {
		t.Error("expected at least 1 failure record")
	}
}

func TestRetryExecutor_ExecuteStream_WithFallback(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{
				RequestID:  uuid.New(),
				Body:       []byte(`stream ok`),
				StatusCode: 200,
			}, nil
		},
	}

	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, sel)

	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	resp, err := exec.ExecuteStream(context.Background(), req, account)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if len(cooldown.successes) != 1 {
		t.Errorf("expected 1 success record, got %d", len(cooldown.successes))
	}
}

func TestRetryExecutor_ExecuteStream_WithFallbackRetryFails(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return nil, retryableError()
		},
	}

	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel)

	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	_, err := exec.ExecuteStream(context.Background(), req, account)
	if err == nil {
		t.Fatal("expected error after stream retry with fallback, got nil")
	}
	if len(cooldown.failures) == 0 {
		t.Error("expected at least 1 failure record")
	}
}

func TestNewExecutor(t *testing.T) {
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return &engine.Response{StatusCode: 200}, nil
		},
	}
	policy := NewPolicy(DefaultConfig())

	exec := NewExecutor(inner, policy, nil)
	if exec == nil {
		t.Fatal("NewExecutor returned nil")
	}

	exec2 := NewExecutor(inner, nil, nil)
	if exec2 == nil {
		t.Fatal("NewExecutor with nil policy returned nil")
	}
}
