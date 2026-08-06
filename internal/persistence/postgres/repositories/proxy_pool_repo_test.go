package repositories

import (
	"context"
	"errors"
	"testing"
	"time"

	"gorouter/internal/domain/provider"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestProxyPoolRepo_List(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		now := time.Now().UTC()
		id1, id2 := uuid.New(), uuid.New()
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{id1, "alpha", "first pool", now, now},
						{id2, "beta", "second pool", now, now},
					},
				}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		pools, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(pools) != 2 {
			t.Fatalf("expected 2, got %d", len(pools))
		}
		if pools[0].Name != "alpha" || pools[1].Name != "beta" {
			t.Error("unexpected pool names")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		pools, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(pools) != 0 {
			t.Fatalf("expected 0, got %d", len(pools))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		_, err := repo.List(context.Background())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyPoolRepo_Create(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		p := &provider.ProxyPool{
			ID: uuid.New(), Name: "test-pool", Description: "test",
			CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Create(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		err := repo.Create(context.Background(), &provider.ProxyPool{
			ID: uuid.New(), Name: "fail",
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyPoolRepo_Update(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		p := &provider.ProxyPool{
			ID: uuid.New(), Name: "updated", Description: "desc",
			UpdatedAt: time.Now().UTC(),
		}
		if err := repo.Update(context.Background(), p); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("update failed")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		err := repo.Update(context.Background(), &provider.ProxyPool{
			ID: uuid.New(), Name: "fail", UpdatedAt: time.Now().UTC(),
		})
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyPoolRepo_Delete(t *testing.T) {
	t.Parallel()

	t.Run("success", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		if err := repo.Delete(context.Background(), uuid.New()); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("exec error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		err := repo.Delete(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyPoolRepo_Members(t *testing.T) {
	t.Parallel()

	t.Run("results", func(t *testing.T) {
		poolID := uuid.New()
		pc1, pc2 := uuid.New(), uuid.New()
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, args ...interface{}) (pgx.Rows, error) {
				return &mockRows{
					rows: [][]interface{}{
						{poolID, pc1, 0},
						{poolID, pc2, 1},
					},
				}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		members, err := repo.Members(context.Background(), poolID)
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 2 {
			t.Fatalf("expected 2, got %d", len(members))
		}
		if members[0].Position != 0 || members[1].Position != 1 {
			t.Error("unexpected positions")
		}
	})

	t.Run("empty", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		members, err := repo.Members(context.Background(), uuid.New())
		if err != nil {
			t.Fatal(err)
		}
		if len(members) != 0 {
			t.Fatalf("expected 0, got %d", len(members))
		}
	})

	t.Run("query error", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		_, err := repo.Members(context.Background(), uuid.New())
		if err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestProxyPoolRepo_SetMembers(t *testing.T) {
	t.Parallel()

	t.Run("success with members uses single CTE exec", func(t *testing.T) {
		poolID := uuid.New()
		pcID1, pcID2 := uuid.New(), uuid.New()
		execCalls := 0
		var lastSQL string
		var lastArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				execCalls++
				lastSQL = sql
				lastArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		members := []provider.PoolMember{
			{ProxyConfigID: pcID1, Position: 0},
			{ProxyConfigID: pcID2, Position: 1},
		}
		if err := repo.SetMembers(context.Background(), poolID, members); err != nil {
			t.Fatal(err)
		}
		if execCalls != 1 {
			t.Errorf("expected 1 exec call (single CTE), got %d", execCalls)
		}
		_ = lastSQL
		_ = lastArgs
	})

	t.Run("empty members clears pool", func(t *testing.T) {
		execCalls := 0
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, _ ...interface{}) (pgconn.CommandTag, error) {
				execCalls++
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		if err := repo.SetMembers(context.Background(), uuid.New(), nil); err != nil {
			t.Fatal(err)
		}
		if execCalls != 1 {
			t.Errorf("expected 1 exec call (DELETE), got %d", execCalls)
		}
	})

	t.Run("CTE exec error returns error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("FK violation")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		err := repo.SetMembers(context.Background(), uuid.New(), []provider.PoolMember{
			{ProxyConfigID: uuid.New(), Position: 0},
		})
		if err == nil {
			t.Fatal("expected error from CTE exec")
		}
	})

	t.Run("empty delete error", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("delete failed")
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		err := repo.SetMembers(context.Background(), uuid.New(), nil)
		if err == nil {
			t.Fatal("expected error")
		}
	})

	t.Run("does not mutate caller input", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &proxyPoolRepo{tx: tx}
		pcID := uuid.New()
		members := []provider.PoolMember{
			{ProxyConfigID: pcID, Position: 0},
		}
		if err := repo.SetMembers(context.Background(), uuid.New(), members); err != nil {
			t.Fatal(err)
		}
		if members[0].PoolID != uuid.Nil {
			t.Error("SetMembers mutated caller's PoolMember.PoolID")
		}
	})
}
