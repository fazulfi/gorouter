// Package refresh implements the application-level refresh use case.
package refresh

import (
	"context"
	"log"
	"time"

	"github.com/google/uuid"

	domainrefresh "gorouter/internal/domain/refresh"
	enginerefresh "gorouter/internal/engine/refresh"
)

// AccountStore provides access to provider account credentials and metadata.
type AccountStore interface {
	GetCredential(ctx context.Context, accountID uuid.UUID) (*domainrefresh.TokenCredential, error)
	PersistCredential(ctx context.Context, accountID uuid.UUID, cred *domainrefresh.TokenCredential) error
	DisableRouting(ctx context.Context, accountID uuid.UUID, reason string) error
}

// AccountLister provides the set of accounts eligible for proactive refresh.
type AccountLister interface {
	ListRefreshableAccounts(ctx context.Context) ([]RefreshableAccount, error)
}

// RefreshableAccount describes an account eligible for scheduled refresh.
type RefreshableAccount struct {
	ID         uuid.UUID
	Credential *domainrefresh.TokenCredential
}

// Service is the application-level refresh orchestrator.
type Service struct {
	coordinator *enginerefresh.Coordinator
	store       AccountStore
	lister      AccountLister
	policy      domainrefresh.RefreshPolicy
}

// NewService creates a refresh service.
func NewService(
	coordinator *enginerefresh.Coordinator,
	store AccountStore,
	lister AccountLister,
) *Service {
	return &Service{
		coordinator: coordinator,
		store:       store,
		lister:      lister,
		policy:      domainrefresh.DefaultPolicy(),
	}
}

// RefreshAccount performs a reactive refresh for the given account ID.
func (s *Service) RefreshAccount(ctx context.Context, accountID uuid.UUID) (*domainrefresh.TokenCredential, error) {
	cred, err := s.store.GetCredential(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if cred == nil {
		return nil, domainrefresh.ErrUnsupported
	}

	newCred, err := s.coordinator.RefreshNow(ctx, accountID, cred.AccessToken)
	if err != nil {
		if domainrefresh.IsDefinitive(err) {
			if disableErr := s.store.DisableRouting(ctx, accountID, err.Error()); disableErr != nil {
				log.Printf("failed to disable routing for account %s: %v", accountID, disableErr)
			}
		}
		return nil, err
	}

	if err := s.store.PersistCredential(ctx, accountID, newCred); err != nil {
		return nil, err
	}
	return newCred, nil
}

// ProactiveRefreshIfNeeded checks and optionally triggers proactive refresh.
func (s *Service) ProactiveRefreshIfNeeded(ctx context.Context, accountID uuid.UUID) bool {
	cred, err := s.store.GetCredential(ctx, accountID)
	if err != nil || cred == nil {
		return false
	}
	return s.coordinator.ProactiveRefreshIfNeeded(ctx, accountID, cred.AccessToken, cred.ExpiresAt)
}

// RunProactiveCycle runs one cycle of proactive refresh for all eligible accounts.
func (s *Service) RunProactiveCycle(ctx context.Context) {
	accounts, err := s.lister.ListRefreshableAccounts(ctx)
	if err != nil {
		log.Printf("failed to list refreshable accounts: %v", err)
		return
	}
	for _, acc := range accounts {
		if acc.Credential == nil {
			continue
		}
		s.coordinator.ProactiveRefreshIfNeeded(ctx, acc.ID, acc.Credential.AccessToken, acc.Credential.ExpiresAt)
	}
}

// NextProactiveTiming returns the duration until the next proactive refresh.
func (s *Service) NextProactiveTiming(ctx context.Context, accountID uuid.UUID) *time.Duration {
	cred, err := s.store.GetCredential(ctx, accountID)
	if err != nil || cred == nil {
		return nil
	}
	return s.coordinator.NextProactiveRefresh(cred.ExpiresAt)
}

// SingleFlight exposes the coordinator's singleflight for external use.
func (s *Service) SingleFlight() *enginerefresh.SingleFlight {
	return s.coordinator.SingleFlight()
}
