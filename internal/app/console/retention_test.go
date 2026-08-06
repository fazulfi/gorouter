package console

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/console"
)

func TestRetentionServicePurgeExpired(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRetentionService(newFakeBeginner(scope))
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	// Seed rows around the purge time; only rows with retention_until
	// strictly before it may be removed.
	before := now.Add(-24 * time.Hour)
	at := now
	after := now.Add(24 * time.Hour)
	for _, e := range []*console.ConsoleLog{
		{Seq: 1, RedactedMessage: "expired", RetentionUntil: &before},
		{Seq: 2, RedactedMessage: "at-cutoff", RetentionUntil: &at},
		{Seq: 3, RedactedMessage: "retained", RetentionUntil: &after},
		{Seq: 4, RedactedMessage: "no-retention"},
	} {
		if err := repo.Append(context.Background(), e); err != nil {
			t.Fatalf("seed Append: %v", err)
		}
	}

	deleted, err := svc.PurgeExpired(context.Background(), now)
	if err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (strictly before now only)", deleted)
	}
	if !repo.purgeCutoff.Equal(now) {
		t.Errorf("repo cutoff = %v, want %v (now passed through unchanged)", repo.purgeCutoff, now)
	}
	if scope.commits != 1 || scope.rollbacks != 0 {
		t.Errorf("tx: commits %d rollbacks %d, want 1/0", scope.commits, scope.rollbacks)
	}
	remaining, err := repo.ListAfter(context.Background(), 0, console.DefaultCap)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(remaining) != 3 {
		t.Fatalf("remaining = %d, want 3", len(remaining))
	}
	for _, e := range remaining {
		if e.Seq == 1 {
			t.Error("row strictly before now survived")
		}
	}
}

// TestRetentionServicePurgeExpiredPins90DayFormula pins the retention
// formula end to end: Append stamps RetentionUntil = appendTime + 90 days
// and PurgeExpired(now) removes rows whose stamp is strictly before now, so
// a row lives exactly 90 days (never ~180) and rows expiring exactly at now
// survive (strict < rule).
func TestRetentionServicePurgeExpiredPins90DayFormula(t *testing.T) {
	scope, repo := newFakeScope()
	appendTime := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	svc := NewConsoleService(newFakeBeginner(scope))
	svc.WithClock(func() time.Time { return appendTime })
	if err := svc.Append(context.Background(), &console.ConsoleLog{RedactedMessage: "line"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	expiry := appendTime.Add(console.RetentionDays * 24 * time.Hour)
	if e := repo.entries[0]; e.RetentionUntil == nil || !e.RetentionUntil.Equal(expiry) {
		t.Fatalf("retention_until = %v, want %v (stamp now+90d)", e.RetentionUntil, expiry)
	}

	ret := NewRetentionService(newFakeBeginner(scope))
	// 89 days: nothing is due.
	if deleted, err := ret.PurgeExpired(context.Background(), appendTime.Add(89*24*time.Hour)); err != nil {
		t.Fatalf("PurgeExpired(89d): %v", err)
	} else if deleted != 0 {
		t.Errorf("deleted at 89 days = %d, want 0", deleted)
	}
	// Exactly 90 days: the row expires at now but survives (strict <).
	if deleted, err := ret.PurgeExpired(context.Background(), expiry); err != nil {
		t.Fatalf("PurgeExpired(90d): %v", err)
	} else if deleted != 0 {
		t.Errorf("deleted at exact expiry = %d, want 0 (strict <)", deleted)
	}
	// One nanosecond past 90 days: deleted — the window is 90, not 180.
	if deleted, err := ret.PurgeExpired(context.Background(), expiry.Add(time.Nanosecond)); err != nil {
		t.Fatalf("PurgeExpired(90d+1ns): %v", err)
	} else if deleted != 1 {
		t.Errorf("deleted after expiry = %d, want 1 (exact 90-day window)", deleted)
	}
}

func TestRetentionServicePurgeExpiredIdempotentSecondRun(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRetentionService(newFakeBeginner(scope))
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	before := now.Add(-24 * time.Hour)
	if err := repo.Append(context.Background(), &console.ConsoleLog{
		Seq: 1, RedactedMessage: "expired", RetentionUntil: &before,
	}); err != nil {
		t.Fatalf("seed Append: %v", err)
	}
	if _, err := svc.PurgeExpired(context.Background(), now); err != nil {
		t.Fatalf("PurgeExpired: %v", err)
	}
	if deleted, err := svc.PurgeExpired(context.Background(), now); err != nil {
		t.Fatalf("PurgeExpired #2: %v", err)
	} else if deleted != 0 {
		t.Errorf("second run deleted = %d, want 0 (idempotent)", deleted)
	}
	if scope.commits != 2 {
		t.Errorf("commits = %d, want 2", scope.commits)
	}
}

func TestRetentionServicePurgeExpiredErrorRollback(t *testing.T) {
	scope, repo := newFakeScope()
	repo.purgeErr = errors.New("delete failed")
	svc := NewRetentionService(newFakeBeginner(scope))
	if _, err := svc.PurgeExpired(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected error")
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
	}
}

func TestRetentionServicePurgeExpiredCommitError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.commitErr = errors.New("commit failed")
	svc := NewRetentionService(newFakeBeginner(scope))
	if _, err := svc.PurgeExpired(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected error")
	}
	if scope.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", scope.rollbacks)
	}
}

func TestRetentionServicePurgeExpiredBeginError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.beginErr = errors.New("begin failed")
	svc := NewRetentionService(newFakeBeginner(scope))
	if _, err := svc.PurgeExpired(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected error")
	}
	if scope.rollbacks != 0 {
		t.Errorf("rollbacks = %d, want 0", scope.rollbacks)
	}
}
