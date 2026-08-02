package worker

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pashagolub/pgxmock/v2"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/jobs"
	"gorouter/internal/persistence/postgres/repositories"
)

// beginRecorder implements the ScopeBeginner seam, recording how many
// transaction scopes are begun.
type beginRecorder struct {
	begins atomic.Int32
	scope  *tx.TxScope
	err    error
}

func (b *beginRecorder) Begin(context.Context) (*tx.TxScope, error) {
	b.begins.Add(1)
	return b.scope, b.err
}

// recordingTx records commit and rollback on the scope's underlying
// transaction.
type recordingTx struct {
	pgx.Tx
	commits   atomic.Int32
	rollbacks atomic.Int32
}

func (t *recordingTx) Commit(context.Context) error   { t.commits.Add(1); return nil }
func (t *recordingTx) Rollback(context.Context) error { t.rollbacks.Add(1); return nil }

// recordingJobRepo records status updates routed through the transaction
// scope and can be configured to fail them.
type recordingJobRepo struct {
	mu        sync.Mutex
	calls     []statusUpdate
	updateErr error
}

func (r *recordingJobRepo) FindByID(context.Context, uuid.UUID) (*jobs.Job, error) { return nil, nil }
func (r *recordingJobRepo) FindPending(context.Context, int) ([]jobs.Job, error)   { return nil, nil }
func (r *recordingJobRepo) Create(context.Context, *jobs.Job) error                { return nil }

func (r *recordingJobRepo) UpdateStatus(_ context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, statusUpdate{JobID: id, Status: status, Result: result, ErrMsg: errMsg})
	return r.updateErr
}

func (r *recordingJobRepo) callCount() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// TestTransactionalJobUpdater_RejectsCancelledContext proves the I1 root
// cause with the production types: the real TransactionManager refuses to
// begin a transaction on a cancelled context (the pool returns ctx.Err()
// before any connection attempt), so a terminal status update issued with
// the cancelled execution context fails and the row would stay running.
func TestTransactionalJobUpdater_RejectsCancelledContext(t *testing.T) {
	pool, err := pgxpool.New(context.Background(),
		"postgres://gorouter:unused@127.0.0.1:59999/gorouter?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	updater := NewTransactionalJobUpdater(tx.NewTransactionManager(pool))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err = updater.UpdateJobStatus(ctx, uuid.New(), jobs.JobFailed, nil, nil)
	if err == nil {
		t.Fatal("expected a status update on a cancelled context to fail")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
}

// TestTransactionalJobUpdater_CommitsSingleScope: a status transition flows
// through exactly one transaction scope — one Begin, the scoped JobRepository
// update, one Commit — and never through a repository outside the scope (the
// updater holds none), so no empty outer transaction plus independent inner
// pool transaction can occur and only one pool connection is consumed.
func TestTransactionalJobUpdater_CommitsSingleScope(t *testing.T) {
	repo := &recordingJobRepo{}
	fakeTx := &recordingTx{}
	beginner := &beginRecorder{
		scope: tx.NewTxScope(fakeTx, nil, nil, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}
	updater := NewTransactionalJobUpdater(beginner)

	jobID := uuid.New()
	result := json.RawMessage(`{"ok":true}`)
	errMsg := "boom"
	if err := updater.UpdateJobStatus(context.Background(), jobID, jobs.JobCompleted, result, &errMsg); err != nil {
		t.Fatalf("UpdateJobStatus: %v", err)
	}

	if got := beginner.begins.Load(); got != 1 {
		t.Fatalf("Begin calls = %d, want exactly 1 (no empty outer transaction)", got)
	}
	if got := repo.callCount(); got != 1 {
		t.Fatalf("scoped UpdateStatus calls = %d, want exactly 1", got)
	}
	repo.mu.Lock()
	call := repo.calls[0]
	repo.mu.Unlock()
	if call.JobID != jobID {
		t.Errorf("updated job id = %s, want %s", call.JobID, jobID)
	}
	if call.Status != jobs.JobCompleted {
		t.Errorf("updated status = %q, want %q", call.Status, jobs.JobCompleted)
	}
	if string(call.Result) != string(result) {
		t.Errorf("updated result = %q, want %q", call.Result, result)
	}
	if call.ErrMsg == nil || *call.ErrMsg != errMsg {
		t.Errorf("updated errMsg = %v, want %q", call.ErrMsg, errMsg)
	}
	if got := fakeTx.commits.Load(); got != 1 {
		t.Fatalf("commits = %d, want exactly 1", got)
	}
}

// TestTransactionalJobUpdater_RollsBackOnScopedUpdateError: when the scoped
// repository update fails, the single scope is rolled back and never
// committed — the rollback coupling is real, not vacuous.
func TestTransactionalJobUpdater_RollsBackOnScopedUpdateError(t *testing.T) {
	repo := &recordingJobRepo{updateErr: errors.New("update failed")}
	fakeTx := &recordingTx{}
	beginner := &beginRecorder{
		scope: tx.NewTxScope(fakeTx, nil, nil, nil, nil, nil, repo, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil),
	}
	updater := NewTransactionalJobUpdater(beginner)

	err := updater.UpdateJobStatus(context.Background(), uuid.New(), jobs.JobFailed, nil, nil)
	if err == nil {
		t.Fatal("expected the scoped update error to propagate")
	}
	if !errors.Is(err, repo.updateErr) {
		t.Fatalf("error = %v, want wrapped %v", err, repo.updateErr)
	}
	if got := beginner.begins.Load(); got != 1 {
		t.Fatalf("Begin calls = %d, want exactly 1", got)
	}
	if got := fakeTx.commits.Load(); got != 0 {
		t.Fatalf("commits = %d, want 0", got)
	}
	if got := fakeTx.rollbacks.Load(); got != 1 {
		t.Fatalf("rollbacks = %d, want exactly 1", got)
	}
}

// TestScopedJobUpdateStatus_SingleStatementInOneTransaction: the scoped
// repository path used by the updater — the production repositories.NewTxScope
// wiring — executes exactly one UPDATE statement inside the transaction
// obtained from Begin; no second transaction is opened and no independent
// pool call is made per status transition.
func TestScopedJobUpdateStatus_SingleStatementInOneTransaction(t *testing.T) {
	mock, err := pgxmock.NewPool()
	if err != nil {
		t.Fatalf("pgxmock.NewPool: %v", err)
	}
	defer mock.Close()

	mock.ExpectBegin()
	dbTx, err := mock.Begin(context.Background())
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}

	scope := repositories.NewTxScope(dbTx)
	jobID := uuid.New()

	mock.ExpectExec("(?i)UPDATE gorouter_jobs").
		WithArgs(string(jobs.JobCompleted), pgxmock.AnyArg(), pgxmock.AnyArg(), jobID).
		WillReturnResult(pgxmock.NewResult("UPDATE", 1))

	if err := scope.Jobs().UpdateStatus(context.Background(), jobID, jobs.JobCompleted, json.RawMessage(`{"ok":true}`), nil); err != nil {
		t.Fatalf("scoped UpdateStatus: %v", err)
	}

	mock.ExpectCommit()
	if err := dbTx.Commit(context.Background()); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("statements on the scope transaction differ from the single expected UPDATE: %v", err)
	}
}
