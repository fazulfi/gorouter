package bootstrap

import (
	"bytes"
	"context"
	"os"
	"strings"
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

// --- config.go edge cases and additional coverage ---

// TestConfigSourceStringUnknown verifies that an invalid ConfigSource value
// returns "unknown".
func TestConfigSourceStringUnknown(t *testing.T) {
	var s ConfigSource = 999
	if got := s.String(); got != "unknown" {
		t.Errorf("ConfigSource(999).String() = %q, want %q", got, "unknown")
	}
}

// TestSourceNilMap verifies that Source returns SourceDefault when the sources
// map has not been initialised.
func TestSourceNilMap(t *testing.T) {
	var cfg Config
	if got := cfg.Source("port"); got != SourceDefault {
		t.Errorf("Source on nil map = %v, want %v", got, SourceDefault)
	}
}

// TestEnvVarName verifies the conversion of JSON config keys to GOROUTER_
// environment variable names.
func TestEnvVarName(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{"log_level", "GOROUTER_LOG_LEVEL"},
		{"host", "GOROUTER_HOST"},
		{"port", "GOROUTER_PORT"},
		{"database_url", "GOROUTER_DATABASE_URL"},
		{"dashboard_user", "GOROUTER_DASHBOARD_USER"},
		{"dashboard_pass", "GOROUTER_DASHBOARD_PASS"},
		{"session_secret", "GOROUTER_SESSION_SECRET"},
		{"log_format", "GOROUTER_LOG_FORMAT"},
		{"mode", "GOROUTER_MODE"},
	}
	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			if got := envVarName(tt.key); got != tt.want {
				t.Errorf("envVarName(%q) = %q, want %q", tt.key, got, tt.want)
			}
		})
	}
}

// TestRedactDatabaseURLEdgeCases directly tests the redactDatabaseURL helper
// with various URL formats.
func TestRedactDatabaseURLEdgeCases(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{"empty string", "", ""},
		{"no user info", "postgres://localhost:5432/db", "postgres://localhost:5432/db"},
		{"user without password", "postgres://alice@localhost:5432/db", "postgres://alice@localhost:5432/db"},
		{"user with password", "postgres://alice:s3cret@localhost:5432/db", "postgres://alice:%2A%2A%2A%2A@localhost:5432/db"},
		{"invalid url", "://not-a-valid-url", "://not-a-valid-url"},
		{"non-postgres scheme", "redis://:pass@localhost:6379/0", "redis://:%2A%2A%2A%2A@localhost:6379/0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := redactDatabaseURL(tt.url); got != tt.want {
				t.Errorf("redactDatabaseURL(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// TestApplyDotEnvAllFields verifies that every field can be set through a .env
// map.
func TestApplyDotEnvAllFields(t *testing.T) {
	env := map[string]string{
		"GOROUTER_PORT":           "3000",
		"GOROUTER_HOST":           "0.0.0.0",
		"GOROUTER_LOG_LEVEL":      "debug",
		"GOROUTER_LOG_FORMAT":     "text",
		"GOROUTER_DATABASE_URL":   "postgres://user:pass@localhost/db",
		"GOROUTER_DASHBOARD_USER": "admin",
		"GOROUTER_DASHBOARD_PASS": "admin123",
		"GOROUTER_SESSION_SECRET": "s3cr3t",
	}

	cfg := defaultConfig()
	cfg = applyDotEnv(cfg, env)

	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.Host != "0.0.0.0" {
		t.Errorf("Host = %q, want %q", cfg.Host, "0.0.0.0")
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "text")
	}
	if cfg.DatabaseURL != "postgres://user:pass@localhost/db" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@localhost/db")
	}
	if cfg.DashboardUser != "admin" {
		t.Errorf("DashboardUser = %q, want %q", cfg.DashboardUser, "admin")
	}
	if cfg.DashboardPass != "admin123" {
		t.Errorf("DashboardPass = %q, want %q", cfg.DashboardPass, "admin123")
	}
	if cfg.SessionSecret != "s3cr3t" {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, "s3cr3t")
	}

	// Verify source tracking
	if got := cfg.Source("port"); got != SourceDotEnv {
		t.Errorf("Source(port) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("host"); got != SourceDotEnv {
		t.Errorf("Source(host) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("log_level"); got != SourceDotEnv {
		t.Errorf("Source(log_level) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("log_format"); got != SourceDotEnv {
		t.Errorf("Source(log_format) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("database_url"); got != SourceDotEnv {
		t.Errorf("Source(database_url) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("dashboard_user"); got != SourceDotEnv {
		t.Errorf("Source(dashboard_user) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("dashboard_pass"); got != SourceDotEnv {
		t.Errorf("Source(dashboard_pass) = %v, want %v", got, SourceDotEnv)
	}
	if got := cfg.Source("session_secret"); got != SourceDotEnv {
		t.Errorf("Source(session_secret) = %v, want %v", got, SourceDotEnv)
	}
}

// TestApplyDotEnvNilMap verifies that applyDotEnv silently returns the input
// config when the env map is nil.
func TestApplyDotEnvNilMap(t *testing.T) {
	cfg := defaultConfig()
	cfg.Port = 9999
	result := applyDotEnv(cfg, nil)
	if result.Port != 9999 {
		t.Error("applyDotEnv with nil map should not alter config")
	}
}

// TestApplyDotEnvInvalidPort verifies that an unparsable port value in .env is
// silently ignored.
func TestApplyDotEnvInvalidPort(t *testing.T) {
	env := map[string]string{"GOROUTER_PORT": "not-a-number"}
	cfg := defaultConfig()
	result := applyDotEnv(cfg, env)
	if result.Port != cfg.Port {
		t.Errorf("invalid port in .env should be ignored; got %d, want %d", result.Port, cfg.Port)
	}
}

// TestApplyEnvAllFields verifies that every field can be set through
// environment variables.
func TestApplyEnvAllFields(t *testing.T) {
	getenv := func(key string) string {
		m := map[string]string{
			"GOROUTER_PORT":           "4000",
			"GOROUTER_HOST":           "192.168.1.1",
			"GOROUTER_LOG_LEVEL":      "warn",
			"GOROUTER_LOG_FORMAT":     "text",
			"GOROUTER_DATABASE_URL":   "postgres://user:pass@pg.example.com/db",
			"GOROUTER_DASHBOARD_USER": "root",
			"GOROUTER_DASHBOARD_PASS": "rootpass",
			"GOROUTER_SESSION_SECRET": "mysecret",
		}
		return m[key]
	}

	cfg := defaultConfig()
	cfg = applyEnv(cfg, getenv)

	if cfg.Port != 4000 {
		t.Errorf("Port = %d, want 4000", cfg.Port)
	}
	if cfg.Host != "192.168.1.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "192.168.1.1")
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "warn")
	}
	if cfg.LogFormat != "text" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "text")
	}
	if cfg.DatabaseURL != "postgres://user:pass@pg.example.com/db" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://user:pass@pg.example.com/db")
	}
	if cfg.DashboardUser != "root" {
		t.Errorf("DashboardUser = %q, want %q", cfg.DashboardUser, "root")
	}
	if cfg.DashboardPass != "rootpass" {
		t.Errorf("DashboardPass = %q, want %q", cfg.DashboardPass, "rootpass")
	}
	if cfg.SessionSecret != "mysecret" {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, "mysecret")
	}

	// Verify source tracking
	if got := cfg.Source("port"); got != SourceEnv {
		t.Errorf("Source(port) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("host"); got != SourceEnv {
		t.Errorf("Source(host) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("log_level"); got != SourceEnv {
		t.Errorf("Source(log_level) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("log_format"); got != SourceEnv {
		t.Errorf("Source(log_format) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("database_url"); got != SourceEnv {
		t.Errorf("Source(database_url) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("dashboard_user"); got != SourceEnv {
		t.Errorf("Source(dashboard_user) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("dashboard_pass"); got != SourceEnv {
		t.Errorf("Source(dashboard_pass) = %v, want %v", got, SourceEnv)
	}
	if got := cfg.Source("session_secret"); got != SourceEnv {
		t.Errorf("Source(session_secret) = %v, want %v", got, SourceEnv)
	}
}

// TestApplyEnvNilGetEnv verifies that applyEnv silently returns the input
// config when getenv is nil.
func TestApplyEnvNilGetEnv(t *testing.T) {
	cfg := defaultConfig()
	cfg.Port = 7777
	result := applyEnv(cfg, nil)
	if result.Port != 7777 {
		t.Error("applyEnv with nil getenv should not alter config")
	}
}

// TestApplyEnvInvalidPort verifies that an unparsable port value in the
// environment is silently ignored.
func TestApplyEnvInvalidPort(t *testing.T) {
	getenv := func(key string) string {
		if key == "GOROUTER_PORT" {
			return "bad-port"
		}
		return ""
	}
	cfg := defaultConfig()
	result := applyEnv(cfg, getenv)
	if result.Port != cfg.Port {
		t.Errorf("invalid port in env should be ignored; got %d, want %d", result.Port, cfg.Port)
	}
}

// TestApplyFlagConfigAllFields verifies that every config field can be set
// through command-line flags.
func TestApplyFlagConfigAllFields(t *testing.T) {
	flags := map[string]string{
		"host":           "10.0.0.1",
		"port":           "9090",
		"log_level":      "error",
		"log_format":     "json",
		"database_url":   "postgres://u:p@host/db",
		"dashboard_user": "admin",
		"dashboard_pass": "secret123",
		"session_secret": "topsecret",
	}

	cfg := defaultConfig()
	cfg = applyFlagConfig(cfg, flags)

	if cfg.Host != "10.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "10.0.0.1")
	}
	if cfg.Port != 9090 {
		t.Errorf("Port = %d, want 9090", cfg.Port)
	}
	if cfg.LogLevel != "error" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "error")
	}
	if cfg.LogFormat != "json" {
		t.Errorf("LogFormat = %q, want %q", cfg.LogFormat, "json")
	}
	if cfg.DatabaseURL != "postgres://u:p@host/db" {
		t.Errorf("DatabaseURL = %q, want %q", cfg.DatabaseURL, "postgres://u:p@host/db")
	}
	if cfg.DashboardUser != "admin" {
		t.Errorf("DashboardUser = %q, want %q", cfg.DashboardUser, "admin")
	}
	if cfg.DashboardPass != "secret123" {
		t.Errorf("DashboardPass = %q, want %q", cfg.DashboardPass, "secret123")
	}
	if cfg.SessionSecret != "topsecret" {
		t.Errorf("SessionSecret = %q, want %q", cfg.SessionSecret, "topsecret")
	}

	// Verify source tracking
	if got := cfg.Source("host"); got != SourceFlag {
		t.Errorf("Source(host) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("port"); got != SourceFlag {
		t.Errorf("Source(port) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("log_level"); got != SourceFlag {
		t.Errorf("Source(log_level) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("log_format"); got != SourceFlag {
		t.Errorf("Source(log_format) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("database_url"); got != SourceFlag {
		t.Errorf("Source(database_url) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("dashboard_user"); got != SourceFlag {
		t.Errorf("Source(dashboard_user) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("dashboard_pass"); got != SourceFlag {
		t.Errorf("Source(dashboard_pass) = %v, want %v", got, SourceFlag)
	}
	if got := cfg.Source("session_secret"); got != SourceFlag {
		t.Errorf("Source(session_secret) = %v, want %v", got, SourceFlag)
	}
}

// TestParseFlagsError verifies that parseFlags returns an error for invalid
// flag values.
func TestParseFlagsError(t *testing.T) {
	// --port expects an int; passing a non-numeric value should fail.
	_, err := parseFlags([]string{"gorouter", "--port", "not-a-number"})
	if err == nil {
		t.Error("expected error for invalid --port value")
	}
}

// TestLoadConfigFlagsError verifies that LoadConfig propagates flag-parsing
// errors.
func TestLoadConfigFlagsError(t *testing.T) {
	_, err := LoadConfig(
		[]string{"gorouter", "--port", "not-a-number"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		nil,
	)
	if err == nil {
		t.Error("expected error when LoadConfig receives invalid flags")
	}
}

// TestLoadConfigEmptyEnv verifies LoadConfig with an empty environment (no env
// vars, no .env file, no DB config).
func TestLoadConfigEmptyEnv(t *testing.T) {
	cfg, err := LoadConfig(
		[]string{"gorouter"},
		func(string) string { return "" },
		func() map[string]string { return map[string]string{} },
		nil,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want default 8080", cfg.Port)
	}
	if cfg.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", cfg.Host, "127.0.0.1")
	}
}

// TestDatabaseURLPrecedence verifies DATABASE_URL precedence through layers.
func TestDatabaseURLPrecedence(t *testing.T) {
	// DB provides a value.
	readDB := func(_ context.Context) (DBConfig, error) {
		return DBConfig{Host: "db.internal"}, nil
	}
	// .env sets DATABASE_URL.
	dotEnv := map[string]string{
		"GOROUTER_DATABASE_URL": "postgres://env:pass@dotenv-host/db",
	}
	// Env overrides .env.
	t.Setenv("GOROUTER_DATABASE_URL", "postgres://env:pass@env-host/db")
	// Flag overrides everything.
	cfg, err := LoadConfig(
		[]string{"gorouter", "--database-url", "postgres://env:pass@flag-host/db"},
		os.Getenv,
		func() map[string]string { return dotEnv },
		readDB,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	if cfg.DatabaseURL != "postgres://env:pass@flag-host/db" {
		t.Errorf("flag should win; got %q", cfg.DatabaseURL)
	}
	if got := cfg.Source("database_url"); got != SourceFlag {
		t.Errorf("Source(database_url) = %v, want %v", got, SourceFlag)
	}
}

// --- modes.go tests ---

// TestDispatchModeServer verifies dispatchServer runs without error.
func TestDispatchModeServer(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(defaultConfig(), WithStderr(&buf))
	code, err := DispatchMode(ModeServer, app)
	if err != nil {
		t.Fatalf("DispatchMode server returned error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(buf.String(), "starting server mode") {
		t.Errorf("expected log output, got %q", buf.String())
	}
}

// TestDispatchModeCLI verifies dispatchCLI runs without error.
func TestDispatchModeCLI(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(defaultConfig(), WithStderr(&buf))
	code, err := DispatchMode(ModeCLI, app)
	if err != nil {
		t.Fatalf("DispatchMode cli returned error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(buf.String(), "starting CLI mode") {
		t.Errorf("expected log output, got %q", buf.String())
	}
}

// TestDispatchModeTray verifies dispatchTray runs without error.
func TestDispatchModeTray(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(defaultConfig(), WithStderr(&buf))
	code, err := DispatchMode(ModeTray, app)
	if err != nil {
		t.Fatalf("DispatchMode tray returned error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(buf.String(), "starting tray mode") {
		t.Errorf("expected log output, got %q", buf.String())
	}
}

// TestDispatchModeHelper verifies dispatchHelper prints usage and exits cleanly.
func TestDispatchModeHelper(t *testing.T) {
	var buf bytes.Buffer
	app := NewApp(defaultConfig(), WithStderr(&buf))
	code, err := DispatchMode(ModeHelper, app)
	if err != nil {
		t.Fatalf("DispatchMode helper returned error: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if !strings.Contains(buf.String(), "gorouter") {
		t.Errorf("expected \"gorouter\" in usage output, got %q", buf.String())
	}
}

// TestDispatchModeUnknown verifies that an unrecognised mode returns an error.
func TestDispatchModeUnknown(t *testing.T) {
	app := NewApp(defaultConfig(), WithStderr(new(bytes.Buffer)))
	code, err := DispatchMode(Mode("bogus"), app)
	if err == nil {
		t.Error("expected error for unknown mode")
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// --- app.go tests ---

// TestNewApp verifies NewApp stores the config and applies functional options.
func TestNewApp(t *testing.T) {
	cfg := defaultConfig()
	var buf bytes.Buffer
	app := NewApp(cfg, WithStderr(&buf), WithStdout(&buf))
	if app.Config.Host != cfg.Host || app.Config.Port != cfg.Port {
		t.Error("NewApp should store the provided config")
	}
	if app.Stderr != &buf {
		t.Error("WithStderr option not applied")
	}
	if app.Stdout != &buf {
		t.Error("WithStdout option not applied")
	}
}

// TestNewAppDefaultWriters verifies NewApp defaults to os.Stderr and os.Stdout.
func TestNewAppDefaultWriters(t *testing.T) {
	cfg := defaultConfig()
	app := NewApp(cfg)
	if app.Stderr != os.Stderr {
		t.Error("default Stderr should be os.Stderr")
	}
	if app.Stdout != os.Stdout {
		t.Error("default Stdout should be os.Stdout")
	}
}

// TestNewLogger verifies newLogger produces JSON output with the configured
// level.
func TestNewLogger(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.LogLevel = "debug"
	logger := newLogger(cfg, &buf)
	logger.Info().Msg("hello")
	output := buf.String()
	if !strings.Contains(output, "\"level\":\"info\"") {
		t.Errorf("expected JSON log with level info, got %q", output)
	}
	if !strings.Contains(output, "\"message\":\"hello\"") {
		t.Errorf("expected message in JSON log, got %q", output)
	}
}

// TestNewLoggerTextFormat verifies newLogger produces text output when format
// is set to "text".
func TestNewLoggerTextFormat(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.LogFormat = "text"
	logger := newLogger(cfg, &buf)
	logger.Info().Msg("hello")
	if buf.Len() == 0 {
		t.Error("text logger should have written output")
	}
}

// TestNewLoggerInvalidLevel verifies newLogger falls back to InfoLevel when the
// configured level cannot be parsed.
func TestNewLoggerInvalidLevel(t *testing.T) {
	var buf bytes.Buffer
	cfg := defaultConfig()
	cfg.LogLevel = "not-a-level"
	logger := newLogger(cfg, &buf)
	logger.Info().Msg("hello")
	output := buf.String()
	if !strings.Contains(output, "\"level\":\"info\"") {
		t.Errorf("expected fallback to info level, got %q", output)
	}
}

// TestAppClose verifies Close is a no-op that returns nil.
func TestAppClose(t *testing.T) {
	app := NewApp(defaultConfig())
	if err := app.Close(); err != nil {
		t.Errorf("Close returned error: %v", err)
	}
}

// TestAppNewLoggerAllLevels verifies each valid log level is accepted.
func TestAppNewLoggerAllLevels(t *testing.T) {
	levels := []string{"trace", "debug", "info", "warn", "error", "fatal", "panic"}
	for _, lvl := range levels {
		t.Run(lvl, func(t *testing.T) {
			var buf bytes.Buffer
			cfg := defaultConfig()
			cfg.LogLevel = lvl
			logger := newLogger(cfg, &buf)
			logger.Log().Msg("test always")
			if buf.Len() == 0 {
				t.Errorf("logger with level %q produced no output", lvl)
			}
		})
	}
}

// TestRedactedSourcedConfig verifies redaction on a fully loaded config with
// source tracking intact.
func TestRedactedSourcedConfig(t *testing.T) {
	cfg, err := LoadConfig(
		[]string{"gorouter", "--dashboard-pass", "flag-secret", "--session-secret", "flag-session", "--database-url", "postgres://user:dbpass@localhost/db"},
		func(string) string { return "" },
		func() map[string]string { return nil },
		nil,
	)
	if err != nil {
		t.Fatalf("LoadConfig returned error: %v", err)
	}
	redacted := cfg.Redacted()

	if redacted.DashboardPass != "****" {
		t.Errorf("DashboardPass = %q, want %q", redacted.DashboardPass, "****")
	}
	if redacted.SessionSecret != "****" {
		t.Errorf("SessionSecret = %q, want %q", redacted.SessionSecret, "****")
	}
	if want := "postgres://user:%2A%2A%2A%2A@localhost/db"; redacted.DatabaseURL != want {
		t.Errorf("DatabaseURL = %q, want %q", redacted.DatabaseURL, want)
	}
	// Non-secret fields should remain intact.
	if redacted.Host != "127.0.0.1" {
		t.Errorf("Host = %q, want %q", redacted.Host, "127.0.0.1")
	}
}
