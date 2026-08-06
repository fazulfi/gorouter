package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"gorouter/internal/domain/settings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestSettingsRepo_Get(t *testing.T) {
	t.Parallel()

	t.Run("success scans all fields", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			queryRowFn: func(_ context.Context, sql string, args ...interface{}) pgx.Row {
				gotSQL = sql
				gotArgs = args
				return &mockRow{
					vals: []interface{}{"rtkEnabled", []byte(`true`), nil,
						time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 2, 0, 0, 0, 0, time.UTC)},
				}
			},
		}
		repo := &settingsRepo{tx: tx}
		s, err := repo.Get(context.Background(), "rtkEnabled")
		if err != nil {
			t.Fatal(err)
		}
		if s.Key != "rtkEnabled" || string(s.Value) != "true" || s.Description != nil {
			t.Errorf("unexpected setting: %+v", s)
		}
		if !strings.Contains(gotSQL, "gorouter_settings") {
			t.Errorf("SQL does not reference gorouter_settings:\n%s", gotSQL)
		}
		if len(gotArgs) != 1 || gotArgs[0] != "rtkEnabled" {
			t.Errorf("args = %v, want [rtkEnabled]", gotArgs)
		}
	})

	t.Run("missing maps to ErrSettingNotFound", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: pgx.ErrNoRows}
			},
		}
		repo := &settingsRepo{tx: tx}
		if _, err := repo.Get(context.Background(), "nope"); !errors.Is(err, settings.ErrSettingNotFound) {
			t.Errorf("err = %v, want ErrSettingNotFound", err)
		}
	})

	t.Run("reserved key refused", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				t.Fatal("reserved key must not reach SQL")
				return nil
			},
		}
		repo := &settingsRepo{tx: tx}
		if _, err := repo.Get(context.Background(), "pricing:overrides"); !errors.Is(err, settings.ErrReservedKey) {
			t.Errorf("err = %v, want ErrReservedKey", err)
		}
	})

	t.Run("scan error propagates", func(t *testing.T) {
		tx := &mockTx{
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{err: errors.New("scan failed")}
			},
		}
		repo := &settingsRepo{tx: tx}
		if _, err := repo.Get(context.Background(), "k"); err == nil || err.Error() != "scan failed" {
			t.Errorf("err = %v, want scan failure", err)
		}
	})
}

func TestSettingsRepo_Set(t *testing.T) {
	t.Parallel()

	t.Run("success upserts without mutating caller", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &settingsRepo{tx: tx}
		desc := "rtk toggle"
		s := &settings.Setting{Key: "rtkEnabled", Value: json.RawMessage(`true`), Description: &desc}
		before := settings.Setting{Key: s.Key, Value: append(json.RawMessage(nil), s.Value...), Description: s.Description}
		if err := repo.Set(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		if string(s.Value) != string(before.Value) || s.Key != before.Key || s.Description != before.Description {
			t.Error("Set mutated caller input")
		}
		if !strings.Contains(gotSQL, "ON CONFLICT") || !strings.Contains(gotSQL, "gorouter_settings") {
			t.Errorf("SQL must upsert into gorouter_settings:\n%s", gotSQL)
		}
		if len(gotArgs) != 3 || gotArgs[0] != "rtkEnabled" || string(gotArgs[1].([]byte)) != "true" || gotArgs[2] != &desc {
			t.Errorf("args = %v, want [key value description]", gotArgs)
		}
	})

	t.Run("reserved key refused", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				t.Fatal("reserved key must not reach SQL")
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &settingsRepo{tx: tx}
		if err := repo.Set(context.Background(), &settings.Setting{Key: "pricing:overrides", Value: json.RawMessage(`[]`)}); !errors.Is(err, settings.ErrReservedKey) {
			t.Errorf("err = %v, want ErrReservedKey", err)
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("insert failed")
			},
		}
		repo := &settingsRepo{tx: tx}
		if err := repo.Set(context.Background(), &settings.Setting{Key: "k", Value: json.RawMessage(`1`)}); err == nil || err.Error() != "insert failed" {
			t.Errorf("err = %v, want insert failure", err)
		}
	})
}

func TestSettingsRepo_List(t *testing.T) {
	t.Parallel()

	t.Run("deterministic order and reserved-key exclusion", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			queryFn: func(_ context.Context, sql string, args ...interface{}) (pgx.Rows, error) {
				gotSQL = sql
				gotArgs = args
				return &mockRows{
					rows: [][]interface{}{
						{"a", []byte(`1`), nil, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
						{"b", []byte(`2`), nil, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)},
					},
				}, nil
			},
		}
		repo := &settingsRepo{tx: tx}
		list, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if len(list) != 2 || list[0].Key != "a" || list[1].Key != "b" {
			t.Errorf("list = %+v", list)
		}
		if !strings.Contains(gotSQL, "ORDER BY key") || !strings.Contains(gotSQL, "gorouter_settings") {
			t.Errorf("SQL must order by key over gorouter_settings:\n%s", gotSQL)
		}
		if len(gotArgs) != 1 {
			t.Fatalf("args = %v, want reserved keys param", gotArgs)
		}
		keys := gotArgs[0].([]string)
		if len(keys) != 1 || keys[0] != "pricing:overrides" {
			t.Errorf("reserved keys param = %v", keys)
		}
	})

	t.Run("empty list when no rows", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}
		repo := &settingsRepo{tx: tx}
		list, err := repo.List(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if list == nil || len(list) != 0 {
			t.Errorf("list = %v, want empty non-nil", list)
		}
	})

	t.Run("query error propagates", func(t *testing.T) {
		tx := &mockTx{
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return nil, errors.New("query failed")
			},
		}
		repo := &settingsRepo{tx: tx}
		if _, err := repo.List(context.Background()); err == nil || err.Error() != "query failed" {
			t.Errorf("err = %v, want query failure", err)
		}
	})
}

func TestSettingsRepo_Wipe(t *testing.T) {
	t.Parallel()

	t.Run("deletes only non-reserved keys", func(t *testing.T) {
		var gotSQL string
		var gotArgs []interface{}
		tx := &mockTx{
			execFn: func(_ context.Context, sql string, args ...interface{}) (pgconn.CommandTag, error) {
				gotSQL = sql
				gotArgs = args
				return pgconn.CommandTag{}, nil
			},
		}
		repo := &settingsRepo{tx: tx}
		if err := repo.Wipe(context.Background()); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(gotSQL, "DELETE FROM gorouter_settings") || !strings.Contains(gotSQL, "<> ALL") {
			t.Errorf("Wipe SQL must delete non-reserved settings:\n%s", gotSQL)
		}
		keys := gotArgs[0].([]string)
		if len(keys) != 1 || keys[0] != "pricing:overrides" {
			t.Errorf("reserved keys param = %v", keys)
		}
	})

	t.Run("exec error propagates", func(t *testing.T) {
		tx := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, errors.New("wipe failed")
			},
		}
		repo := &settingsRepo{tx: tx}
		if err := repo.Wipe(context.Background()); err == nil || err.Error() != "wipe failed" {
			t.Errorf("err = %v, want wipe failure", err)
		}
	})
}
