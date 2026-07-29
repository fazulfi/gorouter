package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// RuntimeLockID is the advisory lock ID used to enforce single-runtime
// exclusivity per PostgreSQL database.
const RuntimeLockID int64 = 0x474F524F55544552 // "GOROUTER" as int64

// ErrLockNotAcquired is returned when TryAcquireAdvisoryLock cannot acquire the lock.
var ErrLockNotAcquired = errors.New("advisory lock not acquired")

// ErrLockReleased is returned when the lock has already been released.
var ErrLockReleased = errors.New("advisory lock already released")

// AdvisoryLock represents a held PostgreSQL advisory lock.
type AdvisoryLock struct {
	conn    *pgxpool.Conn
	lockID  int64
	released bool
}

// AcquireRuntimeLock acquires the runtime exclusivity advisory lock.
// It uses pg_try_advisory_lock in a polling loop to support context cancellation.
// Returns a release function and an error. The second process attempting to
// acquire the same lock will block until the context expires.
func AcquireRuntimeLock(ctx context.Context, pool *pgxpool.Pool) (*AdvisoryLock, error) {
	return AcquireAdvisoryLock(ctx, pool, RuntimeLockID)
}

// AcquireAdvisoryLock acquires a PostgreSQL session-level advisory lock.
// It uses pg_try_advisory_lock in a polling loop with 100ms between attempts
// to support context cancellation. The returned AdvisoryLock must be released
// via Release or the connection will hold the lock until the session ends.
func AcquireAdvisoryLock(ctx context.Context, pool *pgxpool.Pool, lockID int64) (*AdvisoryLock, error) {
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("acquire connection for advisory lock %d: %w", lockID, err)
	}

	lock := &AdvisoryLock{
		conn:   conn,
		lockID: lockID,
	}

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			conn.Release()
			return nil, fmt.Errorf("acquire advisory lock %d: %w", lockID, ctx.Err())
		default:
		}

		var acquired bool
		err := conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", lockID).Scan(&acquired)
		if err != nil {
			conn.Release()
			return nil, fmt.Errorf("acquire advisory lock %d: %w", lockID, err)
		}
		if acquired {
			return lock, nil
		}

		// Wait before retrying
		select {
		case <-ctx.Done():
			conn.Release()
			return nil, fmt.Errorf("acquire advisory lock %d: %w", lockID, ctx.Err())
		case <-ticker.C:
		}
	}
}

// Release releases the advisory lock and returns the connection to the pool.
// Safe to call multiple times - subsequent calls return ErrLockReleased.
func (l *AdvisoryLock) Release(ctx context.Context) error {
	if l == nil {
		return nil
	}
	if l.released {
		return ErrLockReleased
	}
	l.released = true

	// Try to unlock, but proceed with release even if unlock fails
	_, err := l.conn.Exec(ctx, "SELECT pg_advisory_unlock($1)", l.lockID)
	l.conn.Release()
	if err != nil {
		return fmt.Errorf("release advisory lock %d: %w", l.lockID, err)
	}
	return nil
}

// LockID returns the advisory lock ID.
func (l *AdvisoryLock) LockID() int64 {
	if l == nil {
		return 0
	}
	return l.lockID
}

// RuntimeLock acquires the runtime lock and returns a release function.
// This is a convenience wrapper matching the original plan interface.
func RuntimeLock(ctx context.Context, pool *pgxpool.Pool) (release func(context.Context) error, err error) {
	lock, err := AcquireRuntimeLock(ctx, pool)
	if err != nil {
		return nil, err
	}
	return lock.Release, nil
}
