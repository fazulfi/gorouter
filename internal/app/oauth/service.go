// Package oauth provides the application-layer OAuth service that orchestrates
// all flow families, cancel-all restart semantics, and periodic cleanup.
package oauth

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	domainoauth "gorouter/internal/domain/oauth"
)

// Service manages all OAuth/import flow families for the application.
//
// Security guarantees:
//   - On every restart, ALL pending OAuth sessions are unconditionally marked
//     cancelled/failed. No partial sessions are ever reconstructed or resumed.
//   - Expired/replayed callbacks are rejected before any state mutation.
//   - Invalid credentials disable routing but preserve the account record.
//   - Credentials are redacted from errors, logs, metrics, and artifacts.
//   - TokenHash (SHA-256) is stored instead of raw access tokens.
type Service struct {
	repo domainoauth.Repository

	mu         sync.Mutex
	cancelAll  bool
	cleanupInt time.Duration
}

// NewService creates the OAuth service. It immediately cancels all pending
// sessions on construction (cancel-all restart semantics).
func NewService(repo domainoauth.Repository) *Service {
	svc := &Service{
		repo:       repo,
		cleanupInt: 5 * time.Minute,
	}
	// Cancel all pending sessions on restart. Never reconstruct/resume.
	if err := svc.cancelAllPending(context.Background()); err != nil {
		log.Printf("[oauth] cancel-all on restart: %v", err)
	} else {
		log.Printf("[oauth] all pending sessions cancelled on restart")
	}
	return svc
}

func (s *Service) cancelAllPending(ctx context.Context) error {
	n, err := s.repo.CancelPending(ctx)
	if err != nil {
		return fmt.Errorf("cancel pending: %w", err)
	}
	if n > 0 {
		log.Printf("[oauth] cancelled %d pending session(s)", n)
	}
	return nil
}

// StartCleanupLoop runs periodic cleanup of expired sessions in the background.
// This lifecycle is independent of the Phase 4 scheduler.
func (s *Service) StartCleanupLoop(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(s.cleanupInt)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.runCleanup(ctx); err != nil {
					log.Printf("[oauth] cleanup: %v", err)
				}
			}
		}
	}()
}

func (s *Service) runCleanup(ctx context.Context) error {
	expired, err := s.repo.DeleteExpired(ctx)
	if err != nil {
		return fmt.Errorf("delete expired: %w", err)
	}
	total, err := s.repo.Cleanup(ctx)
	if err != nil {
		return fmt.Errorf("cleanup: %w", err)
	}
	if expired > 0 || total > 0 {
		log.Printf("[oauth] cleanup removed %d expired + %d stale sessions", expired, total)
	}
	return nil
}

// CancelPending cancels all pending sessions. Called on restart and on demand.
func (s *Service) CancelPending(ctx context.Context) (int64, error) {
	return s.repo.CancelPending(ctx)
}
