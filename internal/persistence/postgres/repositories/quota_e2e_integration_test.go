package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	appquota "gorouter/internal/app/quota"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/quota"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TestQuotaUnlockAuditedEndToEnd_Integration drives the BE-13 quota service
// through the real wired chain — TransactionManager -> TxScope factory ->
// auditRepo (gorouter_audit_log) — proving the audited unlock contract on
// real PostgreSQL: the audit row is committed in the same transaction with
// the actor ID, the scoped provider target and sanitized before/after
// details, and the nil-provider (global) path writes nothing.
func TestQuotaUnlockAuditedEndToEnd_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	beginner := appquota.QuotaScopeBeginnerFunc(func(ctx context.Context) (appquota.QuotaScope, error) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			return nil, err
		}
		return NewTxScope(tx), nil
	})

	actor := &auth.Actor{UserID: uuid.New(), SessionID: uuid.New(), IsAdmin: true}
	if _, err := pool.Exec(ctx,
		`INSERT INTO gorouter_users (id, email, password_hash, is_admin, is_active)
		 VALUES ($1, $2, $3, true, true)`,
		actor.UserID, "quota-e2e-"+uuid.NewString()[:8]+"@gorouter.local", "test-hash"); err != nil {
		t.Fatalf("insert user: %v", err)
	}
	providerID := uuid.New()
	svc := appquota.NewQuotaService(beginner)

	if err := svc.RecordWindow(ctx, providerID, time.Now().UTC(), 0, 0); err != nil {
		t.Fatalf("RecordWindow: %v", err)
	}
	if err := svc.RecordPingFailure(ctx, providerID, errors.New("upstream 500"), quota.ErrorKindTemporary); err != nil {
		t.Fatalf("RecordPingFailure: %v", err)
	}

	if err := svc.Unlock(ctx, actor, providerID); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	readTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()
	var action, resourceType string
	var resourceID uuid.UUID
	var actorID *uuid.UUID
	var details []byte
	err = readTx.QueryRow(ctx,
		`SELECT action, resource_type, resource_id, actor_id, details
		   FROM gorouter_audit_log
		  WHERE resource_type = 'provider' AND resource_id = $1
		  ORDER BY occurred_at DESC LIMIT 1`, providerID).
		Scan(&action, &resourceType, &resourceID, &actorID, &details)
	if err != nil {
		t.Fatalf("read audit row: %v", err)
	}
	if action != "quota.unlock" {
		t.Errorf("action = %s, want quota.unlock", action)
	}
	if actorID == nil || *actorID != actor.UserID {
		t.Errorf("audit actor = %v, want %s", actorID, actor.UserID)
	}
	if resourceID != providerID {
		t.Errorf("audit resource = %s, want %s", resourceID, providerID)
	}
	var parsed map[string]any
	if err := json.Unmarshal(details, &parsed); err != nil {
		t.Fatalf("audit details not JSON: %v", err)
	}
	if _, ok := parsed["before"]; !ok {
		t.Error("audit details must carry before")
	}
	if _, ok := parsed["after"]; !ok {
		t.Error("audit details must carry after")
	}

	// The nil-provider (global) path must never write an audit row.
	before := auditRowCount(t, ctx, pool)
	if err := svc.Unlock(ctx, actor, uuid.Nil); err == nil {
		t.Fatal("nil provider unlock must be rejected")
	}
	if after := auditRowCount(t, ctx, pool); after != before {
		t.Errorf("rejected global unlock wrote audit rows: %d -> %d", before, after)
	}
}

func auditRowCount(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM gorouter_audit_log`).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}
