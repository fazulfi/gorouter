package repositories

import (
	"context"
	"encoding/json"
	"net"
	"testing"
	"time"

	"gorouter/internal/app/tx"

	"github.com/google/uuid"
)

func mapsEqualString(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestAuditQueryRepo_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	actorA := uuid.New()
	actorB := uuid.New()
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true), ($4, $5, $3, true, true)`,
		actorA, "auditq-a-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash",
		actorB, "auditq-b-"+uuid.NewString()[:8]+"@gorouter.local"); err != nil {
		t.Fatalf("insert users: %v", err)
	}

	pgTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer pgTx.Rollback(ctx)

	repo := NewAuditQueryRepo(pgTx)
	writeRepo := &auditLogRepo{tx: pgTx}
	base := time.Date(2026, 8, 2, 12, 0, 0, 0, time.UTC)
	rows := []tx.AuditLogEntry{
		{ID: uuid.New(), ActorID: &actorA, Action: "settings.set", ResourceType: "setting",
			Details: json.RawMessage(`{"key":"rtkEnabled"}`), IPAddress: net.ParseIP("127.0.0.1"),
			OccurredAt: base.Add(1 * time.Minute)},
		{ID: uuid.New(), ActorID: &actorB, Action: "backup.generate", ResourceType: "backup",
			Details: json.RawMessage(`{"bytes":10}`), IPAddress: net.ParseIP("::1"),
			OccurredAt: base.Add(2 * time.Minute)},
		{ID: uuid.New(), ActorID: &actorA, Action: "settings.set", ResourceType: "setting",
			Details: json.RawMessage(`{"key":"enable_tunnel"}`), IPAddress: net.ParseIP("127.0.0.1"),
			OccurredAt: base.Add(3 * time.Minute)},
	}
	for _, e := range rows {
		if err := writeRepo.Create(ctx, &e); err != nil {
			t.Fatalf("seed audit row: %v", err)
		}
	}

	t.Run("list returns newest first with id tiebreaker", func(t *testing.T) {
		entries, err := repo.List(ctx, tx.AuditFilters{}, tx.AuditPage{Number: 1, Size: 10})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("entries = %d, want 3", len(entries))
		}
		wantOrder := []string{"settings.set", "backup.generate", "settings.set"}
		wantTimes := []time.Time{base.Add(3 * time.Minute), base.Add(2 * time.Minute), base.Add(1 * time.Minute)}
		for i, e := range entries {
			if e.Action != wantOrder[i] {
				t.Errorf("entries[%d].Action = %s, want %s", i, e.Action, wantOrder[i])
			}
			if !e.OccurredAt.Equal(wantTimes[i]) {
				t.Errorf("entries[%d].OccurredAt = %s, want %s (newest first)", i, e.OccurredAt, wantTimes[i])
			}
			if e.ActorKind != "user" {
				t.Errorf("entries[%d].ActorKind = %s, want user (default)", i, e.ActorKind)
			}
			if e.JobID != nil {
				t.Errorf("entries[%d].JobID = %v, want nil for human rows", i, e.JobID)
			}
		}
	})

	t.Run("actor filter narrows results", func(t *testing.T) {
		entries, err := repo.List(ctx, tx.AuditFilters{ActorID: &actorA}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(entries) != 2 {
			t.Fatalf("entries for actorA = %d, want 2", len(entries))
		}
		for _, e := range entries {
			if e.ActorID == nil || *e.ActorID != actorA {
				t.Errorf("entry actor = %v", e.ActorID)
			}
		}
	})

	t.Run("action and time-range filters", func(t *testing.T) {
		action := "settings.set"
		since := base.Add(2 * time.Minute)
		entries, err := repo.List(ctx, tx.AuditFilters{Action: &action, Since: &since}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}
		if entries[0].Details == nil {
			t.Errorf("entry details = nil")
		} else {
			var got, want map[string]string
			if err := json.Unmarshal(entries[0].Details, &got); err != nil {
				t.Fatalf("parse details: %v", err)
			}
			want = map[string]string{"key": "enable_tunnel"}
			if !mapsEqualString(got, want) {
				t.Errorf("entry details = %s, want %v", entries[0].Details, want)
			}
		}
		until := base.Add(90 * time.Second)
		entries, err = repo.List(ctx, tx.AuditFilters{Until: &until}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list until: %v", err)
		}
		if len(entries) != 1 || entries[0].Action != "settings.set" {
			t.Errorf("until entries = %+v, want the earliest settings.set only", entries)
		}
	})

	t.Run("injection-shaped filter values are bound, never interpolated", func(t *testing.T) {
		attack := "settings.set' OR '1'='1"
		attackType := "setting' OR 1=1 --"
		attackActor := uuid.New()
		entries, err := repo.List(ctx, tx.AuditFilters{
			ActorID: &attackActor, Action: &attack, ResourceType: &attackType,
		}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("injection-shaped filters matched %d rows, want 0 (values must be bound parameters)", len(entries))
		}
	})

	t.Run("pagination slices deterministically", func(t *testing.T) {
		page1, err := repo.List(ctx, tx.AuditFilters{}, tx.AuditPage{Number: 1, Size: 2})
		if err != nil {
			t.Fatalf("page 1: %v", err)
		}
		page2, err := repo.List(ctx, tx.AuditFilters{}, tx.AuditPage{Number: 2, Size: 2})
		if err != nil {
			t.Fatalf("page 2: %v", err)
		}
		if len(page1) != 2 || len(page2) != 1 {
			t.Fatalf("page sizes = %d/%d, want 2/1", len(page1), len(page2))
		}
		if page1[0].ID == page2[0].ID {
			t.Error("pages overlap")
		}
	})

	t.Run("export returns everything without pagination", func(t *testing.T) {
		entries, err := repo.Export(ctx, tx.AuditFilters{})
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if len(entries) != 3 {
			t.Fatalf("export = %d, want 3", len(entries))
		}
	})

	t.Run("ip address round-trips", func(t *testing.T) {
		entries, err := repo.Export(ctx, tx.AuditFilters{ActorID: &actorB})
		if err != nil {
			t.Fatalf("export: %v", err)
		}
		if len(entries) != 1 {
			t.Fatalf("entries = %d, want 1", len(entries))
		}
		if entries[0].IPAddress == nil || !entries[0].IPAddress.Equal(net.ParseIP("::1")) {
			t.Errorf("ip = %v, want ::1", entries[0].IPAddress)
		}
	})

	t.Run("job provenance filters by actor_kind and job_id", func(t *testing.T) {
		jobID := uuid.New()
		jobAction := "backup.schedule"
		if _, err := pgTx.Exec(ctx,
			`INSERT INTO gorouter_audit_log (id, actor_id, action, resource_type, resource_id, details, ip_address, occurred_at, job_id, actor_kind)
			 VALUES ($1, NULL, $2, 'job', NULL, '{}'::jsonb, NULL, $3, $4, 'job')`,
			uuid.New(), jobAction, base.Add(4*time.Minute), jobID); err != nil {
			t.Fatalf("seed job audit row: %v", err)
		}
		kind := "job"
		byKind, err := repo.List(ctx, tx.AuditFilters{ActorKind: &kind}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list by actor_kind: %v", err)
		}
		if len(byKind) != 1 {
			t.Fatalf("job rows by kind = %d, want 1", len(byKind))
		}
		if byKind[0].ActorID != nil || byKind[0].JobID == nil || *byKind[0].JobID != jobID {
			t.Errorf("job provenance = actor %v job %v, want nil actor and job %s", byKind[0].ActorID, byKind[0].JobID, jobID)
		}
		if byKind[0].ActorKind != "job" {
			t.Errorf("job row actor_kind = %s, want job", byKind[0].ActorKind)
		}
		byJob, err := repo.List(ctx, tx.AuditFilters{JobID: &jobID}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list by job_id: %v", err)
		}
		if len(byJob) != 1 || byJob[0].Action != jobAction {
			t.Errorf("job rows by job_id = %+v, want the seeded %s", byJob, jobAction)
		}
		userKind := "user"
		byUser, err := repo.List(ctx, tx.AuditFilters{ActorKind: &userKind}, tx.AuditPage{})
		if err != nil {
			t.Fatalf("list by user kind: %v", err)
		}
		if len(byUser) != 3 {
			t.Errorf("user rows by kind = %d, want 3", len(byUser))
		}
	})
}

// TestAuditQueryRepo_ReadOnlyContract_Integration proves the query surface is
// SELECT-only at the database level: the runtime role can list and export, and
// the immutability guarantees (no UPDATE/DELETE from this surface) are the
// schema-level guarantees proven by BE-09's negative role tests.
func TestAuditQueryRepo_ReadOnlyContract_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	pgTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer pgTx.Rollback(ctx)

	repo := NewAuditQueryRepo(pgTx)
	entries, err := repo.Export(ctx, tx.AuditFilters{})
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if entries == nil {
		t.Fatal("export must return a non-nil slice")
	}
}
