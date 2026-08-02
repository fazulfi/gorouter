package repositories

import (
	"context"
	"testing"
	"time"

	appusage "gorouter/internal/app/usage"
	"gorouter/internal/domain/usage"

	"github.com/google/uuid"
)

// TestUsageServicesEndToEnd_Integration drives the BE-12 usage services
// through the real wired chain — TxScope factory -> usageRepo
// (gorouter_request_details / gorouter_request_history / gorouter_usage_daily)
// — proving: immutable per-request recording, durable newest-first history,
// ring hydration/refresh from durable history, idempotent daily aggregation
// that never rewrites immutable history older than 90 days, transactional
// retention purging with a strict cutoff boundary (audit untouched), and
// same-transaction rollback at the repo boundary.
func TestUsageServicesEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	beginner := appusage.UsageScopeBeginnerFunc(func(ctx context.Context) (appusage.UsageScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})

	// ── UsagePipeline.Record persists immutable detail + history ──
	pipe := appusage.NewUsagePipeline(beginner)
	ring := appusage.NewRecentRequestsRing(appusage.NewRequestHistoryService(beginner))
	pipe = pipe.WithRing(ring)

	// TIMESTAMPTZ stores microseconds; truncate to match read-back values.
	now := time.Now().UTC().Truncate(time.Microsecond)
	reqID := uuid.New()
	model := "gpt-4o"
	provider := "openai"
	prompt := 10
	completion := 20
	cost := 0.25
	status := "completed"
	if err := pipe.Record(ctx, nil, usage.UsageRecord{
		RequestID: &reqID, Model: &model, Provider: &provider,
		PromptTokens: &prompt, CompletionTokens: &completion,
		Cost: &cost, Status: &status, OccurredAt: &now,
	}); err != nil {
		t.Fatalf("Record: %v", err)
	}

	var detailCount, histCount int
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gorouter_request_details WHERE model = $1`, model).Scan(&detailCount); err != nil {
		t.Fatalf("count details: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gorouter_request_history WHERE model = $1`, model).Scan(&histCount); err != nil {
		t.Fatalf("count history: %v", err)
	}
	if detailCount != 1 || histCount != 1 {
		t.Fatalf("Record stored %d details / %d history rows, want 1/1", detailCount, histCount)
	}
	if ring.Len() != 1 {
		t.Fatalf("ring not pushed by Record: len %d", ring.Len())
	}

	// A second record appends; nothing is ever rewritten. Distinct tokens so
	// the two records do not share a display dedup key (same minute bucket).
	now2 := now.Add(30 * time.Second)
	if err := pipe.Record(ctx, nil, usage.UsageRecord{
		Model: &model, Provider: &provider, PromptTokens: intPtr(11),
		CompletionTokens: intPtr(21), OccurredAt: &now2,
	}); err != nil {
		t.Fatalf("Record #2: %v", err)
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gorouter_request_details`).Scan(&detailCount); err != nil {
		t.Fatalf("count details: %v", err)
	}
	if detailCount != 2 {
		t.Fatalf("second Record must insert, not rewrite: %d details", detailCount)
	}

	// ── RequestHistoryService.Recent is durable and newest-first ──
	histSvc := appusage.NewRequestHistoryService(beginner)
	recent, err := histSvc.Recent(ctx, 10)
	if err != nil {
		t.Fatalf("Recent: %v", err)
	}
	if len(recent) != 2 {
		t.Fatalf("recent = %d rows, want 2", len(recent))
	}
	if recent[0].OccurredAt == nil || !recent[0].OccurredAt.Equal(now2) {
		t.Errorf("newest first: %v, want %v", recent[0].OccurredAt, now2)
	}

	// ── Ring Refresh hydrates from durable history ──
	refreshRing := appusage.NewRecentRequestsRing(histSvc)
	if err := refreshRing.Refresh(ctx); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
	if refreshRing.Len() != 2 || !refreshRing.Initialized() {
		t.Fatalf("refresh: len %d initialized %v", refreshRing.Len(), refreshRing.Initialized())
	}
	rows := refreshRing.Display()
	if len(rows) != 2 || !rows[0].OccurredAt.Equal(now2) {
		t.Errorf("refreshed display not newest-first: %+v", rows)
	}

	// ── AggregateDaily: idempotent, never rewrites history older than 90d ──
	writeTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin write tx: %v", err)
	}
	writeRepo := NewUsageRepo(writeTx)
	oldDay := now.AddDate(0, 0, -(usage.RetentionDays + 1))
	newDay := now.Add(-2 * time.Hour)
	for _, at := range []time.Time{oldDay, newDay} {
		if err := writeRepo.WriteRequestDetail(ctx, &usage.RequestDetail{
			ID: uuid.New(), Model: &model, PromptTokens: intPtr(3),
			CompletionTokens: intPtr(4), Cost: floatPtr(0.05), OccurredAt: &at,
		}); err != nil {
			t.Fatalf("WriteRequestDetail: %v", err)
		}
	}
	// A pre-existing aggregate row from 120 days ago (written when the day
	// was current). AggregateDaily must never rewrite it: it has no details
	// in the retention window and its day predates the cutoff.
	oldAggDay := now.AddDate(0, 0, -120).UTC().Truncate(24 * time.Hour)
	if err := writeRepo.AggregateDaily(ctx, &usage.DailyAggregate{
		Day: oldAggDay, Requests: intPtr(1), PromptTokens: int64Ptr(9), CompletionTokens: int64Ptr(9),
	}); err != nil {
		t.Fatalf("AggregateDaily (historical row): %v", err)
	}
	if err := writeTx.Commit(ctx); err != nil {
		t.Fatalf("commit write tx: %v", err)
	}
	if err := pipe.AggregateDaily(ctx); err != nil {
		t.Fatalf("AggregateDaily #1: %v", err)
	}
	if err := pipe.AggregateDaily(ctx); err != nil {
		t.Fatalf("AggregateDaily #2 (idempotent): %v", err)
	}

	readTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer readTx.Rollback(ctx)
	repo := NewUsageRepo(readTx)
	todayRows, err := repo.Daily(ctx, now.UTC().Truncate(24*time.Hour))
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(todayRows) != 1 {
		t.Fatalf("today aggregates = %d rows, want exactly 1 (day primary key)", len(todayRows))
	}
	if todayRows[0].Requests == nil || *todayRows[0].Requests != 3 || // two records + one detail
		todayRows[0].PromptTokens == nil || *todayRows[0].PromptTokens != 24 ||
		todayRows[0].CompletionTokens == nil || *todayRows[0].CompletionTokens != 45 {
		t.Errorf("today aggregate = %+v", todayRows[0])
	}
	oldAggs, err := repo.Daily(ctx, oldDay.UTC().Truncate(24*time.Hour))
	if err != nil {
		t.Fatalf("Daily(old): %v", err)
	}
	if len(oldAggs) != 0 {
		t.Errorf("history older than 90 days must never be rewritten, got %d rows", len(oldAggs))
	}
	immutableRows, err := repo.Daily(ctx, oldAggDay)
	if err != nil {
		t.Fatalf("Daily(oldAggDay): %v", err)
	}
	if len(immutableRows) != 1 || immutableRows[0].Requests == nil || *immutableRows[0].Requests != 1 ||
		immutableRows[0].PromptTokens == nil || *immutableRows[0].PromptTokens != 9 {
		t.Errorf("pre-existing immutable aggregate row was rewritten: %+v", immutableRows)
	}

	// ── RetentionService.PurgeBefore: strict boundary, audit untouched ──
	retSvc := appusage.NewRetentionService(beginner)
	cutoff := now.AddDate(0, 0, -usage.RetentionDays)
	stats, err := retSvc.PurgeBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("PurgeBefore: %v", err)
	}
	if stats.DetailsPurged != 1 || stats.HistoryPurged != 0 || stats.DailyPurged != 1 {
		t.Fatalf("purge stats = %+v, want details 1 (91d old), history 0, daily 1 (120d old)", stats)
	}
	gone, err := repo.Daily(ctx, oldAggDay)
	if err != nil {
		t.Fatalf("Daily(oldAggDay) after purge: %v", err)
	}
	if len(gone) != 0 {
		t.Errorf("daily row older than cutoff must be purged, got %d", len(gone))
	}
	if err := pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM gorouter_request_details`).Scan(&detailCount); err != nil {
		t.Fatalf("count details after purge: %v", err)
	}
	if detailCount != 3 {
		t.Errorf("recent details must survive purge, got %d (want 2 records + 1 recent detail)", detailCount)
	}
	var auditCount int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM gorouter_audit_log`).Scan(&auditCount); err != nil {
		t.Fatalf("count audit: %v", err)
	}
	if auditCount != 0 {
		t.Errorf("usage services must never touch the audit log, got %d rows", auditCount)
	}

	// ── Same-transaction rollback at the repo boundary ──
	rollbackTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin rollback tx: %v", err)
	}
	rollbackRepo := NewUsageRepo(rollbackTx)
	dupDetailID := uuid.New()
	if err := rollbackRepo.WriteRequestDetail(ctx, &usage.RequestDetail{
		ID: dupDetailID, Model: &model, OccurredAt: &now,
	}); err != nil {
		t.Fatalf("WriteRequestDetail in rollback tx: %v", err)
	}
	// Force the second statement to fail: duplicate history primary key.
	dupHistID := uuid.New()
	if err := rollbackRepo.AppendHistory(ctx, &usage.RequestHistoryEntry{ID: dupHistID}); err != nil {
		t.Fatalf("AppendHistory in rollback tx: %v", err)
	}
	if err := rollbackRepo.AppendHistory(ctx, &usage.RequestHistoryEntry{ID: dupHistID}); err == nil {
		t.Fatal("duplicate history ID must fail")
	}
	if err := rollbackTx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	verifyTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin verify tx: %v", err)
	}
	defer verifyTx.Rollback(ctx)
	if d, _ := NewUsageRepo(verifyTx).RequestDetail(ctx, dupDetailID); d != nil {
		t.Error("detail written before the failed statement must be rolled back")
	}
}
