package repositories

import (
	"context"
	"strings"
	"testing"
	"time"

	txpkg "gorouter/internal/app/tx"
	"gorouter/internal/domain/auth"
	"gorouter/internal/domain/jobs"
	"gorouter/internal/domain/keys"
	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var testNow = time.Now().UTC()

// sqlCaptureTx is a mockTx that captures the SQL string before delegating.
type sqlCaptureTx struct {
	lastSQL string
	inner   *mockTx
}

func (s *sqlCaptureTx) Begin(ctx context.Context) (pgx.Tx, error) {
	panic("not expected")
}

func (s *sqlCaptureTx) BeginFunc(ctx context.Context, f func(pgx.Tx) error) error {
	panic("not expected")
}

func (s *sqlCaptureTx) Commit(ctx context.Context) error {
	panic("not expected")
}

func (s *sqlCaptureTx) Rollback(ctx context.Context) error {
	panic("not expected")
}

func (s *sqlCaptureTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	panic("not expected")
}

func (s *sqlCaptureTx) Exec(ctx context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
	s.lastSQL = sql
	return s.inner.execFn(ctx, sql, args...)
}

func (s *sqlCaptureTx) LargeObjects() pgx.LargeObjects {
	panic("not expected")
}

func (s *sqlCaptureTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	panic("not expected")
}

func (s *sqlCaptureTx) Query(ctx context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
	s.lastSQL = sql
	return s.inner.queryFn(ctx, sql, args...)
}

func (s *sqlCaptureTx) QueryRow(ctx context.Context, sql string, args ...interface{}) pgx.Row {
	s.lastSQL = sql
	return s.inner.queryRowFn(ctx, sql, args...)
}

func (s *sqlCaptureTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults {
	panic("not expected")
}

func (s *sqlCaptureTx) Conn() *pgx.Conn {
	panic("not expected")
}

// captureLastSQL creates a sqlCaptureTx wrapping a mockTx with no-op handlers.
func captureLastSQL() *sqlCaptureTx {
	return &sqlCaptureTx{
		inner: &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		},
	}
}

// assertTableInSQL checks that the captured SQL contains the given table name.
func assertTableInSQL(t *testing.T, tx *sqlCaptureTx, expectedTable string) {
	t.Helper()
	if !strings.Contains(tx.lastSQL, expectedTable) {
		t.Errorf("SQL does not reference expected table %q.\nSQL: %s", expectedTable, tx.lastSQL)
	}
}

// ---------------------------------------------------------------------------
// UserRepo table name tests
// ---------------------------------------------------------------------------

func TestUserRepo_SQL_UsesGorouterUsers(t *testing.T) {
	t.Parallel()

	t.Run("FindByID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &userRepo{tx: tx}
		_, _ = repo.FindByID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_users")
	})

	t.Run("FindByEmail", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &userRepo{tx: tx}
		_, _ = repo.FindByEmail(context.Background(), "test@test.com")
		assertTableInSQL(t, tx, "gorouter_users")
	})

	t.Run("Create", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &userRepo{tx: tx}
		_ = repo.Create(context.Background(), &auth.User{
			ID: uuid.New(), Email: "a@b.com", PasswordHash: "h",
			CreatedAt: testNow, UpdatedAt: testNow,
		})
		assertTableInSQL(t, tx, "gorouter_users")
	})

	t.Run("Update", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &userRepo{tx: tx}
		_ = repo.Update(context.Background(), &auth.User{ID: uuid.New(), Email: "a@b.com"})
		assertTableInSQL(t, tx, "gorouter_users")
	})

	t.Run("UpdateLastLogin", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &userRepo{tx: tx}
		_ = repo.UpdateLastLogin(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_users")
	})
}

// ---------------------------------------------------------------------------
// SessionRepo table name tests
// ---------------------------------------------------------------------------

func TestSessionRepo_SQL_UsesGorouterSessions(t *testing.T) {
	t.Parallel()

	t.Run("FindByID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &sessionRepo{tx: tx}
		_, _ = repo.FindByID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_sessions")
	})

	t.Run("FindByTokenHash", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &sessionRepo{tx: tx}
		_, _ = repo.FindByTokenHash(context.Background(), "hash")
		assertTableInSQL(t, tx, "gorouter_sessions")
	})

	t.Run("Create", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &sessionRepo{tx: tx}
		_ = repo.Create(context.Background(), &auth.Session{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
		})
		assertTableInSQL(t, tx, "gorouter_sessions")
	})

	t.Run("Revoke", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &sessionRepo{tx: tx}
		_ = repo.Revoke(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_sessions")
	})

	t.Run("DeleteExpired", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &sessionRepo{tx: tx}
		_ = repo.DeleteExpired(context.Background())
		assertTableInSQL(t, tx, "gorouter_sessions")
	})
}

// ---------------------------------------------------------------------------
// APIKeyRepo table name tests
// ---------------------------------------------------------------------------

func TestAPIKeyRepo_SQL_UsesGorouterAPIKeys(t *testing.T) {
	t.Parallel()

	t.Run("FindByID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &apiKeyRepo{tx: tx}
		_, _ = repo.FindByID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_api_keys")
	})

	t.Run("FindByHash", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &apiKeyRepo{tx: tx}
		_, _ = repo.FindByHash(context.Background(), "hash")
		assertTableInSQL(t, tx, "gorouter_api_keys")
	})

	t.Run("FindByUserID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &apiKeyRepo{tx: tx}
		_, _ = repo.FindByUserID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_api_keys")
	})

	t.Run("Create", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &apiKeyRepo{tx: tx}
		_ = repo.Create(context.Background(), &keys.APIKey{
			ID: uuid.New(), UserID: uuid.New(), KeyHash: "h",
		})
		assertTableInSQL(t, tx, "gorouter_api_keys")
	})

	t.Run("Revoke", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &apiKeyRepo{tx: tx}
		_ = repo.Revoke(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_api_keys")
	})
}

// ---------------------------------------------------------------------------
// PATRepo table name tests
// ---------------------------------------------------------------------------

func TestPATRepo_SQL_UsesGorouterPats(t *testing.T) {
	t.Parallel()

	t.Run("FindByID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &patRepo{tx: tx}
		_, _ = repo.FindByID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_pats")
	})

	t.Run("FindByHash", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &patRepo{tx: tx}
		_, _ = repo.FindByHash(context.Background(), "hash")
		assertTableInSQL(t, tx, "gorouter_pats")
	})

	t.Run("FindByUserID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &patRepo{tx: tx}
		_, _ = repo.FindByUserID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_pats")
	})

	t.Run("Create", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &patRepo{tx: tx}
		_ = repo.Create(context.Background(), &keys.PAT{
			ID: uuid.New(), UserID: uuid.New(), TokenHash: "h",
		})
		assertTableInSQL(t, tx, "gorouter_pats")
	})

	t.Run("Revoke", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &patRepo{tx: tx}
		_ = repo.Revoke(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_pats")
	})
}

// ---------------------------------------------------------------------------
// ProviderRepo table name tests
// ---------------------------------------------------------------------------

func TestProviderRepo_SQL_UsesGorouterProviders(t *testing.T) {
	t.Parallel()

	t.Run("FindByID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &providerRepo{tx: tx}
		_, _ = repo.FindByID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_providers")
	})

	t.Run("FindByType", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &providerRepo{tx: tx}
		_, _ = repo.FindByType(context.Background(), provider.ProviderOpenAI)
		assertTableInSQL(t, tx, "gorouter_providers")
	})

	t.Run("List", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &providerRepo{tx: tx}
		_, _ = repo.List(context.Background())
		assertTableInSQL(t, tx, "gorouter_providers")
	})

	t.Run("Create", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &providerRepo{tx: tx}
		_ = repo.Create(context.Background(), &provider.Provider{
			ID: uuid.New(), Name: "n", Type: provider.ProviderCustom, BaseURL: "https://x.com",
		})
		assertTableInSQL(t, tx, "gorouter_providers")
	})

	t.Run("Update", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &providerRepo{tx: tx}
		_ = repo.Update(context.Background(), &provider.Provider{
			ID: uuid.New(), Name: "n", Type: provider.ProviderCustom,
		})
		assertTableInSQL(t, tx, "gorouter_providers")
	})

	t.Run("Delete", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &providerRepo{tx: tx}
		_ = repo.Delete(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_providers")
	})
}

// ---------------------------------------------------------------------------
// JobRepo table name tests
// ---------------------------------------------------------------------------

func TestJobRepo_SQL_UsesGorouterJobs(t *testing.T) {
	t.Parallel()

	t.Run("FindByID", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &jobRepo{tx: tx}
		_, _ = repo.FindByID(context.Background(), uuid.New())
		assertTableInSQL(t, tx, "gorouter_jobs")
	})

	t.Run("FindPending", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &jobRepo{tx: tx}
		_, _ = repo.FindPending(context.Background(), 10)
		assertTableInSQL(t, tx, "gorouter_jobs")
	})

	t.Run("Create", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &jobRepo{tx: tx}
		_ = repo.Create(context.Background(), &jobs.Job{
			ID: uuid.New(), Type: "t", Status: jobs.JobPending,
		})
		assertTableInSQL(t, tx, "gorouter_jobs")
	})

	t.Run("UpdateStatus", func(t *testing.T) {
		tx := captureLastSQL()
		repo := &jobRepo{tx: tx}
		_ = repo.UpdateStatus(context.Background(), uuid.New(), jobs.JobRunning, nil, nil)
		assertTableInSQL(t, tx, "gorouter_jobs")
	})
}

// ---------------------------------------------------------------------------
// AuditLogRepo table name test
// ---------------------------------------------------------------------------

func TestAuditLogRepo_SQL_UsesGorouterAuditLog(t *testing.T) {
	t.Parallel()

	tx := captureLastSQL()
	repo := &auditLogRepo{tx: tx}
	_ = repo.Create(context.Background(), &txpkg.AuditLogEntry{
		ID: uuid.New(), Action: "test", ResourceType: "test",
	})
	assertTableInSQL(t, tx, "gorouter_audit_log")
}
