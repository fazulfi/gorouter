package bootstrap

import (
	"context"
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
	"gorouter/internal/app/executor"
	"gorouter/internal/app/orchestrator"
	"gorouter/internal/app/retry"
	"gorouter/internal/app/routing"
	"gorouter/internal/app/translate"
	"gorouter/internal/app/tx"
	"gorouter/internal/domain/engine"
	"gorouter/internal/domain/keys"
	"gorouter/internal/domain/provider"
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
	translateSvc := translate.NewService()

	orch, cd, err := buildOrchestrator(app, translateSvc)
	if err != nil {
		return 1, fmt.Errorf("build orchestrator: %w", err)
	}

	apiHandler := api.New(api.DefaultConfig(), orch, translateSvc, app.Logger)
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

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
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

func buildOrchestrator(app *App, translateSvc *translate.Service) (engine.Orchestrator, *cooldown.Registry, error) {
	if app.DB == nil || app.TxMgr == nil {
		return nil, nil, fmt.Errorf("database not configured — production orchestrator requires PostgreSQL")
	}

	execFactory := executor.NewFactory(http.DefaultTransport)

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
		translateSvc,
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
