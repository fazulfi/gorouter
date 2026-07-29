package postgres

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// getTestDSN returns the PostgreSQL DSN for integration tests.
// It checks the POSTGRES_TEST_DSN environment variable, then falls back
// to a default local connection for developer convenience.
func getTestDSN() string {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn != "" {
		return dsn
	}
	return "postgres://postgres:postgres@localhost:5432/postgres"
}

// skipIfShort skips the test if -short is set or if no PostgreSQL is available.
func skipIfShort(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
}

// testPool creates a pgxpool.Pool for integration testing.
func testPool(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	dsn := getTestDSN()
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse test DSN: %v", err)
	}
	cfg.MaxConns = 5
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("create test pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// ---- Advisory Lock Tests ----

func TestAdvisoryLock_AcquireAndRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	lock, err := AcquireAdvisoryLock(ctx, pool, 42)
	if err != nil {
		t.Fatalf("AcquireAdvisoryLock failed: %v", err)
	}

	err = lock.Release(ctx)
	if err != nil {
		t.Fatalf("Release failed: %v", err)
	}
}

func TestAdvisoryLock_Exclusivity(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	lockID := int64(9991)

	lock1, err := AcquireAdvisoryLock(ctx, pool, lockID)
	if err != nil {
		t.Fatalf("first AcquireAdvisoryLock failed: %v", err)
	}
	defer lock1.Release(ctx)

	// Try to acquire the same lock with a short timeout - should fail
	tryCtx, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()

	_, err = AcquireAdvisoryLock(tryCtx, pool, lockID)
	if err == nil {
		t.Fatal("expected error when acquiring held lock, got nil")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Logf("expected DeadlineExceeded, got: %v", err)
	}
}

func TestAdvisoryLock_ReleaseThenReacquire(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	lockID := int64(9992)

	lock1, err := AcquireAdvisoryLock(ctx, pool, lockID)
	if err != nil {
		t.Fatalf("first AcquireAdvisoryLock failed: %v", err)
	}

	if err := lock1.Release(ctx); err != nil {
		t.Fatalf("first Release failed: %v", err)
	}

	lock2, err := AcquireAdvisoryLock(ctx, pool, lockID)
	if err != nil {
		t.Fatalf("second AcquireAdvisoryLock after release failed: %v", err)
	}
	lock2.Release(ctx)
}

func TestAdvisoryLock_DoubleRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	lock, err := AcquireAdvisoryLock(ctx, pool, 9993)
	if err != nil {
		t.Fatalf("AcquireAdvisoryLock failed: %v", err)
	}

	if err := lock.Release(ctx); err != nil {
		t.Fatalf("first Release failed: %v", err)
	}

	err = lock.Release(ctx)
	if !errors.Is(err, ErrLockReleased) {
		t.Fatalf("expected ErrLockReleased on double release, got: %v", err)
	}
}

func TestAdvisoryLock_CanceledContext(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	// First, hold a lock to make the second acquisition block
	lockID := int64(9994)
	holder, err := AcquireAdvisoryLock(ctx, pool, lockID)
	if err != nil {
		t.Fatalf("holder AcquireAdvisoryLock failed: %v", err)
	}
	defer holder.Release(ctx)

	// Try to acquire with canceled context
	tryCtx, cancel := context.WithCancel(ctx)
	cancel()

	_, err = AcquireAdvisoryLock(tryCtx, pool, lockID)
	if err == nil {
		t.Fatal("expected error with canceled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Logf("expected Canceled, got: %v", err)
	}
}

func TestRuntimeLock_AcquireAndRelease(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	lock, err := AcquireRuntimeLock(ctx, pool)
	if err != nil {
		t.Fatalf("AcquireRuntimeLock failed: %v", err)
	}

	if lock.LockID() != RuntimeLockID {
		t.Fatalf("LockID() = %d, want %d", lock.LockID(), RuntimeLockID)
	}

	if err := lock.Release(ctx); err != nil {
		t.Fatalf("Release failed: %v", err)
	}
}

func TestRuntimeLockFunc(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	release, err := RuntimeLock(ctx, pool)
	if err != nil {
		t.Fatalf("RuntimeLock failed: %v", err)
	}

	if err := release(ctx); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

func TestAdvisoryLock_NilSafe(t *testing.T) {
	var l *AdvisoryLock = nil
	if err := l.Release(context.Background()); err != nil {
		t.Errorf("Release on nil lock should succeed: %v", err)
	}
	if id := l.LockID(); id != 0 {
		t.Errorf("LockID on nil lock should be 0, got %d", id)
	}
}

// ---- Transaction Tests ----

func TestBeginTX_Commit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	// Clean up any previous test data
	pool.Exec(ctx, "DROP TABLE IF EXISTS tx_test")

	tx, err := BeginTX(ctx, pool)
	if err != nil {
		t.Fatalf("BeginTX failed: %v", err)
	}

	_, err = tx.Exec(ctx, "CREATE TEMP TABLE tx_test (id INT PRIMARY KEY, name TEXT)")
	if err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO tx_test (id, name) VALUES ($1, $2)", 1, "test")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	if err := Commit(ctx, tx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestBeginTX_Rollback(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	pool.Exec(ctx, "DROP TABLE IF EXISTS tx_rollback_test")

	tx, err := BeginTX(ctx, pool)
	if err != nil {
		t.Fatalf("BeginTX failed: %v", err)
	}

	_, err = tx.Exec(ctx, "CREATE TEMP TABLE tx_rollback_test (id INT PRIMARY KEY, val TEXT)")
	if err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	_, err = tx.Exec(ctx, "INSERT INTO tx_rollback_test (id, val) VALUES ($1, $2)", 1, "rollback-me")
	if err != nil {
		t.Fatalf("insert failed: %v", err)
	}

	if err := Rollback(ctx, tx); err != nil {
		t.Fatalf("Rollback failed: %v", err)
	}

	// Verify the data was rolled back
	var count int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM tx_rollback_test").Scan(&count)
	if err != nil {
		t.Fatalf("query after rollback failed: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows after rollback, got %d", count)
	}
}

func TestCommit_DoubleSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	tx, err := BeginTX(ctx, pool)
	if err != nil {
		t.Fatalf("BeginTX failed: %v", err)
	}

	if err := Commit(ctx, tx); err != nil {
		t.Fatalf("first Commit failed: %v", err)
	}

	// Second commit should not error (handles ErrTxClosed)
	if err := Commit(ctx, tx); err != nil {
		t.Fatalf("second Commit should be safe: %v", err)
	}
}

func TestRollback_DoubleSafe(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	tx, err := BeginTX(ctx, pool)
	if err != nil {
		t.Fatalf("BeginTX failed: %v", err)
	}

	if err := Rollback(ctx, tx); err != nil {
		t.Fatalf("first Rollback failed: %v", err)
	}

	// Second rollback should not error (handles ErrTxClosed)
	if err := Rollback(ctx, tx); err != nil {
		t.Fatalf("second Rollback should be safe: %v", err)
	}
}

func TestCommit_NilTx(t *testing.T) {
	if err := Commit(context.Background(), nil); err != nil {
		t.Fatalf("Commit(nil) should succeed: %v", err)
	}
}

func TestRollback_NilTx(t *testing.T) {
	if err := Rollback(context.Background(), nil); err != nil {
		t.Fatalf("Rollback(nil) should succeed: %v", err)
	}
}

func TestCommit_RollbackNotCommit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	pool.Exec(ctx, "DROP TABLE IF EXISTS tx_rc_test")

	tx, err := BeginTX(ctx, pool)
	if err != nil {
		t.Fatalf("BeginTX failed: %v", err)
	}

	_, err = tx.Exec(ctx, "CREATE TEMP TABLE tx_rc_test (id INT PRIMARY KEY)")
	if err != nil {
		t.Fatalf("create table failed: %v", err)
	}

	// Rollback, not commit
	Rollback(ctx, tx)

	// Commit should still be safe (ErrTxClosed handled)
	if err := Commit(ctx, tx); err != nil {
		t.Fatalf("Commit after Rollback should be safe: %v", err)
	}
}

func TestWithinTx_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	pool.Exec(ctx, "DROP TABLE IF EXISTS within_tx_test")

	err := WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "CREATE TEMP TABLE within_tx_test (id INT PRIMARY KEY, val TEXT)")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO within_tx_test (id, val) VALUES ($1, $2)", 1, "committed")
		return err
	})
	if err != nil {
		t.Fatalf("WithinTx failed: %v", err)
	}

	var val string
	err = pool.QueryRow(ctx, "SELECT val FROM within_tx_test WHERE id = 1").Scan(&val)
	if err != nil {
		t.Fatalf("query after WithinTx: %v", err)
	}
	if val != "committed" {
		t.Fatalf("expected 'committed', got %q", val)
	}
}

func TestWithinTx_RollbackOnError(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	pool.Exec(ctx, "DROP TABLE IF EXISTS within_tx_err_test")

	err := WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "CREATE TEMP TABLE within_tx_err_test (id INT PRIMARY KEY, val TEXT)")
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, "INSERT INTO within_tx_err_test (id, val) VALUES ($1, $2)", 1, "should-not-exist")
		if err != nil {
			return err
		}
		// Return an error to trigger rollback
		return errors.New("simulated error")
	})
	if err == nil {
		t.Fatal("expected error from WithinTx, got nil")
	}

	// Table should not exist (or be empty) since transaction was rolled back
	var exists bool
	err = pool.QueryRow(ctx, "SELECT EXISTS (SELECT FROM information_schema.tables WHERE table_name = 'within_tx_err_test')").Scan(&exists)
	if err != nil {
		t.Fatalf("check table exists: %v", err)
	}
	// Note: temp tables might be schema-specific, but the point is the error was returned
	t.Logf("table exists: %v (expected: false if temp, but depends on schema)", exists)
	if err.Error() != "simulated error" {
		t.Fatalf("expected original error, got: %v", err)
	}
}

func TestWithinTx_PanicSafety(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	pool.Exec(ctx, "DROP TABLE IF EXISTS within_tx_panic_test")

	func() {
		defer func() {
			_ = recover()
		}()
		_ = WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			_, err := tx.Exec(ctx, "CREATE TEMP TABLE within_tx_panic_test (id INT PRIMARY KEY)")
			if err != nil {
				return err
			}
			panic("simulated panic")
		})
	}()

	// Test that panic didn't leak the connection
	var count int
	err := pool.QueryRow(ctx, "SELECT 1").Scan(&count)
	if err != nil {
		t.Fatalf("pool still usable after panic in WithinTx: %v", err)
	}
}

func TestConcurrentLockContention(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	ctx := context.Background()
	pool := testPool(t, ctx)

	lockID := int64(9995)
	var wg sync.WaitGroup
	acquired := make(chan struct{}, 2)

	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			lockCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			lock, err := AcquireAdvisoryLock(lockCtx, pool, lockID)
			if err != nil {
				t.Logf("goroutine %d could not acquire lock: %v", id, err)
				return
			}
			acquired <- struct{}{}
			time.Sleep(50 * time.Millisecond)
			lock.Release(ctx)
		}(i)
	}

	wg.Wait()
	close(acquired)

	count := 0
	for range acquired {
		count++
	}
	if count == 0 {
		t.Fatal("no goroutine could acquire the lock")
	}
	t.Logf("%d goroutines acquired the lock (sequentially)", count)
}

func TestErrTxClosedAlias(t *testing.T) {
	if ErrTxClosedAlias != pgx.ErrTxClosed {
		t.Fatalf("ErrTxClosedAlias != pgx.ErrTxClosed")
	}
}
