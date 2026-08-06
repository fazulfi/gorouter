package repositories

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	txpkg "gorouter/internal/app/tx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func auditRow(id uuid.UUID, action string) []interface{} {
	ip := "127.0.0.1"
	return []interface{}{
		id, nil, action, "provider", nil, []byte(`{"k":"v"}`),
		&ip, time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC),
		nil, "user",
	}
}

func TestAuditQueryRepo_List(t *testing.T) {
	t.Parallel()

	t.Run("select only with deterministic order and pagination", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
				gotSQL = sql
				gotArgs = args
				id := uuid.New()
				return &mockRows{rows: [][]interface{}{auditRow(id, "settings.set")}}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		entries, err := repo.List(context.Background(), txpkg.AuditFilters{}, txpkg.AuditPage{Number: 2, Size: 25})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 || entries[0].Action != "settings.set" || entries[0].ActorKind != "user" {
			t.Errorf("entries = %+v", entries)
		}
		upper := strings.ToUpper(gotSQL)
		if !strings.Contains(upper, "SELECT") || !strings.Contains(upper, "FROM GOROUTER_AUDIT_LOG") {
			t.Errorf("SQL must select from gorouter_audit_log:\n%s", gotSQL)
		}
		for _, banned := range []string{"UPDATE", "DELETE", "INSERT", "TRUNCATE"} {
			if strings.Contains(upper, banned) {
				t.Errorf("query repo SQL must be SELECT-only, found %s:\n%s", banned, gotSQL)
			}
		}
		if !strings.Contains(gotSQL, "ORDER BY occurred_at DESC, id DESC") {
			t.Errorf("SQL must have deterministic order:\n%s", gotSQL)
		}
		if !strings.Contains(gotSQL, "LIMIT") || !strings.Contains(gotSQL, "OFFSET") {
			t.Errorf("SQL must paginate:\n%s", gotSQL)
		}
		if len(gotArgs) != 2 || gotArgs[0] != 25 || gotArgs[1] != 25 {
			t.Errorf("pagination args = %v, want limit 25 offset 25", gotArgs)
		}
	})

	t.Run("page defaults and cap", func(t *testing.T) {
		got := map[string]interface{}{}
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
				got["sql"] = sql
				got["args"] = args
				return &mockRows{}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		if _, err := repo.List(context.Background(), txpkg.AuditFilters{}, txpkg.AuditPage{}); err != nil {
			t.Fatal(err)
		}
		args := got["args"].([]interface{})
		if args[0] != 50 || args[1] != 0 {
			t.Errorf("default page args = %v, want limit 50 offset 0", args)
		}
		if _, err := repo.List(context.Background(), txpkg.AuditFilters{}, txpkg.AuditPage{Number: 1, Size: 10000}); err != nil {
			t.Fatal(err)
		}
		args = got["args"].([]interface{})
		if args[0] != 200 {
			t.Errorf("capped size = %v, want 200", args[0])
		}
	})

	t.Run("filters become parameterized where clauses", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
				gotSQL = sql
				gotArgs = args
				return &mockRows{}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		actorID := uuid.New()
		jobID := uuid.New()
		since := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
		until := time.Date(2026, 8, 3, 0, 0, 0, 0, time.UTC)
		kind := "job"
		resType := "backup"
		resID := uuid.New()
		action := "backup.generate"
		f := txpkg.AuditFilters{
			ActorID: &actorID, ResourceType: &resType, ResourceID: &resID,
			Action: &action, ActorKind: &kind, JobID: &jobID,
			Since: &since, Until: &until,
		}
		if _, err := repo.List(context.Background(), f, txpkg.AuditPage{}); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"actor_id = $", "resource_type = $", "resource_id = $", "action = $",
			"actor_kind = $", "job_id = $", "occurred_at >= $", "occurred_at <="} {
			if !strings.Contains(gotSQL, want) {
				t.Errorf("SQL missing filter %q:\n%s", want, gotSQL)
			}
		}
		if len(gotArgs) != 10 {
			t.Errorf("args = %d, want 8 filters + limit + offset", len(gotArgs))
		}
	})

	t.Run("no filters yields no where clause", func(t *testing.T) {
		var gotSQL string
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, _ ...interface{}) (pgx.Rows, error) {
				gotSQL = sql
				return &mockRows{}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		if _, err := repo.List(context.Background(), txpkg.AuditFilters{}, txpkg.AuditPage{}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(gotSQL, "WHERE") {
			t.Errorf("no WHERE expected without filters:\n%s", gotSQL)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &auditQueryRepo{tx: tx}
		if _, err := repo.List(context.Background(), txpkg.AuditFilters{}, txpkg.AuditPage{}); err == nil {
			t.Error("expected error")
		}
	})
}

func TestAuditQueryRepo_Export(t *testing.T) {
	t.Parallel()

	t.Run("select only without pagination", func(t *testing.T) {
		var gotSQL string
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, _ ...interface{}) (pgx.Rows, error) {
				gotSQL = sql
				id := uuid.New()
				return &mockRows{rows: [][]interface{}{auditRow(id, "settings.set")}}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		entries, err := repo.Export(context.Background(), txpkg.AuditFilters{})
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}
		if !strings.Contains(gotSQL, "ORDER BY occurred_at DESC, id DESC") {
			t.Errorf("SQL must have deterministic order:\n%s", gotSQL)
		}
		if strings.Contains(gotSQL, "LIMIT") || strings.Contains(gotSQL, "OFFSET") {
			t.Errorf("Export must not paginate:\n%s", gotSQL)
		}
		upper := strings.ToUpper(gotSQL)
		for _, banned := range []string{"UPDATE", "DELETE", "INSERT", "TRUNCATE"} {
			if strings.Contains(upper, banned) {
				t.Errorf("Export SQL must be SELECT-only, found %s", banned)
			}
		}
	})

	t.Run("scans ip and job provenance", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				id := uuid.New()
				jobID := uuid.New()
				ip := "::1"
				return &mockRows{rows: [][]interface{}{
					{id, nil, "job.run", "job", nil, []byte(`{}`), &ip,
						time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), &jobID, "job"},
				}}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		entries, err := repo.Export(context.Background(), txpkg.AuditFilters{})
		if err != nil {
			t.Fatal(err)
		}
		e := entries[0]
		if e.ActorKind != "job" || e.JobID == nil || *e.JobID != *entries[0].JobID {
			t.Errorf("job provenance not scanned: %+v", e)
		}
		if e.IPAddress == nil || !e.IPAddress.Equal(net.ParseIP("::1")) {
			t.Errorf("ip = %v, want ::1", e.IPAddress)
		}
	})

	t.Run("empty result", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		entries, err := repo.Export(context.Background(), txpkg.AuditFilters{})
		if err != nil {
			t.Fatal(err)
		}
		if entries == nil || len(entries) != 0 {
			t.Errorf("entries = %v, want empty non-nil", entries)
		}
	})

	t.Run("nil ip and nil details stay nil", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				id := uuid.New()
				return &mockRows{rows: [][]interface{}{
					{id, nil, "settings.set", "setting", nil, nil, nil,
						time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC), nil, "user"},
				}}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		entries, err := repo.Export(context.Background(), txpkg.AuditFilters{})
		if err != nil {
			t.Fatal(err)
		}
		e := entries[0]
		if e.IPAddress != nil {
			t.Errorf("ip = %v, want nil", e.IPAddress)
		}
		if e.Details != nil {
			t.Errorf("details = %s, want nil", e.Details)
		}
	})

	t.Run("scan error propagates", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{rows: [][]interface{}{auditRow(uuid.New(), "settings.set")}, err: errors.New("scan failed")}, nil
			},
		}
		repo := &auditQueryRepo{tx: tx}
		if _, err := repo.Export(context.Background(), txpkg.AuditFilters{}); err == nil {
			t.Error("expected scan error")
		}
	})
}
