package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/jobs"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestJobRepo_FindByID(t *testing.T) {
	t.Parallel()

	t.Run("found", func(t *testing.T) {
		id := uuid.New()
		now := time.Now().UTC()
		errMsg := strPtr("something went wrong")
		scheduledAt := now.Add(5 * time.Minute)
		startedAt := now.Add(1 * time.Minute)
		payload := []byte(`{"to":"user@test.com"}`)
		result := []byte(`{"status":"done"}`)

		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, args ...interface{}) pgx.Row {
				if len(args) != 1 || args[0] != id {
					t.Error("unexpected args")
				}
				return &mockRow{
					vals: []interface{}{
						id, string(jobs.JobTypeSendEmail), string(jobs.JobFailed),
						payload, result, errMsg, 2, 5,
						&scheduledAt, &startedAt, (*time.Time)(nil), now, now,
					},
				}
			},
		}
		repo := &jobRepo{tx: tx}
		j, err := repo.FindByID(context.Background(), id)
		if err != nil {
			t.Fatal(err)
		}
		if j == nil {
			t.Fatal("expected non-nil")
		}
		if j.ID != id {
			t.Errorf("ID = %v", j.ID)
		}
		if j.Type != string(jobs.JobTypeSendEmail) {
			t.Errorf("Type = %q", j.Type)
		}
		if j.Status != jobs.JobFailed {
			t.Errorf("Status = %q", j.Status)
		}
		if j.ErrorMessage == nil || *j.ErrorMessage != "something went wrong" {
			t.Error("unexpected ErrorMessage")
		}
		if j.Attempts != 2 || j.MaxAttempts != 5 {
			t.Errorf("Attempts = %d, MaxAttempts = %d", j.Attempts, j.MaxAttempts)
		}
		if j.ScheduledAt == nil || !j.ScheduledAt.Equal(scheduledAt) {
			t.Error("unexpected ScheduledAt")
		}
		if j.StartedAt == nil || !j.StartedAt.Equal(startedAt) {
			t.Error("unexpected StartedAt")
		}
		if j.CompletedAt != nil {
			t.Error("expected CompletedAt to be nil")
		}
		if string(j.Payload) != `{"to":"user@test.com"}` {
			t.Errorf("Payload = %s", string(j.Payload))
		}
		if string(j.Result) != `{"status":"done"}` {
			t.Errorf("Result = %s", string(j.Result))
		}
	})

	t.Run("not found", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &jobRepo{tx: tx}
		j, err := repo.FindByID(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if j != nil {
			t.Fatal("expected nil")
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("db error")}
			},
		}
		repo := &jobRepo{tx: tx}
		_, err := repo.FindByID(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestJobRepo_FindPending(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		now := time.Now().UTC()
		payload := []byte(`{}`)

		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				if len(args) != 1 || args[0] != 10 {
					t.Error("unexpected limit")
				}
				return &mockRows{
					rows: [][]interface{}{
						{uuid.New(), "send_email", string(jobs.JobPending), payload, []byte("{}"),
							(*string)(nil), 0, 3, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now, now},
						{uuid.New(), "revoke_keys", string(jobs.JobPending), payload, []byte("{}"),
							(*string)(nil), 0, 1, (*time.Time)(nil), (*time.Time)(nil), (*time.Time)(nil), now, now},
					},
				}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		jobs, err := repo.FindPending(context.Background(), 10)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 2 {
			t.Fatalf("expected 2 jobs, got %d", len(jobs))
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		jobs, err := repo.FindPending(context.Background(), 5)
		if err != nil {
			t.Fatal(err)
		}
		if len(jobs) != 0 {
			t.Fatalf("expected 0, got %d", len(jobs))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &jobRepo{tx: tx}
		_, err := repo.FindPending(context.Background(), 5)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("scan error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{rows: [][]interface{}{{}}}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		_, err := repo.FindPending(context.Background(), 5)
		if err != nil {
			t.Fatal("expected no error with partial row data")
		}
	})
}

func TestJobRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		now := time.Now().UTC()
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		j := &jobs.Job{
			ID: uuid.New(), Type: "send_email", Status: jobs.JobPending,
			Payload:  json.RawMessage(`{"to":"test@test.com"}`),
			Attempts: 0, MaxAttempts: 3,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := repo.Create(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("nil payload", func(t *testing.T) {
		now := time.Now().UTC()
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		j := &jobs.Job{
			ID: uuid.New(), Type: "cleanup_sessions", Status: jobs.JobPending,
			Attempts: 0, MaxAttempts: 1,
			CreatedAt: now, UpdatedAt: now,
		}
		if err := repo.Create(context.Background(), j); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &jobRepo{tx: tx}
		err := repo.Create(context.Background(), &jobs.Job{
			ID: uuid.New(), Type: "test", Status: jobs.JobPending,
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestJobRepo_UpdateStatus(t *testing.T) {
	t.Parallel()

	t.Run("set running", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		err := repo.UpdateStatus(context.Background(), uuid.New(), jobs.JobRunning, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("set completed", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		err := repo.UpdateStatus(context.Background(), uuid.New(), jobs.JobCompleted,
			json.RawMessage(`{"ok":true}`), nil)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("set completed with nil result", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		err := repo.UpdateStatus(context.Background(), uuid.New(), jobs.JobCompleted, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("set failed with error message", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &jobRepo{tx: tx}
		errMsg := "connection timeout"
		err := repo.UpdateStatus(context.Background(), uuid.New(), jobs.JobFailed,
			json.RawMessage(`{}`), &errMsg)
		if err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &jobRepo{tx: tx}
		err := repo.UpdateStatus(context.Background(), uuid.New(), jobs.JobPending, nil, nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})
}
