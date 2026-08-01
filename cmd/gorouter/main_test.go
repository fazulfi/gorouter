package main

import (
	"os"
	"testing"
)

// The CLI entry path reads global os.Args, os.Stderr and the process
// environment, so these tests are strictly sequential (no t.Parallel) and
// restore every touched global via t.Cleanup / t.Setenv.

// setArgs replaces os.Args for the duration of the test.
func setArgs(t *testing.T, args ...string) {
	t.Helper()
	orig := os.Args
	os.Args = args
	t.Cleanup(func() { os.Args = orig })
}

// silenceStderr redirects process stderr to the null device so the entry
// path can be exercised without polluting test output.
func silenceStderr(t *testing.T) {
	t.Helper()
	orig := os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	os.Stderr = devNull
	t.Cleanup(func() {
		os.Stderr = orig
		devNull.Close()
	})
}

// neutralEnv empties every GOROUTER_* variable the config layer reads so a
// developer's shell environment cannot change the exercised contract.
func neutralEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"GOROUTER_PORT", "GOROUTER_HOST", "GOROUTER_LOG_LEVEL",
		"GOROUTER_LOG_FORMAT", "GOROUTER_DATABASE_URL",
		"GOROUTER_DASHBOARD_USER", "GOROUTER_DASHBOARD_PASS",
		"GOROUTER_SESSION_SECRET",
	} {
		t.Setenv(key, "")
	}
}

// TestRunInvalidFlagFailsClosed pins the CLI entry contract: an
// unparseable flag value must yield a non-zero exit code before any mode
// dispatch.
func TestRunInvalidFlagFailsClosed(t *testing.T) {
	neutralEnv(t)
	setArgs(t, "gorouter", "--port=not-an-int")
	silenceStderr(t)

	if got := run(); got == 0 {
		t.Fatalf("run() with malformed --port = 0, want non-zero exit")
	}
}

// TestRunUnknownFlagFailsClosed pins that an unknown flag is rejected with
// a non-zero exit code.
func TestRunUnknownFlagFailsClosed(t *testing.T) {
	neutralEnv(t)
	setArgs(t, "gorouter", "--no-such-flag")
	silenceStderr(t)

	if got := run(); got == 0 {
		t.Fatalf("run() with unknown flag = 0, want non-zero exit")
	}
}

// TestRunCLIModeReturnsZero pins the successful CLI entry contract: the
// cli mode dispatches without network or database side effects and exits 0.
func TestRunCLIModeReturnsZero(t *testing.T) {
	neutralEnv(t)
	setArgs(t, "gorouter", "cli")
	silenceStderr(t)

	if got := run(); got != 0 {
		t.Fatalf("run(cli) = %d, want 0", got)
	}
}

// TestRunServerModeWithoutDatabaseFailsClosed pins the fail-closed startup
// contract: server mode requires PostgreSQL, so without a configured
// database the entry point must exit non-zero instead of serving.
func TestRunServerModeWithoutDatabaseFailsClosed(t *testing.T) {
	neutralEnv(t)
	setArgs(t, "gorouter")
	silenceStderr(t)

	if got := run(); got == 0 {
		t.Fatalf("run(server, no DB) = 0, want non-zero fail-closed exit")
	}
}
