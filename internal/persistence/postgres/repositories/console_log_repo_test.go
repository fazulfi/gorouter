package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/console"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestConsoleLogRepo_Append(t *testing.T) {
	t.Parallel()

	t.Run("success passes caller fields through unchanged", func(t *testing.T) {
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				gotArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &consoleLogRepo{tx: tx}
		level := "info"
		message := "raw line"
		occurredAt := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		retentionUntil := occurredAt.Add(90 * 24 * time.Hour)
		entry := &console.ConsoleLog{
			Seq:             7,
			Level:           &level,
			Message:         &message,
			RedactedMessage: "redacted line",
			OccurredAt:      &occurredAt,
			RetentionUntil:  &retentionUntil,
		}
		before := *entry
		if err := repo.Append(context.Background(), entry); err != nil {
			t.Fatal(err)
		}
		if len(gotArgs) != 6 {
			t.Fatalf("expected 6 args, got %d", len(gotArgs))
		}
		if gotArgs[0] != int64(7) {
			t.Errorf("arg[0] seq = %v, want 7", gotArgs[0])
		}
		if gotArgs[1] == nil || *gotArgs[1].(*string) != "info" {
			t.Errorf("arg[1] level = %v, want info", gotArgs[1])
		}
		if gotArgs[2] == nil || *gotArgs[2].(*string) != "raw line" {
			t.Errorf("arg[2] message = %v, want raw line", gotArgs[2])
		}
		if gotArgs[3] != "redacted line" {
			t.Errorf("arg[3] redacted_message = %v, want redacted line", gotArgs[3])
		}
		if *entry != before {
			t.Error("Append mutated caller input")
		}
	})

	t.Run("success with nil optional fields", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				if lvl, ok := args[1].(*string); ok && lvl != nil {
					t.Error("level should be nil")
				}
				if msg, ok := args[2].(*string); ok && msg != nil {
					t.Error("message should be nil")
				}
				if occ, ok := args[4].(*time.Time); ok && occ != nil {
					t.Error("occurred_at should be nil")
				}
				if ret, ok := args[5].(*time.Time); ok && ret != nil {
					t.Error("retention_until should be nil")
				}
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &consoleLogRepo{tx: tx}
		if err := repo.Append(context.Background(), &console.ConsoleLog{
			Seq:             1,
			RedactedMessage: "redacted",
		}); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &consoleLogRepo{tx: tx}
		err := repo.Append(context.Background(), &console.ConsoleLog{
			Seq:             1,
			RedactedMessage: "redacted",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestConsoleLogRepo_ListAfter(t *testing.T) {
	t.Parallel()

	t.Run("results in row order with nullable columns", func(t *testing.T) {
		id1, id2 := int64(1), int64(2)
		seq1, seq2 := int64(5), int64(6)
		level1 := "warn"
		message1 := "slow upstream"
		ts1 := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{id1, seq1, &level1, &message1, "redacted warn", &ts1, nil},
						{id2, seq2, nil, nil, "redacted info", nil, nil},
					},
				}, nil
			},
		}
		repo := &consoleLogRepo{tx: tx}
		entries, err := repo.ListAfter(context.Background(), 4, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2, got %d", len(entries))
		}
		if entries[0].ID != 1 || entries[0].Seq != 5 {
			t.Error("first entry id/seq mismatch")
		}
		if entries[0].Level == nil || *entries[0].Level != "warn" {
			t.Error("level mismatch")
		}
		if entries[0].Message == nil || *entries[0].Message != "slow upstream" {
			t.Error("message mismatch")
		}
		if entries[0].RedactedMessage != "redacted warn" {
			t.Error("redacted_message mismatch")
		}
		if entries[0].OccurredAt == nil || !entries[0].OccurredAt.Equal(ts1) {
			t.Error("occurred_at mismatch")
		}
		if entries[1].Level != nil || entries[1].Message != nil || entries[1].OccurredAt != nil {
			t.Error("expected null level/message/occurred_at for second row")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &consoleLogRepo{tx: tx}
		entries, err := repo.ListAfter(context.Background(), 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 0 {
			t.Fatalf("expected 0, got %d", len(entries))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &consoleLogRepo{tx: tx}
		_, err := repo.ListAfter(context.Background(), 0, 50)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestConsoleLogRepo_MaxSeq(t *testing.T) {
	t.Parallel()

	t.Run("returns persisted max", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{int64(42)}}
			},
		}
		repo := &consoleLogRepo{tx: tx}
		max, err := repo.MaxSeq(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if max != 42 {
			t.Errorf("max = %d, want 42", max)
		}
	})

	t.Run("empty table returns zero", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{int64(0)}}
			},
		}
		repo := &consoleLogRepo{tx: tx}
		max, err := repo.MaxSeq(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if max != 0 {
			t.Errorf("max = %d, want 0", max)
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query failed")}
			},
		}
		repo := &consoleLogRepo{tx: tx}
		_, err := repo.MaxSeq(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestConsoleLogRepo_NextSeq(t *testing.T) {
	t.Parallel()

	t.Run("returns the allocated sequence", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{int64(42)}}
			},
		}
		repo := &consoleLogRepo{tx: tx}
		seq, err := repo.NextSeq(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if seq != 42 {
			t.Errorf("seq = %d, want 42", seq)
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query failed")}
			},
		}
		repo := &consoleLogRepo{tx: tx}
		_, err := repo.NextSeq(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestConsoleLogRepo_PurgeBefore(t *testing.T) {
	t.Parallel()

	t.Run("returns rows affected and passes cutoff", func(t *testing.T) {
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				gotArgs = args
				return pgconn.NewCommandTag("DELETE 3"), nil
			},
		}
		repo := &consoleLogRepo{tx: tx}
		cutoff := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
		deleted, err := repo.PurgeBefore(context.Background(), cutoff)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 3 {
			t.Errorf("deleted = %d, want 3", deleted)
		}
		if len(gotArgs) != 1 {
			t.Fatalf("expected 1 arg, got %d", len(gotArgs))
		}
		if !gotArgs[0].(time.Time).Equal(cutoff) {
			t.Errorf("arg[0] cutoff = %v, want %v", gotArgs[0], cutoff)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		repo := &consoleLogRepo{tx: tx}
		_, err := repo.PurgeBefore(context.Background(), time.Now().UTC())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestConsoleLogRepo_DeleteBefore(t *testing.T) {
	t.Parallel()

	t.Run("returns rows affected", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.NewCommandTag("DELETE 3"), nil
			},
		}
		repo := &consoleLogRepo{tx: tx}
		deleted, err := repo.DeleteBefore(context.Background(), 4)
		if err != nil {
			t.Fatal(err)
		}
		if deleted != 3 {
			t.Errorf("deleted = %d, want 3", deleted)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		repo := &consoleLogRepo{tx: tx}
		_, err := repo.DeleteBefore(context.Background(), 1)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
