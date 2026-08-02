package repositories

import (
	"context"
	"testing"
	"time"

	"gorouter/internal/domain/console"
)

// ─────────────────────────────────────────────────────────────
// ConsoleLogRepo real-PG integration tests
// ─────────────────────────────────────────────────────────────

func TestConsoleLogRepo_AppendList_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &consoleLogRepo{tx: tx}
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	t.Run("append and list round-trip preserves caller fields", func(t *testing.T) {
		level := "info"
		message := "raw line"
		occurredAt := base
		retentionUntil := base.Add(90 * 24 * time.Hour)
		entry := &console.ConsoleLog{
			Seq:             1,
			Level:           &level,
			Message:         &message,
			RedactedMessage: "redacted line",
			OccurredAt:      &occurredAt,
			RetentionUntil:  &retentionUntil,
		}
		before := *entry
		if err := repo.Append(ctx, entry); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if *entry != before {
			t.Error("Append mutated caller input")
		}

		got, err := repo.ListAfter(ctx, 0, 50)
		if err != nil {
			t.Fatalf("ListAfter: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if got[0].Seq != 1 || got[0].ID == 0 {
			t.Errorf("seq = %d, id = %d; want seq 1 with DB-assigned id", got[0].Seq, got[0].ID)
		}
		if got[0].Level == nil || *got[0].Level != "info" {
			t.Error("level mismatch")
		}
		if got[0].Message == nil || *got[0].Message != "raw line" {
			t.Error("message mismatch")
		}
		if got[0].RedactedMessage != "redacted line" {
			t.Error("redacted_message mismatch")
		}
		if got[0].OccurredAt == nil || !got[0].OccurredAt.Equal(occurredAt) {
			t.Error("occurred_at mismatch")
		}
		if got[0].RetentionUntil == nil || !got[0].RetentionUntil.Equal(retentionUntil) {
			t.Error("retention_until mismatch")
		}
	})

	t.Run("deterministic ordering by seq then id on ties", func(t *testing.T) {
		first := "first"
		second := "second"
		third := "third"
		// Out-of-order seq appends (distinct range: 20-22).
		for _, e := range []*console.ConsoleLog{
			{Seq: 22, RedactedMessage: third},
			{Seq: 20, RedactedMessage: first},
			{Seq: 21, RedactedMessage: second},
		} {
			if err := repo.Append(ctx, e); err != nil {
				t.Fatalf("Append: %v", err)
			}
		}
		// Tie: two entries with the same seq.
		tieA := "tie-a"
		tieB := "tie-b"
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: 22, RedactedMessage: tieA}); err != nil {
			t.Fatalf("Append tieA: %v", err)
		}
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: 22, RedactedMessage: tieB}); err != nil {
			t.Fatalf("Append tieB: %v", err)
		}

		got, err := repo.ListAfter(ctx, 19, 50)
		if err != nil {
			t.Fatalf("ListAfter: %v", err)
		}
		seqs := make([]int64, 0, len(got))
		redacted := make([]string, 0, len(got))
		for _, e := range got {
			seqs = append(seqs, e.Seq)
			redacted = append(redacted, e.RedactedMessage)
		}
		want := []int64{20, 21, 22, 22, 22}
		for i := range want {
			if seqs[i] != want[i] {
				t.Fatalf("seqs = %v, want %v (ordered by seq asc)", seqs, want)
			}
		}
		// Same-seq rows ordered by id asc == insertion order (BIGSERIAL).
		if redacted[2] != "third" || redacted[3] != "tie-a" || redacted[4] != "tie-b" {
			t.Errorf("tie order = %v, want [third tie-a tie-b] (id asc)", redacted[2:5])
		}
	})

	t.Run("ListAfter boundary is strictly greater-than", func(t *testing.T) {
		got, err := repo.ListAfter(ctx, 21, 50)
		if err != nil {
			t.Fatalf("ListAfter(21): %v", err)
		}
		if seqs := seqsOf(got); len(seqs) != 3 || seqs[0] != 22 || seqs[1] != 22 || seqs[2] != 22 {
			t.Fatalf("ListAfter(21) = seqs %v, want [22 22 22]", seqs)
		}
		after, err := repo.ListAfter(ctx, 22, 50)
		if err != nil {
			t.Fatalf("ListAfter(22): %v", err)
		}
		if len(after) != 0 {
			t.Fatalf("ListAfter(22) = %v, want empty", seqsOf(after))
		}
	})

	t.Run("zero limit returns empty without error", func(t *testing.T) {
		got, err := repo.ListAfter(ctx, 0, 0)
		if err != nil {
			t.Fatalf("ListAfter(0): %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 rows for limit 0, got %d", len(got))
		}
	})

	t.Run("negative limit fails closed", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		spRepo := &consoleLogRepo{tx: sp}
		_, err = spRepo.ListAfter(ctx, 0, -1)
		if err == nil {
			sp.Commit(ctx)
			t.Fatal("expected error for negative limit, got nil")
		}
		sp.Rollback(ctx)
	})

	t.Run("redacted_message NOT NULL is enforced and empty string is valid", func(t *testing.T) {
		sp, err := tx.Begin(ctx)
		if err != nil {
			t.Fatalf("savepoint: %v", err)
		}
		_, err = sp.Exec(ctx,
			`INSERT INTO gorouter_console_logs (seq, level, message, redacted_message)
			 VALUES ($1, 'error', 'raw', NULL)`, int64(900))
		if err == nil {
			sp.Commit(ctx)
			t.Fatal("expected NOT NULL violation for NULL redacted_message, got nil")
		}
		sp.Rollback(ctx)
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: 901, RedactedMessage: ""}); err != nil {
			t.Fatalf("Append with empty redacted_message should succeed: %v", err)
		}
	})

	t.Run("null optional columns round-trip", func(t *testing.T) {
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: 1000, RedactedMessage: "bare"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
		got, err := repo.ListAfter(ctx, 999, 50)
		if err != nil {
			t.Fatalf("ListAfter: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 entry, got %d", len(got))
		}
		if got[0].Level != nil || got[0].Message != nil || got[0].OccurredAt != nil || got[0].RetentionUntil != nil {
			t.Error("optional columns should be NULL")
		}
	})
}

func TestConsoleLogRepo_ListAfter_Cap50_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &consoleLogRepo{tx: tx}
	for i := int64(1); i <= 55; i++ {
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: i, RedactedMessage: "line"}); err != nil {
			t.Fatalf("Append seq %d: %v", i, err)
		}
	}

	got, err := repo.ListAfter(ctx, 0, 50)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(got) != 50 {
		t.Fatalf("expected 50 rows (hard cap), got %d", len(got))
	}
	if got[0].Seq != 1 || got[49].Seq != 50 {
		t.Errorf("cap window = seqs %d..%d, want 1..50", got[0].Seq, got[49].Seq)
	}

	// A limit above 50 is still capped at 50.
	over, err := repo.ListAfter(ctx, 0, 500)
	if err != nil {
		t.Fatalf("ListAfter(500): %v", err)
	}
	if len(over) != 50 {
		t.Fatalf("expected 50 rows for limit 500, got %d", len(over))
	}
}

func TestConsoleLogRepo_DeleteBefore_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &consoleLogRepo{tx: tx}
	for _, seq := range []int64{2000, 2001, 2002, 2003, 2004} {
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "line"}); err != nil {
			t.Fatalf("Append seq %d: %v", seq, err)
		}
	}

	t.Run("deletes strictly-before seqs and reports count", func(t *testing.T) {
		deleted, err := repo.DeleteBefore(ctx, 2003)
		if err != nil {
			t.Fatalf("DeleteBefore(2003): %v", err)
		}
		if deleted != 3 {
			t.Errorf("deleted = %d, want 3 (seqs 2000-2002)", deleted)
		}
		got, err := repo.ListAfter(ctx, 1999, 50)
		if err != nil {
			t.Fatalf("ListAfter: %v", err)
		}
		if seqs := seqsOf(got); len(seqs) != 2 || seqs[0] != 2003 || seqs[1] != 2004 {
			t.Fatalf("remaining seqs = %v, want [2003 2004]", seqs)
		}
	})

	t.Run("boundary seq itself is preserved", func(t *testing.T) {
		deleted, err := repo.DeleteBefore(ctx, 2000)
		if err != nil {
			t.Fatalf("DeleteBefore(2000): %v", err)
		}
		if deleted != 0 {
			t.Errorf("deleted = %d, want 0 (seq 2000 is not < 2000)", deleted)
		}
	})

	t.Run("removes everything below a high watermark", func(t *testing.T) {
		deleted, err := repo.DeleteBefore(ctx, 9999)
		if err != nil {
			t.Fatalf("DeleteBefore(9999): %v", err)
		}
		if deleted != 2 {
			t.Errorf("deleted = %d, want 2", deleted)
		}
		got, err := repo.ListAfter(ctx, 0, 50)
		if err != nil {
			t.Fatalf("ListAfter: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected 0 rows after full sweep, got %d", len(got))
		}
	})
}

func seqsOf(entries []console.ConsoleLog) []int64 {
	seqs := make([]int64, len(entries))
	for i, e := range entries {
		seqs[i] = e.Seq
	}
	return seqs
}
