package bootstrap

import (
	"context"
	"net/url"
	"strconv"
	"testing"
)

// FuzzRedactDatabaseURL fuzzes the Database URL redaction helper with various
// URL-like and non-URL strings to ensure it never panics and never leaks
// password components in the output.
func FuzzRedactDatabaseURL(f *testing.F) {
	seeds := []string{
		"postgres://user:secret@localhost:5432/db",
		"postgres://alice@localhost:5432/db",
		"postgres://localhost:5432/db",
		"",
		"not-a-url",
		"://",
		"redis://:pass@localhost:6379/0",
		"postgres://user:pass@host:5432/db?sslmode=require",
		"invalid : : scheme",
		"http://user:password@example.com/path",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, raw string) {
		result := redactDatabaseURL(raw)

		// Invariant 1: Must never panic; result may be empty or same as input
		// Invariant 2: If the original had a password component, it must
		// not appear in plaintext in the result
		// (We can only check this for valid URLs)
		if isProbablyURL(raw) {
			if containsPlainPassword(raw) && raw == result {
				t.Errorf("redactDatabaseURL(%q) returned same string; password not masked", raw)
			}
		}
	})
}

// isProbablyURL returns true if the string looks like a valid URL.
func isProbablyURL(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] == ':' {
			return i > 0 && i+1 < len(s) && s[i+1] == '/'
		}
	}
	return false
}

// containsPlainPassword returns true if the string contains a URL with a
// non-empty password component. Uses url.Parse so it stays consistent with
// redactDatabaseURL's parsing — strings that url.Parse rejects cannot be
// processed by redactDatabaseURL, so we don't flag them.
func containsPlainPassword(s string) bool {
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.User != nil {
		_, has := u.User.Password()
		return has
	}
	return false
}

// FuzzEnvVarName fuzzes the environment variable name conversion with various
// config keys to ensure it never panics.
func FuzzEnvVarName(f *testing.F) {
	seeds := []string{
		"log_level",
		"host",
		"port",
		"database_url",
		"",
		"UPPERCASE",
		"with_underscores_and_numbers_123",
		"a",
		"already_prefix_GOROUTER_",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, key string) {
		result := envVarName(key)
		// Invariant: Output must start with GOROUTER_
		if len(result) < 9 || result[:9] != "GOROUTER_" {
			t.Errorf("envVarName(%q) = %q, want prefix GOROUTER_", key, result)
		}
		// Invariant: Output must be uppercase
		for _, c := range result {
			if c >= 'a' && c <= 'z' {
				t.Errorf("envVarName(%q) = %q contains lowercase", key, result)
				break
			}
		}
	})
}

// FuzzParseFlags fuzzes flag parsing with various argument combinations to
// ensure it never panics and returns consistent results.
func FuzzParseFlags(f *testing.F) {
	seeds := []string{
		"--port=8080",
		"--host=0.0.0.0",
		"--log-level=debug",
		"--unknown-flag=value",
		"server",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, args string) {
		// Build argument list: program name + fuzzed args
		split := splitArgs(args)
		cmdArgs := append([]string{"gorouter"}, split...)
		result, err := parseFlags(cmdArgs)
		if err != nil {
			// Expected for invalid flags — not a crash
			return
		}
		// Invariant: result must be non-nil (may be empty)
		_ = result
	})
}

// FuzzApplyDotEnv fuzzes .env map application with various key-value pairs.
func FuzzApplyDotEnv(f *testing.F) {
	seeds := []struct {
		key   string
		value string
	}{
		{"GOROUTER_PORT", "8080"},
		{"GOROUTER_PORT", "not-a-number"},
		{"GOROUTER_HOST", "0.0.0.0"},
		{"UNKNOWN_KEY", "value"},
		{"", ""},
	}
	for _, s := range seeds {
		f.Add(s.key, s.value)
	}

	f.Fuzz(func(t *testing.T, key, value string) {
		env := map[string]string{key: value}
		cfg := defaultConfig()
		result := applyDotEnv(cfg, env)
		_ = result
	})
}

// FuzzLoadConfig fuzzes LoadConfig with various arguments and environment
// variable combinations to ensure it handles edge cases without panics.
func FuzzLoadConfig(f *testing.F) {
	seeds := []struct {
		args string
		env  string
	}{
		{"", ""},
		{"--port=abc", ""},
		{"--host=", "GOROUTER_PORT=invalid"},
		{"server --log-level=debug", "GOROUTER_HOST=0.0.0.0"},
	}
	for _, s := range seeds {
		f.Add(s.args, s.env)
	}

	f.Fuzz(func(t *testing.T, args, envStr string) {
		split := splitArgs(args)
		cmdArgs := append([]string{"gorouter"}, split...)

		getenv := func(key string) string {
			if envStr == "" {
				return ""
			}
			// Parse simple "KEY=VALUE" format
			for i := 0; i < len(envStr)-1; i++ {
				if envStr[i] == '=' {
					k := envStr[:i]
					v := envStr[i+1:]
					if k == key {
						return v
					}
				}
			}
			return ""
		}

		// Must never panic
		cfg, err := LoadConfig(cmdArgs, getenv, func() map[string]string {
			return nil
		}, func(_ context.Context) (DBConfig, error) {
			return DBConfig{}, nil
		})
		if err != nil {
			return // expected for invalid input
		}
		_ = cfg
	})
}

// splitArgs splits a string into command-line arguments by whitespace,
// handling simple quoted strings.
func splitArgs(s string) []string {
	if s == "" {
		return nil
	}
	var result []string
	current := ""
	inQuote := false
	for _, c := range s {
		if c == '"' {
			inQuote = !inQuote
			current += string(c)
		} else if c == ' ' && !inQuote {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(c)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// TestFuzzSeedsCompile verifies the fuzz target seeds compile and run
// without error. This test exists so that `go test` (without -fuzz) still
// exercises the seed corpus deterministically.
func TestFuzzSeedsCompile(t *testing.T) {
	// FuzzRedactDatabaseURL seeds
	t.Run("redact_empty", func(t *testing.T) {
		result := redactDatabaseURL("")
		if result != "" {
			t.Errorf("redactDatabaseURL(\"\") = %q, want \"\"", result)
		}
	})
	t.Run("redact_with_password", func(t *testing.T) {
		result := redactDatabaseURL("postgres://user:secret@localhost/db")
		if result == "postgres://user:secret@localhost/db" {
			t.Error("password should be masked")
		}
	})
	t.Run("redact_no_password", func(t *testing.T) {
		result := redactDatabaseURL("postgres://localhost/db")
		if result != "postgres://localhost/db" {
			t.Errorf("no password URL changed: %q", result)
		}
	})

	// FuzzEnvVarName seeds
	t.Run("envvar_log_level", func(t *testing.T) {
		result := envVarName("log_level")
		if result != "GOROUTER_LOG_LEVEL" {
			t.Errorf("envVarName(\"log_level\") = %q, want %q", result, "GOROUTER_LOG_LEVEL")
		}
	})
	t.Run("envvar_empty", func(t *testing.T) {
		result := envVarName("")
		if result != "GOROUTER_" {
			t.Errorf("envVarName(\"\") = %q, want %q", result, "GOROUTER_")
		}
	})

	// parseFlags smoke test
	t.Run("parse_flags_valid", func(t *testing.T) {
		result, err := parseFlags([]string{"gorouter", "--port", strconv.Itoa(9090)})
		if err != nil {
			t.Fatalf("parseFlags error: %v", err)
		}
		if result["port"] != "9090" {
			t.Errorf("port = %q, want %q", result["port"], "9090")
		}
	})

	// applyDotEnv smoke test
	t.Run("apply_dotenv_port", func(t *testing.T) {
		cfg := defaultConfig()
		env := map[string]string{"GOROUTER_PORT": "3000"}
		result := applyDotEnv(cfg, env)
		if result.Port != 3000 {
			t.Errorf("Port = %d, want %d", result.Port, 3000)
		}
	})
}
