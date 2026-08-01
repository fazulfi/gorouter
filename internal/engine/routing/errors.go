package routing

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
)

var (
	errAccountRepoNotConfigured = fmt.Errorf("routing: account repository not configured")
)

func errAccountFind(err error) error {
	return fmt.Errorf("routing: find accounts: %w", err)
}

func errNoAccountsForProvider(id uuid.UUID) error {
	return fmt.Errorf("routing: no accounts for provider %s", id)
}

func errNoAvailableAccounts(id uuid.UUID) error {
	return fmt.Errorf("routing: no available accounts for provider %s", id)
}

// Sentinel errors for routing decisions.
var (
	ErrNoFallbackAvailable = errors.New("routing: no fallback account available")
	ErrTerminalFailure     = errors.New("routing: terminal failure, no fallback")
	ErrMaxAttemptsReached  = errors.New("routing: max fallback attempts reached")
	ErrNoEligibleAccount   = errors.New("routing: no eligible account found")
)
