package postgres

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrTxClosedAlias is provided for convenience, referencing pgx.ErrTxClosed.
// It allows callers to check for closed transactions without importing pgx directly.
var ErrTxClosedAlias = pgx.ErrTxClosed

// BeginTX starts a new transaction from the pool.
func BeginTX(ctx context.Context, pool *pgxpool.Pool) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return tx, nil
}

// Commit commits the transaction. If the transaction is already closed
// (pgx.ErrTxClosed), it returns nil (safe to call multiple times).
func Commit(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return nil
	}
	err := tx.Commit(ctx)
	if err != nil && errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

// Rollback rolls back the transaction. If the transaction is already closed
// (pgx.ErrTxClosed), it returns nil (safe to call multiple times).
func Rollback(ctx context.Context, tx pgx.Tx) error {
	if tx == nil {
		return nil
	}
	err := tx.Rollback(ctx)
	if err != nil && errors.Is(err, pgx.ErrTxClosed) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("rollback tx: %w", err)
	}
	return nil
}

// WithinTx executes fn within a transaction. If fn returns an error,
// the transaction is rolled back and the original error is returned.
// If the transaction commits successfully, nil is returned.
func WithinTx(ctx context.Context, pool *pgxpool.Pool, fn func(context.Context, pgx.Tx) error) error {
	tx, err := BeginTX(ctx, pool)
	if err != nil {
		return err
	}

	// Deferred rollback only on panic or if not committed
	rolledBack := false
	defer func() {
		if !rolledBack {
			_ = Rollback(ctx, tx)
		}
	}()

	if err := fn(ctx, tx); err != nil {
		rolledBack = true
		// Attempt rollback, return original error if rollback succeeds
		if rbErr := Rollback(ctx, tx); rbErr != nil {
			return fmt.Errorf("within tx: %v (rollback error: %w)", err, rbErr)
		}
		return err
	}

	if err := Commit(ctx, tx); err != nil {
		rolledBack = true
		return fmt.Errorf("within tx: %w", err)
	}

	rolledBack = true
	return nil
}
