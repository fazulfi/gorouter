package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type mockTx struct {
	commitErr   error
	rollbackErr error
}

func (m *mockTx) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("mockTx.Begin not expected")
}

func (m *mockTx) BeginFunc(ctx context.Context, f func(pgx.Tx) error) error {
	panic("mockTx.BeginFunc not expected")
}

func (m *mockTx) Commit(ctx context.Context) error {
	return m.commitErr
}

func (m *mockTx) Rollback(ctx context.Context) error {
	return m.rollbackErr
}

func (m *mockTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("mockTx.CopyFrom not expected")
}

func (m *mockTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	panic("mockTx.Exec not expected")
}

func (m *mockTx) LargeObjects() pgx.LargeObjects {
	panic("mockTx.LargeObjects not expected")
}

func (m *mockTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	panic("mockTx.Prepare not expected")
}

func (m *mockTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	panic("mockTx.Query not expected")
}

func (m *mockTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	panic("mockTx.QueryRow not expected")
}

func (m *mockTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("mockTx.SendBatch not expected")
}

func (m *mockTx) Conn() *pgx.Conn {
	panic("mockTx.Conn not expected")
}

func TestCommit_Success(t *testing.T) {
	tx := &mockTx{}
	err := Commit(context.Background(), tx)
	if err != nil {
		t.Errorf("Commit() expected nil, got: %v", err)
	}
}

func TestCommit_ErrTxClosed(t *testing.T) {
	tx := &mockTx{commitErr: pgx.ErrTxClosed}
	err := Commit(context.Background(), tx)
	if err != nil {
		t.Errorf("Commit() expected nil (suppressed ErrTxClosed), got: %v", err)
	}
}

func TestCommit_Error(t *testing.T) {
	expected := errors.New("connection lost")
	tx := &mockTx{commitErr: expected}
	err := Commit(context.Background(), tx)
	if err == nil {
		t.Fatal("Commit() expected error, got nil")
	}
	if !errors.Is(err, expected) {
		t.Errorf("Commit() error should wrap original, got: %v", err)
	}
}

func TestCommit_WrapsError(t *testing.T) {
	expected := errors.New("disk full")
	tx := &mockTx{commitErr: expected}
	err := Commit(context.Background(), tx)
	if err == nil {
		t.Fatal("Commit() expected error, got nil")
	}
	if !contains(err.Error(), "commit tx") {
		t.Errorf("Commit() error = %q, want 'commit tx' prefix", err.Error())
	}
	if !contains(err.Error(), "disk full") {
		t.Errorf("Commit() error = %q, want wrapped 'disk full'", err.Error())
	}
}

func TestRollback_Success(t *testing.T) {
	tx := &mockTx{}
	err := Rollback(context.Background(), tx)
	if err != nil {
		t.Errorf("Rollback() expected nil, got: %v", err)
	}
}

func TestRollback_ErrTxClosed(t *testing.T) {
	tx := &mockTx{rollbackErr: pgx.ErrTxClosed}
	err := Rollback(context.Background(), tx)
	if err != nil {
		t.Errorf("Rollback() expected nil (suppressed ErrTxClosed), got: %v", err)
	}
}

func TestRollback_Error(t *testing.T) {
	expected := errors.New("connection terminated")
	tx := &mockTx{rollbackErr: expected}
	err := Rollback(context.Background(), tx)
	if err == nil {
		t.Fatal("Rollback() expected error, got nil")
	}
	if !errors.Is(err, expected) {
		t.Errorf("Rollback() error should wrap original, got: %v", err)
	}
}

func TestRollback_WrapsError(t *testing.T) {
	expected := errors.New("cancelled")
	tx := &mockTx{rollbackErr: expected}
	err := Rollback(context.Background(), tx)
	if err == nil {
		t.Fatal("Rollback() expected error, got nil")
	}
	if !contains(err.Error(), "rollback tx") {
		t.Errorf("Rollback() error = %q, want 'rollback tx' prefix", err.Error())
	}
	if !contains(err.Error(), "cancelled") {
		t.Errorf("Rollback() error = %q, want wrapped 'cancelled'", err.Error())
	}
}

func TestBeginTX_Error(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("cannot create broken pool: %v", err)
	}
	defer pool.Close()

	_, err = BeginTX(context.Background(), pool)
	if err == nil {
		t.Error("BeginTX with broken pool expected error, got nil")
	}
	if !contains(err.Error(), "begin tx") {
		t.Errorf("BeginTX() error = %q, want 'begin tx' prefix", err.Error())
	}
}

func TestWithinTx_BeginError(t *testing.T) {
	pool, err := brokenPool()
	if err != nil {
		t.Skipf("cannot create broken pool: %v", err)
	}
	defer pool.Close()

	err = WithinTx(context.Background(), pool, func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})
	if err == nil {
		t.Error("WithinTx with broken pool expected error, got nil")
	}
}

func TestErrTxClosedAlias_Match(t *testing.T) {
	if ErrTxClosedAlias != pgx.ErrTxClosed {
		t.Errorf("ErrTxClosedAlias = %v, want %v", ErrTxClosedAlias, pgx.ErrTxClosed)
	}
}
