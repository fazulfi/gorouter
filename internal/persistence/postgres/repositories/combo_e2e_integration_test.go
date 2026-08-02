package repositories

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"gorouter/internal/app/combos"
	"gorouter/internal/app/tx"
	"gorouter/internal/domain/combo"
	enginecombos "gorouter/internal/engine/combos"

	"github.com/google/uuid"
)

type e2eRuntimeState struct{}

func (e2eRuntimeState) Get(context.Context, string) (json.RawMessage, error) { return nil, nil }
func (e2eRuntimeState) Set(context.Context, string, json.RawMessage, time.Duration) error {
	return nil
}

var errE2EInvokeUnused = errors.New("e2e: invoke not used in CRUD test")

// TestComboServiceCRUD_ThroughWiredRepo_Integration proves the ComboService
// CRUD surface end-to-end through the already-wired combo repository (BE-01)
// against real PostgreSQL: definitions and members persist across committed
// transactions and are readable through fresh scopes.
func TestComboServiceCRUD_ThroughWiredRepo_Integration(t *testing.T) {
	pool := getTestPool(t)
	ctx := context.Background()

	// gorouter_combo_members.provider_id is FK-constrained to
	// gorouter_providers(id); seed real provider rows for the members.
	provA := uuid.New()
	provB := uuid.New()
	for _, p := range []struct {
		id   uuid.UUID
		name string
	}{{provA, "e2e-openai"}, {provB, "e2e-anthropic"}} {
		if _, err := pool.Exec(ctx,
			`INSERT INTO gorouter_providers (id, name, type, base_url)
			 VALUES ($1, $2, 'openai', 'https://example.invalid')`, p.id, p.name); err != nil {
			t.Fatalf("insert provider %s: %v", p.name, err)
		}
	}

	newScope := func() *tx.TxScope {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatalf("begin: %v", err)
		}
		t.Cleanup(func() { _ = tx.Rollback(ctx) })
		return NewTxScope(tx)
	}
	newService := func(scope *tx.TxScope) *combos.Service {
		return combos.NewService(scope.Combos(), e2eRuntimeState{},
			enginecombos.Invoker(func(context.Context, combo.Member, []byte) ([]byte, error) {
				return nil, errE2EInvokeUnused
			}),
			combos.CapabilityChecker(func(context.Context, combo.Member, []string) (bool, error) {
				return true, nil
			}),
			combos.JudgeResolver(func(context.Context, string) (*combo.Member, error) {
				return nil, errE2EInvokeUnused
			}),
		)
	}

	// Phase 1: create + update + members in one transaction, then commit.
	scope := newScope()
	svc := newService(scope)
	def, err := svc.Create(ctx, "e2e-combo", combo.StrategyRoundRobin,
		json.RawMessage(`{"sticky_count":2}`),
		[]combo.Member{
			{ProviderID: provA, ModelRef: "openai/gpt-4o", Priority: 1, IsActive: true},
			{ProviderID: provB, ModelRef: "anthropic/claude-3-5-sonnet", Priority: 2, IsActive: true},
		})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if def.ID == uuid.Nil {
		t.Fatal("Create returned nil ID")
	}
	def.Name = "e2e-combo-renamed"
	if err := svc.Update(ctx, def); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if err := svc.UpdateMembers(ctx, def.ID,
		[]combo.Member{{ProviderID: provA, ModelRef: "openai/gpt-4o-mini", Priority: 1, IsActive: true}}); err != nil {
		t.Fatalf("UpdateMembers: %v", err)
	}
	if err := scope.Commit(ctx); err != nil {
		t.Fatalf("commit phase 1: %v", err)
	}

	// Phase 2: a fresh scope sees the committed definition and members.
	scope = newScope()
	svc = newService(scope)
	got, err := svc.Get(ctx, def.ID)
	if err != nil {
		t.Fatalf("Get through fresh scope: %v", err)
	}
	if got.Name != "e2e-combo-renamed" || got.Strategy != combo.StrategyRoundRobin {
		t.Errorf("persisted definition = %+v", got)
	}
	list, err := svc.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(list) != 1 || list[0].ID != def.ID {
		t.Errorf("List = %+v", list)
	}
	members, err := scope.Combos().ListMembers(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListMembers: %v", err)
	}
	if len(members) != 1 || members[0].ModelRef != "openai/gpt-4o-mini" {
		t.Errorf("members = %+v", members)
	}
	if err := svc.SetActive(ctx, def.ID, false); err != nil {
		t.Fatalf("SetActive: %v", err)
	}
	if err := scope.Commit(ctx); err != nil {
		t.Fatalf("commit phase 2: %v", err)
	}

	// Phase 3: deactivation persisted; delete removes definition and members.
	scope = newScope()
	svc = newService(scope)
	got, err = svc.Get(ctx, def.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.IsActive {
		t.Error("SetActive(false) did not persist")
	}
	if err := svc.Delete(ctx, def.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := scope.Commit(ctx); err != nil {
		t.Fatalf("commit phase 3: %v", err)
	}

	scope = newScope()
	svc = newService(scope)
	if _, err := svc.Get(ctx, def.ID); !errors.Is(err, combo.ErrNotFound) {
		t.Errorf("Get after delete = %v, want ErrNotFound", err)
	}
	members, err = scope.Combos().ListMembers(ctx, def.ID)
	if err != nil {
		t.Fatalf("ListMembers after delete: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("members after delete = %+v, want none", members)
	}
}
