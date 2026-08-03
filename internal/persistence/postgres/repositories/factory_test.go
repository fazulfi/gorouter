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
		if scope.Aliases() == nil {
			t.Error("Aliases() returned nil")
		}
		if scope.Combos() == nil {
			t.Error("Combos() returned nil")
		}
		if scope.OAuth() == nil {
			t.Error("OAuth() returned nil")
		}
		if scope.Pools() == nil {
			t.Error("Pools() returned nil")
		}
		if scope.Nodes() == nil {
			t.Error("Nodes() returned nil")
		}
		if scope.Usage() == nil {
			t.Error("Usage() returned nil")
		}
		if scope.ConsoleLogs() == nil {
			t.Error("ConsoleLogs() returned nil")
		}
		if scope.PasswordResets() == nil {
			t.Error("PasswordResets() returned nil")
		}
		if scope.Backups() == nil {
			t.Error("Backups() returned nil")
		}
		if scope.Pricing() == nil {
			t.Error("Pricing() returned nil")
		}
		if scope.Settings() == nil {
			t.Error("Settings() returned nil")
		}
		if scope.AuditQuery() == nil {
			t.Error("AuditQuery() returned nil")
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
		_ = scope.Aliases()
		_ = scope.Combos()
		_ = scope.OAuth()
		_ = scope.Pools()
		_ = scope.Nodes()
		_ = scope.Usage()
		_ = scope.ConsoleLogs()
		_ = scope.PasswordResets()
		_ = scope.Backups()
		_ = scope.Pricing()
		_ = scope.Settings()
		_ = scope.AuditQuery()
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
		if _, ok := scope.Aliases().(*aliasRepo); !ok {
			t.Error("Aliases() is not *aliasRepo")
		}
		if _, ok := scope.Combos().(*comboRepo); !ok {
			t.Error("Combos() is not *comboRepo")
		}
		if _, ok := scope.OAuth().(*oauthRepo); !ok {
			t.Error("OAuth() is not *oauthRepo")
		}
		if _, ok := scope.Pools().(*proxyPoolRepo); !ok {
			t.Error("Pools() is not *proxyPoolRepo")
		}
		if _, ok := scope.Nodes().(*nodeStore); !ok {
			t.Error("Nodes() is not *nodeStore")
		}
		if _, ok := scope.Usage().(*usageRepo); !ok {
			t.Error("Usage() is not *usageRepo")
		}
		if _, ok := scope.ConsoleLogs().(*consoleLogRepo); !ok {
			t.Error("ConsoleLogs() is not *consoleLogRepo")
		}
		if _, ok := scope.PasswordResets().(*passwordResetRepo); !ok {
			t.Error("PasswordResets() is not *passwordResetRepo")
		}
		if _, ok := scope.Backups().(*backupRepo); !ok {
			t.Error("Backups() is not *backupRepo")
		}
		if _, ok := scope.Pricing().(*pricingRepo); !ok {
			t.Error("Pricing() is not *pricingRepo")
		}
		if _, ok := scope.Settings().(*settingsRepo); !ok {
			t.Error("Settings() is not *settingsRepo")
		}
		if _, ok := scope.AuditQuery().(*auditQueryRepo); !ok {
			t.Error("AuditQuery() is not *auditQueryRepo")
		}
	})
}
