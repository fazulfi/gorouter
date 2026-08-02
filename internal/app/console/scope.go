// Package console provides application services over the console domain:
// the redacted console-log projection (durable, strictly ordered, 90-day
// retention) consumed by the realtime console surface.
package console

import (
	"context"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/console"
)

// ConsoleScope is the transaction-scoped persistence surface used by the
// console application services. *tx.TxScope satisfies it, so services can
// be wired directly to TransactionManager.Begin output.
type ConsoleScope interface {
	ConsoleLogs() console.ConsoleLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ ConsoleScope = (*tx.TxScope)(nil)

// ConsoleScopeBeginner begins a console transaction scope. It is an
// interface so tests can drive the services with recording scopes.
type ConsoleScopeBeginner interface {
	Begin(ctx context.Context) (ConsoleScope, error)
}

// ConsoleScopeBeginnerFunc adapts a function to ConsoleScopeBeginner.
type ConsoleScopeBeginnerFunc func(ctx context.Context) (ConsoleScope, error)

// Begin implements ConsoleScopeBeginner.
func (f ConsoleScopeBeginnerFunc) Begin(ctx context.Context) (ConsoleScope, error) { return f(ctx) }
