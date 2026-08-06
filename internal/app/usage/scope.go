// Package usage provides application services over the BE-06 usage domain:
// the usage pipeline (immutable per-request recording and idempotent daily
// aggregation), the durable request-history service, the process-local Recent
// Requests ring (audit 13), and 90-day usage retention.
package usage

import (
	"context"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/usage"
)

// UsageScope is the transaction-scoped persistence surface used by the usage
// application services. *tx.TxScope satisfies it, so services can be wired
// directly to TransactionManager.Begin output.
type UsageScope interface {
	Usage() usage.UsageRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ UsageScope = (*tx.TxScope)(nil)

// UsageScopeBeginner begins a usage transaction scope. It is an interface so
// tests can drive the services with recording scopes.
type UsageScopeBeginner interface {
	Begin(ctx context.Context) (UsageScope, error)
}

// UsageScopeBeginnerFunc adapts a function to UsageScopeBeginner.
type UsageScopeBeginnerFunc func(ctx context.Context) (UsageScope, error)

// Begin implements UsageScopeBeginner.
func (f UsageScopeBeginnerFunc) Begin(ctx context.Context) (UsageScope, error) { return f(ctx) }
