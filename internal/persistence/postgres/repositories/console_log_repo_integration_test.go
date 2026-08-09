package repositories

import (
	"context"
	"os"
	"sort"
	"sync"
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

func TestConsoleLogRepo_MaxSeq_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &consoleLogRepo{tx: tx}

	t.Run("empty table returns zero", func(t *testing.T) {
		max, err := repo.MaxSeq(ctx)
		if err != nil {
			t.Fatalf("MaxSeq: %v", err)
		}
		if max != 0 {
			t.Errorf("max = %d, want 0", max)
		}
	})

	for _, seq := range []int64{3001, 3005, 3003} {
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "line"}); err != nil {
			t.Fatalf("Append seq %d: %v", seq, err)
		}
	}

	t.Run("returns the highest persisted seq", func(t *testing.T) {
		max, err := repo.MaxSeq(ctx)
		if err != nil {
			t.Fatalf("MaxSeq: %v", err)
		}
		if max != 3005 {
			t.Errorf("max = %d, want 3005", max)
		}
	})

	t.Run("partial purge preserves the highest seq", func(t *testing.T) {
		if _, err := repo.DeleteBefore(ctx, 3004); err != nil {
			t.Fatalf("DeleteBefore: %v", err)
		}
		max, err := repo.MaxSeq(ctx)
		if err != nil {
			t.Fatalf("MaxSeq: %v", err)
		}
		if max != 3005 {
			t.Errorf("max = %d, want 3005 (newest row survives purge)", max)
		}
	})

	t.Run("full sweep resets max to zero", func(t *testing.T) {
		if _, err := repo.DeleteBefore(ctx, 99999); err != nil {
			t.Fatalf("DeleteBefore: %v", err)
		}
		max, err := repo.MaxSeq(ctx)
		if err != nil {
			t.Fatalf("MaxSeq: %v", err)
		}
		if max != 0 {
			t.Errorf("max = %d, want 0 (COALESCE over empty table)", max)
		}
	})
}

func TestConsoleLogRepo_PurgeBefore_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &consoleLogRepo{tx: tx}
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	cutoff := base.Add(90 * 24 * time.Hour)
	for _, e := range []*console.ConsoleLog{
		{Seq: 4001, RedactedMessage: "expired", RetentionUntil: timePtr(base.Add(80 * 24 * time.Hour))},
		{Seq: 4002, RedactedMessage: "expiring", RetentionUntil: timePtr(base.Add(89 * 24 * time.Hour))},
		{Seq: 4003, RedactedMessage: "at-cutoff", RetentionUntil: timePtr(cutoff)},
		{Seq: 4004, RedactedMessage: "retained", RetentionUntil: timePtr(base.Add(120 * 24 * time.Hour))},
		{Seq: 4005, RedactedMessage: "null-retention"},
	} {
		if err := repo.Append(ctx, e); err != nil {
			t.Fatalf("Append seq %d: %v", e.Seq, err)
		}
	}

	t.Run("deletes strictly-before cutoff and reports count", func(t *testing.T) {
		deleted, err := repo.PurgeBefore(ctx, cutoff)
		if err != nil {
			t.Fatalf("PurgeBefore: %v", err)
		}
		if deleted != 2 {
			t.Errorf("deleted = %d, want 2 (seqs 4001-4002)", deleted)
		}
		got, err := repo.ListAfter(ctx, 0, 50)
		if err != nil {
			t.Fatalf("ListAfter: %v", err)
		}
		if seqs := seqsOf(got); len(seqs) != 3 || seqs[0] != 4003 || seqs[1] != 4004 || seqs[2] != 4005 {
			t.Fatalf("remaining seqs = %v, want [4003 4004 4005]", seqs)
		}
	})

	t.Run("boundary row at cutoff survives", func(t *testing.T) {
		deleted, err := repo.PurgeBefore(ctx, cutoff.Add(-1*time.Nanosecond))
		if err != nil {
			t.Fatalf("PurgeBefore: %v", err)
		}
		if deleted != 0 {
			t.Errorf("deleted = %d, want 0 (nothing strictly before cutoff-1ns)", deleted)
		}
	})

	t.Run("idempotent second run removes nothing", func(t *testing.T) {
		deleted, err := repo.PurgeBefore(ctx, cutoff)
		if err != nil {
			t.Fatalf("PurgeBefore #2: %v", err)
		}
		if deleted != 0 {
			t.Errorf("deleted = %d, want 0 (idempotent)", deleted)
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

func TestConsoleLogRepo_NextSeq_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	t.Run("fresh database allocates strictly increasing seqs from 1", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		repo := &consoleLogRepo{tx: tx}
		for i, want := range []int64{1, 2, 3} {
			seq, err := repo.NextSeq(ctx)
			if err != nil {
				t.Fatalf("NextSeq %d: %v", i, err)
			}
			if seq != want {
				t.Errorf("NextSeq = %d, want %d", seq, want)
			}
			if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "line"}); err != nil {
				t.Fatalf("Append seq %d: %v", seq, err)
			}
		}
		// The live-row max is informational and consistent with allocation.
		max, err := repo.MaxSeq(ctx)
		if err != nil {
			t.Fatalf("MaxSeq: %v", err)
		}
		if max != 3 {
			t.Errorf("MaxSeq = %d, want 3", max)
		}
	})

	t.Run("seeds above pre-existing rows on first use after upgrade", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		repo := &consoleLogRepo{tx: tx}
		// Legacy rows written before the watermark existed (raw inserts).
		for _, seq := range []int64{6001, 6002, 6003} {
			if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "legacy"}); err != nil {
				t.Fatalf("legacy Append seq %d: %v", seq, err)
			}
		}
		// Simulate an upgraded installation that has rows but no watermark:
		// the first allocation must seed above every historical seq.
		if _, err := tx.Exec(ctx, `DELETE FROM gorouter_runtime_state WHERE key = $1`, consoleWatermarkKey); err != nil {
			t.Fatalf("delete watermark: %v", err)
		}
		seq, err := repo.NextSeq(ctx)
		if err != nil {
			t.Fatalf("NextSeq: %v", err)
		}
		if seq <= 6003 {
			t.Errorf("NextSeq = %d, want > 6003 (seed above historical max)", seq)
		}
	})

	t.Run("full purge never regresses the watermark and replay stays visible", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		defer tx.Rollback(ctx)
		repo := &consoleLogRepo{tx: tx}
		var last int64
		for i := int64(0); i < 3; i++ {
			seq, err := repo.NextSeq(ctx)
			if err != nil {
				t.Fatalf("NextSeq %d: %v", i, err)
			}
			last = seq
			if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "line"}); err != nil {
				t.Fatalf("Append seq %d: %v", seq, err)
			}
		}
		// A full sweep empties the table (the retention job's worst case).
		if _, err := repo.DeleteBefore(ctx, 99999); err != nil {
			t.Fatalf("DeleteBefore: %v", err)
		}
		if max, err := repo.MaxSeq(ctx); err != nil {
			t.Fatalf("MaxSeq: %v", err)
		} else if max != 0 {
			t.Errorf("MaxSeq = %d, want 0 (table empty)", max)
		}
		// The next allocation must still be greater than every historical
		// seq, never a reuse of 1.
		seq, err := repo.NextSeq(ctx)
		if err != nil {
			t.Fatalf("NextSeq after purge: %v", err)
		}
		if seq <= last {
			t.Errorf("NextSeq after full purge = %d, want > %d (no reuse)", seq, last)
		}
		// An old SSE cursor at the pre-purge max must still see the new row.
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "post-purge"}); err != nil {
			t.Fatalf("Append seq %d: %v", seq, err)
		}
		got, err := repo.ListAfter(ctx, last, 50)
		if err != nil {
			t.Fatalf("ListAfter(%d): %v", last, err)
		}
		if len(got) != 1 || got[0].Seq != seq {
			t.Errorf("ListAfter(%d) = seqs %v, want [%d] (new rows visible to old cursor)", last, seqsOf(got), seq)
		}
	})

	t.Run("fresh connection continues from the durable watermark", func(t *testing.T) {
		// Commit a fully-wired allocation so the watermark is durable.
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		repo := &consoleLogRepo{tx: tx}
		seq, err := repo.NextSeq(ctx)
		if err != nil {
			t.Fatalf("NextSeq: %v", err)
		}
		if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "line"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatalf("commit: %v", err)
		}
		// A brand-new connection (process-restart equivalent) continues
		// strictly above the committed watermark.
		tx2, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin #2: %v", err)
		}
		defer tx2.Rollback(ctx)
		repo2 := &consoleLogRepo{tx: tx2}
		seq2, err := repo2.NextSeq(ctx)
		if err != nil {
			t.Fatalf("NextSeq #2: %v", err)
		}
		if seq2 <= seq {
			t.Errorf("NextSeq after restart = %d, want > %d", seq2, seq)
		}
	})
}

// TestConsoleLogRepo_NextSeq_ConcurrentAppends_Integration proves the
// allocation is atomic: concurrent transactions cannot receive duplicate
// sequence numbers, and replay from a pre-allocation cursor sees every row.
func TestConsoleLogRepo_NextSeq_ConcurrentAppends_Integration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}
	if os.Getenv("DATABASE_URL") == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	dbName := setupRepoTestDB(t, ctx)

	bootstrapRolesForRepoTest(t, ctx, os.Getenv("DATABASE_URL"))

	dsn := isolatedTestDSN(t, dbName)
	grantSchemaCreateToDDLRepoTest(t, ctx, dsn)
	pool := migrateAsDDLRepoTest(t, ctx, dsn)
	grantRuntimeFixturePrivilegesRepoTest(t, ctx, dsn) // FIX_C: concurrent writers need gorouter DML grants on tracking tables
	defer pool.Close()

	const writers = 12
	const rounds = 2
	allocs := writers * rounds
	var wg sync.WaitGroup
	start := make(chan struct{})
	seqs := make([]int64, allocs)
	errs := make([]error, allocs)
	for i := 0; i < allocs; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			<-start
			tx, err := pool.Begin(ctx)
			if err != nil {
				errs[idx] = err
				return
			}
			defer tx.Rollback(ctx)
			repo := &consoleLogRepo{tx: tx}
			seq, err := repo.NextSeq(ctx)
			if err != nil {
				errs[idx] = err
				return
			}
			if err := repo.Append(ctx, &console.ConsoleLog{Seq: seq, RedactedMessage: "line"}); err != nil {
				errs[idx] = err
				return
			}
			if err := tx.Commit(ctx); err != nil {
				errs[idx] = err
				return
			}
			seqs[idx] = seq
		}(i)
	}
	close(start)
	wg.Wait()
	for i, e := range errs {
		if e != nil {
			t.Fatalf("concurrent writer %d: %v", i, e)
		}
	}

	seen := make(map[int64]bool, allocs)
	for _, s := range seqs {
		if seen[s] {
			t.Errorf("duplicate seq %d allocated concurrently", s)
		}
		seen[s] = true
	}
	sort.Slice(seqs, func(i, j int) bool { return seqs[i] < seqs[j] })
	if seqs[0] != 1 || seqs[len(seqs)-1] != int64(allocs) {
		t.Errorf("allocated seqs = %v, want exactly 1..%d (no gaps, no reuse)", seqs, allocs)
	}
	for i := 1; i < len(seqs); i++ {
		if seqs[i] <= seqs[i-1] {
			t.Errorf("seqs not strictly increasing: %v", seqs)
		}
	}

	// Replay completeness: a cursor before the first allocation must see
	// every committed row, in order — no row is ever skipped.
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)
	repo := &consoleLogRepo{tx: tx}
	got, err := repo.ListAfter(ctx, 0, 500)
	if err != nil {
		t.Fatalf("ListAfter: %v", err)
	}
	if len(got) != allocs {
		t.Fatalf("replay returned %d rows, want %d (skipped rows)", len(got), allocs)
	}
	for i, e := range got {
		if e.Seq != seqs[i] {
			t.Errorf("replay row %d seq = %d, want %d", i, e.Seq, seqs[i])
		}
	}
	// Allocation continues strictly above the concurrent maximum.
	seq, err := repo.NextSeq(ctx)
	if err != nil {
		t.Fatalf("NextSeq: %v", err)
	}
	if seq <= int64(allocs) {
		t.Errorf("NextSeq after concurrency = %d, want > %d", seq, allocs)
	}
}
