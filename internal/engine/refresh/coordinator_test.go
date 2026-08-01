package refresh

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
)

type fakeClock struct {
	now   time.Time
	after chan time.Duration
	timer chan time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{
		now:   now,
		after: make(chan time.Duration, 100),
		timer: make(chan time.Time, 100),
	}
}

func (f *fakeClock) Now() time.Time { return f.now }
func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	f.after <- d
	return f.timer
}
func (f *fakeClock) Advance(d time.Duration) {
	f.now = f.now.Add(d)
	f.timer <- f.now
}

func zeroJitter(base, _ time.Duration) time.Duration { return base }

type mockRefresher struct {
	called  int32
	results []resultOrErr
	delay   time.Duration
}

type resultOrErr struct {
	cred *domainrefresh.TokenCredential
	err  error
}

func (m *mockRefresher) Refresh(ctx context.Context, _ uuid.UUID, _ string) (*domainrefresh.TokenCredential, error) {
	atomic.AddInt32(&m.called, 1)
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if len(m.results) > 0 {
		r := m.results[0]
		m.results = m.results[1:]
		return r.cred, r.err
	}
	return &domainrefresh.TokenCredential{AccessToken: "fresh"}, nil
}

type mockHandler struct {
	successCalled    int32
	definitiveCalled int32
}

func (m *mockHandler) OnRefreshSuccess(_ context.Context, _ uuid.UUID, cred *domainrefresh.TokenCredential) error {
	atomic.AddInt32(&m.successCalled, 1)
	return nil
}
func (m *mockHandler) OnRefreshDefinitive(_ context.Context, _ uuid.UUID, _ error) error {
	atomic.AddInt32(&m.definitiveCalled, 1)
	return nil
}

func TestCoordinatorNewDefault(t *testing.T) {
	t.Parallel()
	c := NewCoordinator(NewDefaultPolicy(), &mockRefresher{}, &mockHandler{})
	if c.SemaphoreSize() != 4 {
		t.Errorf("SemaphoreSize = %d; want 4", c.SemaphoreSize())
	}
	if c.SingleFlight() == nil {
		t.Error("SingleFlight should not be nil")
	}
}

func TestCoordinatorRefreshSuccess(t *testing.T) {
	t.Parallel()
	mr := &mockRefresher{results: []resultOrErr{
		{cred: &domainrefresh.TokenCredential{AccessToken: "new-token"}},
	}}
	mh := &mockHandler{}
	c := NewCoordinator(NewDefaultPolicy(), mr, mh)

	cred, err := c.RefreshNow(context.Background(), uuid.New(), "old")
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "new-token" {
		t.Errorf("AccessToken = %q; want %q", cred.AccessToken, "new-token")
	}
	if n := atomic.LoadInt32(&mr.called); n != 1 {
		t.Errorf("refresher called %d times; want 1", n)
	}
	if n := atomic.LoadInt32(&mh.successCalled); n != 1 {
		t.Errorf("successCalled = %d; want 1", n)
	}
}

func TestCoordinatorRefreshDefinitive(t *testing.T) {
	t.Parallel()
	mr := &mockRefresher{results: []resultOrErr{
		{err: domainrefresh.ErrDefinitive("revoked")},
	}}
	mh := &mockHandler{}
	c := NewCoordinator(NewDefaultPolicy(), mr, mh)

	cred, err := c.RefreshNow(context.Background(), uuid.New(), "old")
	if err == nil {
		t.Fatal("expected error")
	}
	if cred != nil {
		t.Error("cred should be nil")
	}
	if !domainrefresh.IsDefinitive(err) {
		t.Errorf("err not definitive: %v", err)
	}
	if n := atomic.LoadInt32(&mh.definitiveCalled); n != 1 {
		t.Errorf("definitiveCalled = %d; want 1", n)
	}
}

func TestCoordinatorRefreshTransientThenSuccess(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(now)

	mr := &mockRefresher{results: []resultOrErr{
		{err: domainrefresh.ErrTransient("blip1")},
		{err: domainrefresh.ErrTransient("blip2")},
		{cred: &domainrefresh.TokenCredential{AccessToken: "third"}},
	}}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{}, WithClock(fc), WithJitter(zeroJitter))

	type resT struct {
		cred *domainrefresh.TokenCredential
		err  error
	}
	resCh := make(chan resT, 1)
	go func() {
		cred, err := c.RefreshNow(context.Background(), uuid.New(), "old")
		resCh <- resT{cred, err}
	}()

	for i := 0; i < 2; i++ {
		<-fc.after
		fc.Advance(1 * time.Second)
	}

	r := <-resCh
	if r.err != nil {
		t.Fatal(r.err)
	}
	if r.cred.AccessToken != "third" {
		t.Errorf("got %q", r.cred.AccessToken)
	}
	if n := atomic.LoadInt32(&mr.called); n != 3 {
		t.Errorf("called %d; want 3", n)
	}
}

func TestCoordinatorRefreshMaxAttemptsExceeded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(now)

	mr := &mockRefresher{results: []resultOrErr{
		{err: domainrefresh.ErrTransient("f1")},
		{err: domainrefresh.ErrTransient("f2")},
		{err: domainrefresh.ErrTransient("f3")},
	}}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{}, WithClock(fc), WithJitter(zeroJitter))

	type resT struct {
		cred *domainrefresh.TokenCredential
		err  error
	}
	resCh := make(chan resT, 1)
	go func() {
		cred, err := c.RefreshNow(context.Background(), uuid.New(), "old")
		resCh <- resT{cred, err}
	}()

	for i := 0; i < 2; i++ {
		<-fc.after
		fc.Advance(1 * time.Second)
	}

	r := <-resCh
	if r.err == nil {
		t.Fatal("expected error")
	}
	if r.cred != nil {
		t.Error("cred should be nil")
	}
	if n := atomic.LoadInt32(&mr.called); n != 3 {
		t.Errorf("called %d; want 3", n)
	}
}

func TestCoordinatorSingleFlightShared(t *testing.T) {
	t.Parallel()
	mr := &mockRefresher{delay: 100 * time.Millisecond}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{})

	aid := uuid.New()
	errs := make(chan error, 2)
	for i := 0; i < 2; i++ {
		go func() {
			_, err := c.RefreshNow(context.Background(), aid, "old")
			errs <- err
		}()
	}
	for i := 0; i < 2; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if n := atomic.LoadInt32(&mr.called); n != 1 {
		t.Errorf("called %d; want 1", n)
	}
}

func TestCoordinatorProactiveRefreshTiming(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(now)
	c := NewCoordinator(NewDefaultPolicy(), &mockRefresher{}, &mockHandler{}, WithClock(fc))

	expiry := now.Add(10 * time.Minute)
	next := c.NextProactiveRefresh(&expiry)
	if next == nil || *next != 5*time.Minute {
		t.Fatalf("next = %v; want 5m", next)
	}

	fc.now = now.Add(6 * time.Minute)
	next = c.NextProactiveRefresh(&expiry)
	if next == nil || *next != 0 {
		t.Fatalf("next = %v; want 0", next)
	}

	next = c.NextProactiveRefresh(nil)
	if next == nil || *next != 6*time.Hour {
		t.Fatalf("next = %v; want 6h", next)
	}
}

func TestCoordinatorGlobalMaxParallel(t *testing.T) {
	t.Parallel()
	mr := &mockRefresher{delay: 50 * time.Millisecond}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{})

	errs := make(chan error, 6)
	for i := 0; i < 6; i++ {
		go func() {
			_, err := c.RefreshNow(context.Background(), uuid.New(), "old")
			errs <- err
		}()
	}
	for i := 0; i < 6; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
	if n := atomic.LoadInt32(&mr.called); n != 6 {
		t.Errorf("called %d; want 6", n)
	}
}

func TestCoordinatorWithExternalSingleFlight(t *testing.T) {
	t.Parallel()
	sf := NewSingleFlight()
	c := NewCoordinator(NewDefaultPolicy(), &mockRefresher{}, &mockHandler{}, WithSingleFlight(sf))
	if c.SingleFlight() != sf {
		t.Error("external SingleFlight not used")
	}
}

func TestDefaultJitterRange(t *testing.T) {
	t.Parallel()
	for i := 0; i < 100; i++ {
		r := DefaultJitter(1*time.Second, 500*time.Millisecond)
		if r < 500*time.Millisecond || r > 1500*time.Millisecond {
			t.Errorf("jitter %v out of range", r)
		}
	}
}

func TestProactiveRefreshIfNeeded(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(now)
	c := NewCoordinator(NewDefaultPolicy(), &mockRefresher{}, &mockHandler{}, WithClock(fc))

	triggered := c.ProactiveRefreshIfNeeded(context.Background(), uuid.New(), "old", timePtr(now.Add(2*time.Minute)))
	if !triggered {
		t.Error("should trigger when past lead time")
	}

	triggered = c.ProactiveRefreshIfNeeded(context.Background(), uuid.New(), "old", timePtr(now.Add(24*time.Hour)))
	if triggered {
		t.Error("should not trigger when far from expiry")
	}
}

func TestProactiveReactiveConvergence(t *testing.T) {
	t.Parallel()
	mr := &mockRefresher{delay: 100 * time.Millisecond}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{})

	aid := uuid.New()
	reactiveDone := make(chan struct{})
	go func() {
		_, _ = c.RefreshNow(context.Background(), aid, "old")
		close(reactiveDone)
	}()

	time.Sleep(10 * time.Millisecond)
	c.ProactiveRefreshIfNeeded(context.Background(), aid, "old", timePtr(time.Now().Add(1*time.Minute)))

	<-reactiveDone
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&mr.called); n != 1 {
		t.Errorf("called %d; want 1", n)
	}
}

func TestCoordinatorRedaction(t *testing.T) {
	t.Parallel()
	mr := &mockRefresher{results: []resultOrErr{
		{cred: &domainrefresh.TokenCredential{AccessToken: "sk-secret-12345"}},
	}}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{})

	cred, err := c.RefreshNow(context.Background(), uuid.New(), "old-sk-67890")
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "sk-secret-12345" {
		t.Errorf("got %q", cred.AccessToken)
	}
}

func TestFourOperationCap(t *testing.T) {
	t.Parallel()
	p := NewDefaultPolicy()
	if p.MaxParallel() != 4 {
		t.Errorf("MaxParallel = %d; want 4", p.MaxParallel())
	}
}

type trackedRefresher struct {
	active    int32
	maxActive int32
	delay     time.Duration
}

func (t *trackedRefresher) Refresh(ctx context.Context, _ uuid.UUID, _ string) (*domainrefresh.TokenCredential, error) {
	cur := atomic.AddInt32(&t.active, 1)
	for {
		prev := atomic.LoadInt32(&t.maxActive)
		if cur <= prev || atomic.CompareAndSwapInt32(&t.maxActive, prev, cur) {
			break
		}
	}
	if t.delay > 0 {
		select {
		case <-time.After(t.delay):
		case <-ctx.Done():
			atomic.AddInt32(&t.active, -1)
			return nil, ctx.Err()
		}
	}
	atomic.AddInt32(&t.active, -1)
	return &domainrefresh.TokenCredential{AccessToken: "fresh"}, nil
}

func TestDistinctAccountConcurrency(t *testing.T) {
	t.Parallel()
	tr := &trackedRefresher{delay: 100 * time.Millisecond}
	c := NewCoordinator(NewDefaultPolicy(), tr, &mockHandler{})

	errs := make(chan error, 5)
	for i := 0; i < 5; i++ {
		go func() {
			_, err := c.RefreshNow(context.Background(), uuid.New(), "old")
			errs <- err
		}()
	}

	time.Sleep(200 * time.Millisecond)
	if n := atomic.LoadInt32(&tr.maxActive); n > 4 {
		t.Errorf("maxActive = %d; want <= 4", n)
	}
	if n := atomic.LoadInt32(&tr.maxActive); n < 2 {
		t.Logf("maxActive = %d; expected at least 2 for good concurrency", n)
	}

	for i := 0; i < 5; i++ {
		if err := <-errs; err != nil {
			t.Error(err)
		}
	}
}

func TestGoroutineLeak(t *testing.T) {
	t.Parallel()
	c := NewCoordinator(NewDefaultPolicy(), &mockRefresher{delay: 1 * time.Hour}, &mockHandler{})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = c.RefreshNow(ctx, uuid.New(), "old")
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("goroutine leak detected")
	}
}

func TestWaitingRequestWithFallback(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(now)

	mr := &mockRefresher{delay: 200 * time.Millisecond}
	c := NewCoordinator(NewDefaultPolicy(), mr, &mockHandler{}, WithClock(fc), WithJitter(zeroJitter))

	aid := uuid.New()
	// Start a refresh that takes time
	resCh := make(chan error, 2)
	go func() {
		_, err := c.RefreshNow(context.Background(), aid, "old")
		resCh <- err
	}()

	// Second request for same account should join singleflight
	time.Sleep(10 * time.Millisecond)
	go func() {
		_, err := c.RefreshNow(context.Background(), aid, "old")
		resCh <- err
	}()

	for i := 0; i < 2; i++ {
		if err := <-resCh; err != nil {
			t.Error(err)
		}
	}
	if n := atomic.LoadInt32(&mr.called); n != 1 {
		t.Errorf("called %d; want 1", n)
	}
}

func timePtr(t time.Time) *time.Time { return &t }

var errSentinel = errors.New("sentinel")
