package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestUsageRepo_AggregateDaily(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		agg := &usage.DailyAggregate{
			Day: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		}
		if err := repo.AggregateDaily(context.Background(), agg); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &usageRepo{tx: tx}
		err := repo.AggregateDaily(context.Background(), &usage.DailyAggregate{
			Day: time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUsageRepo_Daily(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		day := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
		provID := uuid.New()
		model := "gpt-4o"
		requests := 12
		prompt := int64(1000)
		completion := int64(2000)
		cost := 0.123456
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{day, &provID, &model, &requests, &prompt, &completion, &cost},
					},
				}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		aggs, err := repo.Daily(context.Background(), day)
		if err != nil {
			t.Fatal(err)
		}
		if len(aggs) != 1 {
			t.Fatalf("expected 1, got %d", len(aggs))
		}
		if aggs[0].Day != day || aggs[0].ProviderID == nil || *aggs[0].ProviderID != provID {
			t.Error("day/provider mismatch")
		}
		if aggs[0].ModelID == nil || *aggs[0].ModelID != model {
			t.Error("model mismatch")
		}
		if aggs[0].Requests == nil || *aggs[0].Requests != requests {
			t.Error("requests mismatch")
		}
		if aggs[0].PromptTokens == nil || *aggs[0].PromptTokens != prompt {
			t.Error("prompt_tokens mismatch")
		}
		if aggs[0].CompletionTokens == nil || *aggs[0].CompletionTokens != completion {
			t.Error("completion_tokens mismatch")
		}
		if aggs[0].Cost == nil || *aggs[0].Cost != cost {
			t.Error("cost mismatch")
		}
	})

	t.Run("null fields round-trip", func(t *testing.T) {
		day := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{{day, nil, nil, nil, nil, nil, nil}},
				}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		aggs, err := repo.Daily(context.Background(), day)
		if err != nil {
			t.Fatal(err)
		}
		if len(aggs) != 1 {
			t.Fatalf("expected 1, got %d", len(aggs))
		}
		if aggs[0].ProviderID != nil || aggs[0].ModelID != nil {
			t.Error("expected nil provider/model after null row")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		aggs, err := repo.Daily(context.Background(), time.Now())
		if err != nil {
			t.Fatal(err)
		}
		if len(aggs) != 0 {
			t.Fatalf("expected 0, got %d", len(aggs))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &usageRepo{tx: tx}
		_, err := repo.Daily(context.Background(), time.Now())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUsageRepo_WriteRequestDetail(t *testing.T) {
	t.Parallel()

	t.Run("success with debug_opt_in false", func(t *testing.T) {
		execCalls := 0
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		detail := &usage.RequestDetail{
			ID:         uuid.New(),
			DebugOptIn: false,
		}
		if err := repo.WriteRequestDetail(context.Background(), detail); err != nil {
			t.Fatal(err)
		}
		if execCalls != 1 {
			t.Errorf("expected 1 exec call, got %d", execCalls)
		}
	})

	t.Run("success with debug_opt_in true", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		if err := repo.WriteRequestDetail(context.Background(), &usage.RequestDetail{
			ID:         uuid.New(),
			DebugOptIn: true,
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
		repo := &usageRepo{tx: tx}
		err := repo.WriteRequestDetail(context.Background(), &usage.RequestDetail{ID: uuid.New()})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUsageRepo_RequestDetail(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		model := "claude-3-5-sonnet"
		status := "completed"
		errorKind := "timeout"
		occurredAt := time.Now().UTC()
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{
					id, nil, nil, &model, nil, nil, nil, &status, &errorKind, &occurredAt, true,
				}}
			},
		}
		repo := &usageRepo{tx: tx}
		detail, err := repo.RequestDetail(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if detail == nil {
			t.Fatal("expected detail")
		}
		if detail.ID != id {
			t.Error("ID mismatch")
		}
		if detail.Model == nil || *detail.Model != model {
			t.Error("model mismatch")
		}
		if detail.ErrorKind == nil || *detail.ErrorKind != errorKind {
			t.Error("error_kind mismatch")
		}
		if !detail.DebugOptIn {
			t.Error("debug_opt_in should be true")
		}
	})

	t.Run("debug_opt_in false round-trips", func(t *testing.T) {
		id := uuid.New()
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{vals: []interface{}{id, nil, nil, nil, nil, nil, nil, nil, nil, nil, false}}
			},
		}
		repo := &usageRepo{tx: tx}
		detail, err := repo.RequestDetail(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if detail == nil {
			t.Fatal("expected detail")
		}
		if detail.DebugOptIn {
			t.Error("debug_opt_in should be false")
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &usageRepo{tx: tx}
		detail, err := repo.RequestDetail(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if detail != nil {
			t.Fatal("expected nil detail for missing row")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("query failed")}
			},
		}
		repo := &usageRepo{tx: tx}
		_, err := repo.RequestDetail(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUsageRepo_AppendHistory(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		if err := repo.AppendHistory(context.Background(), &usage.RequestHistoryEntry{
			ID: uuid.New(),
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
		repo := &usageRepo{tx: tx}
		err := repo.AppendHistory(context.Background(), &usage.RequestHistoryEntry{ID: uuid.New()})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUsageRepo_RecentHistory(t *testing.T) {
	t.Parallel()

	t.Run("results in row order", func(t *testing.T) {
		id1, id2 := uuid.New(), uuid.New()
		model1, model2 := "gpt-4o", "gpt-4o-mini"
		ts1, ts2 := time.Now().UTC(), time.Now().UTC().Add(-time.Minute)
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{id1, &model1, nil, nil, nil, nil, &ts1},
						{id2, &model2, nil, nil, nil, nil, &ts2},
					},
				}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		entries, err := repo.RecentHistory(context.Background(), 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 2 {
			t.Fatalf("expected 2, got %d", len(entries))
		}
		if entries[0].ID != id1 || entries[1].ID != id2 {
			t.Error("row order mismatch")
		}
		if entries[0].Model == nil || *entries[0].Model != model1 {
			t.Error("model mismatch")
		}
	})

	t.Run("zero-token entries are returned (no repo-side filtering)", func(t *testing.T) {
		id := uuid.New()
		zero := 0
		ts := time.Now().UTC()
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{{id, nil, nil, &zero, &zero, nil, &ts}},
				}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		entries, err := repo.RecentHistory(context.Background(), 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("expected 1 zero-token entry, got %d", len(entries))
		}
		if entries[0].PromptTokens == nil || *entries[0].PromptTokens != 0 {
			t.Error("prompt_tokens should be 0")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &usageRepo{tx: tx}
		entries, err := repo.RecentHistory(context.Background(), 50)
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
		repo := &usageRepo{tx: tx}
		_, err := repo.RecentHistory(context.Background(), 50)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
