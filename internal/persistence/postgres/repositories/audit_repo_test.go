package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorouter/internal/app/tx"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestAuditLogRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("with details and IP", func(t *testing.T) {
		var capturedArgs []interface{}
		m := &mockTx{
			execFn: func(_ context.Context, _ string, args ...interface{}) (pgconn.CommandTag, error) {
				capturedArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &auditLogRepo{tx: m}
		e := &tx.AuditLogEntry{
			ID:           uuid.New(),
			ActorID:      uuidPtr(uuid.New()),
			Action:       "user.login",
			ResourceType: "session",
			ResourceID:   uuidPtr(uuid.New()),
			Details:      json.RawMessage(`{"ip":"10.0.0.1"}`),
			OccurredAt:   time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), e); err != nil {
			t.Fatal(err)
		}
		if len(capturedArgs) != 8 {
			t.Fatalf("expected 8 args, got %d", len(capturedArgs))
		}
	})

	t.Run("nil details and nil IP", func(t *testing.T) {
		m := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &auditLogRepo{tx: m}
		e := &tx.AuditLogEntry{
			ID:           uuid.New(),
			Action:       "user.logout",
			ResourceType: "session",
			OccurredAt:   time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil actor and resource IDs", func(t *testing.T) {
		m := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &auditLogRepo{tx: m}
		e := &tx.AuditLogEntry{
			ID:           uuid.New(),
			Action:       "system.startup",
			ResourceType: "system",
			Details:      json.RawMessage(`{"info":"ok"}`),
			OccurredAt:   time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), e); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		m := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &auditLogRepo{tx: m}
		err := repo.Create(context.Background(), &tx.AuditLogEntry{
			ID: uuid.New(), Action: "test", ResourceType: "test",
			OccurredAt: time.Now().UTC(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func uuidPtr(u uuid.UUID) *uuid.UUID { return &u }
