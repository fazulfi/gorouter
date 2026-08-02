package repositories

import (
	"context"
	"testing"
	"time"

	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

// ─────────────────────────────────────────────────────────────
// UsageRepo real-PG integration tests
// ─────────────────────────────────────────────────────────────

func TestUsageRepo_AggregateDaily_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &usageRepo{tx: tx}
	day := time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)
	provA := uuid.New()
	provB := uuid.New()
	model := "gpt-4o"
	one := 1
	big := int64(1500)
	cost := 0.25

	t.Run("write and read aggregate", func(t *testing.T) {
		agg := &usage.DailyAggregate{
			Day: day, ProviderID: &provA, ModelID: &model,
			Requests: &one, PromptTokens: &big, CompletionTokens: &big, Cost: &cost,
		}
		if err := repo.AggregateDaily(ctx, agg); err != nil {
			t.Fatalf("AggregateDaily: %v", err)
		}
		got, err := repo.Daily(ctx, day)
		if err != nil {
			t.Fatalf("Daily: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 aggregate, got %d", len(got))
		}
		if got[0].ProviderID == nil || *got[0].ProviderID != provA {
			t.Error("provider_id mismatch")
		}
		if got[0].ModelID == nil || *got[0].ModelID != model {
			t.Error("model_id mismatch")
		}
		if got[0].Requests == nil || *got[0].Requests != 1 {
			t.Error("requests mismatch")
		}
		if got[0].PromptTokens == nil || *got[0].PromptTokens != big {
			t.Error("prompt_tokens mismatch")
		}
		if got[0].CompletionTokens == nil || *got[0].CompletionTokens != big {
			t.Error("completion_tokens mismatch")
		}
		if got[0].Cost == nil || *got[0].Cost != cost {
			t.Error("cost mismatch")
		}
	})

	t.Run("repeat write for same day replaces values (upsert)", func(t *testing.T) {
		two := 2
		agg := &usage.DailyAggregate{
			Day: day, ProviderID: &provA, ModelID: &model,
			Requests: &two, PromptTokens: &big, CompletionTokens: &big, Cost: &cost,
		}
		if err := repo.AggregateDaily(ctx, agg); err != nil {
			t.Fatalf("AggregateDaily upsert: %v", err)
		}
		got, err := repo.Daily(ctx, day)
		if err != nil {
			t.Fatalf("Daily after upsert: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 aggregate after upsert, got %d", len(got))
		}
		if got[0].Requests == nil || *got[0].Requests != 2 {
			t.Errorf("requests = %v, want 2 (replaced by upsert)", got[0].Requests)
		}
	})

	t.Run("different provider same day collapses to single day row", func(t *testing.T) {
		one := 1
		agg := &usage.DailyAggregate{
			Day: day, ProviderID: &provB, ModelID: &model,
			Requests: &one, PromptTokens: &big, CompletionTokens: &big, Cost: &cost,
		}
		if err := repo.AggregateDaily(ctx, agg); err != nil {
			t.Fatalf("AggregateDaily provB: %v", err)
		}
		got, err := repo.Daily(ctx, day)
		if err != nil {
			t.Fatalf("Daily: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("day is PK: expected exactly 1 row for the day, got %d", len(got))
		}
	})

	t.Run("different day yields separate row", func(t *testing.T) {
		day2 := day.AddDate(0, 0, 1)
		agg := &usage.DailyAggregate{
			Day: day2, ProviderID: &provA, ModelID: &model,
			Requests: &one, PromptTokens: &big, CompletionTokens: &big, Cost: &cost,
		}
		if err := repo.AggregateDaily(ctx, agg); err != nil {
			t.Fatalf("AggregateDaily day2: %v", err)
		}
		got, err := repo.Daily(ctx, day)
		if err != nil {
			t.Fatalf("Daily day1: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("expected 1 row for day1, got %d", len(got))
		}
		got2, err := repo.Daily(ctx, day2)
		if err != nil {
			t.Fatalf("Daily day2: %v", err)
		}
		if len(got2) != 1 {
			t.Fatalf("expected 1 row for day2, got %d", len(got2))
		}
	})

	t.Run("does not mutate caller input", func(t *testing.T) {
		agg := &usage.DailyAggregate{
			Day: day, ProviderID: &provA, ModelID: &model,
			Requests: &one, PromptTokens: &big, CompletionTokens: &big, Cost: &cost,
		}
		orig := *agg
		if err := repo.AggregateDaily(ctx, agg); err != nil {
			t.Fatalf("AggregateDaily: %v", err)
		}
		if *agg.Requests != *orig.Requests || agg.Day != orig.Day {
			t.Error("AggregateDaily mutated caller input")
		}
	})
}

func TestUsageRepo_RequestDetail_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &usageRepo{tx: tx}

	t.Run("metadata-only write/read with debug_opt_in false", func(t *testing.T) {
		detail := &usage.RequestDetail{
			ID:               uuid.New(),
			RequestID:        uuidPtr(uuid.New()),
			ProviderID:       uuidPtr(uuid.New()),
			Model:            strPtr("gpt-4o"),
			PromptTokens:     intPtr(500),
			CompletionTokens: intPtr(700),
			Cost:             floatPtr(0.05),
			Status:           strPtr("completed"),
			ErrorKind:        nil,
			OccurredAt:       timePtr(time.Now().UTC().Truncate(time.Microsecond)),
			DebugOptIn:       false,
		}
		if err := repo.WriteRequestDetail(ctx, detail); err != nil {
			t.Fatalf("WriteRequestDetail: %v", err)
		}
		got, err := repo.RequestDetail(ctx, detail.ID)
		if err != nil {
			t.Fatalf("RequestDetail: %v", err)
		}
		if got == nil {
			t.Fatal("expected detail")
		}
		if got.ID != detail.ID {
			t.Error("ID mismatch")
		}
		if got.RequestID == nil || *got.RequestID != *detail.RequestID {
			t.Error("request_id mismatch")
		}
		if got.ProviderID == nil || *got.ProviderID != *detail.ProviderID {
			t.Error("provider_id mismatch")
		}
		if got.Model == nil || *got.Model != *detail.Model {
			t.Error("model mismatch")
		}
		if got.PromptTokens == nil || *got.PromptTokens != *detail.PromptTokens {
			t.Error("prompt_tokens mismatch")
		}
		if got.CompletionTokens == nil || *got.CompletionTokens != *detail.CompletionTokens {
			t.Error("completion_tokens mismatch")
		}
		if got.Cost == nil || *got.Cost != *detail.Cost {
			t.Error("cost mismatch")
		}
		if got.Status == nil || *got.Status != *detail.Status {
			t.Error("status mismatch")
		}
		if got.ErrorKind != nil {
			t.Error("error_kind should be null")
		}
		if got.OccurredAt == nil || !got.OccurredAt.Equal(*detail.OccurredAt) {
			t.Error("occurred_at mismatch")
		}
		if got.DebugOptIn {
			t.Error("debug_opt_in should be false")
		}
	})

	t.Run("debug_opt_in true and nullable columns round-trip", func(t *testing.T) {
		detail := &usage.RequestDetail{
			ID:         uuid.New(),
			RequestID:  nil,
			ProviderID: nil,
			Model:      nil,
			Status:     strPtr("failed"),
			ErrorKind:  strPtr("rate_limited"),
			DebugOptIn: true,
		}
		if err := repo.WriteRequestDetail(ctx, detail); err != nil {
			t.Fatalf("WriteRequestDetail: %v", err)
		}
		got, err := repo.RequestDetail(ctx, detail.ID)
		if err != nil {
			t.Fatalf("RequestDetail: %v", err)
		}
		if got == nil {
			t.Fatal("expected detail")
		}
		if got.RequestID != nil || got.ProviderID != nil || got.Model != nil {
			t.Error("expected null request_id/provider_id/model")
		}
		if got.Status == nil || *got.Status != "failed" {
			t.Error("status mismatch")
		}
		if got.ErrorKind == nil || *got.ErrorKind != "rate_limited" {
			t.Error("error_kind mismatch")
		}
		if !got.DebugOptIn {
			t.Error("debug_opt_in should be true")
		}
	})

	t.Run("missing detail returns nil", func(t *testing.T) {
		got, err := repo.RequestDetail(ctx, uuid.New())
		if err != nil {
			t.Fatalf("RequestDetail: %v", err)
		}
		if got != nil {
			t.Fatal("expected nil for missing detail")
		}
	})
}

func TestUsageRepo_History_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer tx.Rollback(ctx)

	repo := &usageRepo{tx: tx}
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)

	t.Run("append and read newest-first", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			model := "gpt-4o"
			status := "completed"
			ts := base.Add(time.Duration(i) * time.Minute)
			entry := &usage.RequestHistoryEntry{
				ID:               uuid.New(),
				Model:            &model,
				PromptTokens:     intPtr(10 + i),
				CompletionTokens: intPtr(20 + i),
				Status:           &status,
				OccurredAt:       &ts,
			}
			if err := repo.AppendHistory(ctx, entry); err != nil {
				t.Fatalf("AppendHistory: %v", err)
			}
		}
		entries, err := repo.RecentHistory(ctx, 50)
		if err != nil {
			t.Fatalf("RecentHistory: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}
		if entries[0].PromptTokens == nil || *entries[0].PromptTokens != 12 {
			t.Errorf("entries[0] should be the newest (12), got %v", entries[0].PromptTokens)
		}
		if entries[2].PromptTokens == nil || *entries[2].PromptTokens != 10 {
			t.Errorf("entries[2] should be the oldest (10), got %v", entries[2].PromptTokens)
		}
	})

	t.Run("history is capped at 50 newest rows", func(t *testing.T) {
		// 55 more entries, each with a distinct later timestamp.
		for i := 0; i < 55; i++ {
			model := "claude-3-5-sonnet"
			ts := base.Add(time.Duration(100+i) * time.Minute)
			entry := &usage.RequestHistoryEntry{
				ID:           uuid.New(),
				Model:        &model,
				PromptTokens: intPtr(100 + i),
				OccurredAt:   &ts,
			}
			if err := repo.AppendHistory(ctx, entry); err != nil {
				t.Fatalf("AppendHistory: %v", err)
			}
		}
		entries, err := repo.RecentHistory(ctx, 50)
		if err != nil {
			t.Fatalf("RecentHistory: %v", err)
		}
		if len(entries) != 50 {
			t.Fatalf("expected 50 rows, got %d", len(entries))
		}
		if entries[0].PromptTokens == nil || *entries[0].PromptTokens != 154 {
			t.Errorf("newest entry should be 154, got %v", entries[0].PromptTokens)
		}
		if entries[49].PromptTokens == nil || *entries[49].PromptTokens != 105 {
			t.Errorf("50th entry should be 105, got %v", entries[49].PromptTokens)
		}
	})

	t.Run("smaller limit returns fewer rows", func(t *testing.T) {
		entries, err := repo.RecentHistory(ctx, 10)
		if err != nil {
			t.Fatalf("RecentHistory(10): %v", err)
		}
		if len(entries) != 10 {
			t.Fatalf("expected 10 rows, got %d", len(entries))
		}
	})

	t.Run("zero-token entries are returned (no repo-side filtering)", func(t *testing.T) {
		model := "zero-token-model"
		zero := 0
		ts := base.Add(time.Duration(200) * time.Minute)
		if err := repo.AppendHistory(ctx, &usage.RequestHistoryEntry{
			ID: uuid.New(), Model: &model,
			PromptTokens: &zero, CompletionTokens: &zero,
			OccurredAt: &ts,
		}); err != nil {
			t.Fatalf("AppendHistory zero-token: %v", err)
		}
		entries, err := repo.RecentHistory(ctx, 50)
		if err != nil {
			t.Fatalf("RecentHistory: %v", err)
		}
		if len(entries) != 50 {
			t.Fatalf("expected 50 rows (zero-token entry included), got %d", len(entries))
		}
		if entries[0].Model == nil || *entries[0].Model != "zero-token-model" {
			t.Error("newest zero-token entry missing from history")
		}
	})
}

func intPtr(i int) *int           { return &i }
func floatPtr(f float64) *float64 { return &f }
