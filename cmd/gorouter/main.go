package main

import (
	"fmt"
	"os"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"

	"gorouter/internal/bootstrap"
)

func main() {
	os.Exit(run())
}

func run() int {
	args := os.Args

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
