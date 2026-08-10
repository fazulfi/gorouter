package postgresmanaged

import "context"

type macOSAdapter struct{}

func (macOSAdapter) start(ctx context.Context, config Config) error {
	return runCommand(ctx, config, "pg_ctl", "start", "-D", config.DataDir, "-w")
}
func (macOSAdapter) stop(ctx context.Context, config Config) error {
	return runCommand(ctx, config, "pg_ctl", "stop", "-D", config.DataDir, "-m", "fast", "-w")
}
func (macOSAdapter) verify(_ context.Context, config Config) error { return verifyMarker(config) }
func NewMacOS(config Config) (*Manager, error)                     { return New(config, macOSAdapter{}) }
