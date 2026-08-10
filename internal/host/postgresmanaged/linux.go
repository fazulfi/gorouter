package postgresmanaged

import "context"

type linuxAdapter struct{}

func (linuxAdapter) start(ctx context.Context, config Config) error {
	return runCommand(ctx, config, "pg_ctl", "start", "-D", config.DataDir, "-w")
}

func (linuxAdapter) stop(ctx context.Context, config Config) error {
	return runCommand(ctx, config, "pg_ctl", "stop", "-D", config.DataDir, "-m", "fast", "-w")
}

func (linuxAdapter) verify(_ context.Context, config Config) error { return verifyMarker(config) }
func NewLinux(config Config) (*Manager, error)                     { return New(config, linuxAdapter{}) }
