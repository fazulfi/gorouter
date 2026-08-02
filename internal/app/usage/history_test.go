package usage

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

func TestRequestHistoryServiceAppend(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRequestHistoryService(newFakeBeginner(scope))
	model := "m"
	at := ringBase.Add(-time.Minute)
	entry := usage.RequestHistoryEntry{Model: &model, PromptTokens: intPtr(3), CompletionTokens: intPtr(4), OccurredAt: &at}
	if err := svc.Append(context.Background(), entry); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if scope.commits != 1 || scope.rollbacks != 0 {
		t.Errorf("tx: commits %d rollbacks %d, want 1/0", scope.commits, scope.rollbacks)
	}
	if len(repo.history) != 1 {
		t.Fatalf("history rows = %d, want 1", len(repo.history))
	}
	got := repo.history[0]
	if got.ID == uuid.Nil {
		t.Error("Append must assign an ID when the entry ID is zero")
	}
	if got.Model == nil || *got.Model != model || got.PromptTokens == nil || *got.PromptTokens != 3 ||
		got.CompletionTokens == nil || *got.CompletionTokens != 4 || got.OccurredAt == nil || !got.OccurredAt.Equal(at) {
		t.Errorf("Append must preserve entry fields: %+v", got)
	}
	if entry.ID != uuid.Nil {
		t.Error("Append must not mutate the caller's entry")
	}
}

func TestRequestHistoryServiceAppendPreservesID(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRequestHistoryService(newFakeBeginner(scope))
	id := uuid.New()
	if err := svc.Append(context.Background(), usage.RequestHistoryEntry{ID: id}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if repo.history[0].ID != id {
		t.Errorf("Append must preserve a caller-supplied ID")
	}
}

func TestRequestHistoryServiceAppendErrorRollback(t *testing.T) {
	scope, _ := newFakeScope()
	scope.repo.historyAppendErr = errors.New("insert failed")
	svc := NewRequestHistoryService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), usage.RequestHistoryEntry{}); err == nil {
		t.Fatal("expected error")
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
	}
}

func TestRequestHistoryServiceRecent(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewRequestHistoryService(newFakeBeginner(scope))
	at := ringBase
	want := []usage.RequestHistoryEntry{
		ringEntry("m0", "p0", 1, 2, at),
		ringEntry("m1", "p1", 3, 4, at.Add(-time.Minute)),
	}
	repo.recentOut = want

	t.Run("positive limit", func(t *testing.T) {
		got, err := svc.Recent(context.Background(), 10)
		if err != nil {
			t.Fatalf("Recent: %v", err)
		}
		if repo.recentLimit != 10 {
			t.Errorf("repo limit = %d, want 10", repo.recentLimit)
		}
		if len(got) != 2 || !got[0].OccurredAt.Equal(at) {
			t.Errorf("newest-first passthrough: %+v", got)
		}
	})

	t.Run("zero limit defaults to 50", func(t *testing.T) {
		if _, err := svc.Recent(context.Background(), 0); err != nil {
			t.Fatalf("Recent(0): %v", err)
		}
		if repo.recentLimit != usage.DefaultRingCapacity {
			t.Errorf("repo limit = %d, want %d", repo.recentLimit, usage.DefaultRingCapacity)
		}
	})

	t.Run("negative limit defaults to 50", func(t *testing.T) {
		if _, err := svc.Recent(context.Background(), -5); err != nil {
			t.Fatalf("Recent(-5): %v", err)
		}
		if repo.recentLimit != usage.DefaultRingCapacity {
			t.Errorf("repo limit = %d, want %d", repo.recentLimit, usage.DefaultRingCapacity)
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		repo.recentErr = errors.New("select failed")
		if _, err := svc.Recent(context.Background(), 10); err == nil {
			t.Fatal("expected error")
		}
		repo.recentErr = nil
	})
}

func TestRequestHistoryServiceRecentNoCommit(t *testing.T) {
	scope, repo := newFakeScope()
	repo.recentOut = []usage.RequestHistoryEntry{ringEntry("m", "p", 1, 2, ringBase)}
	svc := NewRequestHistoryService(newFakeBeginner(scope))
	if _, err := svc.Recent(context.Background(), 10); err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("read path must roll back, got commits %d rollbacks %d", scope.commits, scope.rollbacks)
	}
}
