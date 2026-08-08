package providers

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"

	"gorouter/internal/engine/routing"
)

// StateConfig controls the provider state service behaviour.
type StateConfig struct {
	// CheckpointInterval controls how often state is checkpointed to PG.
	CheckpointInterval time.Duration

	// RetentionPeriod is how long state transitions are retained (default 7 days).
	RetentionPeriod time.Duration

	// CleanupInterval controls how often old transitions are purged.
	CleanupInterval time.Duration
}

// DefaultStateConfig returns defaults for the provider state service.
func DefaultStateConfig() StateConfig {
	return StateConfig{
		CheckpointInterval: 5 * time.Minute,
		RetentionPeriod:    7 * 24 * time.Hour,
		CleanupInterval:    1 * time.Hour,
	}
}

// StateService manages the provider account runtime state, cooldowns, and checkpoints.
// It bridges the hot in-memory state (RoutingStateManager) with PG persistence via Checkpointer.
type StateService struct {
	mu         sync.Mutex
	config     StateConfig
	state      *routing.RoutingStateManager
	checkpoint routing.Checkpointer
	stopCh     chan struct{}
	wg         sync.WaitGroup
	now        func() time.Time
}

// NewStateService creates a provider state service with the wall clock.
// Production callers that do not need clock injection should use this
// constructor; it delegates to NewStateServiceWithClock with time.Now.
func NewStateService(config StateConfig, state *routing.RoutingStateManager, checkpoint routing.Checkpointer) *StateService {
	return NewStateServiceWithClock(config, state, checkpoint, time.Now)
}

// NewStateServiceWithClock creates a provider state service with an injectable
// clock, mirroring the routing.NewRoutingStateManagerWithClock pattern. A nil
// clock falls back to time.Now for production safety.
func NewStateServiceWithClock(config StateConfig, state *routing.RoutingStateManager, checkpoint routing.Checkpointer, now func() time.Time) *StateService {
	if now == nil {
		now = time.Now
	}
	if config.CheckpointInterval == 0 {
		config.CheckpointInterval = 5 * time.Minute
	}
	if config.RetentionPeriod == 0 {
		config.RetentionPeriod = 7 * 24 * time.Hour
	}
	if config.CleanupInterval == 0 {
		config.CleanupInterval = 1 * time.Hour
	}
	return &StateService{
		config:     config,
		state:      state,
		checkpoint: checkpoint,
		stopCh:     make(chan struct{}),
		now:        now,
	}
}

// Start launches the background checkpoint and cleanup goroutines.
func (s *StateService) Start(ctx context.Context) {
	s.mu.Lock()
	select {
	case <-s.stopCh:
		s.stopCh = make(chan struct{})
	default:
	}
	s.mu.Unlock()

	s.wg.Add(2)
	go s.checkpointLoop(ctx)
	go s.cleanupLoop(ctx)
}

// Stop signals the background goroutines to exit and waits for them.
func (s *StateService) Stop() {
	s.mu.Lock()
	select {
	case <-s.stopCh:
	default:
		close(s.stopCh)
	}
	s.mu.Unlock()
	s.wg.Wait()
}

// RecordAccountFailure records a failure. State will be checkpointed on the
// next periodic interval; synchronous checkpoint on hot path would cause
// reader-writer deadlock under concurrency.
func (s *StateService) RecordAccountFailure(ctx context.Context, accountID uuid.UUID, class routing.FailureClass, httpStatus int, reason string, err error) {
	s.state.RecordFailure(accountID, class, httpStatus, reason, err)
}

// RecordAccountSuccess records a success. State will be checkpointed on the
// next periodic interval.
func (s *StateService) RecordAccountSuccess(ctx context.Context, accountID uuid.UUID) {
	s.state.RecordSuccess(accountID)
}

// IsAccountEligible checks whether the account is eligible for routing.
func (s *StateService) IsAccountEligible(accountID uuid.UUID) bool {
	return s.state.IsEligible(accountID)
}

// GetAccountState returns the current state for an account.
func (s *StateService) GetAccountState(accountID uuid.UUID) *routing.AccountState {
	return s.state.GetAccount(accountID)
}

// ResetAccountState resets the state for an account (admin unlock per D176).
func (s *StateService) ResetAccountState(accountID uuid.UUID) {
	s.state.RecordSuccess(accountID)
}

// ClassifyFailure classifies an error for provider-aware handling.
func (s *StateService) ClassifyFailure(httpStatus int, err error, isAuthErr, isTimeout, isConnErr bool) routing.FailureClass {
	return routing.ClassifyFailure(httpStatus, err, isAuthErr, isTimeout, isConnErr)
}

// CheckpointNow forces an immediate checkpoint.
func (s *StateService) CheckpointNow(ctx context.Context) error {
	if s.checkpoint == nil {
		return nil
	}
	snapshot := s.state.Checkpoint()
	return s.checkpoint.SaveCheckpoint(ctx, snapshot)
}

// RestoreFromCheckpoint restores state from PG.
func (s *StateService) RestoreFromCheckpoint(ctx context.Context) error {
	if s.checkpoint == nil {
		return nil
	}
	snapshot, err := s.checkpoint.LoadCheckpoint(ctx)
	if err != nil {
		return err
	}
	if len(snapshot) > 0 {
		s.state.Restore(snapshot)
	}
	return nil
}

// SaveTransition records a state transition.
func (s *StateService) SaveTransition(ctx context.Context, t routing.StateTransition) error {
	if s.checkpoint == nil {
		return nil
	}
	return s.checkpoint.SaveTransition(ctx, t)
}

// ListTransitions returns transitions within the retention period.
func (s *StateService) ListTransitions(ctx context.Context, accountID uuid.UUID) ([]routing.StateTransition, error) {
	if s.checkpoint == nil {
		return nil, nil
	}
	since := s.now().Add(-s.config.RetentionPeriod)
	return s.checkpoint.ListTransitions(ctx, accountID, since)
}

// StateManager returns the underlying RoutingStateManager.
func (s *StateService) StateManager() *routing.RoutingStateManager {
	return s.state
}

// Checkpointer returns the configured Checkpointer.
func (s *StateService) Checkpointer() routing.Checkpointer {
	return s.checkpoint
}

func (s *StateService) tryCheckpoint(ctx context.Context) error {
	if s.checkpoint == nil {
		return nil
	}
	snapshot := s.state.Checkpoint()
	return s.checkpoint.SaveCheckpoint(ctx, snapshot)
}

func (s *StateService) checkpointLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.CheckpointInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.finalCheckpoint(ctx)
			return
		case <-s.stopCh:
			s.finalCheckpoint(ctx)
			return
		case <-ticker.C:
			_ = s.tryCheckpoint(ctx)
		}
	}
}

// finalCheckpoint performs one last best-effort checkpoint during shutdown.
// It derives a fresh bounded context from the request context without
// inheriting its cancellation, so the write can still complete after shutdown
// begins without leaking a background goroutine.
func (s *StateService) finalCheckpoint(ctx context.Context) {
	freshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	_ = s.tryCheckpoint(freshCtx)
}

func (s *StateService) cleanupLoop(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopCh:
			return
		case <-ticker.C:
			if s.checkpoint != nil {
				before := s.now().Add(-s.config.RetentionPeriod)
				_, _ = s.checkpoint.CleanupTransitions(ctx, before)
			}
		}
	}
}
