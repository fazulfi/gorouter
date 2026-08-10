package postgresmanaged

import "context"

type windowsAdapter struct{}

func (windowsAdapter) start(ctx context.Context, config Config) error {
	return runCommand(ctx, config, "pg_ctl.exe", "start", "-D", config.DataDir, "-w")
}
func (windowsAdapter) stop(ctx context.Context, config Config) error {
	return runCommand(ctx, config, "pg_ctl.exe", "stop", "-D", config.DataDir, "-m", "fast", "-w")
}
func (windowsAdapter) verify(_ context.Context, config Config) error { return verifyMarker(config) }
func NewWindows(config Config) (*Manager, error)                     { return New(config, windowsAdapter{}) }
