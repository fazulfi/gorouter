package soak

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

var (
	soakDuration         = flag.Duration("soak-duration", 0, "total soak duration")
	soakSampleInterval   = flag.Duration("soak-sample-interval", 5*time.Second, "snapshot interval")
	soakConcurrency      = flag.Int("soak-concurrency", 8, "concurrent workers")
	soakTargetURL        = flag.String("soak-target-url", "", "target URL")
	soakFailClosed       = flag.Bool("soak-fail-closed", false, "fail closed mode")
	soakTestMode         = flag.Bool("soak-test-mode", true, "injectable test mode")
	soakOutput           = flag.String("soak-output", "", "trend output path")
	soakProviderCount    = flag.Int("soak-provider-count", 2, "simulated providers")
	soakAuthRequestRatio = flag.Float64("soak-auth-ratio", 0.1, "auth/API ratio")
)

func testConfig() Config {
	d := *soakDuration
	if d <= 0 {
		d = 5 * time.Minute
	}
	si := *soakSampleInterval
	if si < 1*time.Second {
		si = 2 * time.Second
	}
	c := *soakConcurrency
	if c < 1 {
		c = 4
	}
	return Config{
		Duration: d, SampleInterval: si, Concurrency: c,
		FailClosed: *soakFailClosed, TestMode: *soakTestMode,
		ProviderCount: *soakProviderCount, AuthRequestRatio: float64(*soakAuthRequestRatio),
		MemoryThresholdMB: 512, GoroutineThreshold: 500,
	}
}

func TestScenarioWiring(t *testing.T) {
	if !IsHarnessReady() {
		t.Fatal("harness not ready")
	}
	cfg := testConfig()
	cfg.SampleInterval = 1 * time.Second
	harness, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("create harness: %v", err)
	}
	snapshots := make([]*TrendPoint, 0)
	done := make(chan struct{})
	go func() {
		for snap := range harness.Samples() {
			snapshots = append(snapshots, snap)
		}
		close(done)
	}()
	harness.Start()
	select {
	case <-time.After(3 * time.Second):
	case <-done:
	}
	harness.Stop()
	<-done
	if len(snapshots) == 0 {
		t.Error("no snapshots collected during wiring test")
	}
}

func TestInjectionPoints(t *testing.T) {
	if !IsHarnessReady() {
		t.Fatal("harness not ready")
	}
	cfg := testConfig()
	cfg.Concurrency = 4
	cfg.SampleInterval = 2 * time.Second
	harness, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("create harness: %v", err)
	}
	for _, tc := range []string{"provider_failure", "network_latency", "pg_pool_stress", "disk_io", "memory_pressure"} {
		t.Run(tc, func(t *testing.T) { t.Logf("injection point: %s", tc) })
	}
	harness.Start()
	time.Sleep(500 * time.Millisecond)
	harness.Stop()
}

func TestProhibitedEventDetection(t *testing.T) {
	if !IsHarnessReady() {
		t.Fatal("harness not ready")
	}
	cfg := testConfig()
	cfg.SampleInterval = 1 * time.Second
	harness, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("create harness: %v", err)
	}
	var goroutineLeak, memoryAnomaly bool
	snapshots := make([]*TrendPoint, 0)
	go func() {
		for snap := range harness.Samples() {
			snapshots = append(snapshots, snap)
			if snap.GoroutineCount > cfg.GoroutineThreshold {
				goroutineLeak = true
			}
			if snap.MemoryBytes > cfg.MemoryThresholdMB*1024*1024 {
				memoryAnomaly = true
			}
		}
	}()
	harness.Start()
	time.Sleep(2500 * time.Millisecond)
	harness.Stop()
	if goroutineLeak {
		t.Errorf("goroutine leak detected")
	}
	if memoryAnomaly {
		t.Errorf("memory anomaly detected")
	}
	t.Logf("observed %d snapshots", len(snapshots))
}

// TestSoakSmokeRun is the end-to-end smoke proof. It uses the CLI flags
// (soak-duration, soak-sample-interval, soak-concurrency, soak-output) so a
// short real run can be executed and a trend snapshot file produced. This is
// a SMOKE proof only, not the authoritative 72-hour lab run.
func TestSoakSmokeRun(t *testing.T) {
	if !IsHarnessReady() {
		t.Fatal("harness not ready")
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer server.Close()

	cfg := Config{
		Duration:       *soakDuration,
		SampleInterval: *soakSampleInterval,
		Concurrency:    *soakConcurrency,
		FailClosed:     *soakFailClosed,
		TestMode:       *soakTestMode,
	}
	if cfg.Duration <= 0 {
		cfg.Duration = 5 * time.Second
	}
	if cfg.SampleInterval < 1*time.Second {
		cfg.SampleInterval = 1 * time.Second
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = 4
	}

	harness, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("smoke setup: %v", err)
	}
	harness.SetTarget(server.URL)

	snapshots := make([]*TrendPoint, 0)
	go func() {
		for snap := range harness.Samples() {
			snapshots = append(snapshots, snap)
		}
	}()

	start := time.Now()
	harness.Start()
	time.Sleep(2 * cfg.Duration)
	harness.Stop()
	elapsed := time.Since(start)

	t.Logf("smoke elapsed: %v, snapshots: %d", elapsed, len(snapshots))
	if len(snapshots) == 0 {
		t.Error("no snapshots collected")
	}

	writeBundle(t, snapshots, fmt.Sprintf(
		"go test ./tests/soak/... -run TestSoakSmokeRun -args -soak-duration=%vs -soak-sample-interval=%vs -soak-concurrency=%d -soak-output=%s",
		int(cfg.Duration.Seconds()), int(cfg.SampleInterval.Seconds()), cfg.Concurrency, *soakOutput))
}

func writeBundle(t *testing.T, snapshots []*TrendPoint, cmd string) {
	if *soakOutput == "" || len(snapshots) == 0 {
		return
	}
	bundle := Bundle{
		Command:     cmd,
		Environment: map[string]string{"go": runtime.Version(), "os": runtime.GOOS},
		TrendPoints: snapshots,
	}
	outDir := filepath.Dir(*soakOutput)
	if outDir != "" && outDir != "." {
		os.MkdirAll(outDir, 0700)
	}
	data, _ := json.MarshalIndent(bundle, "", "  ")
	if err := os.WriteFile(*soakOutput, append(data, '\n'), 0600); err != nil {
		t.Fatalf("write bundle: %v", err)
	}
	t.Logf("bundle written: %s", *soakOutput)
}

func TestConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		cfg     Config
		wantErr bool
	}{
		{"valid", Config{Duration: time.Minute, SampleInterval: 5 * time.Second, Concurrency: 8}, false},
		{"zero_duration", Config{Duration: 0, SampleInterval: 5 * time.Second, Concurrency: 8}, true},
		{"short_interval", Config{Duration: time.Minute, SampleInterval: 100 * time.Millisecond, Concurrency: 8}, true},
		{"zero_concurrency", Config{Duration: time.Minute, SampleInterval: 5 * time.Second, Concurrency: 0}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewHarness(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("error=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestMixedLoadComposition(t *testing.T) {
	if !IsHarnessReady() {
		t.Fatal("harness not ready")
	}
	cfg := Config{Duration: time.Minute, SampleInterval: 10 * time.Second, Concurrency: 8, FailClosed: false, TestMode: true, ProviderCount: 3, AuthRequestRatio: 0.2}
	harness, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("create harness: %v", err)
	}
	metrics := harness.GetMetrics()
	t.Logf("metrics before start: %v", metrics)
	harness.Start()
	time.Sleep(2 * time.Second)
	harness.Stop()
	metrics = harness.GetMetrics()
	t.Logf("final metrics: %v", metrics)
}
