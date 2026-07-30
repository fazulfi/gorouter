package repositories

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestNewTxScope(t *testing.T) {
	t.Parallel()

	t.Run("all repositories non-nil", func(t *testing.T) {
		mock := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row {
				return &mockRow{}
			},
			queryFn: func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) {
				return &mockRows{}, nil
			},
		}

		scope := NewTxScope(mock)
		if scope == nil {
			t.Fatal("expected non-nil TxScope")
		}

		if scope.Users() == nil {
			t.Error("Users() returned nil")
		}
		if scope.Sessions() == nil {
			t.Error("Sessions() returned nil")
		}
		if scope.APIKeys() == nil {
			t.Error("APIKeys() returned nil")
		}
		if scope.PATs() == nil {
			t.Error("PATs() returned nil")
		}
		if scope.Providers() == nil {
			t.Error("Providers() returned nil")
		}
		if scope.Jobs() == nil {
			t.Error("Jobs() returned nil")
		}
		if scope.AuditLog() == nil {
			t.Error("AuditLog() returned nil")
		}
		if scope.Accounts() == nil {
			t.Error("Accounts() returned nil")
		}
		if scope.Proxies() == nil {
			t.Error("Proxies() returned nil")
		}
		if scope.Models() == nil {
			t.Error("Models() returned nil")
		}
	})

	t.Run("with nil tx", func(t *testing.T) {
		scope := NewTxScope(nil)
		if scope == nil {
			t.Fatal("expected non-nil TxScope with nil tx")
		}
		if scope.Users() == nil {
			t.Error("Users() returned nil with nil tx")
		}
		if scope.Sessions() == nil {
			t.Error("Sessions() returned nil with nil tx")
		}
		_ = scope.APIKeys()
		_ = scope.PATs()
		_ = scope.Providers()
		_ = scope.Jobs()
		_ = scope.AuditLog()
		_ = scope.Accounts()
		_ = scope.Proxies()
		_ = scope.Models()
	})

	t.Run("each repo is correct type", func(t *testing.T) {
		mock := &mockTx{
			execFn: func(_ context.Context, _ string, _ ...interface{}) (pgconn.CommandTag, error) {
				return pgconn.CommandTag{}, nil
			},
			queryRowFn: func(_ context.Context, _ string, _ ...interface{}) pgx.Row { return &mockRow{} },
			queryFn:    func(_ context.Context, _ string, _ ...interface{}) (pgx.Rows, error) { return &mockRows{}, nil },
		}

		scope := NewTxScope(mock)
		if _, ok := scope.Users().(*userRepo); !ok {
			t.Error("Users() is not *userRepo")
		}
		if _, ok := scope.Sessions().(*sessionRepo); !ok {
			t.Error("Sessions() is not *sessionRepo")
		}
		if _, ok := scope.APIKeys().(*apiKeyRepo); !ok {
			t.Error("APIKeys() is not *apiKeyRepo")
		}
		if _, ok := scope.PATs().(*patRepo); !ok {
			t.Error("PATs() is not *patRepo")
		}
		if _, ok := scope.Providers().(*providerRepo); !ok {
			t.Error("Providers() is not *providerRepo")
		}
		if _, ok := scope.Jobs().(*jobRepo); !ok {
			t.Error("Jobs() is not *jobRepo")
		}
		if _, ok := scope.AuditLog().(*auditLogRepo); !ok {
			t.Error("AuditLog() is not *auditLogRepo")
		}
		if _, ok := scope.Accounts().(*accountRepo); !ok {
			t.Error("Accounts() is not *accountRepo")
		}
		if _, ok := scope.Proxies().(*proxyRepo); !ok {
			t.Error("Proxies() is not *proxyRepo")
		}
		if _, ok := scope.Models().(*ModelRepository); !ok {
			t.Error("Models() is not *ModelRepository")
		}
	})
}
