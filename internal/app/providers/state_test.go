package providers

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/engine/routing"
)

func frozenNow() time.Time {
	return time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC)
}

func newTestStateService() *StateService {
	cd := cooldown.New(cooldown.Config{
		DefaultCooldown:  10 * time.Minute,
		MaxCooldown:      10 * time.Minute,
		FailureThreshold: 1,
		EscalationFactor: 1.0,
		CleanupInterval:  time.Hour,
	})
	state := routing.NewRoutingStateManagerWithClockAndAuthority(frozenNow, cd)
	checkpoint := routing.NewFakeCheckpointer()
	return NewStateService(DefaultStateConfig(), state, checkpoint)
}

func TestStateService_RecordAccountFailure(t *testing.T) {
	svc := newTestStateService()
	ctx := context.Background()
	accountID := uuid.New()

	svc.RecordAccountFailure(ctx, accountID, routing.FailureAuth, 401, "invalid credentials", nil)

	if svc.IsAccountEligible(accountID) {
		t.Error("account should not be eligible after auth failure")
	}
}

func TestStateService_RecordAccountSuccess(t *testing.T) {
	svc := newTestStateService()
	ctx := context.Background()
	accountID := uuid.New()

	svc.RecordAccountFailure(ctx, accountID, routing.FailureUpstream, 502, "bad gateway", nil)
	if svc.IsAccountEligible(accountID) {
		t.Error("account should be ineligible after failure")
	}

	svc.RecordAccountSuccess(ctx, accountID)
	if !svc.IsAccountEligible(accountID) {
		t.Error("account should be eligible after success")
	}
}

func TestStateService_GetAccountState(t *testing.T) {
	svc := newTestStateService()
	accountID := uuid.New()

	state := svc.GetAccountState(accountID)
	if state != nil {
		t.Error("expected nil for unknown account")
	}

	svc.RecordAccountFailure(context.Background(), accountID, routing.FailureAuth, 401, "bad key", nil)
	state = svc.GetAccountState(accountID)
	if state == nil {
		t.Fatal("expected non-nil state")
	}
	if state.LastFailure.Class != routing.FailureAuth {
		t.Errorf("FailureClass = %v, want %v", state.LastFailure.Class, routing.FailureAuth)
	}
}

func TestStateService_ResetAccountState(t *testing.T) {
	svc := newTestStateService()
	ctx := context.Background()
	accountID := uuid.New()

	svc.RecordAccountFailure(ctx, accountID, routing.FailureAuth, 401, "bad key", nil)
	if svc.IsAccountEligible(accountID) {
		t.Error("account should be ineligible")
	}

	svc.ResetAccountState(accountID)
	if !svc.IsAccountEligible(accountID) {
		t.Error("account should be eligible after reset")
	}
}

func TestStateService_ClassifyFailure(t *testing.T) {
	svc := newTestStateService()

	tests := []struct {
		name      string
		http      int
		isAuth    bool
		isTimeout bool
		isConn    bool
		want      routing.FailureClass
	}{
		{"401", 401, false, false, false, routing.FailureAuth},
		{"429", 429, false, false, false, routing.FailureRateLimit},
		{"502", 502, false, false, false, routing.FailureUpstream},
		{"timeout flag", 200, false, true, false, routing.FailureTimeout},
		{"conn flag", 200, false, false, true, routing.FailureConnection},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := svc.ClassifyFailure(tt.http, nil, tt.isAuth, tt.isTimeout, tt.isConn)
			if got != tt.want {
				t.Errorf("ClassifyFailure = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStateService_CheckpointRestore(t *testing.T) {
	svc := newTestStateService()
	ctx := context.Background()
	accountID := uuid.New()

	svc.RecordAccountFailure(ctx, accountID, routing.FailureAuth, 401, "bad key", nil)

	err := svc.CheckpointNow(ctx)
	if err != nil {
		t.Fatalf("CheckpointNow: %v", err)
	}

	svc2 := newTestStateService()
	err = svc2.RestoreFromCheckpoint(ctx)
	if err != nil {
		t.Fatalf("RestoreFromCheckpoint: %v", err)
	}

	if !svc2.IsAccountEligible(accountID) {
		t.Log("account should be restored as disabled")
		state := svc2.GetAccountState(accountID)
		if state == nil {
			t.Error("restored state should not be nil")
		}
	}
}

func TestStateService_SaveListTransitions(t *testing.T) {
	svc := newTestStateService()
	ctx := context.Background()
	accountID := uuid.New()

	transition := routing.StateTransition{
		ID:        uuid.New(),
		AccountID: accountID,
		From:      "active",
		To:        "disabled",
		Reason:    "auth failure",
		CreatedAt: frozenNow(),
	}

	err := svc.SaveTransition(ctx, transition)
	if err != nil {
		t.Fatalf("SaveTransition: %v", err)
	}

	transitions, err := svc.ListTransitions(ctx, accountID)
	if err != nil {
		t.Fatalf("ListTransitions: %v", err)
	}
	if len(transitions) != 1 {
		t.Errorf("got %d transitions, want 1", len(transitions))
	}
}

func TestStateService_ConcurrentSafety(t *testing.T) {
	svc := newTestStateService()
	ctx := context.Background()
	var wg sync.WaitGroup

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id := uuid.New()
			svc.RecordAccountFailure(ctx, id, routing.FailureUpstream, 502, "err", nil)
			svc.IsAccountEligible(id)
			svc.RecordAccountSuccess(ctx, id)
		}()
	}
	wg.Wait()
}

func TestStateService_StartStop(t *testing.T) {
	state := routing.NewRoutingStateManagerWithClock(frozenNow)
	svc := NewStateService(DefaultStateConfig(), state, nil)

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)

	svc.Stop()
	cancel()
}

// recordingCheckpointer records checkpoint writes for lifecycle tests. It is
// safe for concurrent use because it runs from the checkpoint goroutine.
type recordingCheckpointer struct {
	mu           sync.Mutex
	saves        int
	lastSnapshot map[uuid.UUID]*routing.AccountState
	err          error
}

func (r *recordingCheckpointer) SaveCheckpoint(_ context.Context, snapshot map[uuid.UUID]*routing.AccountState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.saves++
	r.lastSnapshot = make(map[uuid.UUID]*routing.AccountState, len(snapshot))
	for id, s := range snapshot {
		r.lastSnapshot[id] = s
	}
	return r.err
}

func (r *recordingCheckpointer) LoadCheckpoint(_ context.Context) (map[uuid.UUID]*routing.AccountState, error) {
	return nil, nil
}

func (r *recordingCheckpointer) SaveTransition(_ context.Context, _ routing.StateTransition) error {
	return nil
}

func (r *recordingCheckpointer) ListTransitions(_ context.Context, _ uuid.UUID, _ time.Time) ([]routing.StateTransition, error) {
	return nil, nil
}

func (r *recordingCheckpointer) CleanupTransitions(_ context.Context, _ time.Time) (int, error) {
	return 0, nil
}

func (r *recordingCheckpointer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.saves
}

func (r *recordingCheckpointer) snapshot() map[uuid.UUID]*routing.AccountState {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastSnapshot
}

// TestStateService_FinalCheckpointOnStop verifies a final checkpoint is
// persisted during Stop even after the request context is cancelled.
func TestStateService_FinalCheckpointOnStop(t *testing.T) {
	state := routing.NewRoutingStateManagerWithClock(frozenNow)
	cp := &recordingCheckpointer{}
	svc := NewStateService(DefaultStateConfig(), state, cp)

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)

	accountID := uuid.New()
	svc.RecordAccountFailure(ctx, accountID, routing.FailureUpstream, 502, "err", nil)

	cancel()
	svc.Stop()

	if cp.count() == 0 {
		t.Fatal("expected at least one checkpoint (final checkpoint on shutdown)")
	}
	snap := cp.snapshot()
	if snap[accountID] == nil {
		t.Error("final checkpoint should include the recorded account failure")
	}
}

// TestStateService_FinalCheckpointOnStopErrorObserved verifies a failed final
// checkpoint is observed without panicking or blocking Stop.
func TestStateService_FinalCheckpointOnStopErrorObserved(t *testing.T) {
	state := routing.NewRoutingStateManagerWithClock(frozenNow)
	cp := &recordingCheckpointer{err: context.DeadlineExceeded}
	svc := NewStateService(DefaultStateConfig(), state, cp)

	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	cancel()
	done := make(chan struct{})
	go func() {
		svc.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return after checkpoint failure")
	}
}

// TestStateService_StopLeaksNoGoroutines verifies Start/Stop leaves no
// checkpoint or cleanup goroutines behind.
func TestStateService_StopLeaksNoGoroutines(t *testing.T) {
	state := routing.NewRoutingStateManagerWithClock(frozenNow)
	svc := NewStateService(DefaultStateConfig(), state, &recordingCheckpointer{})

	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	svc.Start(ctx)
	svc.Stop()
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before+2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("goroutine count after Stop = %d, before = %d", runtime.NumGoroutine(), before)
}

func TestStateService_StateManagerCheckpointer(t *testing.T) {
	svc := newTestStateService()

	sm := svc.StateManager()
	if sm == nil {
		t.Error("StateManager should not be nil")
	}

	cp := svc.Checkpointer()
	if cp == nil {
		t.Error("Checkpointer should not be nil")
	}
}
