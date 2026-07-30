package bootstrap

import (
	"io"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"gorouter/internal/app/tx"
)

// App is the top-level dependency container for gorouter. It wires
// configuration, logging, database, and other shared infrastructure together.
type App struct {
	Config Config
	Logger zerolog.Logger
	Stderr io.Writer
	Stdout io.Writer

	// DB is the optional PostgreSQL connection pool. When nil the database
	// is not available (CLI/helper modes or startup without DatabaseURL).
	DB *pgxpool.Pool

	// TxMgr is the optional transaction manager backed by DB. Nil when DB is
	// nil.
	TxMgr *tx.TransactionManager
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

// Close releases resources held by App.
func (a *App) Close() error {
	if a.DB != nil {
		a.DB.Close()
	}
	return nil
}
