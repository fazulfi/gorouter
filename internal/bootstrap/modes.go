package bootstrap

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"gorouter/internal/app/translate"
	"gorouter/internal/domain/engine"
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

// knownModes is the set of valid runtime modes.
var knownModes = map[string]Mode{
	"server": ModeServer,
	"cli":    ModeCLI,
	"tray":   ModeTray,
	"helper": ModeHelper,
}

// ParseMode extracts the runtime mode from program arguments.
// The mode is expected as the first non-flag argument.
// When no mode is provided or the argument is unrecognized, ModeServer is
// returned as the default.
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
// Each mode returns an exit code and a possible error.
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

// dispatchServer starts the HTTP server with the model API, health endpoints,
// and middleware stack.
func dispatchServer(app *App) (int, error) {
	router := httpserver.New(
		middleware.Correlation,
		middleware.Recovery,
		middleware.ModelCORS(),
	)

	router.Get("/health", health.PublicHandler())

	translateSvc := translate.NewService()
	orch := &noopOrchestrator{}

	apiHandler := api.New(api.DefaultConfig(), orch, translateSvc, app.Logger)
	apiHandler.RegisterRoutes(router)

	addr := app.Config.Host + ":" + strconv.Itoa(app.Config.Port)

	app.Logger.Info().Str("host", app.Config.Host).Int("port", app.Config.Port).
		Msg("starting server mode")

	go func() {
		if err := http.ListenAndServe(addr, router); err != nil && err != http.ErrServerClosed {
			app.Logger.Fatal().Err(err).Msg("server failed")
		}
	}()

	return 0, nil
}

// noopOrchestrator is a stub implementation of engine.Orchestrator for initial
// wiring. It returns an error on every ExecuteRequest call.
type noopOrchestrator struct{}

func (n *noopOrchestrator) ExecuteRequest(_ context.Context, req *engine.Request) (*engine.Response, error) {
	return nil, shared.NewAppError(shared.ErrInternal,
		"orchestrator not yet implemented — this is a placeholder", 0, nil)
}

func (n *noopOrchestrator) CancelStream(_ context.Context, requestID uuid.UUID) error {
	return nil
}

// Ensure noopOrchestrator satisfies the interface at compile time.
var _ engine.Orchestrator = (*noopOrchestrator)(nil)

// dispatchCLI runs CLI commands. Placeholder for future implementation.
func dispatchCLI(app *App) (int, error) {
	app.Logger.Info().Msg("starting CLI mode")
	return 0, nil
}

// dispatchTray starts the system tray integration. Placeholder for future
// implementation.
func dispatchTray(app *App) (int, error) {
	app.Logger.Info().Msg("starting tray mode")
	return 0, nil
}

// dispatchHelper prints usage information and exits.
func dispatchHelper(app *App) (int, error) {
	fmt.Fprintln(app.Stderr, "gorouter - Go router and reverse proxy")
	return 0, nil
}
