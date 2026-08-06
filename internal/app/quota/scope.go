// Package quota provides the application service over the quota domain
// (P4-T08): the per-provider quota status surface, the window countdown, the
// audited per-provider unlock and reset mutations, and the state-recording
// surface the CH-08 auto-ping job calls. Cooldown timing reuses
// internal/app/cooldown; no parallel cooldown store exists.
package quota

import (
	"context"

	"gorouter/internal/app/tx"
)

// QuotaScope is the transaction-scoped persistence surface used by the quota
// application service. *tx.TxScope satisfies it, so the service can be wired
// directly to TransactionManager.Begin output.
type QuotaScope interface {
	AuditLog() tx.AuditLogRepository
	Commit(ctx context.Context) error
	Rollback(ctx context.Context) error
}

var _ QuotaScope = (*tx.TxScope)(nil)

// QuotaScopeBeginner begins a quota transaction scope. It is an interface so
// tests can drive the service with recording scopes.
type QuotaScopeBeginner interface {
	Begin(ctx context.Context) (QuotaScope, error)
}

// QuotaScopeBeginnerFunc adapts a function to QuotaScopeBeginner.
type QuotaScopeBeginnerFunc func(ctx context.Context) (QuotaScope, error)

// Begin implements QuotaScopeBeginner.
func (f QuotaScopeBeginnerFunc) Begin(ctx context.Context) (QuotaScope, error) { return f(ctx) }
