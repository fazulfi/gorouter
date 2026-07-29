package postgres

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgproto3"
	"github.com/jackc/pgx/v5/pgxpool"
)

type mockColumn struct {
	name    string
	typeOID uint32
	size    int16
}

type mockQueryResult struct {
	columns    []mockColumn
	rows       [][]string
	commandTag string
	err        string
	paramOIDs  []uint32
}

type queryHandlerFunc func(query string) *mockQueryResult

type connState struct {
	query  string
	result *mockQueryResult
}

func newTestPool(t *testing.T, handler queryHandlerFunc) *pgxpool.Pool {
	t.Helper()

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("mock PG listen: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		if err := conn.SetDeadline(time.Now().Add(30 * time.Second)); err != nil {
			t.Logf("mock PG: set deadline error: %v", err)
			return
		}

		backend := pgproto3.NewBackend(conn, conn)
		if err := handlePGConnection(ctx, backend, handler); err != nil {
			t.Logf("mock PG connection closed: %v", err)
		}
	}()

	host, portStr, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("parse addr: %v", err)
	}

	dsn := fmt.Sprintf("sslmode=disable host=%s port=%s connect_timeout=5", host, portStr)
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}

	cfg.ConnConfig.DefaultQueryExecMode = pgx.QueryExecModeCacheDescribe
	cfg.MaxConns = 1
	cfg.MinConns = 0
	cfg.HealthCheckPeriod = time.Hour
	cfg.MaxConnLifetime = time.Hour
	cfg.MaxConnIdleTime = time.Hour

	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
		ln.Close()
	})

	return pool
}

func handlePGConnection(ctx context.Context, backend *pgproto3.Backend, handler queryHandlerFunc) error {
	_, err := backend.ReceiveStartupMessage()
	if err != nil {
		return fmt.Errorf("startup: %w", err)
	}

	backend.Send(&pgproto3.AuthenticationOk{})
	backend.Send(&pgproto3.ParameterStatus{Name: "client_encoding", Value: "UTF8"})
	backend.Send(&pgproto3.BackendKeyData{ProcessID: 42, SecretKey: []byte{0, 0, 48, 57}})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	if err := backend.Flush(); err != nil {
		return fmt.Errorf("startup flush: %w", err)
	}

	state := &connState{}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		msg, err := backend.Receive()
		if err != nil {
			return fmt.Errorf("receive: %w", err)
		}

		switch m := msg.(type) {
		case *pgproto3.Query:
			result := handler(m.String)
			if err := sendQueryResponse(backend, result); err != nil {
				return err
			}

		case *pgproto3.Parse:
			state.query = m.Query
			state.result = handler(m.Query)
			backend.Send(&pgproto3.ParseComplete{})
			if err := backend.Flush(); err != nil {
				return err
			}

		case *pgproto3.Describe:
			if err := handleDescribe(backend, state, m); err != nil {
				return err
			}

		case *pgproto3.Bind:
			backend.Send(&pgproto3.BindComplete{})
			if err := backend.Flush(); err != nil {
				return err
			}

		case *pgproto3.Execute:
			if err := handleExecute(backend, state); err != nil {
				return err
			}

		case *pgproto3.Sync:
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			if err := backend.Flush(); err != nil {
				return err
			}

		case *pgproto3.Terminate:
			return nil

		default:
			return fmt.Errorf("unexpected message type: %T", msg)
		}
	}
}

func sendQueryResponse(backend *pgproto3.Backend, result *mockQueryResult) error {
	if result == nil {
		backend.Send(&pgproto3.ErrorResponse{
			Severity: "ERROR",
			Message:  "unexpected query",
		})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return backend.Flush()
	}

	if result.err != "" {
		backend.Send(&pgproto3.ErrorResponse{
			Severity: "ERROR",
			Message:  result.err,
		})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		return backend.Flush()
	}

	if len(result.columns) > 0 {
		backend.Send(newRowDescription(result.columns))
	}

	for _, row := range result.rows {
		values := make([][]byte, len(row))
		for i, v := range row {
			values[i] = []byte(v)
		}
		backend.Send(&pgproto3.DataRow{Values: values})
	}

	tag := result.commandTag
	if tag == "" {
		tag = "SELECT 0"
	}
	backend.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)})
	backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
	return backend.Flush()
}

func handleDescribe(backend *pgproto3.Backend, state *connState, desc *pgproto3.Describe) error {
	result := state.result
	paramOIDs := []uint32{20}

	if desc.ObjectType == 'S' {
		if result != nil && len(result.paramOIDs) > 0 {
			paramOIDs = result.paramOIDs
		}
		backend.Send(&pgproto3.ParameterDescription{ParameterOIDs: paramOIDs})
		if result != nil && len(result.columns) > 0 {
			backend.Send(newRowDescription(result.columns))
		} else {
			backend.Send(&pgproto3.NoData{})
		}
	} else if desc.ObjectType == 'P' {
		if result != nil && len(result.columns) > 0 {
			backend.Send(newRowDescription(result.columns))
		} else {
			backend.Send(&pgproto3.NoData{})
		}
	}

	return backend.Flush()
}

func handleExecute(backend *pgproto3.Backend, state *connState) error {
	result := state.result
	if result == nil {
		backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("SELECT 0")})
		return backend.Flush()
	}

	if result.err != "" {
		backend.Send(&pgproto3.ErrorResponse{
			Severity: "ERROR",
			Message:  result.err,
		})
		return backend.Flush()
	}

	for _, row := range result.rows {
		values := make([][]byte, len(row))
		for i, v := range row {
			values[i] = []byte(v)
		}
		backend.Send(&pgproto3.DataRow{Values: values})
	}

	tag := result.commandTag
	if tag == "" {
		tag = "SELECT 0"
	}
	backend.Send(&pgproto3.CommandComplete{CommandTag: []byte(tag)})
	return backend.Flush()
}

func newRowDescription(columns []mockColumn) *pgproto3.RowDescription {
	fds := make([]pgproto3.FieldDescription, len(columns))
	for i, c := range columns {
		fds[i] = pgproto3.FieldDescription{
			Name:                 []byte(c.name),
			TableOID:             0,
			TableAttributeNumber: 0,
			DataTypeOID:          c.typeOID,
			DataTypeSize:         c.size,
			TypeModifier:         -1,
			Format:               0,
		}
	}
	return &pgproto3.RowDescription{Fields: fds}
}

func mockOKResult(tag string) *mockQueryResult {
	return &mockQueryResult{commandTag: tag}
}

func mockBoolResult(val bool) *mockQueryResult {
	rowVal := "f"
	if val {
		rowVal = "t"
	}
	return &mockQueryResult{
		columns: []mockColumn{
			{name: "pg_try_advisory_lock", typeOID: 16, size: 1},
		},
		rows:       [][]string{{rowVal}},
		commandTag: "SELECT 1",
		paramOIDs:  []uint32{20},
	}
}

func mockErrorResult(msg string) *mockQueryResult {
	return &mockQueryResult{err: msg}
}

// Lock tests

func TestAcquireAdvisoryLock_Success(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockBoolResult(true)
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	lock, err := AcquireAdvisoryLock(ctx, pool, 42)
	if err != nil {
		t.Fatalf("AcquireAdvisoryLock failed: %v", err)
	}
	defer lock.Release(ctx)

	if lock.LockID() != 42 {
		t.Errorf("LockID = %d, want 42", lock.LockID())
	}
}

func TestAcquireAdvisoryLock_QueryRowError(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockErrorResult("query error")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	_, err := AcquireAdvisoryLock(ctx, pool, 42)
	if err == nil {
		t.Fatal("AcquireAdvisoryLock expected error, got nil")
	}
}

func TestAcquireAdvisoryLock_NotAcquiredThenCancel(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockBoolResult(false)
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := AcquireAdvisoryLock(ctx, pool, 42)
	if err == nil {
		t.Fatal("AcquireAdvisoryLock expected timeout error, got nil")
	}
}

func TestAdvisoryLock_Release_Success(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockBoolResult(true)
		case "SELECT pg_advisory_unlock($1)":
			return mockBoolResult(true)
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	lock, err := AcquireAdvisoryLock(ctx, pool, 42)
	if err != nil {
		t.Fatalf("AcquireAdvisoryLock failed: %v", err)
	}

	if err := lock.Release(ctx); err != nil {
		t.Fatalf("Release failed: %v", err)
	}

	if err := lock.Release(ctx); err != ErrLockReleased {
		t.Fatalf("expected ErrLockReleased, got: %v", err)
	}
}

func TestAdvisoryLock_Release_ExecError(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockBoolResult(true)
		case "SELECT pg_advisory_unlock($1)":
			return mockErrorResult("unlock failed")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	lock, err := AcquireAdvisoryLock(ctx, pool, 42)
	if err != nil {
		t.Fatalf("AcquireAdvisoryLock failed: %v", err)
	}

	err = lock.Release(ctx)
	if err == nil {
		t.Fatal("Release expected error for failed unlock, got nil")
	}
	if !contains(err.Error(), "release advisory lock") {
		t.Errorf("Release error = %q, want 'release advisory lock' prefix", err.Error())
	}
}

func TestRuntimeLock_Success(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockBoolResult(true)
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	release, err := RuntimeLock(ctx, pool)
	if err != nil {
		t.Fatalf("RuntimeLock failed: %v", err)
	}
	if release == nil {
		t.Fatal("RuntimeLock returned nil release func")
	}
	if err := release(ctx); err != nil {
		t.Fatalf("release failed: %v", err)
	}
}

func TestAcquireRuntimeLock_Success(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "SELECT pg_try_advisory_lock($1)":
			return mockBoolResult(true)
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	lock, err := AcquireRuntimeLock(ctx, pool)
	if err != nil {
		t.Fatalf("AcquireRuntimeLock failed: %v", err)
	}
	defer lock.Release(ctx)

	if lock.LockID() != RuntimeLockID {
		t.Errorf("LockID = %d, want %d", lock.LockID(), RuntimeLockID)
	}
}

// Transaction tests

func TestBeginTX_MockedSuccess(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "begin":
			return mockOKResult("BEGIN")
		case "commit":
			return mockOKResult("COMMIT")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	tx, err := BeginTX(ctx, pool)
	if err != nil {
		t.Fatalf("BeginTX failed: %v", err)
	}

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
}

func TestWithinTx_MockedSuccess(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "begin":
			return mockOKResult("BEGIN")
		case "commit":
			return mockOKResult("COMMIT")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	err := WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})
	if err != nil {
		t.Fatalf("WithinTx failed: %v", err)
	}
}

func TestWithinTx_FnError(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "begin":
			return mockOKResult("BEGIN")
		case "rollback":
			return mockOKResult("ROLLBACK")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	expectedErr := fmt.Errorf("function error")
	err := WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		return expectedErr
	})
	if err == nil {
		t.Fatal("WithinTx expected error, got nil")
	}
	if !contains(err.Error(), "function error") {
		t.Errorf("WithinTx error = %q, want containing 'function error'", err.Error())
	}
}

func TestWithinTx_FnErrorRollbackFails(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "begin":
			return mockOKResult("BEGIN")
		case "rollback":
			return mockErrorResult("rollback failed")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	err := WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		return fmt.Errorf("fn error")
	})
	if err == nil {
		t.Fatal("WithinTx expected error, got nil")
	}
	if !contains(err.Error(), "rollback error") {
		t.Errorf("WithinTx error = %q, want 'rollback error' wrapper", err.Error())
	}
}

func TestWithinTx_CommitFails(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "begin":
			return mockOKResult("BEGIN")
		case "commit":
			return mockErrorResult("commit failed")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()
	err := WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
		return nil
	})
	if err == nil {
		t.Fatal("WithinTx expected commit error, got nil")
	}
	if !contains(err.Error(), "commit tx") {
		t.Errorf("WithinTx error = %q, want 'commit tx' prefix", err.Error())
	}
}

func TestWithinTx_DeferredRollbackOnPanic(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "begin":
			return mockOKResult("BEGIN")
		case "rollback":
			return mockOKResult("ROLLBACK")
		default:
			return nil
		}
	})
	defer pool.Close()

	ctx := context.Background()

	func() {
		defer func() {
			_ = recover()
		}()

		_ = WithinTx(ctx, pool, func(ctx context.Context, tx pgx.Tx) error {
			panic("test panic")
		})
	}()
}

// Pool tests

func TestPool_Ping_Success(t *testing.T) {
	pool := newTestPool(t, func(query string) *mockQueryResult {
		switch query {
		case "-- ping":
			return &mockQueryResult{commandTag: "PING"}
		default:
			return nil
		}
	})
	defer pool.Close()

	p := &Pool{pgxPool: pool}
	err := p.Ping(context.Background())
	if err != nil {
		t.Fatalf("Ping failed: %v", err)
	}
}

func TestPool_Ping_AcquireError(t *testing.T) {
	cfg, err := pgxpool.ParseConfig("postgres://localhost:1/postgres?connect_timeout=1")
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = 1
	cfg.MinConns = 0

	badPool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	defer badPool.Close()

	p := &Pool{pgxPool: badPool}
	err = p.Ping(context.Background())
	if err == nil {
		t.Fatal("Ping on unreachable pool expected error, got nil")
	}
}

func TestOpen_Success(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))

		backend := pgproto3.NewBackend(conn, conn)
		_, err = backend.ReceiveStartupMessage()
		if err != nil {
			return
		}
		backend.Send(&pgproto3.AuthenticationOk{})
		backend.Send(&pgproto3.BackendKeyData{ProcessID: 1, SecretKey: []byte{0, 0, 0, 1}})
		backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
		backend.Flush()

		msg, err := backend.Receive()
		if err != nil {
			return
		}
		if q, ok := msg.(*pgproto3.Query); ok && q.String == "-- ping" {
			backend.Send(&pgproto3.CommandComplete{CommandTag: []byte("PING")})
			backend.Send(&pgproto3.ReadyForQuery{TxStatus: 'I'})
			backend.Flush()
		}
	}()

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	dsn := fmt.Sprintf("postgres://test@%s:%s/test?sslmode=disable&connect_timeout=5", host, portStr)

	pool, err := Open(ctx, PoolConfig{
		DSN:                 dsn,
		MaxConns:            1,
		HealthCheckInterval: 30 * time.Second,
		ConnTimeout:         5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer pool.Close()

	if pool.Pool() == nil {
		t.Error("Pool() returned nil after Open")
	}
}

// Sentinel error tests

func TestErrLockNotAcquired_Sentinel(t *testing.T) {
	if ErrLockNotAcquired == nil {
		t.Fatal("ErrLockNotAcquired should not be nil")
	}
	if ErrLockNotAcquired.Error() != "advisory lock not acquired" {
		t.Errorf("ErrLockNotAcquired = %q, want 'advisory lock not acquired'", ErrLockNotAcquired.Error())
	}
}

func TestErrLockReleased_Sentinel(t *testing.T) {
	if ErrLockReleased == nil {
		t.Fatal("ErrLockReleased should not be nil")
	}
	if ErrLockReleased.Error() != "advisory lock already released" {
		t.Errorf("ErrLockReleased = %q, want 'advisory lock already released'", ErrLockReleased.Error())
	}
}
