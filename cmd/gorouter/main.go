package main

import (
	"context"
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"gorouter/internal/app/tx"
	"gorouter/internal/bootstrap"
	"gorouter/internal/persistence/postgres"
)

// defaultPoolMaxConns is the default maximum pool size when not overridden.
const defaultPoolMaxConns int32 = 10

func main() {
	os.Exit(run())
}

func run() int {
	args := os.Args
	if len(args) == 1 {
		cfg, cfgErr := bootstrap.LoadConfig(args, os.Getenv, nil, nil)
		if cfgErr != nil || cfg.DatabaseURL == "" {
			return 1
		}
		return runBare()
	}
	if len(args) > 1 && (args[1] == "service" || args[1] == "logs" || args[1] == "exit") {
		return runLifecycleCommand(args[1:], os.Stdout)
	}

	mode, modeErr := bootstrap.ParseMode(args)
	if modeErr != nil {
		fmt.Fprintf(os.Stderr, "error parsing mode: %v\n", modeErr)
		return 1
	}

	logger := zerolog.New(os.Stderr).With().Timestamp().Logger()

	var dotEnv map[string]string
	if _, err := os.Stat(".env"); err == nil {
		envMap, loadErr := godotenv.Read(".env")
		if loadErr != nil {
			logger.Warn().Err(loadErr).Msg("failed to read .env file")
		} else {
			dotEnv = envMap
		}
	}

	cfg, cfgErr := bootstrap.LoadConfig(
		args,
		os.Getenv,
		func() map[string]string { return dotEnv },
		nil,
	)
	if cfgErr != nil {
		logger.Error().Err(cfgErr).Msg("failed to load config")
		return 1
	}

	app := bootstrap.NewApp(cfg,
		bootstrap.WithStderr(os.Stderr),
		bootstrap.WithStdout(os.Stdout),
	)

	// Initialize PostgreSQL connection pool when a database URL is configured.
	if cfg.DatabaseURL != "" {
		pgCfg := postgres.PoolConfig{
			DSN:      cfg.DatabaseURL,
			MaxConns: defaultPoolMaxConns,
		}
		pool, poolErr := postgres.Open(context.Background(), pgCfg)
		if poolErr != nil {
			logger.Error().Err(poolErr).Msg("failed to open database pool")
			return 1
		}
		app.DB = pool.Pool()
		app.TxMgr = tx.NewTransactionManager(app.DB)
		logger.Info().Msg("database pool initialised")
	}

	logger.Info().
		Str("mode", string(mode)).
		Interface("config", cfg.Redacted()).
		Msg("bootstrap complete")

	exitCode, dispatchErr := bootstrap.DispatchMode(mode, app)
	if dispatchErr != nil {
		app.Logger.Error().Err(dispatchErr).Int("exitCode", exitCode).
			Msg("dispatch error")
	}

	if closeErr := app.Close(); closeErr != nil {
		app.Logger.Error().Err(closeErr).Msg("error closing app")
	}

	return exitCode
}
