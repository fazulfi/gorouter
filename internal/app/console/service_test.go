package console

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/console"
)

func TestConsoleRedaction(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))

	raw := "provider call failed: api_key_value=sk-proj-abc123xyz unauthorized"
	if err := svc.Append(context.Background(), &console.ConsoleLog{
		Level: strPtr("error"), Message: &raw,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	// A second line without credentials must survive unchanged.
	plain := "health check ok"
	if err := svc.Append(context.Background(), &console.ConsoleLog{
		Level: strPtr("info"), Message: &plain,
	}); err != nil {
		t.Fatalf("Append #2: %v", err)
	}

	// The persisted entry carries only redacted text.
	if len(repo.entries) != 2 {
		t.Fatalf("stored %d entries, want 2", len(repo.entries))
	}
	stored := repo.entries[0]
	if strings.Contains(stored.RedactedMessage, "sk-proj-abc123xyz") {
		t.Errorf("RedactedMessage %q still contains raw api key", stored.RedactedMessage)
	}
	if stored.Message == nil {
		t.Error("Message not populated with redacted text")
	} else if strings.Contains(*stored.Message, "sk-proj-abc123xyz") {
		t.Errorf("Message %q still contains raw api key", *stored.Message)
	}
	if !strings.Contains(stored.RedactedMessage, "[REDACTED]") {
		t.Errorf("RedactedMessage %q missing mask marker", stored.RedactedMessage)
	}

	// ListAfter replays in ascending sequence order and exposes only
	// redacted text.
	got, err := svc.ListAfter(context.Background(), 0, 50)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListAfter returned %d rows, want 2", len(got))
	}
	if got[0].Seq != 1 || got[1].Seq != 2 {
		t.Errorf("seqs = [%d %d], want [1 2] (ascending)", got[0].Seq, got[1].Seq)
	}
	for _, e := range got {
		if strings.Contains(e.RedactedMessage, "sk-proj-abc123xyz") {
			t.Errorf("row seq %d exposes raw api key in %q", e.Seq, e.RedactedMessage)
		}
	}
	if got[1].RedactedMessage != plain {
		t.Errorf("plain line redacted to %q, want %q", got[1].RedactedMessage, plain)
	}
}

func TestConsoleServiceAppendAssignsMonotonicSeq(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))

	// A caller-supplied Seq is ignored; sequences are derived from the
	// persisted watermark.
	for i, want := range []int64{1, 2, 3} {
		entry := &console.ConsoleLog{Seq: 99, RedactedMessage: "line"}
		if err := svc.Append(context.Background(), entry); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
		if entry.Seq != want {
			t.Errorf("entry.Seq = %d, want %d", entry.Seq, want)
		}
	}
	// A fresh service instance continues from the durable watermark —
	// sequences stay monotonic across process restarts.
	fresh := NewConsoleService(newFakeBeginner(scope))
	if err := fresh.Append(context.Background(), &console.ConsoleLog{RedactedMessage: "line"}); err != nil {
		t.Fatalf("fresh Append: %v", err)
	}
	if len(repo.entries) != 4 || repo.entries[3].Seq != 4 {
		t.Errorf("fresh instance seq = %d, want 4", repo.entries[3].Seq)
	}
}

func TestConsoleServiceAppendStampsRetentionAndOccurredAt(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))
	now := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	svc.WithClock(func() time.Time { return now })

	if err := svc.Append(context.Background(), &console.ConsoleLog{RedactedMessage: "line"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	e := repo.entries[0]
	if e.OccurredAt == nil || !e.OccurredAt.Equal(now) {
		t.Errorf("occurred_at = %v, want %v", e.OccurredAt, now)
	}
	wantUntil := now.Add(console.RetentionDays * 24 * time.Hour)
	if e.RetentionUntil == nil || !e.RetentionUntil.Equal(wantUntil) {
		t.Errorf("retention_until = %v, want %v", e.RetentionUntil, wantUntil)
	}
}

func TestConsoleServiceAppendPreservesCallerFields(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))
	occurredAt := time.Date(2026, 8, 1, 6, 0, 0, 0, time.UTC)
	retentionUntil := occurredAt.Add(30 * 24 * time.Hour)
	msg := "custom message"
	if err := svc.Append(context.Background(), &console.ConsoleLog{
		Level:          strPtr("warn"),
		Message:        &msg,
		OccurredAt:     &occurredAt,
		RetentionUntil: &retentionUntil,
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	e := repo.entries[0]
	if e.Level == nil || *e.Level != "warn" {
		t.Errorf("level = %v, want warn", e.Level)
	}
	if !e.OccurredAt.Equal(occurredAt) {
		t.Errorf("occurred_at = %v, want %v (caller value preserved)", e.OccurredAt, occurredAt)
	}
	if !e.RetentionUntil.Equal(retentionUntil) {
		t.Errorf("retention_until = %v, want %v (caller value preserved)", e.RetentionUntil, retentionUntil)
	}
}

func TestConsoleServiceAppendNilMessageStoresEmptyRedacted(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), &console.ConsoleLog{}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	e := repo.entries[0]
	if e.RedactedMessage != "" {
		t.Errorf("redacted_message = %q, want empty", e.RedactedMessage)
	}
	if e.Message != nil {
		t.Errorf("message = %v, want nil", e.Message)
	}
}

func TestConsoleServiceAppendRequiresEntry(t *testing.T) {
	scope, _ := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), nil); err == nil {
		t.Fatal("expected error for nil entry")
	}
}

func TestConsoleServiceAppendBeginError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.beginErr = errors.New("begin failed")
	svc := NewConsoleService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), &console.ConsoleLog{}); err == nil {
		t.Fatal("expected error")
	}
	if scope.rollbacks != 0 {
		t.Errorf("rollbacks = %d, want 0", scope.rollbacks)
	}
}

func TestConsoleServiceAppendErrorRollback(t *testing.T) {
	scope, repo := newFakeScope()
	repo.appendErr = errors.New("insert failed")
	svc := NewConsoleService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), &console.ConsoleLog{}); err == nil {
		t.Fatal("expected error")
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
	}
}

func TestConsoleServiceAppendMaxSeqErrorRollback(t *testing.T) {
	scope, repo := newFakeScope()
	repo.maxSeqErr = errors.New("max seq failed")
	svc := NewConsoleService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), &console.ConsoleLog{}); err == nil {
		t.Fatal("expected error")
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
	}
}

func TestConsoleServiceAppendCommitError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.commitErr = errors.New("commit failed")
	svc := NewConsoleService(newFakeBeginner(scope))
	if err := svc.Append(context.Background(), &console.ConsoleLog{}); err == nil {
		t.Fatal("expected error")
	}
	if scope.rollbacks != 1 {
		t.Errorf("rollbacks = %d, want 1", scope.rollbacks)
	}
}

func TestConsoleServiceListAfterNormalizesLimit(t *testing.T) {
	scope, repo := newFakeScope()
	svc := NewConsoleService(newFakeBeginner(scope))
	for _, limit := range []int{0, -5, 100, 7} {
		if _, err := svc.ListAfter(context.Background(), 0, limit); err != nil {
			t.Fatalf("ListAfter(%d): %v", limit, err)
		}
	}
	// The normalized limit is what reaches the repository: the default cap
	// (50) for non-positive and over-cap values, the exact value within
	// the cap.
	want := []int{50, 50, 50, 7}
	if len(repo.listLimits) != len(want) {
		t.Fatalf("recorded %d limits, want %d", len(repo.listLimits), len(want))
	}
	for i := range want {
		if repo.listLimits[i] != want[i] {
			t.Errorf("normalized limit = %d, want %d", repo.listLimits[i], want[i])
		}
	}
}

func TestConsoleServiceListAfterErrorRollback(t *testing.T) {
	scope, repo := newFakeScope()
	repo.listErr = errors.New("query failed")
	svc := NewConsoleService(newFakeBeginner(scope))
	if _, err := svc.ListAfter(context.Background(), 0, 50); err == nil {
		t.Fatal("expected error")
	}
	if scope.commits != 0 || scope.rollbacks != 1 {
		t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
	}
}

func TestConsoleServiceListAfterBeginError(t *testing.T) {
	scope, _ := newFakeScope()
	scope.beginErr = errors.New("begin failed")
	svc := NewConsoleService(newFakeBeginner(scope))
	if _, err := svc.ListAfter(context.Background(), 0, 50); err == nil {
		t.Fatal("expected error")
	}
}

func strPtr(s string) *string { return &s }
