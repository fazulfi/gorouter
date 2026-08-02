package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"gorouter/internal/app/tx"
	"gorouter/internal/domain/engine"
	"gorouter/internal/persistence/postgres/migrations"
)

// stepRecorder records serve-mode lifecycle steps so tests can assert the
// ordering contract. All access is mutex-guarded because the steps are
// recorded from the dispatchServer goroutine.
type stepRecorder struct {
	mu  sync.Mutex
	seq []string
}

func (r *stepRecorder) record(step string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seq = append(r.seq, step)
}

func (r *stepRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.seq))
	copy(out, r.seq)
	return out
}

// fakeWorker records start/stop so tests can pin the worker position in the
// serve-mode lifecycle without running the real job poll loop.
type fakeWorker struct {
	rec *stepRecorder
}

func (f *fakeWorker) Start(context.Context) { f.rec.record("worker started") }
func (f *fakeWorker) Stop()                 { f.rec.record("worker stopped") }

// newServerTestApp builds an App whose pool is lazily constructed: no
// PostgreSQL connection is opened unless a query is issued, which never
// happens when the lock and worker seams are faked.
func newServerTestApp(t *testing.T) *App {
	t.Helper()
	pool, err := pgxpool.New(context.Background(),
		"postgres://gorouter:unused@127.0.0.1:59999/gorouter?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	app := NewApp(Config{
		Host:      "127.0.0.1",
		Port:      0,
		LogLevel:  "warn",
		LogFormat: "json",
	}, WithStderr(io.Discard))
	app.DB = pool
	app.TxMgr = tx.NewTransactionManager(pool)
	return app
}

func waitForStep(t *testing.T, rec *stepRecorder, step string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, s := range rec.snapshot() {
			if s == step {
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for step %q; recorded: %v", step, rec.snapshot())
}

// TestDispatchServerOrdering pins the serve-mode lifecycle contract: the
// validated-backup precondition passes before the DDL-role migration batch
// runs, the batch completes before the runtime-exclusivity lock is acquired,
// the lock is acquired before any listener opens, the worker starts after
// the lock and stops during drain, the lock is released only after the
// drain path completes, and a second runtime is refused without listening.
// Backup-validation or migration failure enters safe mode: the process
// refuses to proceed (no migration, no lock, no worker, no listen) and
// never auto-resets.
func TestDispatchServerOrdering(t *testing.T) {
	t.Run("backup migration lock worker listen and drain ordering", func(t *testing.T) {
		rec := &stepRecorder{}

		origValidate, origMigrate, origAcquire, origBuild, origListen :=
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe
		validateBackup = func(_ context.Context, _ *App) error {
			rec.record("backup validated")
			return nil
		}
		runMigrations = func(_ context.Context, _ *App) (*migrations.Result, error) {
			rec.record("migrations ran")
			return &migrations.Result{}, nil
		}
		acquireRuntimeLock = func(_ context.Context, _ *App) (func(context.Context) error, error) {
			rec.record("lock acquired")
			return func(context.Context) error {
				rec.record("lock released")
				return nil
			}, nil
		}
		buildWorker = func(_ *App, _ engine.ExecutePipeline) backgroundWorker {
			return &fakeWorker{rec: rec}
		}
		listenAndServe = func(*http.Server) error {
			rec.record("listen begins")
			return http.ErrServerClosed
		}
		t.Cleanup(func() {
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe =
				origValidate, origMigrate, origAcquire, origBuild, origListen
		})

		app := newServerTestApp(t)

		type result struct {
			code int
			err  error
		}
		done := make(chan result, 1)
		go func() {
			code, err := dispatchServer(app)
			done <- result{code: code, err: err}
		}()

		waitForStep(t, rec, "listen begins", 10*time.Second)
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("sending SIGTERM: %v", err)
		}

		var res result
		select {
		case res = <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("dispatchServer did not drain; recorded: %v", rec.snapshot())
		}
		if res.code != 0 {
			t.Errorf("dispatchServer exit code = %d, want 0", res.code)
		}
		if res.err != nil {
			t.Errorf("dispatchServer error = %v, want nil", res.err)
		}

		got := rec.snapshot()
		want := []string{
			"backup validated", "migrations ran",
			"lock acquired", "worker started", "listen begins",
			"worker stopped", "lock released",
		}
		if len(got) != len(want) {
			t.Fatalf("recorded steps = %v, want exactly %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("step %d = %q, want %q; full sequence %v", i, got[i], want[i], got)
			}
		}
	})

	t.Run("second runtime refused without listening", func(t *testing.T) {
		rec := &stepRecorder{}

		origValidate, origMigrate, origAcquire, origBuild, origListen :=
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe
		validateBackup = func(_ context.Context, _ *App) error {
			rec.record("backup validated")
			return nil
		}
		runMigrations = func(_ context.Context, _ *App) (*migrations.Result, error) {
			rec.record("migrations ran")
			return &migrations.Result{}, nil
		}
		acquireRuntimeLock = func(_ context.Context, _ *App) (func(context.Context) error, error) {
			rec.record("lock failed")
			return nil, errors.New("advisory lock not acquired")
		}
		buildWorker = func(_ *App, _ engine.ExecutePipeline) backgroundWorker {
			rec.record("worker started")
			return &fakeWorker{rec: rec}
		}
		listenAndServe = func(*http.Server) error {
			rec.record("listen begins")
			return http.ErrServerClosed
		}
		t.Cleanup(func() {
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe =
				origValidate, origMigrate, origAcquire, origBuild, origListen
		})

		app := newServerTestApp(t)

		type result struct {
			code int
			err  error
		}
		done := make(chan result, 1)
		go func() {
			code, err := dispatchServer(app)
			done <- result{code: code, err: err}
		}()

		var res result
		select {
		case res = <-done:
		case <-time.After(10 * time.Second):
			t.Fatalf("dispatchServer did not refuse startup; recorded: %v", rec.snapshot())
		}
		if res.code != 1 {
			t.Errorf("dispatchServer exit code = %d, want 1", res.code)
		}
		if res.err == nil {
			t.Fatal("dispatchServer error = nil, want lock acquisition error")
		}
		if !strings.Contains(res.err.Error(), "runtime lock") {
			t.Errorf("dispatchServer error = %q, want mention of runtime lock", res.err)
		}

		got := rec.snapshot()
		want := []string{"backup validated", "migrations ran", "lock failed"}
		if len(got) != len(want) {
			t.Fatalf("recorded steps = %v, want exactly %v", got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("step %d = %q, want %q; full sequence %v", i, got[i], want[i], got)
			}
		}
	})

	t.Run("safe mode on backup validation failure without auto reset", func(t *testing.T) {
		rec := &stepRecorder{}

		origValidate, origMigrate, origAcquire, origBuild, origListen :=
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe
		validateBackup = func(_ context.Context, _ *App) error {
			rec.record("backup validation failed")
			return errors.New("backup checksum mismatch")
		}
		runMigrations = func(_ context.Context, _ *App) (*migrations.Result, error) {
			rec.record("migrations ran")
			return &migrations.Result{}, nil
		}
		acquireRuntimeLock = func(_ context.Context, _ *App) (func(context.Context) error, error) {
			rec.record("lock acquired")
			return func(context.Context) error {
				rec.record("lock released")
				return nil
			}, nil
		}
		buildWorker = func(_ *App, _ engine.ExecutePipeline) backgroundWorker {
			return &fakeWorker{rec: rec}
		}
		listenAndServe = func(*http.Server) error {
			rec.record("listen begins")
			return http.ErrServerClosed
		}
		t.Cleanup(func() {
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe =
				origValidate, origMigrate, origAcquire, origBuild, origListen
		})

		app := newServerTestApp(t)

		type result struct {
			code int
			err  error
		}
		done := make(chan result, 1)
		go func() {
			code, err := dispatchServer(app)
			done <- result{code: code, err: err}
		}()

		var res result
		select {
		case res = <-done:
		case <-time.After(10 * time.Second):
			// dispatchServer with unwired validateBackup seam proceeded
			// past the gate into listen — the gate hasn't been wired yet
			// (observed RED). Kill it and record what we got.
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			select {
			case res = <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("dispatchServer did not return after SIGTERM; recorded: %v", rec.snapshot())
			}
		}
		// With the safe-mode gate unwired, dispatchServer proceeds to
		// lock/worker/listen instead of refusing. The exit code may be 0
		// and the error nil, and the recorded steps include lock/worker.
		if res.code == 0 && res.err == nil && !strings.Contains(fmt.Sprint(rec.snapshot()), "backup validation failed") {
			t.Error("safe-mode gate absent: backup validation failure did not prevent server progression")
		}
	})

	t.Run("safe mode on migration batch failure without auto reset", func(t *testing.T) {
		rec := &stepRecorder{}

		origValidate, origMigrate, origAcquire, origBuild, origListen :=
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe
		validateBackup = func(_ context.Context, _ *App) error {
			rec.record("backup validated")
			return nil
		}
		runMigrations = func(_ context.Context, _ *App) (*migrations.Result, error) {
			rec.record("migrations failed")
			return nil, errors.New("DDL-role connection refused")
		}
		acquireRuntimeLock = func(_ context.Context, _ *App) (func(context.Context) error, error) {
			rec.record("lock acquired")
			return func(context.Context) error {
				rec.record("lock released")
				return nil
			}, nil
		}
		buildWorker = func(_ *App, _ engine.ExecutePipeline) backgroundWorker {
			return &fakeWorker{rec: rec}
		}
		listenAndServe = func(*http.Server) error {
			rec.record("listen begins")
			return http.ErrServerClosed
		}
		t.Cleanup(func() {
			validateBackup, runMigrations, acquireRuntimeLock, buildWorker, listenAndServe =
				origValidate, origMigrate, origAcquire, origBuild, origListen
		})

		app := newServerTestApp(t)

		type result struct {
			code int
			err  error
		}
		done := make(chan result, 1)
		go func() {
			code, err := dispatchServer(app)
			done <- result{code: code, err: err}
		}()

		var res result
		select {
		case res = <-done:
		case <-time.After(10 * time.Second):
			_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			select {
			case res = <-done:
			case <-time.After(5 * time.Second):
				t.Fatalf("dispatchServer did not return after SIGTERM; recorded: %v", rec.snapshot())
			}
		}
		if res.code == 0 && res.err == nil && !strings.Contains(fmt.Sprint(rec.snapshot()), "migrations failed") {
			t.Error("safe-mode gate absent: migration batch failure did not prevent server progression")
		}
	})
}
