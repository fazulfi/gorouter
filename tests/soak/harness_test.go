package soak

import (
	"testing"
	"time"
)

func TestValidateRejectsZeroDuration(t *testing.T) {
	cfg := Config{Duration: 0, SampleInterval: 2 * time.Second, Concurrency: 8}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero duration")
	}
}

func TestValidateRejectsShortInterval(t *testing.T) {
	cfg := Config{Duration: time.Minute, SampleInterval: 100 * time.Millisecond, Concurrency: 8}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for short interval")
	}
}

func TestValidateRejectsZeroConcurrency(t *testing.T) {
	cfg := Config{Duration: time.Minute, SampleInterval: 2 * time.Second, Concurrency: 0}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected error for zero concurrency")
	}
}

func TestValidateRejectsAuthRatioOutOfRange(t *testing.T) {
	for _, r := range []float64{-0.5, 1.5} {
		cfg := Config{Duration: time.Minute, SampleInterval: 2 * time.Second, Concurrency: 8, AuthRequestRatio: r}
		if err := cfg.Validate(); err == nil {
			t.Fatalf("expected error for auth ratio %v", r)
		}
	}
}

func TestValidateAcceptsValidConfig(t *testing.T) {
	cfg := Config{Duration: time.Minute, SampleInterval: 2 * time.Second, Concurrency: 8, AuthRequestRatio: 0.2}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewHarnessRejectsInvalid(t *testing.T) {
	if _, err := NewHarness(Config{}); err == nil {
		t.Fatal("expected error for invalid config")
	}
}

func TestNewHarnessAcceptsValid(t *testing.T) {
	cfg := Config{Duration: time.Minute, SampleInterval: 2 * time.Second, Concurrency: 8}
	h, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if h == nil {
		t.Fatal("nil harness")
	}
	if !IsHarnessReady() {
		t.Fatal("harness not ready")
	}
}

func TestSetTarget(t *testing.T) {
	h := mustHarness(t)
	h.SetTarget("http://example.test")
	if got := h.target(); got != "http://example.test" {
		t.Fatalf("target = %q, want %q", got, "http://example.test")
	}
}

func TestHarnessLifecycleShort(t *testing.T) {
	h := mustHarness(t)
	h.SetTarget("http://127.0.0.1:1")
	h.Start()
	time.Sleep(500 * time.Millisecond)
	h.Stop()
	h.Stop() // Idempotent.
}

func TestHarnessGetMetrics(t *testing.T) {
	h := mustHarness(t)
	m := h.GetMetrics()
	if m.MemoryBytes < 0 {
		t.Fatal("negative memory bytes")
	}
}

func TestHarnessString(t *testing.T) {
	h := mustHarness(t)
	if s := h.String(); s == "" {
		t.Fatal("empty String()")
	}
}

func TestHarnessSamplesChannelClosedOnStop(t *testing.T) {
	h := mustHarness(t)
	h.SetTarget("http://127.0.0.1:1")
	got := 0
	done := make(chan struct{})
	go func() {
		for range h.Samples() {
			got++
		}
		close(done)
	}()
	h.Start()
	time.Sleep(300 * time.Millisecond)
	h.Stop()
	<-done
}

func mustHarness(t *testing.T) *Harness {
	t.Helper()
	cfg := Config{Duration: 30 * time.Second, SampleInterval: 2 * time.Second, Concurrency: 4}
	h, err := NewHarness(cfg)
	if err != nil {
		t.Fatalf("new harness: %v", err)
	}
	return h
}
