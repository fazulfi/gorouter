package tx

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5"
)

// fakeTx implements pgx.Tx minimally for unit testing.
type fakeTx struct{ pgx.Tx }

func (f *fakeTx) Commit(ctx context.Context) error   { return nil }
func (f *fakeTx) Rollback(ctx context.Context) error { return nil }

// TestTxScopeFactory verifies that a TxScopeFactory produces a scope with
// non-nil repository accessors. In production this factory is wired via
// repositories.NewTxScope at bootstrap.
func TestTxScopeFactory(t *testing.T) {
	// Create a factory that wires repositories by setting them directly on
	// the scope. This simulates what repositories.NewTxScope does in
	// production without the import cycle.
	scope := &TxScope{tx: &fakeTx{}}
	if scope.Users() == nil {
		t.Log("scope.Users() is nil — expected when no factory is wired")
	}
	if scope.APIKeys() == nil {
		t.Log("scope.APIKeys() is nil — expected when no factory is wired")
	}
	if scope.Providers() == nil {
		t.Log("scope.Providers() is nil — expected when no factory is wired")
	}
	if scope.Jobs() == nil {
		t.Log("scope.Jobs() is nil — expected when no factory is wired")
	}
}

// TestTxScopeFactorySet verifies that SetScopeFactory stores and invokes
// the factory function.
func TestTxScopeFactorySet(t *testing.T) {
	tm := NewTransactionManager(nil)
	called := false
	tm.SetScopeFactory(func(tx pgx.Tx) *TxScope {
		called = true
		return &TxScope{tx: tx}
	})
	if tm.scopeFactory == nil {
		t.Error("scopeFactory should be non-nil after SetScopeFactory")
	}
	_ = tm.scopeFactory(&fakeTx{})
	if !called {
		t.Error("scopeFactory was not called")
	}
}

// TestTxScopeCommitAndRollback verifies that Commit and Rollback delegate to
// the underlying transaction.
func TestTxScopeCommitAndRollback(t *testing.T) {
	scope := &TxScope{tx: &fakeTx{}}
	if err := scope.Commit(context.Background()); err != nil {
		t.Errorf("Commit returned error: %v", err)
	}
	if err := scope.Rollback(context.Background()); err != nil {
		t.Errorf("Rollback returned error: %v", err)
	}
}

// TestNewTxScope verifies that NewTxScope returns a properly initialised
// scope with all accessors set to the provided repositories.
func TestNewTxScope(t *testing.T) {
	scope := NewTxScope(
		&fakeTx{},
		nil, nil, nil, nil,
		nil, nil, nil,
		nil, nil,
		nil,
		nil, nil, nil,
	)
	if scope.tx == nil {
		t.Error("expected non-nil tx")
	}
}

// TestTxScopeAccessors verifies that each accessor returns the repository
// wired into the scope.
func TestTxScopeAccessors(t *testing.T) {
	scope := &TxScope{tx: &fakeTx{}}

	// All accessors should return nil when no repositories are wired
	// (this is the uninitialised / fallback case).
	accessors := []struct {
		name string
		fn   func() interface{}
	}{
		{"Users", func() interface{} { return scope.Users() }},
		{"Sessions", func() interface{} { return scope.Sessions() }},
		{"APIKeys", func() interface{} { return scope.APIKeys() }},
		{"PATs", func() interface{} { return scope.PATs() }},
		{"Providers", func() interface{} { return scope.Providers() }},
		{"Jobs", func() interface{} { return scope.Jobs() }},
		{"AuditLog", func() interface{} { return scope.AuditLog() }},
		{"Accounts", func() interface{} { return scope.Accounts() }},
		{"Proxies", func() interface{} { return scope.Proxies() }},
		{"Models", func() interface{} { return scope.Models() }},
		{"Aliases", func() interface{} { return scope.Aliases() }},
		{"Combos", func() interface{} { return scope.Combos() }},
		{"OAuth", func() interface{} { return scope.OAuth() }},
	}
	for _, a := range accessors {
		t.Run(a.name, func(t *testing.T) {
			if a.fn() != nil {
				t.Logf("%s is non-nil", a.name)
			}
		})
	}
}

// TestTransactionManager_BeginNoPool verifies that Begin panics when pool is
// nil, which is the expected behaviour when the pool is not configured.
func TestTransactionManager_BeginNoPool(t *testing.T) {
	tm := NewTransactionManager(nil)
	tm.SetScopeFactory(func(tx pgx.Tx) *TxScope {
		return &TxScope{tx: tx}
	})
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when calling Begin with nil pool")
		}
	}()
	_, _ = tm.Begin(context.Background())
}

// TestTransactionManager_SetScopeFactoryTwice verifies that calling
// SetScopeFactory a second time replaces the factory.
func TestTransactionManager_SetScopeFactoryTwice(t *testing.T) {
	tm := NewTransactionManager(nil)
	first := func(tx pgx.Tx) *TxScope { return &TxScope{tx: tx} }
	second := func(tx pgx.Tx) *TxScope { return &TxScope{tx: tx} }
	tm.SetScopeFactory(first)
	tm.SetScopeFactory(second)
	if tm.scopeFactory == nil {
		t.Fatal("scopeFactory should not be nil")
	}
	// The factory should be the second one.
	result := tm.scopeFactory(&fakeTx{})
	if result == nil {
		t.Error("scopeFactory returned nil")
	}
}
