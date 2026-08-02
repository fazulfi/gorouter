package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/usage"
)

func TestRetentionServicePurgeBefore(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRetentionService(newFakeBeginner(scope))
	cutoff := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	repo.purgeOut = usage.PurgeStats{DetailsPurged: 3, HistoryPurged: 5, DailyPurged: 1}

	stats, err := svc.PurgeBefore(context.Background(), cutoff)
	if err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if !repo.purgeCutoff.Equal(cutoff) {
		t.Errorf("repo cutoff = %v, want %v", repo.purgeCutoff, cutoff)
	}
	if stats != repo.purgeOut {
		t.Errorf("stats = %+v, want %+v", stats, repo.purgeOut)
	}
	if scope.commits != 1 || scope.rollbacks != 0 {
		t.Errorf("tx: commits %d rollbacks %d, want 1/0", scope.commits, scope.rollbacks)
	}
}

func TestRetentionServicePurgeBeforeIdempotentSecondRun(t *testing.T) {
	scope, _ := newFakeScope()
	svc := NewRetentionService(newFakeBeginner(scope))
	cutoff := time.Date(2026, 5, 4, 12, 0, 0, 0, time.UTC)
	if _, err := svc.PurgeBefore(context.Background(), cutoff); err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if _, err := svc.PurgeBefore(context.Background(), cutoff); err != nil {
		t.Fatalf("PurgeBefore #2: %v", err)
	}
	if scope.commits != 2 {
		t.Errorf("commits = %d, want 2", scope.commits)
	}
}

func TestRetentionServicePurgeBeforeErrorRollback(t *testing.T) {
	scope, _ := newFakeScope()
	scope.repo.purgeErr = errors.New("delete failed")
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
