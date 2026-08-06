package providers

import (
	"context"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/provider"
	enginerouting "gorouter/internal/engine/routing"
)

// Scope is the transaction-scoped persistence surface used by the providers
// application services. *tx.TxScope satisfies it, so services can be wired
// directly to TransactionManager.Begin output.
type Scope interface {
	Pools() provider.PoolRepository
	Nodes() enginerouting.NodeStore
	Providers() provider.ProviderRepository
	Accounts() provider.AccountRepository
	AuditLog() tx.AuditLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ Scope = (*tx.TxScope)(nil)

// ScopeBeginner begins a transaction scope. It is an interface so tests can
// drive the services with recording scopes.
type ScopeBeginner interface {
	Begin(ctx context.Context) (Scope, error)
}

// ScopeBeginnerFunc adapts a function to ScopeBeginner.
type ScopeBeginnerFunc func(ctx context.Context) (Scope, error)

// Begin implements ScopeBeginner.
func (f ScopeBeginnerFunc) Begin(ctx context.Context) (Scope, error) { return f(ctx) }
