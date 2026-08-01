package refresh

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
	enginerefresh "gorouter/internal/engine/refresh"
)

type mockStore struct {
	getCred    *domainrefresh.TokenCredential
	getErr     error
	persistErr error
	disableErr error
	persistCnt int32
	disableCnt int32
}

func (m *mockStore) GetCredential(_ context.Context, _ uuid.UUID) (*domainrefresh.TokenCredential, error) {
	return m.getCred, m.getErr
}
func (m *mockStore) PersistCredential(_ context.Context, _ uuid.UUID, _ *domainrefresh.TokenCredential) error {
	atomic.AddInt32(&m.persistCnt, 1)
	return m.persistErr
}
func (m *mockStore) DisableRouting(_ context.Context, _ uuid.UUID, _ string) error {
	atomic.AddInt32(&m.disableCnt, 1)
	return m.disableErr
}

type mockLister struct {
	accounts []RefreshableAccount
	err      error
}

func (m *mockLister) ListRefreshableAccounts(_ context.Context) ([]RefreshableAccount, error) {
	return m.accounts, m.err
}

type testRefresher struct {
	called int32
}

func (t *testRefresher) Refresh(ctx context.Context, _ uuid.UUID, _ string) (*domainrefresh.TokenCredential, error) {
	atomic.AddInt32(&t.called, 1)
	return &domainrefresh.TokenCredential{AccessToken: "new-token"}, nil
}

type testFailRefresher struct {
	err error
}

func (t *testFailRefresher) Refresh(ctx context.Context, _ uuid.UUID, _ string) (*domainrefresh.TokenCredential, error) {
	return nil, t.err
}

type testHandler struct {
	successCnt int32
	defCnt     int32
}

func (t *testHandler) OnRefreshSuccess(_ context.Context, _ uuid.UUID, _ *domainrefresh.TokenCredential) error {
	atomic.AddInt32(&t.successCnt, 1)
	return nil
}
func (t *testHandler) OnRefreshDefinitive(_ context.Context, _ uuid.UUID, _ error) error {
	atomic.AddInt32(&t.defCnt, 1)
	return nil
}

func TestServiceRefreshAccountSuccess(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	store := &mockStore{getCred: &domainrefresh.TokenCredential{AccessToken: "old"}}
	s := NewService(coord, store, &mockLister{})

	cred, err := s.RefreshAccount(context.Background(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if cred.AccessToken != "new-token" {
		t.Errorf("got %q", cred.AccessToken)
	}
	if n := atomic.LoadInt32(&store.persistCnt); n != 1 {
		t.Errorf("persistCnt = %d; want 1", n)
	}
}

func TestServiceRefreshAccountGetError(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	store := &mockStore{getErr: errors.New("db error")}
	s := NewService(coord, store, &mockLister{})

	_, err := s.RefreshAccount(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestServiceRefreshAccountDefinitive(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testFailRefresher{err: domainrefresh.ErrDefinitive("revoked")},
		&testHandler{},
	)
	store := &mockStore{getCred: &domainrefresh.TokenCredential{AccessToken: "old"}}
	s := NewService(coord, store, &mockLister{})

	_, err := s.RefreshAccount(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected error")
	}
	if !domainrefresh.IsDefinitive(err) {
		t.Errorf("not definitive: %v", err)
	}
	if n := atomic.LoadInt32(&store.disableCnt); n != 1 {
		t.Errorf("disableCnt = %d; want 1", n)
	}
}

func TestServiceRefreshAccountPersistError(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	store := &mockStore{
		getCred:    &domainrefresh.TokenCredential{AccessToken: "old"},
		persistErr: errors.New("disk full"),
	}
	s := NewService(coord, store, &mockLister{})

	_, err := s.RefreshAccount(context.Background(), uuid.New())
	if err == nil {
		t.Fatal("expected persist error")
	}
}

func TestServiceProactiveRefreshIfNeeded(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	store := &mockStore{
		getCred: &domainrefresh.TokenCredential{
			AccessToken: "old",
			ExpiresAt:   timePtr(time.Now().Add(1 * time.Minute)),
		},
	}
	s := NewService(coord, store, &mockLister{})

	triggered := s.ProactiveRefreshIfNeeded(context.Background(), uuid.New())
	if !triggered {
		t.Error("should trigger for near-expiry credential")
	}
}

func TestServiceProactiveRefreshIfNeededNoCredential(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	s := NewService(coord, &mockStore{}, &mockLister{})

	if s.ProactiveRefreshIfNeeded(context.Background(), uuid.New()) {
		t.Error("should not trigger when no credential")
	}
}

func TestServiceRunProactiveCycle(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	store := &mockStore{getCred: &domainrefresh.TokenCredential{AccessToken: "old"}}
	lister := &mockLister{
		accounts: []RefreshableAccount{
			{ID: uuid.New(), Credential: &domainrefresh.TokenCredential{AccessToken: "a1", ExpiresAt: timePtr(time.Now().Add(1 * time.Minute))}},
			{ID: uuid.New(), Credential: &domainrefresh.TokenCredential{AccessToken: "a2", ExpiresAt: timePtr(time.Now().Add(2 * time.Minute))}},
		},
	}
	s := NewService(coord, store, lister)
	s.RunProactiveCycle(context.Background())
	time.Sleep(50 * time.Millisecond)
}

func TestServiceNextProactiveTiming(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	fc := newFakeClock(now)
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
		enginerefresh.WithClock(fc),
	)
	store := &mockStore{
		getCred: &domainrefresh.TokenCredential{
			AccessToken: "old",
			ExpiresAt:   timePtr(now.Add(10 * time.Minute)),
		},
	}
	s := NewService(coord, store, &mockLister{})

	d := s.NextProactiveTiming(context.Background(), uuid.New())
	if d == nil {
		t.Fatal("expected timing")
	}
	if *d != 5*time.Minute {
		t.Errorf("got %v; want 5m", *d)
	}
}

func TestServiceSingleFlight(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	s := NewService(coord, &mockStore{}, &mockLister{})
	if s.SingleFlight() != coord.SingleFlight() {
		t.Error("SingleFlight mismatch")
	}
}

func TestServiceRefreshAccountNilCredential(t *testing.T) {
	t.Parallel()
	coord := enginerefresh.NewCoordinator(
		enginerefresh.NewDefaultPolicy(),
		&testRefresher{},
		&testHandler{},
	)
	s := NewService(coord, &mockStore{}, &mockLister{})
	_, err := s.RefreshAccount(context.Background(), uuid.New())
	if err != domainrefresh.ErrUnsupported {
		t.Errorf("got %v; want ErrUnsupported", err)
	}
}

type fakeClock struct {
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock { return &fakeClock{now: now} }
func (f *fakeClock) Now() time.Time         { return f.now }
func (f *fakeClock) After(d time.Duration) <-chan time.Time {
	c := make(chan time.Time, 1)
	go func() {
		time.Sleep(d)
		c <- f.now.Add(d)
	}()
	return c
}

func timePtr(t time.Time) *time.Time { return &t }
