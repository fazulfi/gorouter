package soak

import (
	"context"
	"testing"
	"time"
)

func TestNewSnapshotCollectorValid(t *testing.T) {
	c, err := NewSnapshotCollector(2 * time.Second)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c == nil {
		t.Fatal("nil collector")
	}
}

func TestNewSnapshotCollectorRejectsShortInterval(t *testing.T) {
	if _, err := NewSnapshotCollector(100 * time.Millisecond); err == nil {
		t.Fatal("expected error for short interval")
	}
}

func TestSnapshotCollectorStartAndStop(t *testing.T) {
	c, err := NewSnapshotCollector(1 * time.Second)
	if err != nil {
		t.Fatalf("new collector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch := c.Start(ctx)
	time.Sleep(1500 * time.Millisecond)
	cancel()
	count := 0
	for range ch {
		count++
	}
	if count < 1 {
		t.Fatalf("expected at least 1 snapshot, got %d", count)
	}
}

func TestSnapshotCollectorGetLastPoint(t *testing.T) {
	c, err := NewSnapshotCollector(1 * time.Second)
	if err != nil {
		t.Fatalf("new collector: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c.Start(ctx)
	time.Sleep(1200 * time.Millisecond)
	cancel()
	lp := c.GetLastPoint()
	if lp == nil {
		t.Fatal("nil last point")
	}
	if lp.GoroutineCount <= 0 {
		t.Fatalf("invalid goroutine count %d", lp.GoroutineCount)
	}
	if lp.MemoryBytes <= 0 {
		t.Fatalf("invalid memory bytes %d", lp.MemoryBytes)
	}
	if !lp.Timestamp.IsZero() == false {
		t.Fatal("zero timestamp")
	}
}

func TestTrendPointZeroValues(t *testing.T) {
	p := &TrendPoint{}
	if p.CrashIndicator {
		t.Fatal("crash indicator should default false")
	}
	if p.DeadlockIndicator {
		t.Fatal("deadlock indicator should default false")
	}
	if len(p.DuplicateEvents) != 0 {
		t.Fatal("duplicate events should be empty")
	}
}
