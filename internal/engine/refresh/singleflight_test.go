package refresh

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
)

func TestSingleFlightDedup(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	aid := uuid.New()
	callCount := 0

	var mu sync.Mutex
	res1Ch := make(chan struct{})
	res2Ch := make(chan struct{})

	ctx := context.Background()

	go func() {
		_, _ = sf.Do(ctx, aid, func(ctx context.Context) (Result, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			time.Sleep(50 * time.Millisecond)
			return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok1"}}, nil
		})
		close(res1Ch)
	}()

	go func() {
		_, _ = sf.Do(ctx, aid, func(ctx context.Context) (Result, error) {
			mu.Lock()
			callCount++
			mu.Unlock()
			return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok2"}}, nil
		})
		close(res2Ch)
	}()

	<-res1Ch
	<-res2Ch

	if callCount != 1 {
		t.Errorf("callCount = %d; want 1 (dedup)", callCount)
	}
}

func TestSingleFlightForget(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	aid := uuid.New()
	callCount := 0

	ctx := context.Background()
	_, _ = sf.Do(ctx, aid, func(ctx context.Context) (Result, error) {
		callCount++
		return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok1"}}, nil
	})

	sf.Forget(aid)

	_, _ = sf.Do(ctx, aid, func(ctx context.Context) (Result, error) {
		callCount++
		return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok2"}}, nil
	})

	if callCount != 2 {
		t.Errorf("callCount = %d; want 2 (after forget)", callCount)
	}
}

func TestSingleFlightForgetAll(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	ids := []uuid.UUID{uuid.New(), uuid.New()}
	callCount := 0

	ctx := context.Background()
	for _, id := range ids {
		_, _ = sf.Do(ctx, id, func(ctx context.Context) (Result, error) {
			callCount++
			return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok"}}, nil
		})
	}

	sf.ForgetAll()

	for _, id := range ids {
		_, _ = sf.Do(ctx, id, func(ctx context.Context) (Result, error) {
			callCount++
			return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok"}}, nil
		})
	}

	if callCount != 4 {
		t.Errorf("callCount = %d; want 4", callCount)
	}
}

func TestSingleFlightContextCancel(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	aid := uuid.New()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := sf.Do(ctx, aid, func(ctx context.Context) (Result, error) {
		return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok"}}, nil
	})
	if err == nil {
		t.Error("expected error from cancelled context")
	}
}

func TestSingleFlightDifferentAccounts(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	callCount := 0
	var mu sync.Mutex

	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			aid := uuid.New()
			_, _ = sf.Do(ctx, aid, func(ctx context.Context) (Result, error) {
				mu.Lock()
				callCount++
				mu.Unlock()
				time.Sleep(10 * time.Millisecond)
				return Result{Credential: &domainrefresh.TokenCredential{AccessToken: "tok"}}, nil
			})
		}()
	}
	wg.Wait()

	if callCount != 4 {
		t.Errorf("callCount = %d; want 4 (different accounts)", callCount)
	}
}
