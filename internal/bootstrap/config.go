package bootstrap

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/pflag"
)

// ConfigSource represents where a config value was sourced from.
type ConfigSource int

const (
	SourceDefault ConfigSource = iota
	SourceDB
	SourceDotEnv
	SourceEnv
	SourceFlag
)

// sourceNames maps ConfigSource values to human-readable names.
var sourceNames = map[ConfigSource]string{
	SourceDefault: "default",
	SourceDB:      "db",
	SourceDotEnv:  ".env",
	SourceEnv:     "env",
	SourceFlag:    "flag",
}

func (s ConfigSource) String() string {
	if name, ok := sourceNames[s]; ok {
		return name
	}
	return "unknown"
}

// DBConfig holds configuration values that may be persisted in the database
// settings table.
type DBConfig struct {
	LogLevel string
	Host     string
	Port     int
}

// Config holds all runtime configuration for gorouter.
// Precedence: flags > env vars > .env file > DB settings > sensible defaults.
type Config struct {
	Mode Mode `json:"mode"`

	// Server binding
	Host string `json:"host"`
	Port int    `json:"port"`

	// Database
	DatabaseURL string `json:"database_url"`

	// Logging
	LogLevel  string `json:"log_level"`
	LogFormat string `json:"log_format"`

	// Auth (secrets are redacted in logs)
	DashboardUser string `json:"dashboard_user,omitempty"`
	DashboardPass string `json:"-"`
	SessionSecret string `json:"-"`

	// sources tracks where each field's value originated.
	sources map[string]ConfigSource
}

// setSource records the origin of a config field.
func (c *Config) setSource(field string, source ConfigSource) {
	if c.sources == nil {
		c.sources = make(map[string]ConfigSource)
	}
	c.sources[field] = source
}

// Source returns where the value for the given field was sourced from.
func (c Config) Source(field string) ConfigSource {
	if c.sources == nil {
		return SourceDefault
	}
	s, ok := c.sources[field]
	if !ok {
		return SourceDefault
	}
	return s
}

// Redacted returns a copy of Config with secrets masked for safe logging.
func (c Config) Redacted() Config {
	cp := c
	if cp.DashboardPass != "" {
		cp.DashboardPass = "****"
	}
	if cp.SessionSecret != "" {
		cp.SessionSecret = "****"
	}
	if cp.DatabaseURL != "" {
		cp.DatabaseURL = redactDatabaseURL(cp.DatabaseURL)
	}
	return cp
}

// redactDatabaseURL masks the password component of a PostgreSQL connection
// string so it can be safely logged.
func redactDatabaseURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	if u.User != nil {
		if _, has := u.User.Password(); has {
			u.User = url.UserPassword(u.User.Username(), "****")
			return u.String()
		}
	}
	return raw
}

// defaultConfig returns a Config populated with sensible defaults.
func defaultConfig() Config {
	return Config{
		Mode:        ModeServer,
		Host:        "127.0.0.1",
		Port:        8080,
		LogLevel:    "info",
		LogFormat:   "json",
		DatabaseURL: "",
	}
}

// LoadConfig builds a Config by layering overrides in precedence order:
// flags > env vars > .env file > DB settings > defaults.
// Layers that are unavailable (nil readers, empty maps) are silently skipped.
func LoadConfig(
	args []string,
	getenv func(string) string,
	readDotEnv func() map[string]string,
	readDB func(context.Context) (DBConfig, error),
) (Config, error) {
	cfg := defaultConfig()

	// 1. DB layer
	if readDB != nil {
		if dbCfg, err := readDB(context.Background()); err == nil {
			cfg = applyDBConfig(cfg, dbCfg)
		}
	}

	// 2. .env layer
	if readDotEnv != nil {
		cfg = applyDotEnv(cfg, readDotEnv())
	}

	// 3. Environment variable layer
	cfg = applyEnv(cfg, getenv)

	// 4. Flag layer (highest non-code precedence)
	flagCfg, err := parseFlags(args)
	if err != nil {
		return Config{}, fmt.Errorf("parse flags: %w", err)
	}
	cfg = applyFlagConfig(cfg, flagCfg)

	return cfg, nil
}

// applyDBConfig merges DB-sourced values into config.
func applyDBConfig(cfg Config, dbCfg DBConfig) Config {
	if dbCfg.LogLevel != "" {
		cfg.LogLevel = dbCfg.LogLevel
		cfg.setSource("log_level", SourceDB)
	}
	if dbCfg.Host != "" {
		cfg.Host = dbCfg.Host
		cfg.setSource("host", SourceDB)
	}
	if dbCfg.Port != 0 {
		cfg.Port = dbCfg.Port
		cfg.setSource("port", SourceDB)
	}
	return cfg
}

// applyDotEnv merges values from a parsed .env file into config.
func applyDotEnv(cfg Config, env map[string]string) Config {
	if env == nil {
		return cfg
	}
	if v, ok := env["GOROUTER_PORT"]; ok {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
			cfg.setSource("port", SourceDotEnv)
		}
	}
	if v, ok := env["GOROUTER_HOST"]; ok {
		cfg.Host = v
		cfg.setSource("host", SourceDotEnv)
	}
	if v, ok := env["GOROUTER_LOG_LEVEL"]; ok {
		cfg.LogLevel = v
		cfg.setSource("log_level", SourceDotEnv)
	}
	if v, ok := env["GOROUTER_LOG_FORMAT"]; ok {
		cfg.LogFormat = v
		cfg.setSource("log_format", SourceDotEnv)
	}
	if v, ok := env["GOROUTER_DATABASE_URL"]; ok {
		cfg.DatabaseURL = v
		cfg.setSource("database_url", SourceDotEnv)
	}
	if v, ok := env["GOROUTER_DASHBOARD_USER"]; ok {
		cfg.DashboardUser = v
		cfg.setSource("dashboard_user", SourceDotEnv)
	}
	if v, ok := env["GOROUTER_DASHBOARD_PASS"]; ok {
		cfg.DashboardPass = v
		cfg.setSource("dashboard_pass", SourceDotEnv)
	}
	if v, ok := env["GOROUTER_SESSION_SECRET"]; ok {
		cfg.SessionSecret = v
		cfg.setSource("session_secret", SourceDotEnv)
	}
	return cfg
}

// applyEnv merges environment variable values into config.
func applyEnv(cfg Config, getenv func(string) string) Config {
	if getenv == nil {
		return cfg
	}
	if v := getenv("GOROUTER_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			cfg.Port = p
			cfg.setSource("port", SourceEnv)
		}
	}
	if v := getenv("GOROUTER_HOST"); v != "" {
		cfg.Host = v
		cfg.setSource("host", SourceEnv)
	}
	if v := getenv("GOROUTER_LOG_LEVEL"); v != "" {
		cfg.LogLevel = v
		cfg.setSource("log_level", SourceEnv)
	}
	if v := getenv("GOROUTER_LOG_FORMAT"); v != "" {
		cfg.LogFormat = v
		cfg.setSource("log_format", SourceEnv)
	}
	if v := getenv("GOROUTER_DATABASE_URL"); v != "" {
		cfg.DatabaseURL = v
		cfg.setSource("database_url", SourceEnv)
	}
	if v := getenv("GOROUTER_DASHBOARD_USER"); v != "" {
		cfg.DashboardUser = v
		cfg.setSource("dashboard_user", SourceEnv)
	}
	if v := getenv("GOROUTER_DASHBOARD_PASS"); v != "" {
		cfg.DashboardPass = v
		cfg.setSource("dashboard_pass", SourceEnv)
	}
	if v := getenv("GOROUTER_SESSION_SECRET"); v != "" {
		cfg.SessionSecret = v
		cfg.setSource("session_secret", SourceEnv)
	}
	return cfg
}

// parseFlags extracts configuration from command-line flags.
// It returns a map of field names to string values; the map may be empty
// (zero-length) if no flags are provided.
func parseFlags(args []string) (map[string]string, error) {
	f := pflag.NewFlagSet("gorouter", pflag.ContinueOnError)
	f.String("host", "", "bind host (default: 127.0.0.1)")
	f.Int("port", 0, "bind port (default: 8080)")
	f.String("log-level", "", "log level: trace, debug, info, warn, error (default: info)")
	f.String("log-format", "", "log format: json, text (default: json)")
	f.String("database-url", "", "PostgreSQL connection string")
	f.String("dashboard-user", "", "dashboard admin username")
	f.String("dashboard-pass", "", "dashboard admin password")
	f.String("session-secret", "", "session signing secret")

	if err := f.Parse(args[1:]); err != nil {
		return nil, err
	}

	result := make(map[string]string)
	if v, _ := f.GetString("host"); v != "" {
		result["host"] = v
	}
	if v, _ := f.GetInt("port"); v != 0 {
		result["port"] = strconv.Itoa(v)
	}
	if v, _ := f.GetString("log-level"); v != "" {
		result["log_level"] = v
	}
	if v, _ := f.GetString("log-format"); v != "" {
		result["log_format"] = v
	}
	if v, _ := f.GetString("database-url"); v != "" {
		result["database_url"] = v
	}
	if v, _ := f.GetString("dashboard-user"); v != "" {
		result["dashboard_user"] = v
	}
	if v, _ := f.GetString("dashboard-pass"); v != "" {
		result["dashboard_pass"] = v
	}
	if v, _ := f.GetString("session-secret"); v != "" {
		result["session_secret"] = v
	}
	return result, nil
}

// applyFlagConfig merges flag-parsed values into config with highest priority.
func applyFlagConfig(cfg Config, flags map[string]string) Config {
	for k, v := range flags {
		switch k {
		case "host":
			cfg.Host = v
			cfg.setSource("host", SourceFlag)
		case "port":
			if p, err := strconv.Atoi(v); err == nil {
				cfg.Port = p
				cfg.setSource("port", SourceFlag)
			}
		case "log_level":
			cfg.LogLevel = v
			cfg.setSource("log_level", SourceFlag)
		case "log_format":
			cfg.LogFormat = v
			cfg.setSource("log_format", SourceFlag)
		case "database_url":
			cfg.DatabaseURL = v
			cfg.setSource("database_url", SourceFlag)
		case "dashboard_user":
			cfg.DashboardUser = v
			cfg.setSource("dashboard_user", SourceFlag)
		case "dashboard_pass":
			cfg.DashboardPass = v
			cfg.setSource("dashboard_pass", SourceFlag)
		case "session_secret":
			cfg.SessionSecret = v
			cfg.setSource("session_secret", SourceFlag)
		}
	}
	return cfg
}

// envVarName converts a Config JSON key to its GOROUTER_ environment variable
// name. For example "log_level" becomes "GOROUTER_LOG_LEVEL".
func envVarName(key string) string {
	return "GOROUTER_" + strings.ToUpper(key)
}
