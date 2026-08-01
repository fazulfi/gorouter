package refresh

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
)

func TestWaiterFallsBackToAlternateAfterRequestWait(t *testing.T) {
	aid := uuid.New()
	alt := uuid.New()

	dp := domainrefresh.DefaultPolicy()
	dp.RequestWait = 50 * time.Millisecond
	p := NewPolicyFromDomain(dp)

	blockCh := make(chan struct{})
	mr := &chanBlockRefresher{block: blockCh}
	resolver := &simpleResolverAlt{alts: map[uuid.UUID]uuid.UUID{aid: alt}}

	var altCalled int32
	altRefresher := &callbackAltRefresher{fn: func() (*domainrefresh.TokenCredential, error) {
		atomic.AddInt32(&altCalled, 1)
		return &domainrefresh.TokenCredential{AccessToken: "alt-token"}, nil
	}}

	c := NewCoordinator(p, mr, &mockHandler{},
		WithAccountResolver(resolver),
		WithAlternateRefresher(altRefresher),
	)
	ctx := context.Background()

	done := make(chan error, 1)
	go func() {
		_, err := c.RefreshNow(ctx, aid, "old")
		done <- err
	}()
	time.Sleep(20 * time.Millisecond)

	resultCh := make(chan string, 1)
	go func() {
		cred, err := c.RefreshNow(ctx, aid, "old")
		if err != nil {
			resultCh <- "err:" + err.Error()
		} else if cred != nil {
			resultCh <- cred.AccessToken
		} else {
			resultCh <- "nil"
		}
	}()

	select {
	case tok := <-resultCh:
		if tok != "alt-token" {
			t.Fatalf("got %q; want alt-token", tok)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("fallback to alternate account did not occur within RequestWait")
	}
	if n := atomic.LoadInt32(&altCalled); n == 0 {
		t.Error("alternate refresher was never called")
	}
}

func TestWaiterContinuesWaitingWhenNoAlternate(t *testing.T) {
	t.Parallel()
	aid := uuid.New()

	dp := domainrefresh.DefaultPolicy()
	dp.RequestWait = 100 * time.Millisecond
	p := NewPolicyFromDomain(dp)

	resolved := make(chan struct{})
	mr := &delayedRefresher{
		delay:  500 * time.Millisecond,
		result: &domainrefresh.TokenCredential{AccessToken: "original-token"},
	}
	c := NewCoordinator(p, mr, &mockHandler{})
	ctx := context.Background()

	go func() {
		time.Sleep(20 * time.Millisecond)
		cred, err := c.RefreshNow(ctx, aid, "old")
		if err == nil && cred != nil && cred.AccessToken == "original-token" {
			close(resolved)
		}
	}()
	cred, err := c.RefreshNow(ctx, aid, "old")
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "original-token" {
		t.Errorf("got %q; want original-token", cred.AccessToken)
	}
	<-resolved
}

func TestSingleFlightUnderlyingWorkCancelledOnAllCallersDone(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	aid := uuid.New()
	fnCancelled := make(chan struct{})
	fnStarted := make(chan struct{})
	ctx1, cancel1 := context.WithCancel(context.Background())
	ctx2, cancel2 := context.WithCancel(context.Background())

	done1 := make(chan struct{})
	go func() {
		_, _ = sf.Do(ctx1, aid, func(ctx context.Context) (Result, error) {
			close(fnStarted)
			<-ctx.Done()
			close(fnCancelled)
			return Result{}, ctx.Err()
		})
		close(done1)
	}()
	<-fnStarted

	done2 := make(chan struct{})
	go func() {
		_, _ = sf.Do(ctx2, aid, func(ctx context.Context) (Result, error) {
			return Result{}, nil
		})
		close(done2)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel1()
	<-done1
	time.Sleep(20 * time.Millisecond)
	select {
	case <-fnCancelled:
		t.Fatal("underlying work was cancelled while caller2 still waiting")
	default:
	}
	cancel2()
	<-done2
	select {
	case <-fnCancelled:
	case <-time.After(3 * time.Second):
		t.Fatal("underlying work was not cancelled after all callers left")
	}
}

func TestSemaphoreCountsUniqueOperationsNotWaiters(t *testing.T) {
	t.Parallel()
	p := NewDefaultPolicy()
	if p.MaxParallel() != 4 {
		t.Skip("test expects MaxParallel=4")
	}
	mr := &mockRefresher{delay: 500 * time.Millisecond}
	c := NewCoordinator(p, mr, &mockHandler{})
	ctx := context.Background()
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.RefreshNow(ctx, uuid.New(), "old")
		}()
	}
	wg.Wait()
	if n := atomic.LoadInt32(&mr.called); n != 5 {
		t.Errorf("called %d; want 5", n)
	}
}

func TestGoroutineLeakAfterAllCancelled(t *testing.T) {
	t.Parallel()
	dp := domainrefresh.DefaultPolicy()
	dp.AttemptTimeout = 100 * time.Millisecond
	dp.RequestWait = 50 * time.Millisecond
	p := NewPolicyFromDomain(dp)

	c := NewCoordinator(p, &mockRefresher{delay: 1 * time.Hour}, &mockHandler{})
	aid := uuid.New()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = c.RefreshNow(ctx, aid, "old")
		close(done)
	}()
	time.Sleep(20 * time.Millisecond)
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine leak: waiter did not return after context cancel")
	}
}

type chanBlockRefresher struct {
	block chan struct{}
}

func (b *chanBlockRefresher) Refresh(ctx context.Context, _ uuid.UUID, _ string) (*domainrefresh.TokenCredential, error) {
	select {
	case <-b.block:
		return nil, ctx.Err()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

type simpleResolverAlt struct {
	alts map[uuid.UUID]uuid.UUID
}

func (f *simpleResolverAlt) AlternateAccount(_ context.Context, id uuid.UUID) (uuid.UUID, bool, error) {
	alt, ok := f.alts[id]
	return alt, ok, nil
}

type callbackAltRefresher struct {
	fn func() (*domainrefresh.TokenCredential, error)
}

func (c *callbackAltRefresher) RefreshAlternate(ctx context.Context, _ uuid.UUID) (*domainrefresh.TokenCredential, error) {
	return c.fn()
}

type delayedRefresher struct {
	delay  time.Duration
	result *domainrefresh.TokenCredential
}

func (d *delayedRefresher) Refresh(ctx context.Context, _ uuid.UUID, _ string) (*domainrefresh.TokenCredential, error) {
	select {
	case <-time.After(d.delay):
		return d.result, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
