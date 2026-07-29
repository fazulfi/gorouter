package bootstrap

import (
	"io"
	"os"

	"github.com/rs/zerolog"
)

// App is the top-level dependency container for gorouter. It wires
// configuration, logging, and other shared infrastructure together.
type App struct {
	Config Config
	Logger zerolog.Logger
	Stderr io.Writer
	Stdout io.Writer
}

// AppOption is a functional option for configuring App.
type AppOption func(*App)

// WithStderr sets the stderr writer for the App.
func WithStderr(w io.Writer) AppOption {
	return func(a *App) {
		a.Stderr = w
	}
}

// WithStdout sets the stdout writer for the App.
func WithStdout(w io.Writer) AppOption {
	return func(a *App) {
		a.Stdout = w
	}
}

// NewApp creates a new App with the given config and optional overrides.
// It bootstraps the logger first, then returns a ready App.
func NewApp(cfg Config, opts ...AppOption) *App {
	a := &App{
		Config: cfg,
		Stderr: os.Stderr,
		Stdout: os.Stdout,
	}
	for _, opt := range opts {
		opt(a)
	}
	a.Logger = newLogger(cfg, a.Stderr)
	return a
}

// newLogger creates a zerolog.Logger from the given config.
func newLogger(cfg Config, w io.Writer) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	output := w
	if cfg.LogFormat == "text" {
		output = zerolog.NewConsoleWriter(func(c *zerolog.ConsoleWriter) {
			c.Out = w
		})
	}
	return zerolog.New(output).
		Level(level).
		With().
		Timestamp().
		Logger()
}

// Close releases resources held by App. Currently a no-op.
func (a *App) Close() error {
	return nil
}
