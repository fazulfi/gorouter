package retry

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/provider"
	"gorouter/internal/shared"
)

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

func (m *mockInnerExecutor) SupportsFormat(format engine.RequestFormat) bool { return true }
func (m *mockInnerExecutor) ProviderType() provider.ProviderType             { return provider.ProviderOpenAI }

type mockRefresher struct {
	refreshFn func(ctx context.Context, account *provider.Account) (*provider.Account, error)
}

func (m *mockRefresher) Refresh(ctx context.Context, account *provider.Account) (*provider.Account, error) {
	return m.refreshFn(ctx, account)
}

func retryableError() error {
	return &shared.AppError{Code: ErrCodeUpstream, Message: "upstream error", HTTPStatus: 503}
}

func terminalError() error {
	return &shared.AppError{Code: shared.ErrValidation, Message: "invalid request", HTTPStatus: 400}
}

func authError() error {
	return &shared.AppError{Code: shared.ErrUnauthorized, Message: "auth failed", HTTPStatus: 401}
}

func newTestAccount() *provider.Account {
	return &provider.Account{ID: uuid.New(), CredentialRef: "test-key", AuthType: "api_key", IsEnabled: true}
}

func candidates(primary *provider.Account, extra ...*provider.Account) []provider.Account {
	result := []provider.Account{*primary}
	for _, a := range extra {
		result = append(result, *a)
	}
	return result
}

func TestRetryExecutor_Execute_SuccessFirstAttempt(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
		},
	}
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil, nil)
	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account, candidates(account))
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

func TestNewExecutor(t *testing.T) {
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return &engine.Response{StatusCode: 200}, nil
		},
	}
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, nil, nil)
	if exec == nil {
		t.Fatal("expected non-nil Executor")
	}
}

func TestRetryExecutor_Execute_NilPolicy(t *testing.T) {
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return &engine.Response{StatusCode: 200}, nil
		},
	}
	exec := NewExecutor(inner, nil, nil, nil)
	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account, candidates(account))
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRetryExecutor_Execute_WithFallbackAndCooldown(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
		},
	}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, sel, nil)
	req := &engine.Request{Model: "gpt-4"}
	account := newTestAccount()
	resp, err := exec.Execute(context.Background(), req, account, candidates(account))
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

func TestRetryExecutor_ExecuteStream_WithFallback(t *testing.T) {
	var callCount atomic.Int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			callCount.Add(1)
			return &engine.Response{RequestID: uuid.New(), Body: []byte("stream ok"), StatusCode: 200}, nil
		},
	}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	policy := NewPolicy(DefaultConfig())
	exec := NewExecutor(inner, policy, sel, nil)
	req := &engine.Request{Model: "gpt-4", Stream: true}
	account := newTestAccount()
	resp, err := exec.ExecuteStream(context.Background(), req, account, candidates(account))
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

func TestRefresherExecute_401RefreshSuccess(t *testing.T) {
	refreshedKey := "refreshed-key"
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			if account.CredentialRef == refreshedKey {
				return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
			}
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			u := *account
			u.CredentialRef = refreshedKey
			return &u, nil
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "old-key", IsEnabled: true}
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil, refresher)
	resp, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary))
	if err != nil {
		t.Fatalf("expected success after refresh, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if c := atomic.LoadInt32(&callCount); c != 2 {
		t.Errorf("expected 2 calls, got %d", c)
	}
}

func TestRefresherExecute_RefreshUnsupportedThenFallback(t *testing.T) {
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, authError()
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("10000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("20000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil)
	_, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary, fallbackAcct))
	if err == nil {
		t.Fatal("expected error when all accounts fail, got nil")
	}
	if c := atomic.LoadInt32(&callCount); c != 2 {
		t.Errorf("expected 2 calls (both accounts), got %d", c)
	}
	if len(cooldown.failures) != 2 {
		t.Errorf("expected 2 failure records, got %d", len(cooldown.failures))
	}
}

func TestRefresherExecute_DefinitiveRefreshFailureThenFallback(t *testing.T) {
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			return nil, ErrRefreshFailed
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("30000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("40000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil, refresher)
	_, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary, fallbackAcct))
	if err == nil {
		t.Fatal("expected error after definitive refresh failure, got nil")
	}
	if c := atomic.LoadInt32(&callCount); c != 1 {
		t.Errorf("expected 1 call, got %d", c)
	}
}

func TestRefresherExecute_TransientRefreshRetry(t *testing.T) {
	var refreshCount int32
	refreshedKey := "refreshed-key"
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			if account.CredentialRef == refreshedKey {
				return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
			}
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			n := atomic.AddInt32(&refreshCount, 1)
			if n < 3 {
				return nil, errors.New("transient refresh error")
			}
			u := *account
			u.CredentialRef = refreshedKey
			return &u, nil
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "old-key", IsEnabled: true}
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil, refresher)
	resp, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary))
	if err != nil {
		t.Fatalf("expected success after transient refresh retries, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if c := atomic.LoadInt32(&refreshCount); c != 3 {
		t.Errorf("expected 3 refresh attempts, got %d", c)
	}
}

func TestRefresherExecute_TransientRefreshExhaustedThenFallback(t *testing.T) {
	var refreshCount int32
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			atomic.AddInt32(&refreshCount, 1)
			return nil, errors.New("always transient")
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("50000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("60000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := Config{MaxAttempts: 1, BaseDelay: time.Millisecond}
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, refresher)
	_, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary, fallbackAcct))
	if err == nil {
		t.Fatal("expected error after transient refresh exhaustion, got nil")
	}
	if c := atomic.LoadInt32(&refreshCount); c != (MaxRefreshAttempts+1)*2 {
		t.Errorf("expected %d refresh attempts ((MaxRefreshAttempts+1)*2 accounts), got %d", (MaxRefreshAttempts+1)*2, c)
	}
	if c := atomic.LoadInt32(&callCount); c != (MaxRefreshAttempts+1)*2 {
		t.Errorf("expected %d calls ((MaxRefreshAttempts+1)*2 accounts), got %d", (MaxRefreshAttempts+1)*2, c)
	}
}

func TestRefresherExecute_AllCandidatesExhausted(t *testing.T) {
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, authError()
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("70000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallback1 := &provider.Account{ID: uuid.MustParse("80000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	fallback2 := &provider.Account{ID: uuid.MustParse("90000000-0000-0000-0000-000000000003"), CredentialRef: "key3", IsEnabled: true, Priority: 3}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil)
	_, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary, fallback1, fallback2))
	if err == nil {
		t.Fatal("expected error when all accounts exhausted, got nil")
	}
	if c := atomic.LoadInt32(&callCount); c != 3 {
		t.Errorf("expected 3 calls (all accounts), got %d", c)
	}
	if len(cooldown.failures) != 3 {
		t.Errorf("expected 3 failure records, got %d", len(cooldown.failures))
	}
}

func TestRefresherExecute_CooldownSkippedOnRefreshSuccess(t *testing.T) {
	refreshedKey := "refreshed-key"
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			if account.CredentialRef == refreshedKey {
				return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
			}
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			u := *account
			u.CredentialRef = refreshedKey
			return &u, nil
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "old-key", IsEnabled: true}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, refresher)
	_, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if len(cooldown.failures) != 0 {
		t.Errorf("expected 0 failures (refresh bypassed cooldown), got %d", len(cooldown.failures))
	}
	if len(cooldown.successes) != 1 {
		t.Errorf("expected 1 success record, got %d", len(cooldown.successes))
	}
}

func TestRefresherExecute_AuthThenRetryableFallback(t *testing.T) {
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			n := atomic.AddInt32(&callCount, 1)
			if n == 1 {
				return nil, authError()
			}
			if n == 2 {
				return nil, retryableError()
			}
			return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("a0000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("b0000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil)
	resp, err := exec.Execute(context.Background(), &engine.Request{Model: "gpt-4"}, primary, candidates(primary, fallbackAcct))
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if c := atomic.LoadInt32(&callCount); c != 3 {
		t.Errorf("expected 3 calls, got %d", c)
	}
}

func TestRefresherExecuteStream_AuthRefreshSuccess(t *testing.T) {
	refreshedKey := "refreshed-key"
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			if account.CredentialRef == refreshedKey {
				return &engine.Response{RequestID: uuid.New(), Body: []byte("stream ok"), StatusCode: 200}, nil
			}
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			u := *account
			u.CredentialRef = refreshedKey
			return &u, nil
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "old-key", IsEnabled: true}
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil, refresher)
	resp, err := exec.ExecuteStream(context.Background(), &engine.Request{Model: "gpt-4", Stream: true}, primary, candidates(primary))
	if err != nil {
		t.Fatalf("expected success after stream refresh, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
}

func TestRefresherExecuteStream_FallbackOnAuth(t *testing.T) {
	var callCount int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return nil, authError()
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("c0000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("d0000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil)
	_, err := exec.ExecuteStream(context.Background(), &engine.Request{Model: "gpt-4", Stream: true}, primary, candidates(primary, fallbackAcct))
	if err == nil {
		t.Fatal("expected error after all stream accounts fail, got nil")
	}
	if c := atomic.LoadInt32(&callCount); c != 2 {
		t.Errorf("expected 2 attempts (both accounts), got %d", c)
	}
}

func TestRefresherExecuteStream_RefreshThenFallbackOnRetry(t *testing.T) {
	var callCount int32
	refreshedKey := "refreshed-key"
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			if account.CredentialRef == refreshedKey {
				return nil, retryableError()
			}
			return nil, authError()
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("e0000000-0000-0000-0000-000000000001"), CredentialRef: "old-key", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("f0000000-0000-0000-0000-000000000002"), CredentialRef: "key3", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := Config{MaxAttempts: 1, BaseDelay: time.Millisecond}
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil /* refresher */)
	_, err := exec.ExecuteStream(context.Background(), &engine.Request{Model: "gpt-4", Stream: true}, primary, candidates(primary, fallbackAcct))
	if err == nil {
		t.Fatal("expected error after all stream accounts fail, got nil")
	}
	if c := atomic.LoadInt32(&callCount); c != 2 {
		t.Errorf("expected 2 attempts (primary then fallback), got %d", c)
	}
}

func TestRefresherExecute_ContextCancelledDuringAuth(t *testing.T) {
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			return nil, authError()
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "key", IsEnabled: true}
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, nil, refresher)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := exec.Execute(ctx, &engine.Request{Model: "gpt-4"}, primary, candidates(primary))
	if err == nil {
		t.Fatal("expected error for cancelled context, got nil")
	}
}

func TestExecuteStream_FallbackRetryWithBackoff(t *testing.T) {
	// Verifies that a fallback account experiencing transient errors gets
	// retried per policy (BL-5 symmetry fix). The primary account always
	// fails with auth; after refresh fails, the fallback is picked and
	// must go through the full retry cycle with backoff.
	var callCount int32
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			// First call (primary): auth error, Second call (fallback attempt 1):
			// retryable error, Third call (fallback attempt 2): success.
			n := atomic.LoadInt32(&callCount)
			if n == 1 {
				return nil, authError()
			}
			if n == 2 {
				return nil, retryableError()
			}
			return &engine.Response{RequestID: uuid.New(), Body: []byte("stream ok"), StatusCode: 200}, nil
		},
	}
	primary := &provider.Account{ID: uuid.MustParse("f1000000-0000-0000-0000-000000000001"), CredentialRef: "key1", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.MustParse("f2000000-0000-0000-0000-000000000002"), CredentialRef: "key2", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := Config{MaxAttempts: 3, BaseDelay: time.Millisecond}
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil)
	resp, err := exec.ExecuteStream(context.Background(), &engine.Request{Model: "gpt-4", Stream: true}, primary, candidates(primary, fallbackAcct))
	if err != nil {
		t.Fatalf("expected success after fallback retry, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if c := atomic.LoadInt32(&callCount); c != 3 {
		t.Errorf("expected 3 calls (1 primary auth + 2 fallback attempts), got %d", c)
	}
}

func TestRefresherExecute_Race(t *testing.T) {
	var callCount int32
	inner := &mockInnerExecutor{
		executeFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			atomic.AddInt32(&callCount, 1)
			return &engine.Response{RequestID: uuid.New(), Body: []byte("ok"), StatusCode: 200}, nil
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "key", IsEnabled: true}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := DefaultConfig()
	cfg.BaseDelay = time.Millisecond
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, nil)
	done := make(chan struct{}, 10)
	for range 10 {
		go func() {
			defer func() { done <- struct{}{} }()
			req := &engine.Request{Model: "gpt-4"}
			_, err := exec.Execute(context.Background(), req, primary, candidates(primary))
			if err != nil {
				t.Errorf("race: unexpected error: %v", err)
			}
		}()
	}
	for range 10 {
		<-done
	}
	if c := atomic.LoadInt32(&callCount); c != 10 {
		t.Errorf("expected 10 calls (one per goroutine), got %d", c)
	}
}

func TestExecuteStream_RefreshBackoffThenFallback(t *testing.T) {
	var callCount int32
	refreshedKey := "refreshed-key"
	inner := &mockInnerExecutor{
		executeStreamFn: func(ctx context.Context, req *engine.Request, account *provider.Account) (*engine.Response, error) {
			n := atomic.AddInt32(&callCount, 1)
			if n == 1 {
				return nil, authError()
			}
			if n == 2 || n == 3 {
				return nil, retryableError()
			}
			return &engine.Response{RequestID: uuid.New(), Body: []byte("stream ok"), StatusCode: 200}, nil
		},
	}
	refresher := &mockRefresher{
		refreshFn: func(ctx context.Context, account *provider.Account) (*provider.Account, error) {
			u := *account
			u.CredentialRef = refreshedKey
			return &u, nil
		},
	}
	primary := &provider.Account{ID: uuid.New(), CredentialRef: "old-key", IsEnabled: true, Priority: 1}
	fallbackAcct := &provider.Account{ID: uuid.New(), CredentialRef: "fb-key", IsEnabled: true, Priority: 2}
	cooldown := newMockCooldownRegistry()
	sel := NewFallbackSelector(nil, cooldown)
	cfg := Config{MaxAttempts: 3, BaseDelay: time.Millisecond}
	policy := NewPolicy(cfg)
	exec := NewExecutor(inner, policy, sel, refresher)
	resp, err := exec.ExecuteStream(context.Background(), &engine.Request{Model: "gpt-4", Stream: true}, primary, candidates(primary, fallbackAcct))
	if err != nil {
		t.Fatalf("expected success after refresh+backoff+fallback, got: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Errorf("expected 200, got %d", resp.StatusCode)
	}
	if c := atomic.LoadInt32(&callCount); c != 4 {
		t.Errorf("expected 4 calls (1 auth + 1 refresh retry + 2 fallback), got %d", c)
	}
}
