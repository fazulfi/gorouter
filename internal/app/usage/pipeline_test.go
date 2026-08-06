package usage

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

func TestUsagePipelineRecord(t *testing.T) {
	scope, repo := newFakeScope()
	beginner := newFakeBeginner(scope)
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	pipe := &UsagePipeline{beginner: beginner, retentionDays: usage.RetentionDays, now: func() time.Time { return now }}

	reqID := uuid.New()
	provID := uuid.New()
	model := "gpt-4o"
	provider := "openai"
	prompt := 10
	completion := 20
	cost := 0.25
	status := "ok"
	errKind := "rate_limit"
	occurred := now.Add(-time.Minute)
	record := usage.UsageRecord{
		RequestID: &reqID, ProviderID: &provID, Model: &model, Provider: &provider,
		PromptTokens: &prompt, CompletionTokens: &completion, Cost: &cost,
		Status: &status, ErrorKind: &errKind, OccurredAt: &occurred, DebugOptIn: true,
	}

	if err := pipe.Record(context.Background(), nil, record); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if scope.commits != 1 || scope.rollbacks != 0 {
		t.Errorf("tx: commits %d rollbacks %d, want 1/0", scope.commits, scope.rollbacks)
	}
	if len(repo.details) != 1 || len(repo.history) != 1 {
		t.Fatalf("stored %d details %d history, want 1/1", len(repo.details), len(repo.history))
	}
	d := repo.details[0]
	if d.ID == uuid.Nil || d.RequestID == nil || *d.RequestID != reqID || d.ProviderID == nil || *d.ProviderID != provID {
		t.Errorf("detail ids wrong: %+v", d)
	}
	if d.Model == nil || *d.Model != model || d.PromptTokens == nil || *d.PromptTokens != prompt ||
		d.CompletionTokens == nil || *d.CompletionTokens != completion || d.Cost == nil || *d.Cost != cost ||
		d.Status == nil || *d.Status != status || d.ErrorKind == nil || *d.ErrorKind != errKind ||
		d.OccurredAt == nil || !d.OccurredAt.Equal(occurred) || !d.DebugOptIn {
		t.Errorf("detail fields not preserved: %+v", d)
	}
	h := repo.history[0]
	if h.ID == uuid.Nil || h.Model == nil || *h.Model != model || h.Provider == nil || *h.Provider != provider ||
		h.PromptTokens == nil || *h.PromptTokens != prompt || h.CompletionTokens == nil || *h.CompletionTokens != completion ||
		h.Status == nil || *h.Status != status || h.OccurredAt == nil || !h.OccurredAt.Equal(occurred) {
		t.Errorf("history entry not derived from record: %+v", h)
	}
	if record.OccurredAt != &occurred || record.Model != &model {
		t.Error("Record must not mutate the input record")
	}
}

func TestUsagePipelineRecordDefaultsOccurredAt(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
	if err := pipe.Record(context.Background(), &auth.Actor{UserID: uuid.New()}, usage.UsageRecord{}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if repo.details[0].OccurredAt == nil || !repo.details[0].OccurredAt.Equal(now) {
		t.Errorf("occurred_at not stamped with pipeline clock: %v", repo.details[0].OccurredAt)
	}
	if repo.history[0].OccurredAt == nil || !repo.history[0].OccurredAt.Equal(now) {
		t.Errorf("history occurred_at not stamped: %v", repo.history[0].OccurredAt)
	}
}

func TestUsagePipelineRecordPreservesNilFields(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
	model := "m"
	if err := pipe.Record(context.Background(), nil, usage.UsageRecord{Model: &model}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	d := repo.details[0]
	if d.RequestID != nil || d.ProviderID != nil || d.PromptTokens != nil || d.CompletionTokens != nil ||
		d.Cost != nil || d.Status != nil || d.ErrorKind != nil || d.DebugOptIn {
		t.Errorf("nil record fields must stay nil: %+v", d)
	}
}

func TestUsagePipelineRecordPushesRingAfterCommit(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	ring := NewRecentRequestsRing(&fakeRingSource{})
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays,
		now: func() time.Time { return now }, ring: ring}
	model := "m"
	prompt := 5
	completion := 6
	at := now.Add(-time.Minute)
	if err := pipe.Record(context.Background(), nil, usage.UsageRecord{
		Model: &model, PromptTokens: &prompt, CompletionTokens: &completion, OccurredAt: &at,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}
	if ring.Len() != 1 {
		t.Fatalf("ring len = %d, want 1 after commit", ring.Len())
	}
	got := ring.Display()
	if len(got) != 1 || got[0].Model == nil || *got[0].Model != model {
		t.Errorf("ring display missing recorded entry: %+v", got)
	}
	_ = repo
}

func TestUsagePipelineRecordErrorRollback(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)

	t.Run("detail write fails", func(t *testing.T) {
		scope, _ := newFakeScope()
		scope.repo.detailWriteErr = errors.New("insert failed")
		pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
		if err := pipe.Record(context.Background(), nil, usage.UsageRecord{}); err == nil {
			t.Fatal("expected error")
		}
		if scope.commits != 0 || scope.rollbacks != 1 {
			t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
		}
	})

	t.Run("history append fails", func(t *testing.T) {
		scope, _ := newFakeScope()
		scope.repo.historyAppendErr = errors.New("insert failed")
		pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
		if err := pipe.Record(context.Background(), nil, usage.UsageRecord{}); err == nil {
			t.Fatal("expected error")
		}
		if scope.commits != 0 || scope.rollbacks != 1 {
			t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
		}
	})

	t.Run("commit fails", func(t *testing.T) {
		scope, _ := newFakeScope()
		scope.commitErr = errors.New("commit failed")
		pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
		if err := pipe.Record(context.Background(), nil, usage.UsageRecord{}); err == nil {
			t.Fatal("expected error")
		}
		if scope.rollbacks != 1 {
			t.Errorf("rollbacks = %d, want 1", scope.rollbacks)
		}
	})

	t.Run("begin fails", func(t *testing.T) {
		scope, _ := newFakeScope()
		scope.beginErr = errors.New("no pool")
		pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
		if err := pipe.Record(context.Background(), nil, usage.UsageRecord{}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestUsagePipelineRecordImmutableNoUpdatePath(t *testing.T) {
	// The domain repository contract pins the exact method set: insert/select
	// for details and history, plus idempotent aggregation and retention-only
	// purge. There is no update or delete path for request details or history.
	rt := reflect.TypeOf((*usage.UsageRepository)(nil)).Elem()
	want := map[string]bool{
		"AggregateDaily":     true,
		"Daily":              true,
		"WriteRequestDetail": true,
		"RequestDetail":      true,
		"AppendHistory":      true,
		"RecentHistory":      true,
		"DetailsBetween":     true,
		"PurgeBefore":        true,
	}
	if rt.NumMethod() != len(want) {
		t.Errorf("UsageRepository has %d methods, want exactly %d", rt.NumMethod(), len(want))
	}
	for i := 0; i < rt.NumMethod(); i++ {
		if !want[rt.Method(i).Name] {
			t.Errorf("unexpected UsageRepository method %s", rt.Method(i).Name)
		}
	}
}

func TestUsagePipelineAggregateDaily(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, 0, -usage.RetentionDays)
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}

	detail := func(at time.Time, prompt, completion int, cost float64) usage.RequestDetail {
		return usage.RequestDetail{
			ID: uuid.New(), Model: strPtr("m"), PromptTokens: intPtr(prompt),
			CompletionTokens: intPtr(completion), Cost: &cost, OccurredAt: &at,
		}
	}
	repo.details = []usage.RequestDetail{
		detail(now.Add(-2*time.Hour), 10, 20, 0.1),                      // today
		detail(now.Add(-3*time.Hour), 5, 5, 0.2),                        // today (same day bucket)
		detail(now.Add(-24*time.Hour), 30, 40, 0.5),                     // yesterday
		detail(now.AddDate(0, 0, -(usage.RetentionDays+1)), 1, 1, 0.05), // older than 90d: immutable, excluded
		{ID: uuid.New(), OccurredAt: nil},                               // no day bucket: excluded
		detail(cutoff.Add(2*time.Hour), 7, 8, 0.1234567),                // cutoff day, day start < cutoff: immutable, skipped
	}

	if err := pipe.AggregateDaily(context.Background()); err != nil {
		t.Fatalf("AggregateDaily: %v", err)
	}
	if scope.commits != 1 {
		t.Errorf("commits = %d, want 1", scope.commits)
	}
	if len(repo.aggWrites) != 2 {
		t.Fatalf("aggregates written = %d, want 2 (today + yesterday only)", len(repo.aggWrites))
	}
	// Ordered by day ascending: yesterday first, then today.
	y := repo.aggWrites[0]
	wantDay := now.Add(-24 * time.Hour).UTC().Truncate(24 * time.Hour)
	if !y.Day.Equal(wantDay) || y.Requests == nil || *y.Requests != 1 || y.PromptTokens == nil || *y.PromptTokens != 30 ||
		y.CompletionTokens == nil || *y.CompletionTokens != 40 || y.Cost == nil || *y.Cost != 0.5 {
		t.Errorf("yesterday aggregate wrong: %+v", y)
	}
	td := repo.aggWrites[1]
	wantToday := now.UTC().Truncate(24 * time.Hour)
	if !td.Day.Equal(wantToday) || td.Requests == nil || *td.Requests != 2 || td.PromptTokens == nil || *td.PromptTokens != 15 ||
		td.CompletionTokens == nil || *td.CompletionTokens != 25 || td.Cost == nil || *td.Cost != 0.3 {
		t.Errorf("today aggregate wrong: %+v", td)
	}
	if td.ProviderID != nil || td.ModelID != nil {
		t.Errorf("whole-day aggregate must have nil provider/model: %+v", td)
	}
}

func TestUsagePipelineAggregateDailyIdempotent(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
	at := now.Add(-time.Hour)
	repo.details = []usage.RequestDetail{{ID: uuid.New(), PromptTokens: intPtr(10), CompletionTokens: intPtr(20), Cost: floatPtr(0.25), OccurredAt: &at}}

	if err := pipe.AggregateDaily(context.Background()); err != nil {
		t.Fatalf("AggregateDaily #1: %v", err)
	}
	if err := pipe.AggregateDaily(context.Background()); err != nil {
		t.Fatalf("AggregateDaily #2: %v", err)
	}
	if len(repo.aggWrites) != 2 {
		t.Fatalf("agg writes = %d, want 2 (one per run)", len(repo.aggWrites))
	}
	first, second := repo.aggWrites[0], repo.aggWrites[1]
	if !first.Day.Equal(second.Day) || derefInt(first.Requests) != derefInt(second.Requests) ||
		derefInt64(first.PromptTokens) != derefInt64(second.PromptTokens) ||
		derefInt64(first.CompletionTokens) != derefInt64(second.CompletionTokens) ||
		first.Cost == nil || second.Cost == nil || *first.Cost != *second.Cost {
		t.Errorf("idempotent recompute differs: %+v vs %+v", first, second)
	}
}

func TestUsagePipelineAggregateDailyCostRounding(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
	at := now.Add(-time.Hour)
	repo.details = []usage.RequestDetail{
		{ID: uuid.New(), Cost: floatPtr(0.1234567), OccurredAt: &at},
		{ID: uuid.New(), Cost: floatPtr(0.1), OccurredAt: &at},
	}
	if err := pipe.AggregateDaily(context.Background()); err != nil {
		t.Fatalf("AggregateDaily: %v", err)
	}
	if len(repo.aggWrites) != 1 || repo.aggWrites[0].Cost == nil || *repo.aggWrites[0].Cost != 0.223457 {
		t.Errorf("cost must round to 6dp, got %+v", repo.aggWrites[0].Cost)
	}
}

func TestUsagePipelineAggregateDailyEmpty(t *testing.T) {
	scope, repo := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
	if err := pipe.AggregateDaily(context.Background()); err != nil {
		t.Fatalf("AggregateDaily on empty store: %v", err)
	}
	if len(repo.aggWrites) != 0 {
		t.Errorf("no details -> no aggregates, got %d", len(repo.aggWrites))
	}
}

func TestUsagePipelineAggregateDailyErrorRollback(t *testing.T) {
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	t.Run("load fails", func(t *testing.T) {
		scope, _ := newFakeScope()
		scope.repo.betweenErr = errors.New("select failed")
		pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
		if err := pipe.AggregateDaily(context.Background()); err == nil {
			t.Fatal("expected error")
		}
		if scope.commits != 0 || scope.rollbacks != 1 {
			t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
		}
	})
	t.Run("upsert fails", func(t *testing.T) {
		scope, repo := newFakeScope()
		scope.repo.aggErr = errors.New("upsert failed")
		at := now.Add(-time.Hour)
		repo.details = []usage.RequestDetail{{ID: uuid.New(), PromptTokens: intPtr(1), OccurredAt: &at}}
		pipe := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
		if err := pipe.AggregateDaily(context.Background()); err == nil {
			t.Fatal("expected error")
		}
		if scope.commits != 0 || scope.rollbacks != 1 {
			t.Errorf("tx: commits %d rollbacks %d, want 0/1", scope.commits, scope.rollbacks)
		}
	})
}

func TestUsagePipelineWithRingCopies(t *testing.T) {
	scope, _ := newFakeScope()
	now := time.Date(2026, 8, 3, 12, 0, 0, 0, time.UTC)
	base := &UsagePipeline{beginner: newFakeBeginner(scope), retentionDays: usage.RetentionDays, now: func() time.Time { return now }}
	ring := NewRecentRequestsRing(&fakeRingSource{})
	wired := base.WithRing(ring)
	if base.ring != nil || wired.ring != ring {
		t.Error("WithRing must return a copy leaving the receiver unwired")
	}
	if wired.retentionDays != usage.RetentionDays {
		t.Error("copy must preserve pipeline settings")
	}
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
func floatPtr(f float64) *float64 {
	return &f
}
func derefInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
func derefInt64(p *int64) int64 {
	if p == nil {
		return 0
	}
	return *p
}
