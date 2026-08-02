package console

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/console"
)

func TestRetentionServicePurgeBefore(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRetentionService(newFakeBeginner(scope))
	cutoff := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)

	// Seed rows around the cutoff; only rows with retention_until
	// strictly before the cutoff may be removed.
	before := cutoff.Add(-24 * time.Hour)
	at := cutoff
	after := cutoff.Add(24 * time.Hour)
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

	deleted, err := svc.PurgeBefore(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1 (strictly before cutoff only)", deleted)
	}
	if !repo.purgeCutoff.Equal(cutoff) {
		t.Errorf("repo cutoff = %v, want %v", repo.purgeCutoff, cutoff)
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
			t.Error("row strictly before cutoff survived")
		}
	}
}

func TestRetentionServicePurgeBeforeIdempotentSecondRun(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRetentionService(newFakeBeginner(scope))
	cutoff := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	before := cutoff.Add(-24 * time.Hour)
	if err := repo.Append(context.Background(), &console.ConsoleLog{
		Seq: 1, RedactedMessage: "expired", RetentionUntil: &before,
	}); err != nil {
		t.Fatalf("seed Append: %v", err)
	}
	if _, err := svc.PurgeBefore(context.Background(), cutoff); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if deleted, err := svc.PurgeBefore(context.Background(), cutoff); err != nil {
		t.Fatalf("PurgeBefore #2: %v", err)
	} else if deleted != 0 {
		t.Errorf("second run deleted = %d, want 0 (idempotent)", deleted)
	}
	if scope.commits != 2 {
		t.Errorf("commits = %d, want 2", scope.commits)
	}
}

func TestRetentionServicePurgeBeforeErrorRollback(t *testing.T) {
	scope, repo := newFakeScope()
	repo.purgeErr = errors.New("delete failed")
	svc := NewRetentionService(newFakeBeginner(scope))
	if _, err := svc.PurgeBefore(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected error")
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
	}
}

func TestRetentionServicePurgeBeforeCommitError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.commitErr = errors.New("commit failed")
	svc := NewRetentionService(newFakeBeginner(scope))
	if _, err := svc.PurgeBefore(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected error")
	}
	if scope.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", scope.rollbacks)
	}
}

func TestRetentionServicePurgeBeforeBeginError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.beginErr = errors.New("begin failed")
	svc := NewRetentionService(newFakeBeginner(scope))
	if _, err := svc.PurgeBefore(context.Background(), time.Now().UTC()); err == nil {
		t.Fatal("expected error")
	}
	if scope.rollbacks != 0 {
		t.Errorf("rollbacks = %d, want 0", scope.rollbacks)
	}
}
