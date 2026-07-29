package bootstrap

import (
	"fmt"
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

// dispatchServer starts the HTTP server. Placeholder for future implementation.
func dispatchServer(app *App) (int, error) {
	app.Logger.Info().Str("host", app.Config.Host).Int("port", app.Config.Port).
		Msg("starting server mode")
	return 0, nil
}

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
