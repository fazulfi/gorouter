package bootstrap

import (
	"context"
	"os"
	"testing"
)

// TestDefaults ensures LoadConfig returns sensible defaults when no overrides
// are provided.
func TestDefaults(t *testing.T) {
	cfg, err := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		nil, // no DB config source
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Mode != ModeServer {
		t.Errorf("default Mode = %q, want %q", cfg.Mode, ModeServer)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("default Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
	if cfg.Port != 8080 {
		t.Errorf("default Port = %d, want %d", cfg.Port, 8080)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel = %q, want %q", cfg.LogLevel, "info")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("default LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
}

// TestPrecedenceFlagsOverrideEnv ensures flags win over environment variables.
func TestPrecedenceFlagsOverrideEnv(t *testing.T) {
	// Set env var but also pass a flag; flag must win.
	t.Setenv("GOROUTER_PORT", "9090")

	cfg, err := LoadConfig(
		[]string{"gorouter", "--port", "7070"},
		os.Getenv,
		func() map[string]string { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.Port != 7070 {
		t.Errorf("flag --port=7070 should override env GOROUTER_PORT=9090; got Port=%d", cfg.Port)
	}
	if got := cfg.Source("port"); got != SourceFlag {
		t.Errorf("Source(\"port\") = %v, want %v", got, SourceFlag)
	}
}

// TestPrecedenceEnvOverrideDotEnv ensures env vars win over .env file values.
func TestPrecedenceEnvOverrideDotEnv(t *testing.T) {
	t.Setenv("GOROUTER_LOG_LEVEL", "warn")

	dotEnv := map[string]string{
		"GOROUTER_LOG_LEVEL": "debug",
	}

	cfg, err := LoadConfig(
		[]string{"gorouter"},
		os.Getenv,
		func() map[string]string { return dotEnv },
		nil,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.LogLevel != "warn" {
		t.Errorf("env GOROUTER_LOG_LEVEL=warn should override .env debug; got LogLevel=%q", cfg.LogLevel)
	}
	if got := cfg.Source("log_level"); got != SourceEnv {
		t.Errorf("Source(\"log_level\") = %v, want %v", got, SourceEnv)
	}
}

// TestPrecedenceDotEnvOverrideDB ensures .env file values win over DB config.
func TestPrecedenceDotEnvOverrideDB(t *testing.T) {
	dotEnv := map[string]string{
		"GOROUTER_LOG_LEVEL": "error",
	}

	readDB := func(_ context.Context) (DBConfig, error) {
		return DBConfig{LogLevel: "trace"}, nil
	}

	cfg, err := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return dotEnv },
		readDB,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.LogLevel != "error" {
		t.Errorf(".env LogLevel=error should override DB trace; got LogLevel=%q", cfg.LogLevel)
	}
	if got := cfg.Source("log_level"); got != SourceDotEnv {
		t.Errorf("Source(\"log_level\") = %v, want %v", got, SourceDotEnv)
	}
}

// TestPrecedenceDBOverrideDefaults ensures DB config overrides defaults.
func TestPrecedenceDBOverrideDefaults(t *testing.T) {
	readDB := func(_ context.Context) (DBConfig, error) {
		return DBConfig{LogLevel: "debug"}, nil
	}

	cfg, err := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		readDB,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("DB LogLevel=debug should override default info; got LogLevel=%q", cfg.LogLevel)
	}
	if got := cfg.Source("log_level"); got != SourceDB {
		t.Errorf("Source(\"log_level\") = %v, want %v", got, SourceDB)
	}
}

// TestFullPrecedenceChain tests all layers together.
func TestFullPrecedenceChain(t *testing.T) {
	// DB provides some value.
	readDB := func(_ context.Context) (DBConfig, error) {
		return DBConfig{LogLevel: "trace"}, nil
	}

	// .env overrides DB.
	dotEnv := map[string]string{
		"GOROUTER_LOG_LEVEL": "debug",
	}

	// Env overrides .env.
	t.Setenv("GOROUTER_LOG_LEVEL", "warn")

	// Flag overrides everything.
	cfg, err := LoadConfig(
		[]string{"gorouter", "--log-level", "error"},
		os.Getenv,
		func() map[string]string { return dotEnv },
		readDB,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.LogLevel != "error" {
		t.Errorf("flag --log-level=error should win chain; got LogLevel=%q", cfg.LogLevel)
	}
	if got := cfg.Source("log_level"); got != SourceFlag {
		t.Errorf("Source(\"log_level\") = %v, want %v", got, SourceFlag)
	}

	// Port should still be default (not set by any layer).
	if cfg.Port != 8080 {
		t.Errorf("default Port should remain 8080; got %d", cfg.Port)
	}
	if got := cfg.Source("port"); got != SourceDefault {
		t.Errorf("Source(\"port\") = %v, want %v", got, SourceDefault)
	}
}

// TestRedactedDashboardPass verifies DashboardPass is masked.
func TestRedactedDashboardPass(t *testing.T) {
	cfg := Config{
		DashboardPass: "super-secret-123",
		SessionSecret: "session-secret-456",
	}
	redacted := cfg.Redacted()

	if redacted.DashboardPass != "****" {
		t.Errorf("Redacted DashboardPass = %q, want %q", redacted.DashboardPass, "****")
	}
	if redacted.SessionSecret != "****" {
		t.Errorf("Redacted SessionSecret = %q, want %q", redacted.SessionSecret, "****")
	}
}

// TestRedactedDatabaseURL verifies password in DatabaseURL is masked.
func TestRedactedDatabaseURL(t *testing.T) {
	cfg := Config{
		DatabaseURL: "postgres://user:mysecret@localhost:5432/gorouter",
	}
	redacted := cfg.Redacted()

	if redacted.DatabaseURL == cfg.DatabaseURL {
		t.Errorf("Redacted DatabaseURL should differ from original; got %q", redacted.DatabaseURL)
	}
	if redacted.DatabaseURL == "" {
		t.Fatal("Redacted DatabaseURL should not be empty")
	}
}

// TestRedactedEmptyFields verifies empty secrets stay empty after redaction.
func TestRedactedEmptyFields(t *testing.T) {
	cfg := Config{}
	redacted := cfg.Redacted()

	if redacted.DashboardPass != "" {
		t.Errorf("Redacted empty DashboardPass should stay empty; got %q", redacted.DashboardPass)
	}
	if redacted.SessionSecret != "" {
		t.Errorf("Redacted empty SessionSecret should stay empty; got %q", redacted.SessionSecret)
	}
	if redacted.DatabaseURL != "" {
		t.Errorf("Redacted empty DatabaseURL should stay empty; got %q", redacted.DatabaseURL)
	}
}

// TestParseModeServer verifies "server" mode parsing.
func TestParseModeServer(t *testing.T) {
	mode, err := ParseMode([]string{"gorouter", "server"})
	if err != nil {
		t.Fatalf("ParseMode returned error: %v", err)
	}
	if mode != ModeServer {
		t.Errorf("ParseMode = %q, want %q", mode, ModeServer)
	}
}

// TestParseModeCLI verifies "cli" mode parsing.
func TestParseModeCLI(t *testing.T) {
	mode, err := ParseMode([]string{"gorouter", "cli"})
	if err != nil {
		t.Fatalf("ParseMode returned error: %v", err)
	}
	if mode != ModeCLI {
		t.Errorf("ParseMode = %q, want %q", mode, ModeCLI)
	}
}

// TestParseModeTray verifies "tray" mode parsing.
func TestParseModeTray(t *testing.T) {
	mode, err := ParseMode([]string{"gorouter", "tray"})
	if err != nil {
		t.Fatalf("ParseMode returned error: %v", err)
	}
	if mode != ModeTray {
		t.Errorf("ParseMode = %q, want %q", mode, ModeTray)
	}
}

// TestParseModeHelper verifies "helper" mode parsing.
func TestParseModeHelper(t *testing.T) {
	mode, err := ParseMode([]string{"gorouter", "helper"})
	if err != nil {
		t.Fatalf("ParseMode returned error: %v", err)
	}
	if mode != ModeHelper {
		t.Errorf("ParseMode = %q, want %q", mode, ModeHelper)
	}
}

// TestParseModeDefaultsToServer ensures unknown/missing args default to server.
func TestParseModeDefaultsToServer(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no args", []string{"gorouter"}},
		{"unknown mode", []string{"gorouter", "unknown"}},
		{"flag only", []string{"gorouter", "--port", "8080"}},
		{"empty", []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode, err := ParseMode(tt.args)
			if err != nil {
				t.Fatalf("ParseMode returned error: %v", err)
			}
			if mode != ModeServer {
				t.Errorf("ParseMode(%v) = %q, want %q", tt.args, mode, ModeServer)
			}
		})
	}
}

// TestModeValidValues verifies all Mode constants have valid string values.
func TestModeValidValues(t *testing.T) {
	if string(ModeServer) != "server" {
		t.Errorf("ModeServer = %q, want %q", ModeServer, "server")
	}
	if string(ModeCLI) != "cli" {
		t.Errorf("ModeCLI = %q, want %q", ModeCLI, "cli")
	}
	if string(ModeTray) != "tray" {
		t.Errorf("ModeTray = %q, want %q", ModeTray, "tray")
	}
	if string(ModeHelper) != "helper" {
		t.Errorf("ModeHelper = %q, want %q", ModeHelper, "helper")
	}
}

// TestSourceTracking verifies source tracking for each field.
func TestSourceTracking(t *testing.T) {
	readDB := func(_ context.Context) (DBConfig, error) {
		return DBConfig{LogLevel: "trace"}, nil
	}
	dotEnv := map[string]string{
		"GOROUTER_LOG_LEVEL": "error",
	}
	t.Setenv("GOROUTER_PORT", "3000")

	cfg, err := LoadConfig(
		[]string{"gorouter", "--host", "0.0.0.0"},
		os.Getenv,
		func() map[string]string { return dotEnv },
		readDB,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	tests := []struct {
		field string
		want  ConfigSource
	}{
		{"host", SourceFlag},
		{"port", SourceEnv},
		{"log_level", SourceDotEnv},
		{"log_format", SourceDefault},
		{"mode", SourceDefault},
	}
	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			if got := cfg.Source(tt.field); got != tt.want {
				t.Errorf("Source(%q) = %v, want %v", tt.field, got, tt.want)
			}
		})
	}
}

// TestDBConfigLogLevel verifies DBConfig fields are populated correctly.
func TestDBConfigFields(t *testing.T) {
	readDB := func(_ context.Context) (DBConfig, error) {
		return DBConfig{
			LogLevel: "debug",
			Host:     "10.0.0.1",
			Port:     5432,
		}, nil
	}
	cfg, err := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		readDB,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}

	if cfg.LogLevel != "debug" {
		t.Errorf("DB LogLevel should be debug; got %q", cfg.LogLevel)
	}
}

// TestStableDefaultPort ensures there is no random-port fallback.
func TestStableDefaultPort(t *testing.T) {
	cfg1, _ := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		nil,
	)
	cfg2, _ := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		nil,
	)
	if cfg1.Port != cfg2.Port {
		t.Errorf("Port is not stable: cfg1.Port=%d, cfg2.Port=%d", cfg1.Port, cfg2.Port)
	}
	if cfg1.Port != 8080 {
		t.Errorf("Expected default Port 8080; got %d", cfg1.Port)
	}
}
