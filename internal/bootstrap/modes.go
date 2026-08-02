package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"gorouter/internal/app/cooldown"
	"gorouter/internal/app/orchestrator"
	"gorouter/internal/app/retry"
	"gorouter/internal/app/routing"
	"gorouter/internal/app/tx"
	"gorouter/internal/app/worker"
	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/jobs"
	"gorouter/internal/domain/keys"
	"gorouter/internal/domain/provider"
	"gorouter/internal/engine/formats"
	"gorouter/internal/engine/providers/registry"
	"gorouter/internal/persistence/postgres"
	"gorouter/internal/persistence/postgres/repositories"
	"gorouter/internal/shared"
	"gorouter/internal/transport/httpserver"
	"gorouter/internal/transport/httpserver/api"
	"gorouter/internal/transport/httpserver/health"
	"gorouter/internal/transport/middleware"
)

// Mode represents the runtime execution mode for gorouter.
type Mode string

const (
	ModeServer Mode = "server"
	ModeCLI    Mode = "cli"
	ModeTray   Mode = "tray"
	ModeHelper Mode = "helper"
)

var knownModes = map[string]Mode{
	"server": ModeServer,
	"cli":    ModeCLI,
	"tray":   ModeTray,
	"helper": ModeHelper,
}

// ParseMode extracts the runtime mode from program arguments.
func ParseMode(args []string) (Mode, error) {
	if len(args) < 2 {
		return ModeServer, nil
	}
	candidate := args[1]
	if m, ok := knownModes[candidate]; ok {
		return m, nil
	}
	return ModeServer, nil
}

// DispatchMode runs the appropriate entry point for the given mode.
func DispatchMode(mode Mode, app *App) (exitCode int, err error) {
	switch mode {
	case ModeServer:
		return dispatchServer(app)
	case ModeCLI:
		return dispatchCLI(app)
	case ModeTray:
		return dispatchTray(app)
	case ModeHelper:
		return dispatchHelper(app)
	default:
		return 1, fmt.Errorf("unknown mode: %s", mode)
	}
}

func dispatchServer(app *App) (int, error) {
	router := httpserver.New(
		middleware.Correlation,
		middleware.Recovery,
		middleware.ModelCORS(),
	)

	router.Get("/health", health.PublicHandler())
	modelKeyValidator := buildKeyValidator(app)
	detector := formats.NewDetector()

	orch, cd, err := buildOrchestrator(app, detector)
	if err != nil {
		return 1, fmt.Errorf("build orchestrator: %w", err)
	}

	apiHandler := api.New(api.DefaultConfig(), orch, app.Logger)
	router.Group(func(r chi.Router) {
		r.Use(middleware.ModelKeyAuth(modelKeyValidator))
		apiHandler.RegisterRoutes(r)
	})

	addr := app.Config.Host + ":" + strconv.Itoa(app.Config.Port)

	app.Logger.Info().Str("host", app.Config.Host).Int("port", app.Config.Port).
		Msg("starting server mode")

	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown: block on ListenAndServe while listening for
	// SIGINT/SIGTERM, then drain with a bounded 30s deadline.
	shutdownCtx, stop := signal.NotifyContext(context.Background(),
		syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Acquire the runtime-exclusivity advisory lock before any listener is
	// opened: a second runtime on the same database is rejected. The lock
	// is retained until the drain path completes and released on return.
	releaseLock, err := acquireRuntimeLock(shutdownCtx, app)
	if err != nil {
		// The drain path below is unreachable on this refusal, so stop the
		// cooldown cleanup goroutine before returning.
		cd.Stop()
		return 1, fmt.Errorf("acquire runtime lock: %w (another runtime may already be running on this database)", err)
	}
	defer func() {
		if err := releaseLock(context.Background()); err != nil {
			app.Logger.Error().Err(err).Msg("release runtime lock error")
		}
	}()

	// Start the background job worker after the lock is held and before
	// the listener opens; it is stopped during drain.
	wk := buildWorker(app, orch)
	wk.Start(context.Background())

	go func() {
		if err := listenAndServe(srv); err != nil && err != http.ErrServerClosed {
			app.Logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	<-shutdownCtx.Done()
	app.Logger.Info().Msg("shutting down server gracefully")

	drainCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := srv.Shutdown(drainCtx); err != nil {
		app.Logger.Error().Err(err).Msg("server shutdown error")
	}

	cd.Stop()
	wk.Stop()

	return 0, nil
}

func buildKeyValidator(app *App) middleware.ModelKeyValidator {
	if app.DB == nil || app.TxMgr == nil {
		return middleware.ModelKeyValidatorFunc(
			func(_ context.Context, _ string) (uuid.UUID, error) {
				return uuid.Nil, shared.NewAppError(shared.ErrInternal,
					"database not configured — key validation unavailable", 0, nil)
			},
		)
	}

	return middleware.ModelKeyValidatorFunc(
		func(ctx context.Context, rawKey string) (uuid.UUID, error) {
			conn, err := app.DB.Acquire(ctx)
			if err != nil {
				return uuid.Nil, fmt.Errorf("key validation: acquire conn: %w", err)
			}
			defer conn.Release()

			pgTx, err := conn.Begin(ctx)
			if err != nil {
				return uuid.Nil, fmt.Errorf("key validation: begin tx: %w", err)
			}
			defer pgTx.Rollback(ctx)

			scope := repositories.NewTxScope(pgTx)
			svc := keys.NewModelKeyService(scope.APIKeys())
			apiKey, err := svc.Validate(ctx, rawKey)
			if err != nil {
				return uuid.Nil, err
			}
			return apiKey.UserID, nil
		},
	)
}

func buildOrchestrator(app *App, detector *formats.Detector) (engine.Orchestrator, *cooldown.Registry, error) {
	if app.DB == nil || app.TxMgr == nil {
		return nil, nil, fmt.Errorf("database not configured — production orchestrator requires PostgreSQL")
	}

	execFactory := registry.NewFactory(http.DefaultTransport)

	cd := cooldown.New(cooldown.DefaultConfig())
	cd.Start(context.Background())

	// Wire the TxScope factory so TransactionManager.Begin returns fully-wired
	// scopes with concrete repository implementations.
	app.TxMgr.SetScopeFactory(repositories.NewTxScope)

	adapter := &poolAccountRepo{pool: app.DB}
	accountSel := routing.NewAccountSelector(adapter, cd)

	resolver := routing.NewResolver(func(ctx context.Context) (*tx.TxScope, error) {
		conn, err := app.DB.Acquire(ctx)
		if err != nil {
			return nil, err
		}
		pgTx, err := conn.Begin(ctx)
		if err != nil {
			conn.Release()
			return nil, err
		}
		return repositories.NewTxScope(pgTx), nil
	})

	o := orchestrator.New(
		orchestrator.DefaultConfig(),
		resolver,
		detector,
		execFactory,
		accountSel,
		adapter,
		cd,
		retry.DefaultConfig(),
		app.TxMgr,
		app.Logger,
	)

	return o, cd, nil
}

// poolAccountRepo implements provider.AccountRepository by acquiring a pool
// connection and opening a transaction for each method call. This adapter
// bridges the statically-wired orchestrator with per-request persistence.
type poolAccountRepo struct {
	pool *pgxpool.Pool
}

func (r *poolAccountRepo) withTx(ctx context.Context, fn func(*tx.TxScope) error) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	pgTx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer pgTx.Rollback(ctx)
	scope := repositories.NewTxScope(pgTx)
	if err := fn(scope); err != nil {
		return err
	}
	return pgTx.Commit(ctx)
}

func (r *poolAccountRepo) FindByID(ctx context.Context, id uuid.UUID) (*provider.Account, error) {
	var result *provider.Account
	err := r.withTx(ctx, func(scope *tx.TxScope) error {
		var err error
		result, err = scope.Accounts().FindByID(ctx, id)
		return err
	})
	return result, err
}

func (r *poolAccountRepo) FindByProviderID(ctx context.Context, providerID uuid.UUID) ([]provider.Account, error) {
	var result []provider.Account
	err := r.withTx(ctx, func(scope *tx.TxScope) error {
		var err error
		result, err = scope.Accounts().FindByProviderID(ctx, providerID)
		return err
	})
	return result, err
}

func (r *poolAccountRepo) Create(ctx context.Context, account *provider.Account) error {
	return r.withTx(ctx, func(scope *tx.TxScope) error {
		return scope.Accounts().Create(ctx, account)
	})
}

func (r *poolAccountRepo) Update(ctx context.Context, account *provider.Account) error {
	return r.withTx(ctx, func(scope *tx.TxScope) error {
		return scope.Accounts().Update(ctx, account)
	})
}

func (r *poolAccountRepo) Delete(ctx context.Context, id uuid.UUID) error {
	return r.withTx(ctx, func(scope *tx.TxScope) error {
		return scope.Accounts().Delete(ctx, id)
	})
}

// acquireRuntimeLock acquires the runtime-exclusivity advisory lock before
// any listener is opened, enforcing single-runtime ownership of the database.
// It is a package-level variable so bootstrap tests can assert the serve-mode
// ordering contract without a live PostgreSQL instance; the production
// implementation delegates to postgres.AcquireRuntimeLock.
var acquireRuntimeLock = func(ctx context.Context, app *App) (func(context.Context) error, error) {
	lock, err := postgres.AcquireRuntimeLock(ctx, app.DB)
	if err != nil {
		return nil, err
	}
	return lock.Release, nil
}

// backgroundWorker is the subset of the job worker used by serve mode. It is
// defined as an interface so bootstrap tests can substitute a fake and assert
// start/stop ordering; *worker.Worker satisfies it.
type backgroundWorker interface {
	Start(ctx context.Context)
	Stop()
}

// buildWorker constructs the background job worker used by serve mode. It is
// a package-level variable so bootstrap tests can substitute a fake and
// observe worker start/stop ordering; *worker.Worker satisfies backgroundWorker.
var buildWorker = func(app *App, pipeline engine.ExecutePipeline) backgroundWorker {
	return worker.New(worker.DefaultConfig(), &poolJobRepo{pool: app.DB}, pipeline, app.TxMgr, app.Logger)
}

// listenAndServe starts the HTTP server listener. It is a package-level
// variable so bootstrap tests can assert ordering without binding real
// sockets; the production implementation is http.Server.ListenAndServe.
var listenAndServe = func(srv *http.Server) error { return srv.ListenAndServe() }

// poolJobRepo implements jobs.JobRepository by acquiring a pool connection
// and opening a transaction for each method call. This adapter bridges the
// long-lived worker with per-operation persistence, mirroring poolAccountRepo.
type poolJobRepo struct {
	pool *pgxpool.Pool
}

func (r *poolJobRepo) withTx(ctx context.Context, fn func(*tx.TxScope) error) error {
	conn, err := r.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()
	pgTx, err := conn.Begin(ctx)
	if err != nil {
		return err
	}
	defer pgTx.Rollback(ctx)
	scope := repositories.NewTxScope(pgTx)
	if err := fn(scope); err != nil {
		return err
	}
	return pgTx.Commit(ctx)
}

func (r *poolJobRepo) FindByID(ctx context.Context, id uuid.UUID) (*jobs.Job, error) {
	var result *jobs.Job
	err := r.withTx(ctx, func(scope *tx.TxScope) error {
		var err error
		result, err = scope.Jobs().FindByID(ctx, id)
		return err
	})
	return result, err
}

func (r *poolJobRepo) FindPending(ctx context.Context, limit int) ([]jobs.Job, error) {
	var result []jobs.Job
	err := r.withTx(ctx, func(scope *tx.TxScope) error {
		var err error
		result, err = scope.Jobs().FindPending(ctx, limit)
		return err
	})
	return result, err
}

func (r *poolJobRepo) Create(ctx context.Context, job *jobs.Job) error {
	return r.withTx(ctx, func(scope *tx.TxScope) error {
		return scope.Jobs().Create(ctx, job)
	})
}

func (r *poolJobRepo) UpdateStatus(ctx context.Context, id uuid.UUID, status jobs.JobStatus, result json.RawMessage, errMsg *string) error {
	return r.withTx(ctx, func(scope *tx.TxScope) error {
		return scope.Jobs().UpdateStatus(ctx, id, status, result, errMsg)
	})
}

func dispatchCLI(app *App) (int, error) {
	app.Logger.Info().Msg("starting CLI mode")
	return 0, nil
}

func dispatchTray(app *App) (int, error) {
	app.Logger.Info().Msg("starting tray mode")
	return 0, nil
}

func dispatchHelper(app *App) (int, error) {
	fmt.Fprintln(app.Stderr, "gorouter - Go router and reverse proxy")
	return 0, nil
}
